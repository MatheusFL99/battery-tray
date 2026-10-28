package main

import (
	"fmt"
	"time"

	hid "github.com/sstallion/go-hid"
)

// req monta um report HID++ longo CORRETO: byte3 = (fn << 4) | swID.
// (O bug anterior colocava fn no nibble baixo, fazendo o aparelho sempre
// executar a função 0 — por isso toda resposta saía idêntica.)
func req(devIdx, featIdx, fn byte, params ...byte) []byte {
	r := []byte{0x11, devIdx, featIdx, (fn << 4) | swID, 0x00, 0x00, 0x00, swID}
	for i, p := range params {
		if i+4 < len(r)-1 {
			r[4+i] = p
		}
	}
	return r
}

// phase8: nome do aparelho pareado via feature 0x0005, com a codificação correta.
func phase8() {
	fmt.Println("\n=== FASE 8: nome via 0x0005 (fn no nibble alto) ===")

	var recvs []string
	_ = hid.Enumerate(0x046D, 0, func(di *hid.DeviceInfo) error {
		if di.UsagePage == 0xFF00 && di.Usage == 0x0002 {
			recvs = append(recvs, di.Path)
		}
		return nil
	})

	for _, path := range recvs {
		dev, err := hid.OpenPath(path)
		if err != nil {
			continue
		}
		flush(dev)

		for idx := byte(1); idx <= 6; idx++ {
			flush(dev)
			dev.Write(req(idx, 0x00, 0x01)) // ping (fn 0x01)
			pr, ok := readRaw(dev, 800*time.Millisecond)
			if !ok {
				continue
			}
			fmt.Printf("\n  idx %d presente — ping: % X\n", idx, pr)

			// indice da feature 0x0005
			flush(dev)
			dev.Write(req(idx, 0x00, 0x00, 0x00, 0x05, 0x00))
			fr, ok := readRaw(dev, 800*time.Millisecond)
			if !ok {
				fmt.Println("    sem resposta getFeatureIndex(0x0005)")
				continue
			}
			fi := fr[4]
			if fi == 0 {
				fi = fr[5]
			}
			fmt.Printf("    0x0005 -> idx=%02X (resp=% X)\n", fi, fr)
			if fi == 0 || fi > 0x40 {
				continue
			}

			// varre funções com codificação correta
			for fn := byte(0); fn <= 0x07; fn++ {
				flush(dev)
				dev.Write(req(idx, fi, fn, 0x00, 0x00, 0x00))
				vr, ok := readRaw(dev, 600*time.Millisecond)
				if !ok {
					fmt.Printf("        fn %02X: sem resposta\n", fn)
					continue
				}
				if vr[3]&0xF0 == 0xF0 { // resposta de erro (0x8F/0x9F...)
					fmt.Printf("        fn %02X: ERRO (%02X)\n", fn, vr[3])
					continue
				}
				fmt.Printf("        fn %02X: % X  |%s|\n", fn, vr[4:], ascii(vr[4:]))
			}

			// tenta ler o nome em blocos (offset em param0/param1)
			for _, off := range []byte{0, 16, 32} {
				flush(dev)
				dev.Write(req(idx, fi, 0x05, off, 0x00, 0x00))
				vr, ok := readRaw(dev, 600*time.Millisecond)
				if ok {
					fmt.Printf("        fn05(off=%d): % X  |%s|\n", off, vr[4:], ascii(vr[4:]))
				}
			}
		}
		dev.Close()
	}
}
