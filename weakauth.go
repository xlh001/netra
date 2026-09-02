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
	Username    string
	Password    string
	MatchedRule string
	Confidence  string
	StatusCode  int
}

type pendingHTTPAuthRequest struct {
	hasCred     bool
	username    string
	password    string
	matchedRule string
}

type httpAuthFlowState struct {
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
	out     chan WeakAuthFinding
	dropped uint64
}

func newWeakAuthManager(cfg *Config, dict *weakPasswordDict) *weakAuthManager {
	return &weakAuthManager{
		flows: map[xdpflowFlowKey]*httpAuthFlowState{},
		cfg:   cfg,
		dict:  dict,
		out:   make(chan WeakAuthFinding, 1024),
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
	sport := binary.LittleEndian.Uint16(raw[8:10])
	dport := binary.LittleEndian.Uint16(raw[10:12])
	payloadLen := binary.LittleEndian.Uint32(raw[12:16])
	truncated := raw[16] != 0

	payload := raw[httpAuthEventHeaderLen:]
	if int(payloadLen) <= len(payload) {
		payload = payload[:payloadLen]
	}
	if len(payload) == 0 {
		return
	}

	isRequest := dport == 80
	var canonicalKey xdpflowFlowKey
	if isRequest {
		canonicalKey = xdpflowFlowKey{Saddr: saddr, Daddr: daddr, Sport: sport, Dport: dport, Proto: tcpProto}
	} else {
		canonicalKey = xdpflowFlowKey{Saddr: daddr, Daddr: saddr, Sport: dport, Dport: sport, Proto: tcpProto}
	}

	m.mu.Lock()
	state, ok := m.flows[canonicalKey]
	if !ok {
		if len(m.flows) >= httpAuthMaxTrackedFlows {
			m.mu.Unlock()
			return
		}
		state = &httpAuthFlowState{}
		m.flows[canonicalKey] = state
	}
	state.seenAt = time.Now()

	if isRequest {
		state.reqBuf = append(state.reqBuf, payload...)
		if len(state.reqBuf) > httpAuthPerFlowBufCap {
			state.reqBuf = nil
		}

		parsed, remaining := parseHTTPAuthRequests(state.reqBuf, m.dict)
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

	responses, remaining := parseHTTPAuthResponses(state.respBuf)
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
			Username: req.username, Password: req.password, MatchedRule: req.matchedRule,
			Confidence: classifyConfidence(resp.statusCode, resp.hasCookie, resp.failureKeyword),
			StatusCode: resp.statusCode,
		})
	}
	m.mu.Unlock()

	for _, f := range toEmit {
		m.emit(f)
	}
}

func parseHTTPAuthRequests(buf []byte, dict *weakPasswordDict) ([]pendingHTTPAuthRequest, []byte) {
	var out []pendingHTTPAuthRequest
	pos := 0
	for {
		headerEnd := bytes.Index(buf[pos:], []byte("\r\n\r\n"))
		if headerEnd < 0 {
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
		out = append(out, pendingHTTPAuthRequest{hasCred: hasCred, username: username, password: password, matchedRule: rule})
		pos = msgEnd

		if len(out) >= httpAuthMaxPendingPerFlow*2 {
			break
		}
	}
	return out, append([]byte(nil), buf[pos:]...)
}

type parsedHTTPResponse struct {
	statusCode     int
	hasCookie      bool
	failureKeyword bool
}

func parseHTTPAuthResponses(buf []byte) ([]parsedHTTPResponse, []byte) {
	var out []parsedHTTPResponse
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

		contentLength, hasCL := parseContentLength(headerBlock)
		if hasCL && contentLength > httpAuthPerFlowBufCap {
			return out, nil
		}
		if !hasCL {
			out = append(out, parsedHTTPResponse{statusCode: statusCode, hasCookie: hasCookie, failureKeyword: failureKeyword})
			return out, nil
		}
		if len(buf)-bodyStart < contentLength {
			break
		}

		out = append(out, parsedHTTPResponse{statusCode: statusCode, hasCookie: hasCookie, failureKeyword: failureKeyword})
		pos = bodyStart + contentLength
	}
	return out, append([]byte(nil), buf[pos:]...)
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
