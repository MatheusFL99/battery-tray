// Bandeja de bateria de dispositivos sem nenhum software oficial dos
// fabricantes (leitura HID direta, só consultas).
//
// Os aparelhos são descobertos automaticamente — receptores Logitech via
// HID++ (índices 1..6) e o dongle MCHOSE — e cada um vira um item do menu,
// criado e removido conforme aparecem/desaparecem.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/getlantern/systray"
)

//go:embed icon.ico
var iconICO []byte

// logPath: arquivo de diagnóstico ao lado do executável.
var logPath = func() string {
	exe, err := os.Executable()
	if err != nil {
		return "batterytray.log"
	}
	return filepath.Join(filepath.Dir(exe), "batterytray.log")
}()

const pollInterval = 60 * time.Second

// logf registra em arquivo para diagnóstico sem depender de console.
func logf(format string, args ...interface{}) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

// devResult é a leitura de um dispositivo em um ciclo, usada pelo menu e alertas.
type devResult struct {
	id    string
	name  string
	title string // título já formatado ("nome: 28% (3747 mV)")
	bat   Battery
}

// maxDeviceSlots: slots de dispositivo pré-alocados no topo do menu. O systray
// ordena os itens pelo ID de criação (visibleItems é mantido ordenado por ID),
// então reservar slots ANTES dos itens fixos garante que um aparelho descoberto
// depois (ex.: mouse que estava dormindo) apareça no topo, e não após "Sair".
const maxDeviceSlots = 16

// devSlot é um item de menu reservado para um dispositivo.
type devSlot struct {
	item   *systray.MenuItem
	hidden bool
	devID  string // "" = livre
}

var (
	devSlots    []*devSlot
	extraItems  = map[string]*systray.MenuItem{} // estouro do pool (fim do menu)
	lastUpd     time.Time
	lastResults []devResult
	itemStatus  *systray.MenuItem
)

func fmtBat(b Battery) string {
	if b.Err != "" {
		return "⚠ " + b.Err
	}
	if b.Asleep || b.Percent == nil {
		return "dormindo / sem nível"
	}
	s := fmt.Sprintf("%d%%", *b.Percent)
	if b.Charging {
		s += " · carregando"
	}
	if b.Millivolt > 0 {
		s += fmt.Sprintf(" (%d mV)", b.Millivolt)
	}
	return s
}

// syncDevices mantém um item por dispositivo. A ordem é dada pelo slot
// (ID baixo = topo do menu), com atribuição por posição na lista ordenada —
// assim a ordem dos aparelhos não depende de quem acordou primeiro.
func syncDevices(list []devResult) {
	ordered := append([]devResult(nil), list...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].id < ordered[j].id })

	// slot desejado por posição: slot i = i-ésimo dispositivo da ordem estável
	desired := make([]string, len(devSlots))
	present := make(map[string]bool, len(ordered))
	for i, r := range ordered {
		present[r.id] = true
		if i < len(desired) {
			desired[i] = r.id
		}
	}

	// fase 1: libera os slots cujo ocupante não é o desejado
	for i, s := range devSlots {
		if s.devID != "" && s.devID != desired[i] {
			s.item.Hide()
			s.hidden = true
			s.devID = ""
		}
	}
	// fase 2: atribui/preenche (SetTitle num item escondido o reinsere no topo)
	for i, id := range desired {
		if id == "" {
			continue
		}
		s := devSlots[i]
		s.devID = id
		for _, r := range ordered {
			if r.id == id {
				s.item.SetTitle(r.title)
				break
			}
		}
		s.hidden = false
	}
	// fase 3: slots sem dispositivo ficam ocultos
	for _, s := range devSlots {
		if s.devID == "" && !s.hidden {
			s.item.Hide()
			s.hidden = true
		}
	}

	// estouro do pool (> maxDeviceSlots aparelhos): itens extras, no fim do menu
	for id, it := range extraItems {
		if !present[id] {
			it.Hide()
			delete(extraItems, id)
		}
	}
	for _, r := range ordered {
		if slotOfDev(r.id) || extraItems[r.id] != nil {
			continue
		}
		it := systray.AddMenuItem(r.title, r.name)
		it.Disable()
		extraItems[r.id] = it
		logf("aviso: pool de slots estourou; %s no fim do menu", r.name)
	}
}

// slotOfDev informa se um dispositivo ocupa algum slot.
func slotOfDev(id string) bool {
	for _, s := range devSlots {
		if s.devID == id {
			return true
		}
	}
	return false
}

func refresh() {
	// descoberta + leituras: uma falha não afeta as demais
	devs := devices()
	results := make([]devResult, 0, len(devs))
	for _, d := range devs {
		bat := d.read()
		results = append(results, devResult{
			id:    d.ID,
			name:  d.Name,
			title: fmt.Sprintf("%s: %s", d.Name, fmtBat(bat)),
			bat:   bat,
		})
	}
	syncDevices(results)

	lastUpd = time.Now()

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	lastResults = results
	logf("%s | heap=%dMB sys=%dMB idle=%dMB released=%dMB gc=%d",
		strings.Join(resultLines(results), " | "),
		ms.HeapAlloc/1024/1024, ms.Sys/1024/1024,
		ms.HeapIdle/1024/1024, ms.HeapReleased/1024/1024, ms.NumGC)
	updateStatus()
	evaluateAlerts(results)
}

// resultLines formata as leituras para log e tooltip.
func resultLines(results []devResult) []string {
	parts := make([]string, 0, len(results))
	for _, r := range results {
		parts = append(parts, r.name+"="+fmtBat(r.bat))
	}
	return parts
}

// updateStatus atualiza a linha de status (só depois que o item existir).
func updateStatus() {
	if itemStatus == nil {
		return
	}
	itemStatus.SetTitle("Atualizado às " + lastUpd.Format("15:04:05"))
	lines := resultLines(lastResults)
	if len(lines) == 0 {
		lines = []string{"Nenhum dispositivo encontrado"}
	}
	itemStatus.SetTooltip(strings.Join(lines, "\n"))
}

func onReady() {
	systray.SetIcon(iconICO)
	systray.SetTooltip("Bateria dos dispositivos (sem softwares dos fabricantes)")

	// slots de dispositivo ANTES de qualquer outro item: como o systray ordena
	// o menu pelo ID de criação, eles ficam sempre no topo — mesmo que o
	// aparelho só seja descoberto depois (mouse dormindo na inicialização)
	for i := 0; i < maxDeviceSlots; i++ {
		it := systray.AddMenuItem("", "")
		it.Disable()
		it.Hide()
		devSlots = append(devSlots, &devSlot{item: it, hidden: true})
	}

	systray.AddSeparator()
	itemStatus = systray.AddMenuItem("Atualizando…", "Dispositivos descobertos")
	itemStatus.Disable()

	itemRefresh := systray.AddMenuItem("Atualizar agora", "Força uma nova leitura")

	alertMu.Lock()
	started := notifyOn
	alertMu.Unlock()
	itemNotify := systray.AddMenuItemCheckbox(
		"Notificar bateria baixa (10% e 5%)",
		"Mostra um aviso quando um dos dispositivos chegar a 10% ou 5%",
		started)
	itemTest := systray.AddMenuItem("Testar notificação",
		"Mostra um exemplo de aviso de bateria baixa")
	systray.AddSeparator()
	itemQuit := systray.AddMenuItem("Sair", "Encerra a bandeja")

	startToastLoop() // janela de notificação (criada uma única vez)

	// modo de teste: BATTERYTRAY_TEST=1 dispara um aviso ao iniciar
	// (usado para validar o desenho do toast sem precisar drenar a bateria)
	if os.Getenv("BATTERYTRAY_TEST") != "" {
		go func() {
			time.Sleep(1200 * time.Millisecond)
			ShowToast(toastMsg{title: "Bateria baixa",
				body:  "Exemplo: Mouse com 9% — carregue em breve.",
				level: levelWarn, percent: 9})
		}()
	}

	// primeira leitura (descoberta + leituras), depois ciclo automático
	logf("iniciado, poll a cada %s", pollInterval)
	refresh()
	go func() {
		for {
			select {
			case <-time.After(pollInterval):
				refresh()
			case <-itemRefresh.ClickedCh:
				refresh()
			case <-itemNotify.ClickedCh:
				if toggleNotify() {
					itemNotify.Check()
				} else {
					itemNotify.Uncheck()
				}
			case <-itemTest.ClickedCh:
				// exemplo de aviso (não altera o estado de alerta real)
				go ShowToast(toastMsg{
					title: "Bateria baixa",
					body:  "Exemplo: Mouse com 9% — carregue em breve.",
					level: levelWarn, percent: 9,
				})
			case <-itemQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func main() {
	// limita o heap do Go para manter a residente baixa
	debug.SetMemoryLimit(16 << 20) // 16 MiB
	systray.Run(onReady, func() {
		logf("encerrado")
	})
}
