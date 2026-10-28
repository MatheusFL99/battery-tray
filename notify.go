package main

import (
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Notificação toast própria em Win32: uma janela pop-up no canto inferior
// direito, sem roubar foco (WS_EX_NOACTIVATE) e sem spawnar processos externos.

const (
	toastWidth  = 360
	toastHeight = 88
	toastMs     = 5000 // duração em ms
)

// níveis de alerta (mesmos limiares usados na detecção)
const (
	levelWarn  = 10
	levelCrit  = 5
)

type toastMsg struct {
	title   string
	body    string
	level   int // 10 ou 5 — define a cor
	percent int // nível real, para a barra de progresso
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassExW  = user32.NewProc("RegisterClassExW")
	pCreateWindowExW   = user32.NewProc("CreateWindowExW")
	pDefWindowProcW    = user32.NewProc("DefWindowProcW")
	pShowWindow        = user32.NewProc("ShowWindow")
	pDestroyWindow     = user32.NewProc("DestroyWindow")
	pSetTimer          = user32.NewProc("SetTimer")
	pKillTimer         = user32.NewProc("KillTimer")
	pGetMessageW       = user32.NewProc("GetMessageW")
	pTranslateMessage  = user32.NewProc("TranslateMessage")
	pDispatchMessageW  = user32.NewProc("DispatchMessageW")
	pBeginPaint        = user32.NewProc("BeginPaint")
	pEndPaint          = user32.NewProc("EndPaint")
	pSetWindowPos      = user32.NewProc("SetWindowPos")
	pPostMessageW      = user32.NewProc("PostMessageW")
	pGetClientRect     = user32.NewProc("GetClientRect")
	pSystemParamInfoW  = user32.NewProc("SystemParametersInfoW")
	pSetBkMode         = user32.NewProc("SetBkMode")
	pSetTextColor      = user32.NewProc("SetTextColor")
	pDrawTextW         = user32.NewProc("DrawTextW")
	pGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
	pCreateSolidBrush  = gdi32.NewProc("CreateSolidBrush")
	pCreatePen         = gdi32.NewProc("CreatePen")
	pCreateFontW       = gdi32.NewProc("CreateFontW")
	pSelectObject      = gdi32.NewProc("SelectObject")
	pDeleteObject      = gdi32.NewProc("DeleteObject")
	pRoundRect         = gdi32.NewProc("RoundRect")
	pRectangle         = gdi32.NewProc("Rectangle")
)

// constantes de janela/mensagem
const (
	wmPaint   = 0x000F
	wmDestroy = 0x0002
	wmTimer   = 0x0113
	wmApp     = 0x8000 // WM_APP — sinal de nova notificação

	wsPopup      = 0x80000000
	wsExTopmost  = 0x00000008
	wsExToolWnd  = 0x00000080
	wsExNoActive = 0x08000000
	swHide       = 0
	swShow       = 5

	spiGetWorkArea = 0x0030

	transparentBk = 1
	psSolid       = 0
	fwBold        = 700
	defaultCharSet = 1

	dtSingleLine = 0x00002000
	dtEndEllipsis = 0x00008000
	dtNoPrefix    = 0x00000800
	dtLeft        = 0x00000000
	dtRight       = 0x00000200
	dtVCenter     = 0x00000004
)

type rect struct{ Left, Top, Right, Bottom int32 }

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type paintStruct struct {
	hdc       windows.Handle
	erase     int32
	rcPaint   rect
	restore   int32
	incUpdate int32
	reserved  [32]byte
}

type msgStruct struct {
	hwnd    windows.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// rgb monta um COLORREF (ordem 0x00BBGGRR).
func rgb(r, g, b byte) uintptr {
	return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16
}

var (
	toastOnce  sync.Once
	toastHwnd  windows.Handle // guardado sob toastMu
	toastMu    sync.Mutex
	toastQueue []toastMsg
	toastCur   toastMsg
	toastVis   bool
	toastProc  uintptr
	toastClass = utf16("BatteryTrayToast")
)

func utf16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

// ShowToast enfileira uma notificação e acorda a janela.
func ShowToast(t toastMsg) {
	toastMu.Lock()
	toastQueue = append(toastQueue, t)
	hwnd := toastHwnd
	toastMu.Unlock()

	if hwnd == 0 {
		return // janela de toast ainda não criada
	}
	pPostMessageW.Call(uintptr(hwnd), wmApp+1, 0, 0)
}

// startToastLoop cria a janela e roda a message loop numa goroutine dedicada
// (a janela e o loop precisam viver na mesma OS thread).
func startToastLoop() {
	toastOnce.Do(func() {
		go func() {
			// A janela Win32 pertence à OS thread que a criou: sem isto o
			// goroutine migra de thread e o WM_TIMER deixa de chegar,
			// prendendo o toast na tela para sempre.
			runtime.LockOSThread()
			defer func() { recover() }()

			hInst, _, _ := pGetModuleHandleW.Call(0)
			toastProc = syscall.NewCallback(toastWndProc)

			wc := wndClassEx{
				cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
				lpfnWndProc:   toastProc,
				hInstance:     windows.Handle(hInst),
				lpszClassName: toastClass,
			}
			pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

			ex := uintptr(wsExTopmost | wsExToolWnd | wsExNoActive)
			style := uintptr(wsPopup)
			hwnd, _, err := pCreateWindowExW.Call(
				ex,
				uintptr(unsafe.Pointer(toastClass)),
				uintptr(unsafe.Pointer(utf16(""))),
				style,
				0, 0, uintptr(toastWidth), uintptr(toastHeight),
				0, 0, hInst, 0,
			)
			if hwnd == 0 {
				_ = err
				logf("falha ao criar janela de toast: %v", err)
				return
			}
			pShowWindow.Call(hwnd, swHide)
			logf("janela de toast criada (hwnd=%d)", hwnd)

			toastMu.Lock()
			toastHwnd = windows.Handle(hwnd)
			toastMu.Unlock()

			var m msgStruct
			for {
				r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
				if int32(r) <= 0 { // WM_QUIT ou erro
					return
				}
				pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
				pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
			}
		}()
	})
}

// toastWndProc processa as mensagens da janela de notificação.
func toastWndProc(hwnd windows.Handle, wMsg uint32, wParam, lParam uintptr) uintptr {
	defer func() { recover() }()

	switch wMsg {
	case wmPaint:
		paintToast(hwnd)
		return 0
	case wmTimer:
		pKillTimer.Call(uintptr(hwnd), wParam)
		pShowWindow.Call(uintptr(hwnd), swHide)
		toastMu.Lock()
		toastVis = false
		toastMu.Unlock()
		logf("toast oculto (timer)")
		showNext(hwnd)
		return 0
	case wmApp + 1:
		showNext(hwnd)
		return 0
	case wmDestroy:
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(wMsg), wParam, lParam)
	return r
}

// showNext exibe a próxima notificação da fila, se nenhuma estiver visível.
func showNext(hwnd windows.Handle) {
	toastMu.Lock()
	if toastVis || len(toastQueue) == 0 {
		toastMu.Unlock()
		return
	}
	toastCur = toastQueue[0]
	toastQueue = toastQueue[1:]
	toastVis = true
	toastMu.Unlock()

	// posiciona no canto inferior direito da área de trabalho
	var wa rect
	pSystemParamInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0)
	x := int32(wa.Right) - toastWidth - 16
	y := int32(wa.Bottom) - toastHeight - 16

	// re-posiciona a janela existente
	setPos(hwnd, x, y)
	pShowWindow.Call(uintptr(hwnd), swShow)
	pSetTimer.Call(uintptr(hwnd), 1, toastMs, 0)
}

func setPos(hwnd windows.Handle, x, y int32) {
	const swpNoSize = 0x0001
	const swpNoZOrder = 0x0004
	const swpNoActivate = 0x0010
	pSetWindowPos.Call(uintptr(hwnd), 0, uintptr(x), uintptr(y),
		uintptr(toastWidth), uintptr(toastHeight),
		swpNoSize|swpNoZOrder|swpNoActivate)
}

// paintToast desenha o conteúdo do toast.
func paintToast(hwnd windows.Handle) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	var rc rect
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
	w, h := int(rc.Right), int(rc.Bottom)
	if w <= 0 || h <= 0 {
		return
	}

	cur := toastMsg{}
	toastMu.Lock()
	cur = toastCur
	toastMu.Unlock()

	// cores conforme o nível
	accent := rgb(251, 191, 36) // laranja (10%)
	if cur.level <= levelCrit {
		accent = rgb(248, 113, 113) // vermelho (5%)
	}
	bg := rgb(30, 36, 48)
	border := rgb(58, 68, 86)
	fg := rgb(240, 244, 250)
	track := rgb(50, 58, 72)

	// fundo arredondado
	brush, _, _ := pCreateSolidBrush.Call(bg)
	pen, _, _ := pCreatePen.Call(psSolid, 1, border)
	oldB, _, _ := pSelectObject.Call(hdc, brush)
	oldP, _, _ := pSelectObject.Call(hdc, pen)
	pRoundRect.Call(hdc, 0, 0, uintptr(w-1), uintptr(h-1), 28, 28)

	// barra em blocos (estilo bateria), não barra contínua — evita a leitura
	// de "loading" quando o nível é baixo
	const (
		barX0     = 16
		barY0     = 64
		barH      = 14
		segCount  = 10
		segGap    = 2
		segMargin = 1 // margem interna do contorno da bateria
	)
	barW := w - 32
	segW := (barW - segMargin*2 - segGap*(segCount-1)) / segCount
	if segW < 2 {
		segW = 2
	}

	pct := 0
	if cur.percent > 0 && cur.percent <= 100 {
		pct = cur.percent
	}
	// quantos blocos preencher (arredonda para cima, para 9% não virar 0 blocos)
	filled := (pct*segCount + 99) / 100
	if pct > 0 && filled == 0 {
		filled = 1
	}
	if filled > segCount {
		filled = segCount
	}

	for i := 0; i < segCount; i++ {
		x := barX0 + segMargin + i*(segW+segGap)
		color := track
		if i < filled {
			color = accent
		}
		br, _, _ := pCreateSolidBrush.Call(color)
		pn, _, _ := pCreatePen.Call(psSolid, 1, border)
		oldBr, _, _ := pSelectObject.Call(hdc, br)
		oldPn, _, _ := pSelectObject.Call(hdc, pn)
		pRectangle.Call(hdc, uintptr(x), uintptr(barY0),
			uintptr(x+segW), uintptr(barY0+barH))
		pSelectObject.Call(hdc, oldPn)
		pSelectObject.Call(hdc, oldBr)
		pDeleteObject.Call(pn)
		pDeleteObject.Call(br)
	}

	// texto
	pSetBkMode.Call(hdc, transparentBk)
	// CreateFontW espera alturas negativas (altura de caracteres); usamos
	// variáveis tipadas porque constantes negativas não convertem para uintptr.
	hTitle, hBody, hPct := int32(-17), int32(-14), int32(-26)
	cchNull := int32(-1)
	titleFont, _, _ := pCreateFontW.Call(
		uintptr(hTitle), 0, 0, 0, fwBold, 0, 0, 0,
		defaultCharSet, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16("Segoe UI"))))
	bodyFont, _, _ := pCreateFontW.Call(
		uintptr(hBody), 0, 0, 0, 0, 0, 0, 0,
		defaultCharSet, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16("Segoe UI"))))
	pctFont, _, _ := pCreateFontW.Call(
		uintptr(hPct), 0, 0, 0, fwBold, 0, 0, 0,
		defaultCharSet, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16("Segoe UI"))))

	// drawText renderiza `text` em `r`; align é dtLeft/dtRight/dtCenter.
	drawText := func(text string, font, color uintptr, r rect, align uintptr) {
		old, _, _ := pSelectObject.Call(hdc, font)
		pSetTextColor.Call(hdc, color)
		pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(utf16(text))), uintptr(cchNull),
			uintptr(unsafe.Pointer(&r)),
			dtSingleLine|dtEndEllipsis|dtNoPrefix|dtVCenter|align)
		pSelectObject.Call(hdc, old)
	}

	title := cur.title
	if title == "" {
		title = "Bateria baixa"
	}
	// linha de título: aviso à esquerda, percentual grande à direita
	pctStr := "—"
	if cur.percent > 0 {
		pctStr = strconv.Itoa(cur.percent) + "%"
	}
	pctW := int32(72)
	titleW := int32(w) - 32 - pctW
	drawText(title, titleFont, accent, rect{16, 8, 16 + titleW, 34}, dtLeft)
	drawText(pctStr, pctFont, accent,
		rect{int32(w) - 16 - pctW, 4, int32(w) - 16, 36}, dtRight)

	// linha de corpo: nome + ação
	drawText(cur.body, bodyFont, fg, rect{16, 36, int32(w) - 16, 58}, dtLeft)

	pDeleteObject.Call(titleFont)
	pDeleteObject.Call(bodyFont)
	pDeleteObject.Call(pctFont)
	pSelectObject.Call(hdc, oldP)
	pSelectObject.Call(hdc, oldB)
	pDeleteObject.Call(pen)
	pDeleteObject.Call(brush)
}
