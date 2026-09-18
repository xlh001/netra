package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"strings"
	"unicode/utf8"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
)

const httpEventHeaderLen = 16

func startHTTPReader(m *ebpf.Map, onDomain func(key xdpflowFlowKey, hostname string), onServerFingerprint func(saddr uint32, sport uint16, value string)) (*ringbuf.Reader, error) {
	reader, err := ringbuf.NewReader(m)
	if err != nil {
		return nil, err
	}

	go func() {
		defer recoverAndLog("http reader")
		for {
			record, err := reader.Read()
			if err != nil {
				if !errors.Is(err, ringbuf.ErrClosed) {

					log.Printf("http reader: unexpected error, HTTP host capture has stopped: %v", err)
				}
				return
			}
			handleHTTPEventSafely(record.RawSample, onDomain, onServerFingerprint)
		}
	}()

	return reader, nil
}

func handleHTTPEventSafely(raw []byte, onDomain func(key xdpflowFlowKey, hostname string), onServerFingerprint func(saddr uint32, sport uint16, value string)) {
	defer recoverAndLog("http event handler")
	handleHTTPEvent(raw, onDomain, onServerFingerprint)
}

func handleHTTPEvent(raw []byte, onDomain func(key xdpflowFlowKey, hostname string), onServerFingerprint func(saddr uint32, sport uint16, value string)) {
	if len(raw) < httpEventHeaderLen {
		return
	}
	saddr := binary.LittleEndian.Uint32(raw[0:4])
	daddr := binary.LittleEndian.Uint32(raw[4:8])
	sport := binary.LittleEndian.Uint16(raw[8:10])
	dport := binary.LittleEndian.Uint16(raw[10:12])
	payloadLen := binary.LittleEndian.Uint32(raw[12:16])

	payload := raw[httpEventHeaderLen:]
	if int(payloadLen) <= len(payload) {
		payload = payload[:payloadLen]
	}

	if hostname, ok := parseHTTPHostHeader(payload); ok && hostname != "" {
		key := xdpflowFlowKey{Saddr: saddr, Daddr: daddr, Sport: sport, Dport: dport, Proto: tcpProto}
		onDomain(key, hostname)
	}

	if bytes.HasPrefix(payload, []byte("HTTP/")) {
		if value := extractHeader(payload, "Server"); value != "" {
			realSport := binary.BigEndian.Uint16(raw[8:10])
			onServerFingerprint(saddr, realSport, value)
		}
	}
}

func parseHTTPHostHeader(b []byte) (string, bool) {
	text := string(b)

	idx := strings.IndexAny(text, "\r\n")
	if idx < 0 {
		return "", false
	}

	const prefix = "host:"
	for _, line := range strings.Split(text[idx:], "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if len(line) > len(prefix) && strings.EqualFold(line[:len(prefix)], prefix) {
			host := strings.TrimSpace(line[len(prefix):])
			if !utf8.ValidString(host) || !looksLikeHostname(host) {
				return "", false
			}
			if isIPLiteral(host) {

				return "", false
			}
			return host, true
		}
	}

	return "", false
}

func looksLikeHostname(h string) bool {
	if len(h) < 2 {
		return false
	}
	hasLetterOrDigit := false
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z':
			hasLetterOrDigit = true
		case r >= 'A' && r <= 'Z':
			hasLetterOrDigit = true
		case r >= '0' && r <= '9':
			hasLetterOrDigit = true
		case r == '.' || r == '-' || r == ':' || r == '[' || r == ']':
		default:
			return false
		}
	}
	return hasLetterOrDigit
}

func isIPLiteral(host string) bool {
	h := host
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		h = hostOnly
	} else if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		h = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	return net.ParseIP(h) != nil
}
