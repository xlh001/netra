package main

import (
	"encoding/binary"
	"errors"
	"log"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
)

const sniEventHeaderLen = 16

func startSNIReader(m *ebpf.Map, onSNI func(key xdpflowFlowKey, hostname string)) (*ringbuf.Reader, error) {
	reader, err := ringbuf.NewReader(m)
	if err != nil {
		return nil, err
	}

	go func() {
		defer recoverAndLog("sni reader")
		for {
			record, err := reader.Read()
			if err != nil {
				if !errors.Is(err, ringbuf.ErrClosed) {

					log.Printf("sni reader: unexpected error, SNI capture has stopped: %v", err)
				}
				return
			}
			handleSNIEventSafely(record.RawSample, onSNI)
		}
	}()

	return reader, nil
}

func handleSNIEventSafely(raw []byte, onSNI func(key xdpflowFlowKey, hostname string)) {
	defer recoverAndLog("sni event handler")
	handleSNIEvent(raw, onSNI)
}

func handleSNIEvent(raw []byte, onSNI func(key xdpflowFlowKey, hostname string)) {
	if len(raw) < sniEventHeaderLen {
		return
	}
	saddr := binary.LittleEndian.Uint32(raw[0:4])
	daddr := binary.LittleEndian.Uint32(raw[4:8])
	sport := binary.LittleEndian.Uint16(raw[8:10])
	dport := binary.LittleEndian.Uint16(raw[10:12])
	payloadLen := binary.LittleEndian.Uint32(raw[12:16])

	payload := raw[sniEventHeaderLen:]
	if int(payloadLen) <= len(payload) {
		payload = payload[:payloadLen]
	}

	hostname, ok := parseTLSClientHelloSNI(payload)
	if !ok || hostname == "" || isIPLiteral(hostname) {
		return
	}

	key := xdpflowFlowKey{Saddr: saddr, Daddr: daddr, Sport: sport, Dport: dport, Proto: 6}
	onSNI(key, hostname)
}

func parseTLSClientHelloSNI(b []byte) (string, bool) {

	if len(b) < 5 || b[0] != 0x16 {
		return "", false
	}
	b = b[5:]

	if len(b) < 4 || b[0] != 0x01 {
		return "", false
	}
	b = b[4:]

	if len(b) < 34 {
		return "", false
	}
	b = b[34:]

	if len(b) < 1 {
		return "", false
	}
	sidLen := int(b[0])
	b = b[1:]
	if len(b) < sidLen {
		return "", false
	}
	b = b[sidLen:]

	if len(b) < 2 {
		return "", false
	}
	csLen := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if len(b) < csLen {
		return "", false
	}
	b = b[csLen:]

	if len(b) < 1 {
		return "", false
	}
	cmLen := int(b[0])
	b = b[1:]
	if len(b) < cmLen {
		return "", false
	}
	b = b[cmLen:]

	if len(b) < 2 {
		return "", false
	}
	extTotalLen := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if len(b) > extTotalLen {
		b = b[:extTotalLen]
	}

	for len(b) >= 4 {
		extType := binary.BigEndian.Uint16(b[0:2])
		extLen := int(binary.BigEndian.Uint16(b[2:4]))
		b = b[4:]
		if len(b) < extLen {
			return "", false
		}
		extData := b[:extLen]
		b = b[extLen:]

		if extType != 0x0000 {
			continue
		}

		if len(extData) < 2 {
			return "", false
		}
		list := extData[2:]
		if len(list) < 3 {
			return "", false
		}
		nameType := list[0]
		nameLen := int(binary.BigEndian.Uint16(list[1:3]))
		list = list[3:]
		if nameType != 0 || len(list) < nameLen {
			return "", false
		}
		return string(list[:nameLen]), true
	}

	return "", false
}
