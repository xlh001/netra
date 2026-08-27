package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"log"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
)

const dpiEventHeaderLen = 20

func startDPIReader(m *ebpf.Map, onDetect func(key xdpflowFlowKey, service string)) (*ringbuf.Reader, error) {
	reader, err := ringbuf.NewReader(m)
	if err != nil {
		return nil, err
	}

	go func() {
		defer recoverAndLog("dpi reader")
		for {
			record, err := reader.Read()
			if err != nil {
				if !errors.Is(err, ringbuf.ErrClosed) {
					log.Printf("dpi reader: unexpected error, DPI capture has stopped: %v", err)
				}
				return
			}
			handleDPIEventSafely(record.RawSample, onDetect)
		}
	}()

	return reader, nil
}

func handleDPIEventSafely(raw []byte, onDetect func(key xdpflowFlowKey, service string)) {
	defer recoverAndLog("dpi event handler")
	handleDPIEvent(raw, onDetect)
}

func handleDPIEvent(raw []byte, onDetect func(key xdpflowFlowKey, service string)) {
	if len(raw) < dpiEventHeaderLen {
		return
	}
	saddr := binary.LittleEndian.Uint32(raw[0:4])
	daddr := binary.LittleEndian.Uint32(raw[4:8])
	sport := binary.LittleEndian.Uint16(raw[8:10])
	dport := binary.LittleEndian.Uint16(raw[10:12])
	proto := raw[12]
	payloadLen := binary.LittleEndian.Uint32(raw[16:20])

	payload := raw[dpiEventHeaderLen:]
	if int(payloadLen) <= len(payload) {
		payload = payload[:payloadLen]
	}

	service, ok := detectDPIService(payload)
	if !ok {
		return
	}

	key := xdpflowFlowKey{Saddr: saddr, Daddr: daddr, Sport: sport, Dport: dport, Proto: proto}
	onDetect(key, service)
}

func detectDPIService(payload []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(payload, []byte("SSH-")):
		return "ssh", true
	case bytes.HasPrefix(payload, []byte("+OK")):
		return "pop3", true
	case bytes.HasPrefix(payload, []byte("* OK")):
		return "imap", true
	case bytes.HasPrefix(payload, []byte("RFB ")):
		return "vnc", true
	case bytes.HasPrefix(payload, []byte("AMQP")):
		return "amqp", true
	case bytes.HasPrefix(payload, []byte("PRI * HTTP/2.0")):
		return "http2", true
	case len(payload) > 3 && payload[0] == '2' && payload[1] == '2' && payload[2] == '0' && (payload[3] == ' ' || payload[3] == '-'):
		return detectGreetingService(payload)
	case isTLSClientHello(payload):
		return "https", true
	case isRDPConnectionRequest(payload):
		return "rdp", true
	case isMySQLHandshake(payload):
		return "mysql", true
	case isPostgresStartup(payload):
		return "postgresql", true
	case isMongoDBWireMessage(payload):
		return "mongodb", true
	case isRedisCommand(payload):
		return "redis", true
	}
	return "", false
}

func detectGreetingService(payload []byte) (string, bool) {
	line := payload
	if idx := bytes.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	lower := strings.ToLower(string(line))
	switch {
	case strings.Contains(lower, "ftp"):
		return "ftp", true
	case strings.Contains(lower, "smtp"):
		return "smtp", true
	}
	return "", false
}

// isTLSClientHello mirrors the check bpf/xdp_flow.c already does for the
// dedicated SNI capture, just without the dport==443 gate: TLS record type
// 0x16 (handshake) + handshake message type 0x01 (ClientHello) at offset 5.
func isTLSClientHello(payload []byte) bool {
	return len(payload) >= 6 && payload[0] == 0x16 && payload[5] == 0x01
}

// isRDPConnectionRequest matches the TPKT + X.224 Connection Request TPDU
// a client sends first: "03 00 <len:2> <li> E0 ...".
func isRDPConnectionRequest(payload []byte) bool {
	return len(payload) >= 6 && payload[0] == 0x03 && payload[1] == 0x00 && payload[5] == 0xE0
}

// isMySQLHandshake matches the server's initial handshake packet: a 4-byte
// packet header (3-byte length + 1-byte sequence id) followed by protocol
// version 0x0A and a null-terminated printable-ASCII server version string.
func isMySQLHandshake(payload []byte) bool {
	if len(payload) < 6 || payload[4] != 0x0A {
		return false
	}
	for i := 5; i < len(payload) && i < 5+32; i++ {
		if payload[i] == 0 {
			return i > 5
		}
		if payload[i] < 0x20 || payload[i] > 0x7e {
			return false
		}
	}
	return false
}

// isPostgresStartup matches the client's StartupMessage: 4-byte length
// followed by protocol version 3.0 (0x00030000).
func isPostgresStartup(payload []byte) bool {
	return len(payload) >= 8 && payload[4] == 0x00 && payload[5] == 0x03 && payload[6] == 0x00 && payload[7] == 0x00
}

// isMongoDBWireMessage checks the fixed 16-byte MongoDB wire protocol header
// for a known opCode: OP_MSG (2013) or the legacy OP_QUERY (2004).
func isMongoDBWireMessage(payload []byte) bool {
	if len(payload) < 16 {
		return false
	}
	opCode := binary.LittleEndian.Uint32(payload[12:16])
	return opCode == 2013 || opCode == 2004
}

// isRedisCommand matches a RESP-encoded command array, e.g. "*1\r\n$4\r\nPING\r\n".
func isRedisCommand(payload []byte) bool {
	if len(payload) < 4 || payload[0] != '*' || payload[1] < '0' || payload[1] > '9' {
		return false
	}
	idx := bytes.Index(payload, []byte("\r\n$"))
	return idx > 0 && idx < 12
}
