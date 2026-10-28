// Dispositivos suportados: unidade de descoberta + leitura que alimenta o menu.
//
// Cada fonte declara seus aparelhos com um ID estável (o menu e os alertas
// são indexados por ele) e uma função de leitura. Novas fontes entram aqui.
package main

import (
	"sync"

	hid "github.com/sstallion/go-hid"
)

// Device é um aparelho com bateria rastreado pela bandeja.
type Device struct {
	ID   string         // estável entre reinícios (ex.: "logi:<path>:1")
	Name string         // nome exibido no menu e nas notificações
	read func() Battery // consulta a bateria (só leitura)
}

var (
	mchoseOnce sync.Once
	mchoseDev  *Device
)

// devices reúne os aparelhos suportados neste PC:
//   - Logitech: descoberta automática nos receptores (índices 1..6, HID++)
//   - MCHOSE: dongle próprio, presente só enquanto estiver conectado
func devices() []*Device {
	out := logiDevices()
	if _, ok := findMchose(); ok {
		out = append(out, mchoseDevice())
	}
	return out
}

// mchoseDevice monta o device do headset; o nome vem da própria string HID
// (ex.: "MCHOSE V9 Pro 2"), resolvida uma única vez.
func mchoseDevice() *Device {
	mchoseOnce.Do(func() {
		name := "Headset MCHOSE"
		if path, ok := findMchose(); ok {
			if d, err := hid.OpenPath(path); err == nil {
				if prod, err := d.GetProductStr(); err == nil && prod != "" {
					name = prod
				}
				d.Close()
			}
		}
		mchoseDev = &Device{
			ID:   "mchose:3837:6011",
			Name: name,
			read: readMchose,
		}
	})
	return mchoseDev
}
