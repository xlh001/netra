package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpCallTimeout = 15 * time.Second
const mcpHealthCheckInterval = 1 * time.Minute

type mcpConnStatus struct {
	Connected bool
	Error     string
	ToolCount int
	CheckedAt time.Time
}

type mcpConnection struct {
	record  MCPServerRecord
	session *mcp.ClientSession
	tools   []*mcp.Tool
}

type mcpManager struct {
	mu sync.RWMutex
	// records holds every currently-enabled server's config, regardless of
	// whether its connection is currently up -- the health-check loop uses it
	// to retry servers that failed to connect without needing the DB.
	records map[int]MCPServerRecord
	conns   map[int]*mcpConnection
	status  map[int]mcpConnStatus
}

func newMCPManager() *mcpManager {
	return &mcpManager{records: map[int]MCPServerRecord{}, conns: map[int]*mcpConnection{}, status: map[int]mcpConnStatus{}}
}

func (m *mcpManager) connectedCounts() (connected, total int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total = len(m.records)
	for _, st := range m.status {
		if st.Connected {
			connected++
		}
	}
	return connected, total
}

// authRoundTripper injects a static Authorization header into every outgoing
// request, used to apply an MCP server's configured Basic/Bearer credentials.
type authRoundTripper struct {
	header string
}

func (rt *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", rt.header)
	return http.DefaultTransport.RoundTrip(req)
}

func mcpHTTPClient(rec MCPServerRecord) *http.Client {
	switch rec.AuthType {
	case "basic":
		token := base64.StdEncoding.EncodeToString([]byte(rec.AuthUsername + ":" + rec.AuthPassword))
		return &http.Client{Transport: &authRoundTripper{header: "Basic " + token}}
	case "bearer":
		return &http.Client{Transport: &authRoundTripper{header: "Bearer " + rec.AuthToken}}
	default:
		return nil
	}
}

func mcpTransport(rec MCPServerRecord) (mcp.Transport, error) {
	switch rec.Transport {
	case "http":
		if rec.Endpoint == "" {
			return nil, fmt.Errorf("endpoint is required for http transport")
		}
		// DisableStandaloneSSE: Netra only ever initiates tool discovery/calls
		// itself and never needs the server to push unprompted messages, so we
		// skip the optional GET-based standalone SSE stream entirely. Several
		// real-world MCP servers implement only the POST request/response path
		// and hang indefinitely on that GET instead of the spec-mandated 405,
		// which otherwise blocks Connect() until mcpCallTimeout expires.
		return &mcp.StreamableClientTransport{Endpoint: rec.Endpoint, HTTPClient: mcpHTTPClient(rec), DisableStandaloneSSE: true}, nil
	case "stdio":
		if rec.Command == "" {
			return nil, fmt.Errorf("command is required for stdio transport")
		}
		var args []string
		if rec.Args != "" {
			if err := json.Unmarshal([]byte(rec.Args), &args); err != nil {
				return nil, fmt.Errorf("invalid args (expected JSON array): %w", err)
			}
		}
		return &mcp.CommandTransport{Command: exec.Command(rec.Command, args...)}, nil
	default:
		return nil, fmt.Errorf("unknown transport %q (must be http or stdio)", rec.Transport)
	}
}

func mcpConnect(ctx context.Context, rec MCPServerRecord) (*mcp.ClientSession, []*mcp.Tool, error) {
	transport, err := mcpTransport(rec)
	if err != nil {
		return nil, nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "netra", Version: "1.0.0"}, nil)
	connectCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()
	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	listCtx, cancel2 := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel2()
	res, err := session.ListTools(listCtx, nil)
	if err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("list tools: %w", err)
	}
	return session, res.Tools, nil
}

// testMCPConnection connects, lists tools, and immediately disconnects -- used
// by the "test connection" admin action, independent of the live manager state.
func testMCPConnection(rec MCPServerRecord) ([]string, error) {
	ctx := context.Background()
	session, tools, err := mcpConnect(ctx, rec)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names, nil
}

// sync connects to every enabled server and closes connections for servers
// that are no longer present or no longer enabled. Failures to connect are
// logged and skipped -- one broken external server should not affect the rest.
func (m *mcpManager) sync(records []MCPServerRecord) {
	wanted := map[int]MCPServerRecord{}
	for _, r := range records {
		if r.Enabled {
			wanted[r.ID] = r
		}
	}

	m.mu.Lock()
	for id, conn := range m.conns {
		if _, ok := wanted[id]; !ok {
			conn.session.Close()
			delete(m.conns, id)
			delete(m.status, id)
		}
	}
	for id := range m.records {
		if _, ok := wanted[id]; !ok {
			delete(m.records, id)
		}
	}
	for id, rec := range wanted {
		m.records[id] = rec
	}
	m.mu.Unlock()

	for id, rec := range wanted {
		session, tools, err := mcpConnect(context.Background(), rec)
		if err != nil {
			log.Printf("mcp: connect to %q failed, skipping: %v", rec.Name, err)
			m.mu.Lock()
			m.status[id] = mcpConnStatus{Connected: false, Error: err.Error(), CheckedAt: time.Now()}
			m.mu.Unlock()
			continue
		}
		m.mu.Lock()
		if old, ok := m.conns[id]; ok {
			old.session.Close()
		}
		m.conns[id] = &mcpConnection{record: rec, session: session, tools: tools}
		m.status[id] = mcpConnStatus{Connected: true, ToolCount: len(tools), CheckedAt: time.Now()}
		m.mu.Unlock()
		log.Printf("mcp: connected to %q (%s), %d tool(s) available", rec.Name, rec.Transport, len(tools))
	}
}

// upsert (re)connects a single server after it's been created/edited, or
// disconnects it if it was disabled/deleted. Called right after a CRUD write
// so config changes take effect without a process restart.
func (m *mcpManager) upsert(rec MCPServerRecord) {
	m.mu.Lock()
	if old, ok := m.conns[rec.ID]; ok {
		old.session.Close()
		delete(m.conns, rec.ID)
	}
	if !rec.Enabled {
		delete(m.status, rec.ID)
		delete(m.records, rec.ID)
	} else {
		m.records[rec.ID] = rec
	}
	m.mu.Unlock()

	if !rec.Enabled {
		return
	}
	session, tools, err := mcpConnect(context.Background(), rec)
	if err != nil {
		log.Printf("mcp: connect to %q failed: %v", rec.Name, err)
		m.mu.Lock()
		m.status[rec.ID] = mcpConnStatus{Connected: false, Error: err.Error(), CheckedAt: time.Now()}
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	m.conns[rec.ID] = &mcpConnection{record: rec, session: session, tools: tools}
	m.status[rec.ID] = mcpConnStatus{Connected: true, ToolCount: len(tools), CheckedAt: time.Now()}
	m.mu.Unlock()
	log.Printf("mcp: connected to %q (%s), %d tool(s) available", rec.Name, rec.Transport, len(tools))
}

// runHealthLoop periodically re-verifies every enabled server's connection:
// live sessions get a lightweight ListTools() call (which doubles as picking
// up tools added/removed on the remote server since the last check), and
// sessions that fail that check -- or servers that were never successfully
// connected in the first place (e.g. a transient network failure) -- get a
// fresh reconnect attempt. Runs for the lifetime of the process; intended to
// be started with `go mcpMgr.runHealthLoop(...)`.
func (m *mcpManager) runHealthLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		m.healthCheck()
	}
}

func (m *mcpManager) healthCheck() {
	m.mu.RLock()
	recs := make(map[int]MCPServerRecord, len(m.records))
	for id, r := range m.records {
		recs[id] = r
	}
	m.mu.RUnlock()

	for id, rec := range recs {
		m.mu.RLock()
		conn, connected := m.conns[id]
		m.mu.RUnlock()

		if connected {
			ctx, cancel := context.WithTimeout(context.Background(), mcpCallTimeout)
			res, err := conn.session.ListTools(ctx, nil)
			cancel()
			if err == nil {
				m.mu.Lock()
				conn.tools = res.Tools
				m.status[id] = mcpConnStatus{Connected: true, ToolCount: len(res.Tools), CheckedAt: time.Now()}
				m.mu.Unlock()
				continue
			}
			log.Printf("mcp: health check for %q failed, reconnecting: %v", rec.Name, err)
			conn.session.Close()
			m.mu.Lock()
			delete(m.conns, id)
			m.mu.Unlock()
		}

		session, tools, err := mcpConnect(context.Background(), rec)
		if err != nil {
			m.mu.Lock()
			m.status[id] = mcpConnStatus{Connected: false, Error: err.Error(), CheckedAt: time.Now()}
			m.mu.Unlock()
			continue
		}
		m.mu.Lock()
		m.conns[id] = &mcpConnection{record: rec, session: session, tools: tools}
		m.status[id] = mcpConnStatus{Connected: true, ToolCount: len(tools), CheckedAt: time.Now()}
		m.mu.Unlock()
		log.Printf("mcp: reconnected to %q (%s), %d tool(s) available", rec.Name, rec.Transport, len(tools))
	}
}

func (m *mcpManager) remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if conn, ok := m.conns[id]; ok {
		conn.session.Close()
		delete(m.conns, id)
	}
	delete(m.status, id)
	delete(m.records, id)
}

func (m *mcpManager) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, conn := range m.conns {
		conn.session.Close()
		delete(m.conns, id)
	}
}

// statusFor reports the last known connection outcome for a server. Disabled
// servers (or ones never attempted) report Connected=false with no error.
func (m *mcpManager) statusFor(id int) mcpConnStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status[id]
}

type mcpToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// toolsFor lists the tools discovered on a server's current connection.
// Returns an empty (non-nil) slice if the server isn't currently connected.
func (m *mcpManager) toolsFor(id int) []mcpToolInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.conns[id]
	if !ok {
		return []mcpToolInfo{}
	}
	out := make([]mcpToolInfo, 0, len(conn.tools))
	for _, t := range conn.tools {
		out = append(out, mcpToolInfo{Name: t.Name, Description: t.Description})
	}
	return out
}

// mcpServerWithStatus is the API-facing shape of an MCP server record: the
// persisted config plus its current (in-memory, not persisted) connection
// status, so the admin UI can show connected/error/disconnected at a glance.
type mcpServerWithStatus struct {
	MCPServerRecord
	Status      string `json:"status"`
	StatusError string `json:"statusError,omitempty"`
}

func withMCPStatus(rec MCPServerRecord, mgr *mcpManager) mcpServerWithStatus {
	out := mcpServerWithStatus{MCPServerRecord: rec, Status: "disconnected"}
	if !rec.Enabled {
		return out
	}
	st := mgr.statusFor(rec.ID)
	switch {
	case st.Connected:
		out.Status = "connected"
	case st.Error != "":
		out.Status = "error"
		out.StatusError = st.Error
	}
	return out
}

func mcpToolName(serverID int, toolName string) string {
	return fmt.Sprintf("mcp_%d_%s", serverID, toolName)
}

// mcpToolSchemaOrDefault marshals an MCP tool's input schema for use as an
// OpenAI-style function's "parameters", coercing it into something providers
// will actually accept. Two distinct failure modes have shown up in real MCP
// servers, both reported by providers as "got 'type: null'":
//  1. A tool with no input parameters has InputSchema == nil, which marshals
//     successfully to the 4-byte literal `null` (not a marshal error, not
//     zero-length).
//  2. A tool WITH real parameters whose schema simply omits the top-level
//     "type": "object" key (common -- many schema authors consider it
//     implied by the presence of "properties" and skip it), which OpenAI's
//     strict validator still rejects outright.
//
// Both are fixed the same way: decode to a map, force "type": "object" if
// it isn't already exactly that, and fall back to an empty object schema
// only when the value isn't a JSON object at all (nothing sensible to patch).
func mcpToolSchemaOrDefault(schema any) json.RawMessage {
	def := json.RawMessage(`{"type":"object","properties":{}}`)
	if schema == nil {
		return def
	}
	b, err := json.Marshal(schema)
	if err != nil || len(b) == 0 {
		return def
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return def
	}
	if t, _ := m["type"].(string); t != "object" {
		m["type"] = "object"
	}
	fixed, err := json.Marshal(m)
	if err != nil {
		return def
	}
	return fixed
}

func (m *mcpManager) listChatTools() []toolSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []toolSpec
	for id, conn := range m.conns {
		for _, t := range conn.tools {
			schema := mcpToolSchemaOrDefault(t.InputSchema)
			out = append(out, toolSpec{
				Type: "function",
				Function: toolFunction{
					Name:        mcpToolName(id, t.Name),
					Description: fmt.Sprintf("[%s] %s", conn.record.Name, t.Description),
					Parameters:  schema,
				},
			})
		}
	}
	return out
}

func (m *mcpManager) isMCPTool(name string) bool {
	return strings.HasPrefix(name, "mcp_")
}

func (m *mcpManager) callTool(ctx context.Context, fullName, argsJSON string) (string, error) {
	m.mu.RLock()
	var target *mcpConnection
	var toolName string
	for id, conn := range m.conns {
		prefix := fmt.Sprintf("mcp_%d_", id)
		if strings.HasPrefix(fullName, prefix) {
			target = conn
			toolName = strings.TrimPrefix(fullName, prefix)
			break
		}
	}
	m.mu.RUnlock()
	if target == nil {
		return "", fmt.Errorf("unknown mcp tool: %s", fullName)
	}

	var args map[string]any
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid tool arguments: %w", err)
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()
	res, err := target.session.CallTool(callCtx, &mcp.CallToolParams{Name: toolName, Arguments: args})
	if err != nil {
		return "", fmt.Errorf("call %s: %w", fullName, err)
	}

	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	if res.IsError {
		return "", fmt.Errorf("tool returned an error: %s", sb.String())
	}
	return sb.String(), nil
}
