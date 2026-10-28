package main

import (
	"fmt"
	"time"

	hid "github.com/sstallion/go-hid"
)

// phase9: compara ping de 7 bytes (funcional na fase 6) vs 8 bytes (req()).
func phase9() {
	fmt.Println("\\n=== FASE 9: ping 7 vs 8 bytes ===")

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

	dev, err := hid.OpenPath(recvs[0])
	if err != nil {
		fmt.Println("  abriu:", err)
		return
	}
	defer dev.Close()
	flush(dev)

	// variante A: 7 bytes (formato antigo da fase 6)
	dev.Write([]byte{0x11, 0x01, 0x00, 0x1A, 0x00, 0x00, swID})
	if r, ok := readRaw(dev, 800*time.Millisecond); ok {
		fmt.Printf("  A (7 bytes): OK  % X\\n", r[:8])
	} else {
		fmt.Println("  A (7 bytes): sem resposta")
	}
	flush(dev)

	// variante B: 8 bytes com fn no nibble alto (req)
	dev.Write(req(0x01, 0x00, 0x01))
	if r, ok := readRaw(dev, 800*time.Millisecond); ok {
		fmt.Printf("  B (8 bytes req): OK  % X\\n", r[:8])
	} else {
		fmt.Println("  B (8 bytes req): sem resposta")
	}
	flush(dev)

	// variante C: 7 bytes com fn no nibble alto (sem byte extra)
	c := []byte{0x11, 0x01, 0x00, (0x01 << 4) | swID, 0x00, 0x00, 0x00}
	dev.Write(c)
	if r, ok := readRaw(dev, 800*time.Millisecond); ok {
		fmt.Printf("  C (7 bytes fn alto): OK  % X\\n", r[:8])
	} else {
		fmt.Println("  C (7 bytes fn alto): sem resposta")
	}
}
