package main

import (
	"fmt"
	"time"

	hid "github.com/sstallion/go-hid"
)

// phase10: diagnóstico bruto — imprime QUALQUER leitura e o erro, sem filtro.
func phase10() {
	fmt.Println("=== FASE 10: leitura bruta sem filtro ===")

	var recvs []string
	_ = hid.Enumerate(0x046D, 0, func(di *hid.DeviceInfo) error {
		if di.UsagePage == 0xFF00 && di.Usage == 0x0002 {
			recvs = append(recvs, di.Path)
		}
		return nil
	})
	fmt.Printf("  receptores HID++: %d\n", len(recvs))
	if len(recvs) == 0 {
		return
	}
	fmt.Printf("  path: %s\n", recvs[0])

	dev, err := hid.OpenPath(recvs[0])
	if err != nil {
		fmt.Println("  OpenPath:", err)
		return
	}
	defer dev.Close()
	fmt.Println("  aberto com sucesso")

	// 1) só lê, sem escrever (vê se há tráfego espontâneo)
	fmt.Println("  -- leitura passiva 1s --")
	buf := make([]byte, 64)
	n, err := dev.ReadWithTimeout(buf, 1000*time.Millisecond)
	fmt.Printf("     n=%d err=%v data=% X\n", n, err, buf[:max(0, n)])

	// 2) escreve o ping clássico de 7 bytes e lê filtrando e sem filtrar
	fmt.Println("  -- ping 7 bytes --")
	wn, werr := dev.Write([]byte{0x11, 0x01, 0x00, 0x1A, 0x00, 0x00, swID})
	fmt.Printf("     write n=%d err=%v\n", wn, werr)

	for i := 0; i < 3; i++ {
		n, err := dev.ReadWithTimeout(buf, 700*time.Millisecond)
		fmt.Printf("     try%d n=%d err=%v data=% X\n", i, n, err, buf[:max(0, n)])
	}

	// 3) tenta também escrever e ler com o report de 8 bytes longo
	fmt.Println("  -- ping 8 bytes --")
	wn, werr = dev.Write([]byte{0x11, 0x01, 0x00, 0x1A, 0x00, 0x00, 0x00, swID})
	fmt.Printf("     write n=%d err=%v\n", wn, werr)
	for i := 0; i < 3; i++ {
		n, err := dev.ReadWithTimeout(buf, 700*time.Millisecond)
		fmt.Printf("     try%d n=%d err=%v data=% X\n", i, n, err, buf[:max(0, n)])
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
