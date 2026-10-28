package main

import (
	"fmt"
	"sort"

	hid "github.com/sstallion/go-hid"
)

type strRow struct {
	Key    string
	VID    uint16
	PID    uint16
	UP     uint16
	U      uint16
	Mfr    string
	Prod   string
	Serial string
}

// phase5 imprime as strings HID que o próprio Windows expõe — a fonte de nome
// mais barata possível (nenhum protocolo proprietário necessário).
func phase5() {
	fmt.Println("\n=== FASE 5: strings HID por interface (fonte de nomes) ===")

	var rows []strRow
	_ = hid.Enumerate(0, 0, func(di *hid.DeviceInfo) error {
		dev, err := hid.OpenPath(di.Path)
		if err != nil {
			rows = append(rows, strRow{
				VID: di.VendorID, PID: di.ProductID, UP: di.UsagePage, U: di.Usage,
				Prod: "(sem acesso)",
			})
			return nil
		}
		mfr, _ := dev.GetMfrStr()
		prod, _ := dev.GetProductStr()
		ser, _ := dev.GetSerialNbr()
		dev.Close()
		rows = append(rows, strRow{
			VID: di.VendorID, PID: di.ProductID, UP: di.UsagePage, U: di.Usage,
			Mfr: mfr, Prod: prod, Serial: ser,
		})
		return nil
	})

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].VID != rows[j].VID {
			return rows[i].VID < rows[j].VID
		}
		if rows[i].PID != rows[j].PID {
			return rows[i].PID < rows[j].PID
		}
		return rows[i].U < rows[j].U
	})

	last := ""
	for _, r := range rows {
		key := fmt.Sprintf("%04X:%04X", r.VID, r.PID)
		if key != last {
			fmt.Printf("\n-- %s --\n", key)
			last = key
		}
		fmt.Printf("   up=%04X u=%04X  mfr=%-22q prod=%-26q serial=%q\n",
			r.UP, r.U, r.Mfr, r.Prod, r.Serial)
	}
}
