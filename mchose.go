// Leitor de bateria do headset MCHOSE V9 Pro 2 (dongle 3837:6011).
// Protocolo: frame AA do M HUB na coleção vendor 0xFF07/0x222, report 0x55.
//
//	pedido:   55 AA 01 00 01 00 01 0B 00 ...  (64 bytes)
//	resposta: 55 AA 01 <len> 00 01 0B <sleepState> <bit7 charging|percent> <checksum>
package main

import (
	"time"

	hid "github.com/sstallion/go-hid"
)

const (
	mchoseVID       = 0x3837
	mchosePID       = 0x6011
	mchoseUsagePage = 0xFF07
	mchoseUsage     = 0x0222
	mchoseReportID  = 0x55
	mchoseReadMs    = 900
)

type Battery struct {
	Percent   *int // nil = sem nível (dormindo/indisponível)
	Charging  bool
	Asleep    bool
	Millivolt int // apenas Logitech
	Err       string
}

// checksumOk valida o frame AA: flags, length, cmdType, cmd e payload XORados.
func checksumOk(frame []byte) bool {
	if len(frame) < 8 {
		return false
	}
	flags, length := frame[2], frame[3]
	if length < 1 || len(frame) < 7+int(length) {
		return false
	}
	acc := flags ^ length ^ frame[4] ^ frame[5] ^ frame[6]
	for i := 7; i < 7+int(length)-1; i++ {
		acc ^= frame[i]
	}
	expected := byte(0)
	if flags == 1 {
		expected = acc
	}
	return frame[7+int(length)-1] == expected
}

// parseMchose decodifica a resposta; ok=false quando não é um reply nosso.
func parseMchose(buf []byte) (pct int, charging, asleep, ok bool) {
	if len(buf) < 11 {
		return
	}
	// hidapi pode entregar o report id já na posição 0 (Windows)
	data := buf
	if data[0] != mchoseReportID {
		if len(data) > 1 && data[1] == mchoseReportID {
			data = data[1:]
		} else {
			return
		}
	}
	if data[0] != mchoseReportID || data[1] != 0xAA {
		return
	}
	if data[5] != 0x00 || data[6] != 0x01 || data[7] != 0x0B {
		return
	}
	if !checksumOk(data[1:]) {
		return
	}
	length := int(data[4])
	if length < 3 || len(data) < 8+length-1 {
		return
	}
	payload := data[8 : 8+length-1]
	if payload[0] == 0 {
		return 0, false, true, true
	}
	v := int(payload[1] & 0x7F)
	if v > 100 {
		return
	}
	return v, payload[1]&0x80 != 0, false, true
}

func findMchose() (string, bool) {
	found := ""
	_ = hid.Enumerate(mchoseVID, mchosePID, func(di *hid.DeviceInfo) error {
		if di.UsagePage == mchoseUsagePage && di.Usage == mchoseUsage && found == "" {
			found = di.Path
		}
		return nil
	})
	return found, found != ""
}

// readMchose consulta a bateria do headset.
func readMchose() Battery {
	path, ok := findMchose()
	if !ok {
		return Battery{Err: "dongle não encontrado"}
	}
	dev, err := hid.OpenPath(path)
	if err != nil {
		return Battery{Err: "sem acesso ao dongle: " + err.Error()}
	}
	defer dev.Close()

	req := make([]byte, 64)
	req[0] = mchoseReportID
	req[1] = 0xAA
	req[2] = 0x01
	req[3] = 0x00
	req[4] = 0x01
	req[5] = 0x00
	req[6] = 0x01
	req[7] = 0x0B
	req[8] = 0x00

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := dev.Write(req); err != nil {
			return Battery{Err: "escrita recusada: " + err.Error()}
		}
		buf := make([]byte, 64)
		n, err := dev.ReadWithTimeout(buf, mchoseReadMs*time.Millisecond)
		if err != nil || n <= 0 {
			continue
		}
		if pct, charging, asleep, ok := parseMchose(buf[:n]); ok {
			if asleep {
				return Battery{Asleep: true}
			}
			return Battery{Percent: &pct, Charging: charging}
		}
		// resposta de outro tipo (telemetria) — tenta de novo
	}
	return Battery{Err: "headset não respondeu (desligado ou dormindo?)"}
}
