// Alertas de bateria baixa (10% e 5%) com notificação toast.
// O estado do alerta é por dispositivo e por limiar: dispara uma vez ao cruzar
// o limiar e re-arma quando o nível volta acima dele (ex.: colocou para carregar).
package main

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

// cfgPath: preferência persistida ao lado do executável.
var cfgPath = strings.TrimSuffix(logPath, ".log") + ".cfg"

var (
	alertMu  sync.Mutex
	notifyOn = loadNotifyPref()
	// alerted: limiares já disparados, por ID de dispositivo (e por limiar)
	alerted = map[string]map[int]bool{}
)

func loadNotifyPref() bool {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return true // padrão: habilitado
	}
	return strings.TrimSpace(string(b)) != "0"
}

func saveNotifyPref(on bool) {
	v := "1"
	if !on {
		v = "0"
	}
	_ = os.WriteFile(cfgPath, []byte(v), 0o644)
}

// checkAlert dispara a notificação apropriada, se necessário.
func checkAlert(name string, alerted map[int]bool, b Battery) {
	if b.Err != "" || b.Asleep || b.Percent == nil {
		return
	}
	pct := *b.Percent

	// re-arma os limiares que o nível já ultrapassou (subiu de novo)
	for _, th := range []int{levelWarn, levelCrit} {
		if pct > th {
			delete(alerted, th)
		}
	}

	// escolhe o limiar mais severo atingido e ainda não disparado
	for _, th := range []int{levelCrit, levelWarn} {
		if pct <= th && !alerted[th] {
			alerted[th] = true
			lvl := "Bateria baixa"
			if th == levelCrit {
				lvl = "Bateria crítica"
			}
			body := name + " com " + strconv.Itoa(pct) + "% — carregue em breve."
			logf("alerta %s (%d%%) para %s", lvl, pct, name)
			go ShowToast(toastMsg{title: lvl, body: body, level: th, percent: pct})
			return // uma notificação por leitura
		}
	}
}

// evaluateAlerts roda os alertas de todos os dispositivos lidos, se habilitados.
func evaluateAlerts(results []devResult) {
	alertMu.Lock()
	defer alertMu.Unlock()
	if !notifyOn {
		return
	}
	for _, r := range results {
		m := alerted[r.id]
		if m == nil {
			m = map[int]bool{}
			alerted[r.id] = m
		}
		checkAlert(r.name, m, r.bat)
	}
}

// toggleNotify alterna e persiste a preferência.
func toggleNotify() bool {
	alertMu.Lock()
	notifyOn = !notifyOn
	saveNotifyPref(notifyOn)
	v := notifyOn
	alertMu.Unlock()
	logf("notificações de bateria baixa: %v", v)
	return v
}
