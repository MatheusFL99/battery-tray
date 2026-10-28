package main

import (
	"fmt"
	"time"

	hid "github.com/sstallion/go-hid"
)

// readRaw lê QUALQUER resposta de report longo, sem filtrar erro.
func readRaw(dev *hid.Device, timeout time.Duration) ([]byte, bool) {
	deadline := time.Now().Add(timeout)
	for {
		rem := time.Until(deadline)
		if rem <= 0 {
			return nil, false
		}
		buf := make([]byte, 64)
		n, err := dev.ReadWithTimeout(buf, rem)
		if err != nil || n <= 0 {
			return nil, false
		}
		if n >= 4 && buf[0] == 0x11 {
			return buf[:n], true
		}
	}
}

// flush drena respostas pendentes para não ler mensagens antigas.
func flush(dev *hid.Device) {
	buf := make([]byte, 64)
	for {
		n, err := dev.ReadWithTimeout(buf, 60*time.Millisecond)
		if err != nil || n <= 0 {
			return
		}
		fmt.Printf("        [lixeira] % X\n", buf[:n])
	}
}

// ascii torna bytes legíveis para inspeção de strings.
func ascii(b []byte) string {
	r := make([]rune, 0, len(b))
	for _, c := range b {
		if c >= 0x20 && c < 0x7F {
			r = append(r, rune(c))
		} else {
			r = append(r, '.')
		}
	}
	return string(r)
}
