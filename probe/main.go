// Sonda: inventário HID do sistema + varredura de receptores Logitech.
// Não faz parte do app (package main próprio); roda com `go run ./probe`.
package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	hid "github.com/sstallion/go-hid"
)

// sysArgs é um alias para permitir injeção em teste; em produção, os.Args.
var sysArgs = os.Args

const swID = 0x0A

// ---------- fase 1: inventário HID + descritores ----------

type iface struct {
	Path      string
	VID, PID  uint16
	UsagePage uint16
	Usage     uint16
	Product   string
}

// parseReportDescriptor extrai os pares (usagePage, usage) declarados.
func parseReportDescriptor(d []byte) [][2]uint16 {
	var out [][2]uint16
	var curPage uint16 = 0xFFFF
	i := 0
	for i < len(d) {
		b := d[i]
		if b == 0xFE { // item longo: 0xFE, tag, size, dados
			if i+2 >= len(d) {
				break
			}
			i += 3 + int(d[i+2])
			continue
		}
		size := int(b & 0x03)
		if size == 3 {
			size = 4
		}
		typ := (b >> 2) & 0x03
		tag := b >> 4
		if i+1+size > len(d) {
			break
		}
		data := d[i+1 : i+1+size]

		// Usage Page (global, tag 0x00)
		if typ == 0x01 && tag == 0x00 && size > 0 {
			var v uint16
			for k, c := range data {
				v |= uint16(c) << (8 * uint(k))
			}
			curPage = v
		}
		// Usage (local, tag 0x00)
		if typ == 0x02 && tag == 0x00 && size > 0 {
			var v uint16
			for k, c := range data {
				v |= uint16(c) << (8 * uint(k))
			}
			out = append(out, [2]uint16{curPage, v})
		}
		i += 1 + size
	}
	return out
}

func phase1() {
	var all []iface
	_ = hid.Enumerate(0, 0, func(di *hid.DeviceInfo) error {
		all = append(all, iface{
			Path: di.Path, VID: di.VendorID, PID: di.ProductID,
			UsagePage: di.UsagePage, Usage: di.Usage, Product: di.ProductStr,
		})
		return nil
	})

	fmt.Printf("\n=== FASE 1: %d interfaces HID ===\n", len(all))

	// agrupa por VID:PID
	byDev := map[string][]iface{}
	for _, f := range all {
		k := fmt.Sprintf("%04X:%04X", f.VID, f.PID)
		byDev[k] = append(byDev[k], f)
	}
	keys := make([]string, 0, len(byDev))
	for k := range byDev {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		prod := byDev[k][0].Product
		fmt.Printf("%-11s x%-2d %s\n", k, len(byDev[k]), prod)
	}

	// procura páginas de bateria em cada descritor
	fmt.Println("\n--- interfaces com página de bateria (0x04 Power / 0x06 Battery) ---")
	anyBat := false
	seenPath := map[string]bool{}
	for _, f := range all {
		if seenPath[f.Path] {
			continue
		}
		seenPath[f.Path] = true
		dev, err := hid.OpenPath(f.Path)
		if err != nil {
			continue
		}
		buf := make([]byte, 4096)
		n, err := dev.GetReportDescriptor(buf)
		dev.Close()
		if err != nil || n <= 0 {
			continue
		}
		us := parseReportDescriptor(buf[:n])
		var hit [][2]uint16
		for _, u := range us {
			if u[0] == 0x06 || u[0] == 0x04 {
				hit = append(hit, u)
			}
		}
		if len(hit) > 0 {
			anyBat = true
			fmt.Printf("  %-11s usagePage=%04X usage=%02X %s -> pagina(s) de bateria: %v\n",
				fmt.Sprintf("%04X:%04X", f.VID, f.PID), f.UsagePage, f.Usage, f.Product, hit)
		}
	}
	if !anyBat {
		fmt.Println("  NENHUMA interface declara página de bateria (0x04/0x06)")
	}
}

// ---------- fase 2: receptores Logitech, indices 1..6 ----------

func readReply(dev *hid.Device, timeout time.Duration) ([]byte, bool) {
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
		b := buf[:n]
		if len(b) >= 4 && b[0] == 0x11 && b[3] != 0x8F && b[3]&0x0F == swID {
			return b, true
		}
	}
}

func longReport(devIdx, featureIdx, fn byte, params ...byte) []byte {
	r := []byte{0x11, devIdx, featureIdx, fn | 0x80, 0x00, 0x00, swID}
	if fn&0x80 == 0 { // short: sem byte extra
		r = []byte{0x11, devIdx, featureIdx, fn, 0x00, 0x00, swID}
	}
	return r
}

func phase2() {
	fmt.Println("\n=== FASE 2: receptores Logitech, dispositivos pareados (indice 1..6) ===")

	var recvs []string
	_ = hid.Enumerate(0x046D, 0, func(di *hid.DeviceInfo) error {
		if di.UsagePage == 0xFF00 && di.Usage == 0x0002 {
			recvs = append(recvs, di.Path)
		}
		return nil
	})
	if len(recvs) == 0 {
		fmt.Println("  nenhum receptor com colecao HID++ (0xFF00/0x0002)")
		return
	}

	for _, path := range recvs {
		dev, err := hid.OpenPath(path)
		if err != nil {
			fmt.Printf("  nao abriu %s: %v\n", path, err)
			continue
		}
		fmt.Printf("\n  receptor: %s\n", path)

		for idx := byte(1); idx <= 6; idx++ {
			// ping: report 0x1A, feature index 0x00
			ping := []byte{0x11, idx, 0x00, 0x1A, 0x00, 0x00, swID}
			if _, err := dev.Write(ping); err != nil {
				continue
			}
			rep, ok := readReply(dev, 700*time.Millisecond)
			if !ok {
				continue
			}
			// resposta de ping traz tipo de dispositivo e versao de firmware
			devType, fwMajor, fwMinor := rep[4], rep[5], rep[6]
			fmt.Printf("    idx %d: tipo=%d fw=%d.%d  raw=% X\n", idx, devType, fwMajor, fwMinor, rep[:8])

			// procura features de bateria 0x1004 (nivel) e 0x1001 (voltagem)
			for _, feat := range []uint16{0x1004, 0x1001} {
				q := []byte{0x11, idx, 0x00, 0x0A, byte(feat >> 8), byte(feat & 0xFF), 0x00, swID}
				if _, err := dev.Write(q); err != nil {
					continue
				}
				fr, ok := readReply(dev, 700*time.Millisecond)
				if !ok {
					continue
				}
				// erro = 0x8F (ja filtrado) ou indice 0
				fi := fr[4]
				if fi == 0 {
					fi = fr[5]
				}
				if fi == 0 || fi > 0x40 {
					fmt.Printf("      feature %04X: inexistente\n", feat)
					continue
				}
				fmt.Printf("      feature %04X: idx=%02X  raw=% X\n", feat, fi, fr[:8])

				// le o valor (fn 0x00, params 00 00 00)
				rd := []byte{0x11, idx, fi, 0x0A, 0x00, 0x00, 0x00, swID}
				if _, err := dev.Write(rd); err != nil {
					continue
				}
				vr, ok := readReply(dev, 700*time.Millisecond)
				if !ok || len(vr) < 7 {
					fmt.Println("        sem leitura")
					continue
				}
				fmt.Printf("        valor=% X (mv? %d)\n", vr[4:7],
					int(vr[4])<<8|int(vr[5]))
			}
		}
		dev.Close()
	}
}

func main() {
	if err := hid.Init(); err != nil {
		fmt.Println("hid.Init:", err)
		return
	}
	defer hid.Exit()

	// fase opcional via argumento: `go run ./probe 8` roda só a fase 8
	if len(sysArgs) > 1 {
		switch sysArgs[1] {
		case "1":
			phase1() // inventário HID + páginas de bateria
		case "2":
			phase2() // receptores Logitech, índices 1..6
		case "5":
			phase5() // strings HID (fonte de nomes)
		case "8":
			phase8() // nome via 0x0005 (codificação correta)
		case "9":
			phase9() // ping de 7 vs 8 bytes
		case "10":
			phase10() // leitura bruta sem filtro
		case "11":
			phase11() // nome completo em blocos
		default:
			fmt.Println("fases: 1,2,5,8,9,10,11")
		}
		return
	}

	phase1()
	phase2()
	phase5()
	phase8()
	phase11()
}
