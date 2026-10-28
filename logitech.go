// Leitor genérico de bateria para receptores Logitech via HID++ 2.0.
//
// Descobre sozinho os aparelhos pareados (índices 1..6 de qualquer receptor
// com coleção HID++ 0xFF00/0x0002), lê o nome pela feature 0x0005 e o nível
// pela feature 0x1004 (percentual) — com queda para 0x1001 (voltagem + LUT),
// que é o que este mouse expõe. Apenas consultas: nada é gravado.
package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	hid "github.com/sstallion/go-hid"
)

const (
	logiVID      = 0x046D
	swID         = 0x0A
	nameFeat     = 0x0005 // Device Name (fn 0x01 = getNameBytes, offset em blocos de 16)
	levelFeat    = 0x1004 // Battery Status (percentual direto)
	voltFeat     = 0x1001 // Battery Voltage Status (mV + flags)
	logiReadMs   = 900
	logiPingMs   = 400 // ping da descoberta: mais curto, varre 6 índices
	logiMaxIdx   = 6   // máximo de aparelhos pareados num receptor
	featIdxMaxOK = 0x40
)

// LUT de voltagem (mV) -> %, índice 0 = 100%. Fonte: LGSTrayBattery / Solaar.
var voltageLUT = [100]uint16{
	4186, 4156, 4143, 4133, 4122, 4113, 4103, 4094, 4086, 4075, 4067, 4059, 4051, 4043, 4035, 4027,
	4019, 4011, 4003, 3997, 3989, 3983, 3976, 3969, 3961, 3955, 3949, 3942, 3935, 3929, 3922, 3916,
	3909, 3902, 3896, 3890, 3883, 3877, 3870, 3865, 3859, 3853, 3848, 3842, 3837, 3833, 3828, 3824,
	3819, 3815, 3811, 3808, 3804, 3800, 3797, 3793, 3790, 3787, 3784, 3781, 3778, 3775, 3772, 3770,
	3767, 3764, 3762, 3759, 3757, 3754, 3751, 3748, 3744, 3741, 3737, 3734, 3730, 3726, 3724, 3720,
	3717, 3714, 3710, 3706, 3702, 3697, 3693, 3688, 3683, 3677, 3671, 3666, 3662, 3658, 3654, 3646,
	3633, 3612, 3579, 3537,
}

func mvToPercent(mv uint16) int {
	for i, th := range voltageLUT {
		if mv > th {
			return 100 - i
		}
	}
	return 0
}

// ---------- primitivas HID++ ----------

// hreq monta um report longo: 11 | devIdx | featIdx | (fn<<4)|swID | params | swID.
// O nibble alto do byte 3 é a função (a versão anterior do probe a colocava no
// nibble baixo e o aparelho sempre executava a função 0).
func hreq(devIdx, featIdx, fn byte, params ...byte) []byte {
	r := []byte{0x11, devIdx, featIdx, (fn << 4) | swID, 0x00, 0x00, 0x00, swID}
	for i, p := range params {
		if i < 3 { // params em 4..6; o byte 7 é sempre o swID
			r[4+i] = p
		}
	}
	return r
}

// replyTo filtra a resposta longa do índice devIdx, sem erro e com nosso swID
// (respostas de erro trazem o nibble baixo 0x0F, ex.: 0x8F).
func replyTo(devIdx byte) func([]byte) bool {
	return func(b []byte) bool {
		return len(b) >= 4 && b[0] == 0x11 && b[1] == devIdx && b[3]&0x0F == swID
	}
}

// hflush drena respostas antigas para não confundir a próxima leitura.
func hflush(dev *hid.Device) {
	buf := make([]byte, 64)
	for {
		n, err := dev.ReadWithTimeout(buf, 40*time.Millisecond)
		if err != nil || n <= 0 {
			return
		}
	}
}

// readMatching lê reports até satisfazer o filtro ou estourar o tempo.
func readMatching(dev *hid.Device, timeout time.Duration, filter func([]byte) bool) ([]byte, bool) {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, false
		}
		buf := make([]byte, 64)
		n, err := dev.ReadWithTimeout(buf, remaining)
		if err != nil || n <= 0 {
			return nil, false
		}
		if filter(buf[:n]) {
			return buf[:n], true
		}
	}
}

// hping acorda/sonda o aparelho no índice devIdx.
func hping(dev *hid.Device, devIdx byte, timeout time.Duration) bool {
	hflush(dev)
	if _, err := dev.Write(hreq(devIdx, 0x00, 0x01)); err != nil {
		return false
	}
	_, ok := readMatching(dev, timeout, replyTo(devIdx))
	return ok
}

// hfeature resolve o índice interno de uma feature (0 = inexistente).
func hfeature(dev *hid.Device, devIdx byte, feat uint16) (byte, bool) {
	hflush(dev)
	if _, err := dev.Write(hreq(devIdx, 0x00, 0x00, byte(feat>>8), byte(feat&0xFF), 0x00)); err != nil {
		return 0, false
	}
	fr, ok := readMatching(dev, logiReadMs*time.Millisecond, replyTo(devIdx))
	if !ok {
		return 0, false
	}
	fi := fr[4]
	if fi == 0 {
		fi = fr[5]
	}
	if fi == 0 || fi > featIdxMaxOK {
		return 0, false
	}
	return fi, true
}

// hfn executa uma função de uma feature e devolve a resposta (params em r[4:]).
func hfn(dev *hid.Device, devIdx, featIdx, fn byte, params ...byte) ([]byte, bool) {
	hflush(dev)
	if _, err := dev.Write(hreq(devIdx, featIdx, fn, params...)); err != nil {
		return nil, false
	}
	return readMatching(dev, logiReadMs*time.Millisecond, func(b []byte) bool {
		return replyTo(devIdx)(b) && (featIdx == 0 || b[2] == featIdx)
	})
}

// ---------- descoberta ----------

// logiRecvPaths localiza as coleções HID++ (usagePage 0xFF00, usage 0x0002)
// de todos os receptores Logitech conectados — qualquer PID, não só o C539.
func logiRecvPaths() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	_ = hid.Enumerate(logiVID, 0, func(di *hid.DeviceInfo) error {
		if di.UsagePage == 0xFF00 && di.Usage == 0x0002 {
			add(di.Path)
		}
		return nil
	})
	if len(out) == 0 {
		// fallback: a MI_02&Col02 clássica do receptor
		_ = hid.Enumerate(logiVID, 0, func(di *hid.DeviceInfo) error {
			if containsMI02Col02(di.Path) {
				add(di.Path)
			}
			return nil
		})
	}
	return out
}

func containsMI02Col02(p string) bool {
	const key = "MI_02&Col02"
	if len(p) < len(key) {
		return false
	}
	for i := 0; i+len(key) <= len(p); i++ {
		if p[i:i+len(key)] == key {
			return true
		}
	}
	return false
}

// hname lê o nome do aparelho pareado (feature 0x0005, fn 0x01, em blocos de
// 16 bytes). Ex.: "G Pro Wireless Gaming Mouse". Vazio se o aparelho não expor.
func hname(dev *hid.Device, devIdx byte) string {
	fi, ok := hfeature(dev, devIdx, nameFeat)
	if !ok {
		return ""
	}
	var sb strings.Builder
	for off := byte(0); off < 64 && sb.Len() < 64; off += 16 {
		r, ok := hfn(dev, devIdx, fi, 0x01, off, 0x00, 0x00)
		if !ok || len(r) < 5 {
			break
		}
		chunk := r[4:]
		n := 0
		for n < len(chunk) && chunk[n] != 0 {
			n++
		}
		sb.Write(chunk[:n])
		if n < 16 {
			break // terminou antes do fim do bloco
		}
	}
	return strings.TrimSpace(sb.String())
}

// logiDiscover varre os índices 1..6 de cada receptor e devolve os aparelhos
// que responderam ao ping (pareados e acordados).
func logiDiscover() []*Device {
	var out []*Device
	for _, path := range logiRecvPaths() {
		dev, err := hid.OpenPath(path)
		if err != nil {
			logf("receptor Logitech inacessível (%s): %v", recvName(path), err)
			continue
		}
		for i := 1; i <= logiMaxIdx; i++ {
			idx := byte(i)
			if !hping(dev, idx, logiPingMs*time.Millisecond) {
				continue
			}
			name := hname(dev, idx)
			if name == "" {
				name = fmt.Sprintf("Logitech %s (índice %d)", recvName(path), idx)
			}
			out = append(out, &Device{
				ID:   fmt.Sprintf("logi:%s:%d", path, idx),
				Name: name,
				read: func() Battery { return readLogiOn(path, idx) },
			})
		}
		dev.Close()
	}
	return out
}

// recvName reduz o path do receptor a algo legível (ex.: "C539").
func recvName(path string) string {
	if i := strings.Index(path, "PID_"); i >= 0 && i+8 <= len(path) {
		return path[i+4 : i+8]
	}
	return "receptor"
}

// ---------- cache de dispositivos descobertos ----------

var (
	logiMu    sync.Mutex
	logiCache = map[string]*Device{} // preservado entre descobertas (sono não remove)
)

// logiDevices faz a descoberta e devolve a lista completa (cache + novos).
// Um aparelho que entra em sono não é removido: o menu passa a mostrar
// "dormindo" em vez de fazer o item sumir toda vez.
func logiDevices() []*Device {
	fresh := logiDiscover()
	logiMu.Lock()
	for _, d := range fresh {
		logiCache[d.ID] = d
	}
	out := make([]*Device, 0, len(logiCache))
	for _, d := range logiCache {
		out = append(out, d)
	}
	logiMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ---------- leitura de bateria ----------

// readLogiOn consulta a bateria de um aparelho pareado num receptor.
func readLogiOn(path string, devIdx byte) Battery {
	dev, err := hid.OpenPath(path)
	if err != nil {
		return Battery{Err: "sem acesso ao receptor: " + err.Error()}
	}
	defer dev.Close()

	// 1. ping (acorda o aparelho)
	if !hping(dev, devIdx, logiReadMs*time.Millisecond) {
		return Battery{Asleep: true}
	}

	// 2. preferência: 0x1004 (percentual direto)
	if fi, ok := hfeature(dev, devIdx, levelFeat); ok {
		if r, ok := hfn(dev, devIdx, fi, 0x00, 0x00, 0x00, 0x00); ok && len(r) > 5 {
			if lvl := int(r[4]); lvl <= 100 {
				pct := lvl
				// Sem hardware aqui que exponha 0x1004 para validar as flags de
				// carregamento, mantém-se false em vez de arriscar leitura errada.
				return Battery{Percent: &pct}
			}
		}
	}

	// 3. fallback: 0x1001 (voltagem) + LUT mV->% (é o que o G PRO Wireless tem)
	fi, ok := hfeature(dev, devIdx, voltFeat)
	if !ok {
		return Battery{Err: "receptor não expôs feature de bateria"}
	}
	vr, ok := hfn(dev, devIdx, fi, 0x00, 0x00, 0x00, 0x00)
	if !ok || len(vr) < 7 {
		return Battery{Err: "sem leitura de voltagem"}
	}
	mv := uint16(vr[4])<<8 | uint16(vr[5])
	if mv < 2500 || mv > 4500 {
		return Battery{Err: "voltagem fora do intervalo"}
	}
	flags := vr[6]
	pct := mvToPercent(mv)
	return Battery{
		Percent:   &pct,
		Millivolt: int(mv),
		Charging:  flags&0x80 != 0 && flags&0x07 != 2,
	}
}
