package main

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oschwald/geoip2-golang"
)

//go:embed web
var webAssets embed.FS

type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.gz.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	w.gz.Flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(fsys, path); err != nil {
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

func startWebServer(addr string, agg *aggregator, geoDB *geoip2.Reader, asnDB *geoip2.Reader, store *Store, cfg *Config, kafkaExp *kafkaExporter, secret []byte, mon *monitor, ipTags *ipTagCache, mcpMgr *mcpManager) {
	sub, err := fs.Sub(webAssets, "web")
	if err != nil {
		log.Fatalf("embed web assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", spaHandler(sub))

	auth := requireAuth(secret)
	adminOnly := func(h http.HandlerFunc) http.Handler { return auth(requireAdmin(h)) }

	mux.Handle("/api/report", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		windowParam := r.URL.Query().Get("window")
		window, err := parseWindow(windowParam)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		topN, err := parseTopN(r.URL.Query().Get("topn"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		to := time.Now()
		report, err := store.QueryReportRange(to.Add(-window), to, topN)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// live detection state (who's currently over the anomaly
		// threshold), not a historical query -- stays on the aggregator
		// regardless of DB speed, see project memory for why.
		report.ScanAlerts = agg.threatAlerts()
		report.Window = windowParam
		annotateCountries(geoDB, &report)
		annotateOrgs(asnDB, &report)
		annotateIPTagsReport(ipTags, &report)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(report); err != nil {
			log.Printf("encode report: %v", err)
		}
	})))
	mux.Handle("/api/timeseries", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		windowParam := r.URL.Query().Get("window")
		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ts, err := store.QueryTimeSeries(from, to)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ts.Window = windowParam
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(ts); err != nil {
			log.Printf("encode timeseries: %v", err)
		}
	})))
	mux.Handle("/api/flowrate", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		to := time.Now()
		fr, err := store.QueryFlowRate(to.Add(-5*time.Minute), to)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fr); err != nil {
			log.Printf("encode flowrate: %v", err)
		}
	})))
	mux.Handle("/api/geo", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		windowParam := r.URL.Query().Get("window")
		window, err := parseWindow(windowParam)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		const geoCandidates = 500
		to := time.Now()
		candidates, err := store.QueryTopIPsInRange(to.Add(-window), to, geoCandidates)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		report := buildGeoReport(geoDB, asnDB, candidates)
		report.Window = windowParam
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(report); err != nil {
			log.Printf("encode geo report: %v", err)
		}
	})))
	mux.Handle("/api/topology", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		windowParam := r.URL.Query().Get("window")
		window, err := parseWindow(windowParam)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		to := time.Now()
		topo, err := store.QueryTopology(to.Add(-window), to)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		topo.Window = windowParam
		annotateIPTagsTopology(ipTags, &topo)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(topo); err != nil {
			log.Printf("encode topology report: %v", err)
		}
	})))

	mux.Handle("/api/admin/flows", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		page, pageSize, err := parsePaging(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		flows, total, err := store.QueryFlowsPaged(from, to, page, pageSize, FlowFilter{IP: r.URL.Query().Get("ip")})
		if err != nil {
			writeStoreQueryError(w, err)
			return
		}
		annotateIPTagsFlows(ipTags, flows)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Total int        `json:"total"`
			Page  int        `json:"page"`
			Flows []FlowStat `json:"flows"`
		}{total, page, flows})
	})))
	mux.Handle("/api/admin/ips", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		page, pageSize, err := parsePaging(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ips, total, err := store.QueryIPsPaged(from, to, page, pageSize, r.URL.Query().Get("q"))
		if err != nil {
			writeStoreQueryError(w, err)
			return
		}
		annotateIPTagsIPs(ipTags, ips)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Total int      `json:"total"`
			Page  int      `json:"page"`
			IPs   []IPStat `json:"ips"`
		}{total, page, ips})
	})))
	mux.Handle("/api/admin/ports", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		page, pageSize, err := parsePaging(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ports, total, err := store.QueryPortsPaged(from, to, page, pageSize, r.URL.Query().Get("q"))
		if err != nil {
			writeStoreQueryError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Total int        `json:"total"`
			Page  int        `json:"page"`
			Ports []PortStat `json:"ports"`
		}{total, page, ports})
	})))
	mux.Handle("/api/admin/service-categories", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		categories, err := store.QueryServiceCategories(from, to)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Categories []CategoryStat `json:"categories"`
		}{categories})
	})))
	mux.Handle("/api/admin/domains", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		page, pageSize, err := parsePaging(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		domains, total, err := store.QueryDomainsPaged(from, to, page, pageSize, r.URL.Query().Get("q"))
		if err != nil {
			writeStoreQueryError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Total   int          `json:"total"`
			Page    int          `json:"page"`
			Domains []DomainStat `json:"domains"`
		}{total, page, domains})
	})))
	mux.Handle("/api/admin/threat-alerts", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, pageSize, err := parsePaging(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		alerts, total, err := store.QueryThreatAlerts(page, pageSize, r.URL.Query().Get("q"), r.URL.Query().Get("kind"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		annotateIPTagsAlerts(ipTags, alerts)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Total  int                 `json:"total"`
			Page   int                 `json:"page"`
			Alerts []ThreatAlertRecord `json:"alerts"`
		}{total, page, alerts})
	})))

	mux.Handle("GET /api/config", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg.Snapshot())
	})))
	mux.Handle("PUT /api/config", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		var dto ConfigDTO
		if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := validateConfig(dto); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if store != nil {
			if err := store.SaveConfig(dto); err != nil {
				http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		cfg.Apply(dto)
		applyAnomalyConfig(agg, cfg)
		applyCapacityConfig(agg, cfg)
		applyKafkaConfig(kafkaExp, cfg)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg.Snapshot())
	}))

	mux.Handle("POST /api/admin/ai/test", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			BaseURL string `json:"baseURL"`
			APIKey  string `json:"apiKey"`
			Model   string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := testAIProvider(req.BaseURL, req.APIKey, req.Model); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.Handle("GET /api/admin/ai/chat/sessions", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := claimsFromContext(r.Context())
		sessions, err := store.ListChatSessions(claims.UserID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessions)
	}))
	mux.Handle("POST /api/admin/ai/chat/sessions", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := claimsFromContext(r.Context())
		session, err := store.CreateChatSession(claims.UserID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session)
	}))
	mux.Handle("DELETE /api/admin/ai/chat/sessions/{id}", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		claims, _ := claimsFromContext(r.Context())
		owner, ok, err := store.ChatSessionOwner(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok || owner != claims.UserID {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		if err := store.DeleteChatSession(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.Handle("GET /api/admin/ai/chat/sessions/{id}/messages", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		claims, _ := claimsFromContext(r.Context())
		owner, ok, err := store.ChatSessionOwner(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok || owner != claims.UserID {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		messages, err := store.ListChatMessages(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(messages)
	}))

	mux.Handle("POST /api/admin/ai/chat/sessions/{id}/messages", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		claims, _ := claimsFromContext(r.Context())
		owner, ok, err := store.ChatSessionOwner(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok || owner != claims.UserID {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}

		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Content) == "" {
			http.Error(w, "content is required", http.StatusBadRequest)
			return
		}

		snap := cfg.Snapshot()
		if !snap.AIEnabled || snap.AIBaseURL == "" || snap.AIModel == "" {
			http.Error(w, "AI is not enabled/configured -- set it up on the 设置 tab first", http.StatusBadRequest)
			return
		}

		history, err := store.ListChatMessages(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		messages := make([]chatCompletionMessage, 0, len(history)+2)
		messages = append(messages, chatCompletionMessage{Role: "system", Content: buildSystemPrompt(time.Now())})
		for _, m := range history {
			messages = append(messages, chatCompletionMessage{Role: m.Role, Content: m.Content})
		}
		messages = append(messages, chatCompletionMessage{Role: "user", Content: body.Content})

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		sink := func(kind string, payload any) {
			b, _ := json.Marshal(payload)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b)
			flusher.Flush()
		}

		turnStart := time.Now()
		finalText, toolsUsed, promptTokens, completionTokens, err := runChatTurn(r.Context(), snap, store, ipTags, mcpMgr, messages, sink)
		elapsedMs := time.Since(turnStart).Milliseconds()
		if err != nil {
			sink("error", map[string]string{"message": err.Error()})
			return
		}

		if err := store.TouchChatSession(id, body.Content); err != nil {
			log.Printf("touch chat session: %v", err)
		}
		if _, err := store.AppendChatMessage(id, "user", body.Content, nil, "", 0, 0, 0); err != nil {
			log.Printf("append user chat message: %v", err)
		}
		if _, err := store.AppendChatMessage(id, "assistant", finalText, toolsUsed, snap.AIModel, elapsedMs, promptTokens, completionTokens); err != nil {
			log.Printf("append assistant chat message: %v", err)
		}

		sink("done", map[string]any{
			"model": snap.AIModel, "elapsedMs": elapsedMs,
			"promptTokens": promptTokens, "completionTokens": completionTokens,
		})
	}))

	mux.Handle("POST /api/admin/webhooks/test", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Channel string `json:"channel"`
			URL     string `json:"url"`
			Secret  string `json:"secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}
		testAlert := ThreatAlert{Kind: AlertKindScan, IP: "203.0.113.1", DistinctPeers: 999}
		var err error
		switch req.Channel {
		case "wecom":
			err = sendWeComAlert(req.URL, testAlert, time.Now(), "")
		case "dingtalk":
			err = sendDingTalkAlert(req.URL, req.Secret, testAlert, time.Now(), "")
		case "feishu":
			err = sendFeishuAlert(req.URL, testAlert, time.Now(), "")
		default:
			http.Error(w, "unknown channel: "+req.Channel, http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.Handle("GET /api/admin/webhooks", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		hooks, err := store.ListWebhooks()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hooks)
	}))
	mux.Handle("POST /api/admin/webhooks", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Channel string `json:"channel"`
			URL     string `json:"url"`
			Secret  string `json:"secret"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Channel != "wecom" && body.Channel != "dingtalk" && body.Channel != "feishu" {
			http.Error(w, "channel must be wecom, dingtalk, or feishu", http.StatusBadRequest)
			return
		}
		if body.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}
		hook, err := store.CreateWebhook(body.Channel, body.URL, body.Secret, body.Enabled)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hook)
	}))
	mux.Handle("PUT /api/admin/webhooks/{id}", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		var body struct {
			URL     string `json:"url"`
			Secret  string `json:"secret"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}
		hook, err := store.UpdateWebhook(id, body.URL, body.Secret, body.Enabled)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hook)
	}))
	mux.Handle("DELETE /api/admin/webhooks/{id}", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := store.DeleteWebhook(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.Handle("GET /api/admin/ip-tags", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		page, pageSize, err := parsePaging(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		tags, total, err := store.QueryIPTags(page, pageSize, r.URL.Query().Get("q"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Total int           `json:"total"`
			Page  int           `json:"page"`
			Tags  []IPTagRecord `json:"tags"`
		}{total, page, tags})
	}))
	mux.Handle("POST /api/admin/ip-tags", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
			Label string `json:"label"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Kind != "ip" && body.Kind != "cidr" {
			http.Error(w, "kind must be ip or cidr", http.StatusBadRequest)
			return
		}
		if body.Label == "" {
			http.Error(w, "label is required", http.StatusBadRequest)
			return
		}
		if body.Kind == "ip" {
			if _, err := ipToUint32(body.Value); err != nil {
				http.Error(w, "invalid ip: "+err.Error(), http.StatusBadRequest)
				return
			}
		} else if _, _, err := net.ParseCIDR(body.Value); err != nil {
			http.Error(w, "invalid cidr: "+err.Error(), http.StatusBadRequest)
			return
		}
		tag, err := store.CreateIPTag(body.Kind, body.Value, body.Label)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if all, err := store.ListIPTags(); err == nil {
			ipTags.rebuild(all)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tag)
	}))
	mux.Handle("PUT /api/admin/ip-tags/{id}", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		var body struct {
			Label string `json:"label"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Label == "" {
			http.Error(w, "label is required", http.StatusBadRequest)
			return
		}
		tag, err := store.UpdateIPTag(id, body.Label)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if all, err := store.ListIPTags(); err == nil {
			ipTags.rebuild(all)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tag)
	}))
	mux.Handle("DELETE /api/admin/ip-tags/{id}", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := store.DeleteIPTag(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if all, err := store.ListIPTags(); err == nil {
			ipTags.rebuild(all)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.Handle("GET /api/admin/port-mappings", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		page, pageSize, err := parsePaging(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mappings, total, err := store.QueryPortMappings(page, pageSize, r.URL.Query().Get("q"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Total    int                 `json:"total"`
			Page     int                 `json:"page"`
			Mappings []PortMappingRecord `json:"mappings"`
		}{total, page, mappings})
	}))
	mux.Handle("POST /api/admin/port-mappings", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Port    int    `json:"port"`
			Service string `json:"service"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Port <= 0 || body.Port > 65535 {
			http.Error(w, "port must be between 1 and 65535", http.StatusBadRequest)
			return
		}
		if body.Service == "" {
			http.Error(w, "service is required", http.StatusBadRequest)
			return
		}
		mapping, err := store.CreatePortMapping(uint16(body.Port), body.Service)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if all, err := store.ListPortMappings(); err == nil {
			portMappings.rebuild(all)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mapping)
	}))
	mux.Handle("PUT /api/admin/port-mappings/{port}", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		port, err := strconv.Atoi(r.PathValue("port"))
		if err != nil || port <= 0 || port > 65535 {
			http.Error(w, "invalid port", http.StatusBadRequest)
			return
		}
		var body struct {
			Service string `json:"service"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Service == "" {
			http.Error(w, "service is required", http.StatusBadRequest)
			return
		}
		mapping, err := store.UpdatePortMapping(uint16(port), body.Service)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if all, err := store.ListPortMappings(); err == nil {
			portMappings.rebuild(all)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mapping)
	}))
	mux.Handle("DELETE /api/admin/port-mappings/{port}", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		port, err := strconv.Atoi(r.PathValue("port"))
		if err != nil || port <= 0 || port > 65535 {
			http.Error(w, "invalid port", http.StatusBadRequest)
			return
		}
		if err := store.DeletePortMapping(uint16(port)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if all, err := store.ListPortMappings(); err == nil {
			portMappings.rebuild(all)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.Handle("GET /api/admin/mcp-servers", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		servers, err := store.ListMCPServers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := make([]mcpServerWithStatus, 0, len(servers))
		for _, s := range servers {
			out = append(out, withMCPStatus(s, mcpMgr))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}))
	mux.Handle("GET /api/admin/mcp-servers/{id}/tools", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Tools []mcpToolInfo `json:"tools"`
		}{mcpMgr.toolsFor(id)})
	}))
	mux.Handle("POST /api/admin/mcp-servers/test", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Transport    string `json:"transport"`
			Endpoint     string `json:"endpoint"`
			Command      string `json:"command"`
			Args         string `json:"args"`
			AuthType     string `json:"authType"`
			AuthUsername string `json:"authUsername"`
			AuthPassword string `json:"authPassword"`
			AuthToken    string `json:"authToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		names, err := testMCPConnection(MCPServerRecord{
			Transport: body.Transport, Endpoint: body.Endpoint, Command: body.Command, Args: body.Args,
			AuthType: body.AuthType, AuthUsername: body.AuthUsername, AuthPassword: body.AuthPassword, AuthToken: body.AuthToken,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Tools []string `json:"tools"`
		}{names})
	}))
	mux.Handle("POST /api/admin/mcp-servers", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name         string `json:"name"`
			Transport    string `json:"transport"`
			Endpoint     string `json:"endpoint"`
			Command      string `json:"command"`
			Args         string `json:"args"`
			Enabled      bool   `json:"enabled"`
			AuthType     string `json:"authType"`
			AuthUsername string `json:"authUsername"`
			AuthPassword string `json:"authPassword"`
			AuthToken    string `json:"authToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if body.Transport != "http" && body.Transport != "stdio" {
			http.Error(w, "transport must be http or stdio", http.StatusBadRequest)
			return
		}
		if body.AuthType == "" {
			body.AuthType = "none"
		}
		if body.AuthType != "none" && body.AuthType != "basic" && body.AuthType != "bearer" {
			http.Error(w, "authType must be none, basic, or bearer", http.StatusBadRequest)
			return
		}
		rec, err := store.CreateMCPServer(body.Name, body.Transport, body.Endpoint, body.Command, body.Args, body.Enabled, body.AuthType, body.AuthUsername, body.AuthPassword, body.AuthToken)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mcpMgr.upsert(rec)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(withMCPStatus(rec, mcpMgr))
	}))
	mux.Handle("PUT /api/admin/mcp-servers/{id}", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		var body struct {
			Name         string `json:"name"`
			Endpoint     string `json:"endpoint"`
			Command      string `json:"command"`
			Args         string `json:"args"`
			Enabled      bool   `json:"enabled"`
			AuthType     string `json:"authType"`
			AuthUsername string `json:"authUsername"`
			AuthPassword string `json:"authPassword"`
			AuthToken    string `json:"authToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if body.AuthType == "" {
			body.AuthType = "none"
		}
		if body.AuthType != "none" && body.AuthType != "basic" && body.AuthType != "bearer" {
			http.Error(w, "authType must be none, basic, or bearer", http.StatusBadRequest)
			return
		}
		rec, err := store.UpdateMCPServer(id, body.Name, body.Endpoint, body.Command, body.Args, body.Enabled, body.AuthType, body.AuthUsername, body.AuthPassword, body.AuthToken)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mcpMgr.upsert(rec)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(withMCPStatus(rec, mcpMgr))
	}))
	mux.Handle("DELETE /api/admin/mcp-servers/{id}", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := store.DeleteMCPServer(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mcpMgr.remove(id)
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.Handle("GET /api/admin/monitor", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mon.snapshot(agg))
	}))

	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		u, err := store.GetUserByUsername(body.Username)

		if err != nil || !checkPassword(u.PasswordHash, body.Password) {
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		ttl := tokenTTLFor(u)
		token, err := issueToken(secret, u, ttl)
		if err != nil {
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}
		setAuthCookie(w, token, ttl)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(toAuthUserDTO(u.Username, u.Role, time.Now().Add(ttl)))
	})
	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		clearAuthCookie(w)
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /api/auth/me", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := claimsFromContext(r.Context())
		w.Header().Set("Content-Type", "application/json")

		json.NewEncoder(w).Encode(toAuthUserDTO(claims.Username, claims.Role, claims.ExpiresAt.Time))
	})))

	mux.Handle("GET /api/admin/users", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		users, err := store.ListUsers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		dtos := make([]UserDTO, len(users))
		for i, u := range users {
			dtos[i] = toUserDTO(u)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dtos)
	}))
	mux.Handle("POST /api/admin/users", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username    string `json:"username"`
			Password    string `json:"password"`
			Role        string `json:"role"`
			Description string `json:"description"`
			LongLived   bool   `json:"longLived"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if err := validateNewUser(body.Username, body.Password, body.Role); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		hash, err := hashPassword(body.Password)
		if err != nil {
			http.Error(w, "failed to hash password", http.StatusInternalServerError)
			return
		}
		u, err := store.CreateUser(body.Username, hash, body.Role, body.Description, body.LongLived)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(toUserDTO(u))
	}))
	mux.Handle("PUT /api/admin/users/{id}", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		var body struct {
			Role        string `json:"role"`
			Password    string `json:"password,omitempty"`
			Description string `json:"description"`
			LongLived   bool   `json:"longLived"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if body.Role != "admin" && body.Role != "normal" {
			http.Error(w, "role must be admin or normal", http.StatusBadRequest)
			return
		}
		var hash string
		if body.Password != "" {
			if len(body.Password) < 8 {
				http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
				return
			}
			hash, err = hashPassword(body.Password)
			if err != nil {
				http.Error(w, "failed to hash password", http.StatusInternalServerError)
				return
			}
		}
		if err := store.UpdateUser(id, body.Role, hash, body.Description, body.LongLived); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		u, err := store.GetUserByID(id)
		if err != nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(toUserDTO(u))
	}))
	mux.Handle("DELETE /api/admin/users/{id}", adminOnly(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		if claims, ok := claimsFromContext(r.Context()); ok && claims.UserID == id {
			http.Error(w, "cannot delete your own account", http.StatusBadRequest)
			return
		}
		if err := store.DeleteUser(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	log.Printf("web dashboard listening on %s", addr)
	go func() {
		if err := http.ListenAndServe(addr, gzipMiddleware(mux)); err != nil {
			log.Printf("web server stopped: %v", err)
		}
	}()
}

// writeStoreQueryError maps a store-layer query error to an HTTP response.
// errFetchTooDeep means the request itself is the problem (an offset far
// enough into the result set that satisfying it would require an
// almost-unbounded sort) -- that's a 400, not a 500, since the server
// isn't malfunctioning, it's refusing a request it correctly identified
// as too expensive to run. Everything else stays a generic 500.
func writeStoreQueryError(w http.ResponseWriter, err error) {
	if errors.Is(err, errFetchTooDeep) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func parsePaging(r *http.Request) (page, pageSize int, err error) {
	page = 0
	pageSize = 50
	if v := r.URL.Query().Get("page"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &page); err != nil || page < 0 {
			return 0, 0, fmt.Errorf("invalid page %q", v)
		}
	}
	if v := r.URL.Query().Get("pageSize"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &pageSize); err != nil || pageSize <= 0 {
			return 0, 0, fmt.Errorf("invalid pageSize %q", v)
		}
		if pageSize > 500 {
			pageSize = 500
		}
	}
	return page, pageSize, nil
}

func validateConfig(dto ConfigDTO) error {
	if dto.RefreshIntervalMs <= 0 {
		return fmt.Errorf("refreshIntervalMs must be positive, got %d", dto.RefreshIntervalMs)
	}
	if dto.DBFlowTopK <= 0 {
		return fmt.Errorf("dbFlowTopK must be positive, got %d", dto.DBFlowTopK)
	}
	if dto.TopKPerBucket <= 0 {
		return fmt.Errorf("topKPerBucket must be positive, got %d", dto.TopKPerBucket)
	}
	if dto.AnomalyWindowSec <= 0 {
		return fmt.Errorf("anomalyWindowSec must be positive, got %d", dto.AnomalyWindowSec)
	}
	if dto.AnomalyPeerThreshold <= 0 {
		return fmt.Errorf("anomalyPeerThreshold must be positive, got %d", dto.AnomalyPeerThreshold)
	}
	if dto.AnomalyAvgPacketsThreshold <= 0 {
		return fmt.Errorf("anomalyAvgPacketsThreshold must be positive, got %v", dto.AnomalyAvgPacketsThreshold)
	}
	if dto.VolumeThresholdBytes <= 0 {
		return fmt.Errorf("volumeThresholdBytes must be positive, got %d", dto.VolumeThresholdBytes)
	}

	if dto.AIEnabled && dto.AIBaseURL == "" {
		return fmt.Errorf("aiBaseURL is required when AI is enabled")
	}
	if dto.AIEnabled && dto.AIModel == "" {
		return fmt.Errorf("aiModel is required when AI is enabled")
	}
	if dto.KafkaEnabled && dto.KafkaBrokers == "" {
		return fmt.Errorf("kafkaBrokers is required when Kafka export is enabled")
	}
	if dto.KafkaEnabled && dto.KafkaTopic == "" {
		return fmt.Errorf("kafkaTopic is required when Kafka export is enabled")
	}
	return nil
}

type UserDTO struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	CreatedAt   string `json:"createdAt"`
	Description string `json:"description,omitempty"`
	LongLived   bool   `json:"longLived"`
}

func toUserDTO(u User) UserDTO {
	return UserDTO{
		ID: u.ID, Username: u.Username, Role: u.Role, CreatedAt: u.CreatedAt.Format(time.RFC3339),
		Description: u.Description, LongLived: u.LongLived,
	}
}

type AuthUserDTO struct {
	Username  string `json:"username"`
	Role      string `json:"role"`
	ExpiresAt string `json:"expiresAt"`
}

func toAuthUserDTO(username, role string, expiresAt time.Time) AuthUserDTO {
	return AuthUserDTO{Username: username, Role: role, ExpiresAt: expiresAt.Format(time.RFC3339)}
}

func validateNewUser(username, password, role string) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if role != "admin" && role != "normal" {
		return fmt.Errorf("role must be admin or normal")
	}
	return nil
}

func parseTopN(s string) (int, error) {
	if s == "" {
		return 10, nil
	}
	switch s {
	case "10", "20", "30", "50", "100":
		n := 0
		fmt.Sscanf(s, "%d", &n)
		return n, nil
	default:
		return 0, fmt.Errorf("invalid topn %q, expected one of 10/20/30/50/100", s)
	}
}

func parseWindow(s string) (time.Duration, error) {
	switch s {
	case "15m":
		return 15 * time.Minute, nil
	case "30m":
		return 30 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	case "15d":
		return 15 * 24 * time.Hour, nil
	case "1mo":

		return 30 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid window %q, expected one of 15m/30m/1h/1d/15d/1mo", s)
	}
}

func parseRange(r *http.Request) (from, to time.Time, err error) {
	fromParam := r.URL.Query().Get("from")
	toParam := r.URL.Query().Get("to")
	if fromParam != "" || toParam != "" {
		fromSec, err := strconv.ParseInt(fromParam, 10, 64)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid from %q: %w", fromParam, err)
		}
		toSec, err := strconv.ParseInt(toParam, 10, 64)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid to %q: %w", toParam, err)
		}
		if toSec <= fromSec {
			return time.Time{}, time.Time{}, fmt.Errorf("to must be after from")
		}
		return time.Unix(fromSec, 0), time.Unix(toSec, 0), nil
	}
	window, err := parseWindow(r.URL.Query().Get("window"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	now := time.Now()
	return now.Add(-window), now, nil
}
