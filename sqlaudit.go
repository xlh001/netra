package main

import (
	"encoding/binary"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	sqlAuditPerFlowBufCap   = 16 * 1024
	sqlAuditQueryTextCap    = 4000
	sqlAuditFlowTTL         = 30 * time.Minute
	sqlAuditMaxTrackedFlows = 20000
	tcpProto                = 6
)

type SQLAuditRecord struct {
	Time      time.Time
	DBType    string
	SrcIP     uint32
	SrcPort   uint16
	DstIP     uint32
	DstPort   uint16
	QueryText string
	Truncated bool
}

type sqlAuditFlowState struct {
	dbType string
	buf    []byte
	seenAt time.Time
}

type sqlAuditManager struct {
	mu      sync.Mutex
	flows   map[xdpflowFlowKey]*sqlAuditFlowState
	flagMap *ebpf.Map
	cfg     *Config
	out     chan SQLAuditRecord
	dropped uint64
}

func newSQLAuditManager(cfg *Config, flagMap *ebpf.Map) *sqlAuditManager {
	return &sqlAuditManager{
		flows:   map[xdpflowFlowKey]*sqlAuditFlowState{},
		flagMap: flagMap,
		cfg:     cfg,
		out:     make(chan SQLAuditRecord, 4096),
	}
}

func (m *sqlAuditManager) onDPIService(key xdpflowFlowKey, service string) {
	if !m.cfg.Snapshot().SQLAuditEnabled {
		return
	}
	var dbType string
	registerBothDirections := false
	switch service {
	case "mysql":
		dbType = "mysql"
	case "mongodb":
		dbType = "mongodb"
		registerBothDirections = true
	default:
		return
	}

	rev := xdpflowFlowKey{Saddr: key.Daddr, Daddr: key.Saddr, Sport: key.Dport, Dport: key.Sport, Proto: key.Proto}
	targets := []xdpflowFlowKey{rev}
	if registerBothDirections {
		targets = append(targets, key)
	}

	m.mu.Lock()
	if len(m.flows) < sqlAuditMaxTrackedFlows {
		now := time.Now()
		for _, k := range targets {
			m.flows[k] = &sqlAuditFlowState{dbType: dbType, seenAt: now}
		}
	}
	m.mu.Unlock()

	var flag uint8 = 1
	for _, k := range targets {
		if err := m.flagMap.Put(k, flag); err != nil {
			log.Printf("sql audit: register flow flag failed: %v", err)
		}
	}
}

func (m *sqlAuditManager) cleanup(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := now.Add(-sqlAuditFlowTTL)
	for k, s := range m.flows {
		if s.seenAt.Before(cutoff) {
			delete(m.flows, k)
		}
	}
}

func (m *sqlAuditManager) drain(max int) []SQLAuditRecord {
	var out []SQLAuditRecord
	for len(out) < max {
		select {
		case r := <-m.out:
			out = append(out, r)
		default:
			return out
		}
	}
	return out
}

func (m *sqlAuditManager) emit(r SQLAuditRecord) {
	select {
	case m.out <- r:
	default:
		m.mu.Lock()
		m.dropped++
		m.mu.Unlock()
	}
}

func (m *sqlAuditManager) droppedCount() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dropped
}

const sqlAuditEventHeaderLen = 20

func startSQLAuditReader(events *ebpf.Map, m *sqlAuditManager) (*ringbuf.Reader, error) {
	reader, err := ringbuf.NewReader(events)
	if err != nil {
		return nil, err
	}
	go func() {
		defer recoverAndLog("sql audit reader")
		for {
			record, err := reader.Read()
			if err != nil {
				if !errors.Is(err, ringbuf.ErrClosed) {
					log.Printf("sql audit reader: unexpected error, capture stopped: %v", err)
				}
				return
			}
			handleSQLAuditEventSafely(record.RawSample, m)
		}
	}()
	return reader, nil
}

func handleSQLAuditEventSafely(raw []byte, m *sqlAuditManager) {
	defer recoverAndLog("sql audit event handler")
	handleSQLAuditEvent(raw, m)
}

func handleSQLAuditEvent(raw []byte, m *sqlAuditManager) {
	if len(raw) < sqlAuditEventHeaderLen {
		return
	}
	saddr := binary.LittleEndian.Uint32(raw[0:4])
	daddr := binary.LittleEndian.Uint32(raw[4:8])
	sport := binary.LittleEndian.Uint16(raw[8:10])
	dport := binary.LittleEndian.Uint16(raw[10:12])
	payloadLen := binary.LittleEndian.Uint32(raw[12:16])
	truncated := raw[16] != 0

	payload := raw[sqlAuditEventHeaderLen:]
	if int(payloadLen) <= len(payload) {
		payload = payload[:payloadLen]
	}
	if len(payload) == 0 {
		return
	}

	key := xdpflowFlowKey{Saddr: saddr, Daddr: daddr, Sport: sport, Dport: dport, Proto: tcpProto}

	m.mu.Lock()
	state, ok := m.flows[key]
	if !ok {
		m.mu.Unlock()
		return
	}
	state.seenAt = time.Now()
	state.buf = append(state.buf, payload...)
	if len(state.buf) > sqlAuditPerFlowBufCap {
		state.buf = state.buf[:0]
		m.mu.Unlock()
		return
	}
	dbType := state.dbType
	buf := state.buf
	m.mu.Unlock()

	var records []SQLAuditRecord
	var remaining []byte
	switch dbType {
	case "mysql":
		records, remaining = parseMySQLQueries(buf, saddr, sport, daddr, dport)
	case "mongodb":
		records, remaining = parseMongoMessages(buf, saddr, sport, daddr, dport)
	default:
		return
	}

	if truncated && len(remaining) > 0 {
		remaining = nil
	}

	m.mu.Lock()
	if s, ok := m.flows[key]; ok {
		s.buf = remaining
	}
	m.mu.Unlock()

	for _, r := range records {
		m.emit(r)
	}
}

func parseMySQLQueries(buf []byte, saddr uint32, sport uint16, daddr uint32, dport uint16) ([]SQLAuditRecord, []byte) {
	var records []SQLAuditRecord
	pos := 0
	for {
		if len(buf)-pos < 4 {
			break
		}
		pktLen := int(buf[pos]) | int(buf[pos+1])<<8 | int(buf[pos+2])<<16
		if pktLen < 0 || pktLen > sqlAuditPerFlowBufCap {
			return records, nil
		}
		if len(buf)-pos < 4+pktLen {
			break
		}
		body := buf[pos+4 : pos+4+pktLen]
		pos += 4 + pktLen

		if len(body) >= 1 && (body[0] == 0x03 || body[0] == 0x16) {
			raw := normalizeText(string(body[1:]))
			if looksLikeSetCommand(raw) {
				continue
			}
			text, truncated := capText(raw, sqlAuditQueryTextCap)
			records = append(records, SQLAuditRecord{
				Time: time.Now(), DBType: "mysql",
				SrcIP: saddr, SrcPort: sport, DstIP: daddr, DstPort: dport,
				QueryText: text, Truncated: truncated,
			})
		}
	}
	return records, append([]byte(nil), buf[pos:]...)
}

var mongoCommandNames = map[string]bool{
	"find": true, "insert": true, "update": true, "delete": true,
	"aggregate": true, "count": true, "distinct": true, "findandmodify": true,
	"getmore": true, "killcursors": true, "hello": true, "ismaster": true,
	"ping": true, "listcollections": true, "listindexes": true,
	"createindexes": true, "dropindexes": true, "drop": true, "create": true,
	"collstats": true, "dbstats": true, "explain": true, "validate": true,
}

func parseMongoMessages(buf []byte, saddr uint32, sport uint16, daddr uint32, dport uint16) ([]SQLAuditRecord, []byte) {
	var records []SQLAuditRecord
	pos := 0
	for {
		if len(buf)-pos < 16 {
			break
		}
		msgLen := int(int32(binary.LittleEndian.Uint32(buf[pos : pos+4])))
		if msgLen < 16 || msgLen > sqlAuditPerFlowBufCap {
			return records, nil
		}
		if len(buf)-pos < msgLen {
			break
		}
		msg := buf[pos : pos+msgLen]
		pos += msgLen

		opCode := int32(binary.LittleEndian.Uint32(msg[12:16]))
		body := msg[16:]
		switch opCode {
		case 2013:
			if rec, ok := parseMongoOpMsg(body, saddr, sport, daddr, dport); ok {
				records = append(records, rec)
			}
		case 2004:
			if rec, ok := parseMongoOpQuery(body, saddr, sport, daddr, dport); ok {
				records = append(records, rec)
			}
		}
	}
	return records, append([]byte(nil), buf[pos:]...)
}

func parseMongoOpMsg(body []byte, saddr uint32, sport uint16, daddr uint32, dport uint16) (SQLAuditRecord, bool) {
	if len(body) < 4 {
		return SQLAuditRecord{}, false
	}
	rest := body[4:]
	for len(rest) > 0 {
		kind := rest[0]
		rest = rest[1:]
		switch kind {
		case 0:
			doc, n, err := decodeBSONDocument(rest)
			if err != nil {
				return SQLAuditRecord{}, false
			}
			rest = rest[n:]
			if !mongoCommandNames[strings.ToLower(doc.firstKey())] {
				continue
			}
			text, truncated := capText(bsonDocToJSON(doc), sqlAuditQueryTextCap)
			return SQLAuditRecord{
				Time: time.Now(), DBType: "mongodb",
				SrcIP: saddr, SrcPort: sport, DstIP: daddr, DstPort: dport,
				QueryText: text, Truncated: truncated,
			}, true
		case 1:
			if len(rest) < 4 {
				return SQLAuditRecord{}, false
			}
			sectionSize := int(int32(binary.LittleEndian.Uint32(rest[:4])))
			if sectionSize < 4 || sectionSize > len(rest) {
				return SQLAuditRecord{}, false
			}
			rest = rest[sectionSize:]
		default:
			return SQLAuditRecord{}, false
		}
	}
	return SQLAuditRecord{}, false
}

func parseMongoOpQuery(body []byte, saddr uint32, sport uint16, daddr uint32, dport uint16) (SQLAuditRecord, bool) {
	if len(body) < 4 {
		return SQLAuditRecord{}, false
	}
	rest := body[4:]
	nameEnd := indexZeroByte(rest)
	if nameEnd < 0 {
		return SQLAuditRecord{}, false
	}
	collName := string(rest[:nameEnd])
	rest = rest[nameEnd+1:]
	if len(rest) < 8 {
		return SQLAuditRecord{}, false
	}
	rest = rest[8:]
	doc, _, err := decodeBSONDocument(rest)
	if err != nil {
		return SQLAuditRecord{}, false
	}
	wrapped := bsonDoc{{Name: "collection", Value: collName}, {Name: "query", Value: doc}}
	text, truncated := capText(bsonDocToJSON(wrapped), sqlAuditQueryTextCap)
	return SQLAuditRecord{
		Time: time.Now(), DBType: "mongodb",
		SrcIP: saddr, SrcPort: sport, DstIP: daddr, DstPort: dport,
		QueryText: text, Truncated: truncated,
	}, true
}

func normalizeText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	if decoded, err := simplifiedchinese.GBK.NewDecoder().String(s); err == nil && utf8.ValidString(decoded) {
		return decoded
	}
	return s
}

func looksLikeSetCommand(s string) bool {
	trimmed := strings.TrimSpace(s)
	return len(trimmed) >= 4 && strings.EqualFold(trimmed[:4], "SET ")
}

func capText(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	return s[:max], true
}
