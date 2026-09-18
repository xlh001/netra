package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
)

const (
	httpAuthPerFlowBufCap      = 16 * 1024
	httpAuthFlowTTL            = 5 * time.Minute
	httpAuthMaxTrackedFlows    = 20000
	httpAuthMaxPendingPerFlow  = 8
	httpAuthBodyPreviewCap     = 2048
	httpAuthEventHeaderLen     = 20
	weakAuthMaxFindingsPerTick = 200
)

type WeakAuthFinding struct {
	Time        time.Time
	SrcIP       uint32
	SrcPort     uint16
	DstIP       uint32
	DstPort     uint16
	Proto       string
	Domain      string
	Username    string
	Password    string
	MatchedRule string
	Confidence  string
	StatusCode  int
}

type pendingHTTPAuthRequest struct {
	hasCred     bool
	domain      string
	username    string
	password    string
	matchedRule string
}

type httpAuthFlowState struct {
	proto   string
	reqBuf  []byte
	respBuf []byte
	pending []pendingHTTPAuthRequest
	seenAt  time.Time
}

type weakAuthManager struct {
	mu      sync.Mutex
	flows   map[xdpflowFlowKey]*httpAuthFlowState
	cfg     *Config
	dict    *weakPasswordDict
	flagMap *ebpf.Map
	out     chan WeakAuthFinding
	dropped uint64
}

func newWeakAuthManager(cfg *Config, dict *weakPasswordDict, flagMap *ebpf.Map) *weakAuthManager {
	return &weakAuthManager{
		flows:   map[xdpflowFlowKey]*httpAuthFlowState{},
		cfg:     cfg,
		dict:    dict,
		flagMap: flagMap,
		out:     make(chan WeakAuthFinding, 1024),
	}
}

var weakAuthPlainProtoServices = map[string]bool{"ftp": true, "pop3": true, "imap": true, "smtp": true}

func (m *weakAuthManager) onDPIService(key xdpflowFlowKey, service string) {
	if !m.cfg.Snapshot().WeakAuthEnabled {
		return
	}
	if !weakAuthPlainProtoServices[service] {
		return
	}

	rev := xdpflowFlowKey{Saddr: key.Daddr, Daddr: key.Saddr, Sport: key.Dport, Dport: key.Sport, Proto: key.Proto}

	m.mu.Lock()
	if len(m.flows) < httpAuthMaxTrackedFlows {
		if _, exists := m.flows[rev]; !exists {
			m.flows[rev] = &httpAuthFlowState{proto: service, seenAt: time.Now()}
		}
	}
	m.mu.Unlock()

	var flag uint8 = 1
	if err := m.flagMap.Put(key, flag); err != nil {
		log.Printf("weak auth: register flow flag failed: %v", err)
	}
	if err := m.flagMap.Put(rev, flag); err != nil {
		log.Printf("weak auth: register flow flag failed: %v", err)
	}
}

func (m *weakAuthManager) cleanup(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := now.Add(-httpAuthFlowTTL)
	for k, s := range m.flows {
		if s.seenAt.Before(cutoff) {
			delete(m.flows, k)
		}
	}
}

func (m *weakAuthManager) drain(max int) []WeakAuthFinding {
	var out []WeakAuthFinding
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

func (m *weakAuthManager) emit(r WeakAuthFinding) {
	select {
	case m.out <- r:
	default:
		m.mu.Lock()
		m.dropped++
		m.mu.Unlock()
	}
}

func (m *weakAuthManager) droppedCount() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dropped
}

func startWeakAuthReader(events *ebpf.Map, m *weakAuthManager) (*ringbuf.Reader, error) {
	reader, err := ringbuf.NewReader(events)
	if err != nil {
		return nil, err
	}
	go func() {
		defer recoverAndLog("weak auth reader")
		for {
			record, err := reader.Read()
			if err != nil {
				if !errors.Is(err, ringbuf.ErrClosed) {
					log.Printf("weak auth reader: unexpected error, capture stopped: %v", err)
				}
				return
			}
			handleWeakAuthEventSafely(record.RawSample, m)
		}
	}()
	return reader, nil
}

func handleWeakAuthEventSafely(raw []byte, m *weakAuthManager) {
	defer recoverAndLog("weak auth event handler")
	handleWeakAuthEvent(raw, m)
}

func handleWeakAuthEvent(raw []byte, m *weakAuthManager) {
	if !m.cfg.Snapshot().WeakAuthEnabled {
		return
	}
	if len(raw) < httpAuthEventHeaderLen {
		return
	}
	saddr := binary.LittleEndian.Uint32(raw[0:4])
	daddr := binary.LittleEndian.Uint32(raw[4:8])
	sport := binary.BigEndian.Uint16(raw[8:10])
	dport := binary.BigEndian.Uint16(raw[10:12])
	payloadLen := binary.LittleEndian.Uint32(raw[12:16])
	truncated := raw[16] != 0

	payload := raw[httpAuthEventHeaderLen:]
	if int(payloadLen) <= len(payload) {
		payload = payload[:payloadLen]
	}
	if len(payload) == 0 {
		return
	}

	packetKey := xdpflowFlowKey{Saddr: saddr, Daddr: daddr, Sport: sport, Dport: dport, Proto: tcpProto}
	reverseKey := xdpflowFlowKey{Saddr: daddr, Daddr: saddr, Sport: dport, Dport: sport, Proto: tcpProto}

	m.mu.Lock()
	var canonicalKey xdpflowFlowKey
	var isRequest bool
	var state *httpAuthFlowState
	if s, ok := m.flows[packetKey]; ok {
		state, canonicalKey, isRequest = s, packetKey, true
	} else if s, ok := m.flows[reverseKey]; ok {
		state, canonicalKey, isRequest = s, reverseKey, false
	} else {
		isRequest = dport == 80
		if isRequest {
			canonicalKey = packetKey
		} else {
			canonicalKey = reverseKey
		}
		if len(m.flows) >= httpAuthMaxTrackedFlows {
			m.mu.Unlock()
			return
		}
		state = &httpAuthFlowState{proto: "http"}
		m.flows[canonicalKey] = state
	}
	state.seenAt = time.Now()

	if isRequest {
		state.reqBuf = append(state.reqBuf, payload...)
		if len(state.reqBuf) > httpAuthPerFlowBufCap {
			state.reqBuf = nil
		}

		parsed, remaining := parseAuthRequests(state.proto, state.reqBuf, m.dict, truncated)
		if truncated && len(remaining) > 0 {
			remaining = nil
		}
		state.reqBuf = remaining
		for _, p := range parsed {
			state.pending = append(state.pending, p)
			if len(state.pending) > httpAuthMaxPendingPerFlow {
				state.pending = state.pending[1:]
			}
		}
		m.mu.Unlock()
		return
	}

	state.respBuf = append(state.respBuf, payload...)
	if len(state.respBuf) > httpAuthPerFlowBufCap {
		state.respBuf = nil
	}

	responses, remaining := parseAuthResponses(state.proto, state.respBuf)
	if truncated && len(remaining) > 0 {
		remaining = nil
	}
	state.respBuf = remaining

	var toEmit []WeakAuthFinding
	for _, resp := range responses {
		if len(state.pending) == 0 {
			continue
		}
		req := state.pending[0]
		state.pending = state.pending[1:]
		if !req.hasCred {
			continue
		}
		toEmit = append(toEmit, WeakAuthFinding{
			Time: time.Now(), SrcIP: canonicalKey.Saddr, SrcPort: canonicalKey.Sport,
			DstIP: canonicalKey.Daddr, DstPort: canonicalKey.Dport,
			Proto:    state.proto,
			Domain:   req.domain,
			Username: req.username, Password: req.password, MatchedRule: req.matchedRule,
			Confidence: resp.confidence,
			StatusCode: resp.statusCode,
		})
	}
	m.mu.Unlock()

	for _, f := range toEmit {
		m.emit(f)
	}
}

func parseAuthRequests(proto string, buf []byte, dict *weakPasswordDict, finalChunk bool) ([]pendingHTTPAuthRequest, []byte) {
	switch proto {
	case "ftp", "pop3":
		return parseUserPassRequests(buf, dict)
	case "imap":
		return parseIMAPRequests(buf, dict)
	case "smtp":
		return parseSMTPRequests(buf, dict)
	default:
		return parseHTTPAuthRequests(buf, dict, finalChunk)
	}
}

func parseAuthResponses(proto string, buf []byte) ([]parsedAuthResponse, []byte) {
	switch proto {
	case "ftp":
		return parseFTPResponses(buf)
	case "pop3":
		return parsePOP3Responses(buf)
	case "imap":
		return parseIMAPResponses(buf)
	case "smtp":
		return parseSMTPResponses(buf)
	default:
		return parseHTTPAuthResponses(buf)
	}
}

func parseHTTPAuthRequests(buf []byte, dict *weakPasswordDict, finalChunk bool) ([]pendingHTTPAuthRequest, []byte) {
	var out []pendingHTTPAuthRequest
	pos := 0
	for {
		headerEnd := bytes.Index(buf[pos:], []byte("\r\n\r\n"))
		if headerEnd < 0 {
			// No complete header block. Previously, on the final (truncated)
			// chunk we still tried to pull credentials out of this partial
			// buffer -- but a snapped request can carry a cut-off password
			// (a strong "nuR..." seen as just "nuR"), which then trips the
			// weak-password heuristics (too_short / all_lowercase) and fires a
			// false positive. Incomplete data is unreliable, so drop it.
			break
		}
		headerBlock := buf[pos : pos+headerEnd]
		bodyStart := pos + headerEnd + 4

		contentLength, hasCL := parseContentLength(headerBlock)
		if hasCL && contentLength > httpAuthPerFlowBufCap {
			return out, nil
		}

		var body []byte
		msgEnd := bodyStart
		if hasCL {
			if len(buf)-bodyStart < contentLength {
				break
			}
			body = buf[bodyStart : bodyStart+contentLength]
			msgEnd = bodyStart + contentLength
		}

		username, password, rule, hasCred := extractHTTPCredentials(headerBlock, body, dict)
		domain := extractRequestDomain(headerBlock)
		out = append(out, pendingHTTPAuthRequest{hasCred: hasCred, domain: domain, username: username, password: password, matchedRule: rule})
		pos = msgEnd

		if len(out) >= httpAuthMaxPendingPerFlow*2 {
			break
		}
	}
	return out, append([]byte(nil), buf[pos:]...)
}

func extractRequestDomain(headerBlock []byte) string {
	host := extractHeader(headerBlock, "Host")
	if host == "" || !utf8.ValidString(host) || !looksLikeHostname(host) || isIPLiteral(host) {
		return ""
	}
	return host
}

type parsedAuthResponse struct {
	statusCode int
	confidence string
}

func parseHTTPAuthResponses(buf []byte) ([]parsedAuthResponse, []byte) {
	var out []parsedAuthResponse
	pos := 0
	for {
		headerEnd := bytes.Index(buf[pos:], []byte("\r\n\r\n"))
		if headerEnd < 0 {
			break
		}
		headerBlock := buf[pos : pos+headerEnd]
		bodyStart := pos + headerEnd + 4

		statusCode := parseStatusCode(headerBlock)
		hasCookie := extractHeader(headerBlock, "Set-Cookie") != ""

		previewEnd := bodyStart + httpAuthBodyPreviewCap
		if previewEnd > len(buf) {
			previewEnd = len(buf)
		}
		failureKeyword := containsFailureKeyword(buf[bodyStart:previewEnd])
		out = append(out, parsedAuthResponse{statusCode: statusCode, confidence: classifyConfidence(statusCode, hasCookie, failureKeyword)})

		contentLength, hasCL := parseContentLength(headerBlock)
		if hasCL && contentLength <= httpAuthPerFlowBufCap && len(buf)-bodyStart >= contentLength {
			pos = bodyStart + contentLength
			continue
		}
		pos = len(buf)
		break
	}
	return out, append([]byte(nil), buf[pos:]...)
}

func splitCRLFLines(buf []byte) ([][]byte, int) {
	var lines [][]byte
	pos := 0
	for {
		idx := bytes.Index(buf[pos:], []byte("\r\n"))
		if idx < 0 {
			break
		}
		lines = append(lines, buf[pos:pos+idx])
		pos += idx + 2
	}
	return lines, pos
}

func parseUserPassRequests(buf []byte, dict *weakPasswordDict) ([]pendingHTTPAuthRequest, []byte) {
	lines, consumed := splitCRLFLines(buf)
	var out []pendingHTTPAuthRequest
	var pendingUser string
	for _, line := range lines {
		s := string(line)
		switch {
		case len(s) >= 5 && strings.EqualFold(s[:5], "USER "):
			pendingUser = strings.TrimSpace(s[5:])
		case len(s) >= 5 && strings.EqualFold(s[:5], "PASS "):
			pwd := strings.TrimSpace(s[5:])
			if pendingUser != "" {
				rule, weak := dict.checkWithUsername(pendingUser, pwd)
				out = append(out, pendingHTTPAuthRequest{hasCred: weak, username: pendingUser, password: pwd, matchedRule: rule})
			}
			pendingUser = ""
		}
	}
	return out, append([]byte(nil), buf[consumed:]...)
}

func parseFTPResponses(buf []byte) ([]parsedAuthResponse, []byte) {
	lines, consumed := splitCRLFLines(buf)
	var out []parsedAuthResponse
	for _, line := range lines {
		s := string(line)
		if len(s) < 3 {
			continue
		}
		code, err := strconv.Atoi(s[:3])
		if err != nil {
			continue
		}
		switch code {
		case 230:
			out = append(out, parsedAuthResponse{statusCode: code, confidence: "medium"})
		case 530:
			out = append(out, parsedAuthResponse{statusCode: code, confidence: "low"})
		}
	}
	return out, append([]byte(nil), buf[consumed:]...)
}

func parsePOP3Responses(buf []byte) ([]parsedAuthResponse, []byte) {
	lines, consumed := splitCRLFLines(buf)
	var out []parsedAuthResponse
	for _, line := range lines {
		s := string(line)
		switch {
		case strings.HasPrefix(s, "-ERR"):
			out = append(out, parsedAuthResponse{confidence: "low"})
		case strings.HasPrefix(s, "+OK"):
			lower := strings.ToLower(s)
			if strings.Contains(lower, "user") || strings.Contains(lower, "pass") {
				continue
			}
			out = append(out, parsedAuthResponse{confidence: "medium"})
		}
	}
	return out, append([]byte(nil), buf[consumed:]...)
}

func imapSplitFields(line string) []string {
	var fields []string
	var cur strings.Builder
	inQuotes := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			inQuotes = !inQuotes
			cur.WriteByte(c)
		case c == ' ' && !inQuotes:
			if cur.Len() > 0 {
				fields = append(fields, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		fields = append(fields, cur.String())
	}
	return fields
}

func imapUnquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func parseIMAPRequests(buf []byte, dict *weakPasswordDict) ([]pendingHTTPAuthRequest, []byte) {
	lines, consumed := splitCRLFLines(buf)
	var out []pendingHTTPAuthRequest
	for _, line := range lines {
		fields := imapSplitFields(string(line))
		if len(fields) < 4 || !strings.EqualFold(fields[1], "LOGIN") {
			continue
		}
		username := imapUnquote(fields[2])
		password := imapUnquote(fields[3])
		if username == "" || password == "" {
			continue
		}
		rule, weak := dict.checkWithUsername(username, password)
		out = append(out, pendingHTTPAuthRequest{hasCred: weak, username: username, password: password, matchedRule: rule})
	}
	return out, append([]byte(nil), buf[consumed:]...)
}

func parseIMAPResponses(buf []byte) ([]parsedAuthResponse, []byte) {
	lines, consumed := splitCRLFLines(buf)
	var out []parsedAuthResponse
	for _, line := range lines {
		fields := imapSplitFields(string(line))
		if len(fields) < 2 {
			continue
		}
		switch strings.ToUpper(fields[1]) {
		case "OK":
			out = append(out, parsedAuthResponse{confidence: "medium"})
		case "NO", "BAD":
			out = append(out, parsedAuthResponse{confidence: "low"})
		}
	}
	return out, append([]byte(nil), buf[consumed:]...)
}

func decodeSMTPPlain(b64 string) (username, password string, ok bool) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", "", false
	}
	parts := bytes.Split(raw, []byte{0})
	if len(parts) != 3 {
		return "", "", false
	}
	return string(parts[1]), string(parts[2]), true
}

func parseSMTPRequests(buf []byte, dict *weakPasswordDict) ([]pendingHTTPAuthRequest, []byte) {
	lines, consumed := splitCRLFLines(buf)
	var out []pendingHTTPAuthRequest
	for _, line := range lines {
		s := strings.TrimSpace(string(line))
		upper := strings.ToUpper(s)
		if !strings.HasPrefix(upper, "AUTH PLAIN ") {
			continue
		}
		u, p, ok := decodeSMTPPlain(s[len("AUTH PLAIN "):])
		if !ok {
			continue
		}
		rule, weak := dict.checkWithUsername(u, p)
		out = append(out, pendingHTTPAuthRequest{hasCred: weak, username: u, password: p, matchedRule: rule})
	}
	return out, append([]byte(nil), buf[consumed:]...)
}

func parseSMTPResponses(buf []byte) ([]parsedAuthResponse, []byte) {
	lines, consumed := splitCRLFLines(buf)
	var out []parsedAuthResponse
	for _, line := range lines {
		s := string(line)
		if len(s) < 3 {
			continue
		}
		code, err := strconv.Atoi(s[:3])
		if err != nil {
			continue
		}
		switch code {
		case 235:
			out = append(out, parsedAuthResponse{statusCode: code, confidence: "medium"})
		case 535, 534:
			out = append(out, parsedAuthResponse{statusCode: code, confidence: "low"})
		}
	}
	return out, append([]byte(nil), buf[consumed:]...)
}

func parseStatusCode(headerBlock []byte) int {
	line := headerBlock
	if idx := bytes.IndexByte(headerBlock, '\n'); idx >= 0 {
		line = headerBlock[:idx]
	}
	parts := strings.Fields(string(line))
	if len(parts) < 2 {
		return 0
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	return code
}

var httpAuthFailureKeywords = []string{
	"invalid", "incorrect", "denied", "unauthorized", "authentication failed", "login failed",
	"密码错误", "账号或密码错误", "用户名或密码错误", "登录失败", "认证失败", "账号不存在",
}

func containsFailureKeyword(body []byte) bool {
	lower := strings.ToLower(string(body))
	for _, kw := range httpAuthFailureKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func classifyConfidence(statusCode int, hasCookie, failureKeyword bool) string {
	if failureKeyword || statusCode == 401 || statusCode == 403 {
		return "low"
	}
	successish := statusCode == 200 || statusCode == 302 || statusCode == 303
	if hasCookie && successish {
		return "high"
	}
	if successish {
		return "medium"
	}
	return "low"
}

func parseContentLength(headerBlock []byte) (int, bool) {
	v := extractHeader(headerBlock, "Content-Length")
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func extractHeader(headerBlock []byte, name string) string {
	for _, line := range strings.Split(string(headerBlock), "\r\n") {
		idx := strings.IndexByte(line, ':')
		if idx <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(line[:idx]), name) {
			return strings.TrimSpace(line[idx+1:])
		}
	}
	return ""
}

var httpAuthUsernameFields = map[string]bool{"username": true, "user": true, "login": true, "uid": true, "email": true, "account": true}
var httpAuthPasswordFields = map[string]bool{"password": true, "pass": true, "pwd": true, "passwd": true}

func extractHTTPCredentials(headerBlock, body []byte, dict *weakPasswordDict) (username, password, matchedRule string, hasCred bool) {
	if u, p, ok := extractBasicAuth(headerBlock); ok {
		if rule, weak := dict.checkWithUsername(u, p); weak {
			return u, p, rule, true
		}
		return "", "", "", false
	}

	contentType := strings.ToLower(extractHeader(headerBlock, "Content-Type"))
	var u, p string
	var ok bool
	switch {
	case strings.Contains(contentType, "application/x-www-form-urlencoded"):
		u, p, ok = extractFormCredentials(body)
	case strings.Contains(contentType, "application/json"):
		u, p, ok = extractJSONCredentials(body)
	}
	if !ok {
		return "", "", "", false
	}
	if rule, weak := dict.checkWithUsername(u, p); weak {
		return u, p, rule, true
	}
	return "", "", "", false
}

func extractBasicAuth(headerBlock []byte) (string, string, bool) {
	auth := extractHeader(headerBlock, "Authorization")
	const prefix = "Basic "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(auth[len(prefix):]))
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func extractFormCredentials(body []byte) (string, string, bool) {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return "", "", false
	}
	var username, password string
	for key, vals := range values {
		if len(vals) == 0 {
			continue
		}
		lk := strings.ToLower(key)
		if httpAuthUsernameFields[lk] && username == "" {
			username = vals[0]
		}
		if httpAuthPasswordFields[lk] && password == "" {
			password = vals[0]
		}
	}
	if username == "" || password == "" {
		return "", "", false
	}
	return username, password, true
}

func extractJSONCredentials(body []byte) (string, string, bool) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", "", false
	}
	var username, password string
	for key, val := range raw {
		s, ok := val.(string)
		if !ok {
			continue
		}
		lk := strings.ToLower(key)
		if httpAuthUsernameFields[lk] && username == "" {
			username = s
		}
		if httpAuthPasswordFields[lk] && password == "" {
			password = s
		}
	}
	if username == "" || password == "" {
		return "", "", false
	}
	return username, password, true
}
