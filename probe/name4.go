package main

import (
	"fmt"
	"time"

	hid "github.com/sstallion/go-hid"
)

// phase11: nome completo do aparelho pareado, lido em blocos de 16 bytes
// (feature 0x0005, fn 0x01 = getNameBytes, offset em param0).
func phase11() {
	fmt.Println("=== FASE 11: nome completo em blocos ===")

	var recvs []string
	_ = hid.Enumerate(0x046D, 0, func(di *hid.DeviceInfo) error {
		if di.UsagePage == 0xFF00 && di.Usage == 0x0002 {
			recvs = append(recvs, di.Path)
		}
		return nil
	})
	if len(recvs) == 0 {
		fmt.Println("  nenhum receptor")
		return
	}

	for _, path := range recvs {
		dev, err := hid.OpenPath(path)
		if err != nil {
			continue
		}

		for idx := byte(1); idx <= 6; idx++ {
			flush(dev)
			dev.Write(req(idx, 0x00, 0x01)) // ping
			if _, ok := readRaw(dev, 800*time.Millisecond); !ok {
				continue
			}

			// indice da feature 0x0005
			flush(dev)
			dev.Write(req(idx, 0x00, 0x00, 0x00, 0x05, 0x00))
			fr, ok := readRaw(dev, 800*time.Millisecond)
			if !ok {
				continue
			}
			fi := fr[4]
			if fi == 0 {
				fi = fr[5]
			}
			if fi == 0 || fi > 0x40 {
				fmt.Printf("  idx %d: sem feature 0x0005\n", idx)
				continue
			}

			var name []byte
			for off := byte(0); off < 80; off += 16 {
				flush(dev)
				dev.Write(req(idx, fi, 0x01, off, 0x00, 0x00))
				vr, ok := readRaw(dev, 700*time.Millisecond)
				if !ok || vr[3]&0xF0 == 0xF0 {
					break
				}
				chunk := vr[4:]
				// corta no primeiro 0x00
				end := 0
				for end < len(chunk) && chunk[end] != 0 {
					end++
				}
				name = append(name, chunk[:end]...)
				if end < 16 {
					break // terminou
				}
			}
			fmt.Printf("  idx %d: nome=%q\n", idx, string(name))

			// tambem: ping tipo/fw + bateria (0x1001 voltagem)
			flush(dev)
			dev.Write(req(idx, 0x00, 0x01))
			if pr, ok := readRaw(dev, 700*time.Millisecond); ok {
				fmt.Printf("          tipo=%d fw=%d.%d\n", pr[4], pr[5], pr[6])
			}
		}
		dev.Close()
	}
}
