package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const dbWriteQueueSize = 16

type flowSample struct {
	key     xdpflowFlowKey
	packets uint64
	bytes   uint64
	domain  string
}

type tickSnapshot struct {
	start             time.Time
	protoPackets      map[uint8]uint64
	protoBytes        map[uint8]uint64
	flows             []flowSample
	ips               map[uint32]xdpflowFlowStats
	ports             map[portKey]xdpflowFlowStats
	distinctFlowCount int
	scanAlerts        []ThreatAlert
}

type Store struct {
	db        *sql.DB
	ts        *tsStore
	retention time.Duration
	writeCh   chan tickSnapshot
	closeCh   chan struct{}
}

func NewStore(path string, retention time.Duration, hotPeriod time.Duration) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-20000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(4)

	if err := ensureIncrementalVacuum(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("ensure incremental vacuum: %w", err)
	}

	if err := ensureThreatAlertsSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate threat_alerts: %w", err)
	}
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err := seedPortMappingsIfEmpty(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed port mappings: %w", err)
	}
	if err := ensureAppConfigCapacityColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate app_config: %w", err)
	}
	if err := ensureUserExtraColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate users: %w", err)
	}
	if err := ensureChatMessagesMetadataColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate chat_messages: %w", err)
	}
	if err := ensureMCPServerAuthColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate mcp_servers: %w", err)
	}

	ts, err := newTSStore(path+".tsdata", hotPeriod)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open time-series store: %w", err)
	}

	s := &Store{
		db:        db,
		ts:        ts,
		retention: retention,
		writeCh:   make(chan tickSnapshot, dbWriteQueueSize),
		closeCh:   make(chan struct{}),
	}
	go s.writeLoop()
	go s.retentionLoop()
	return s, nil
}

func createSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS bucket_summary (
			ts INTEGER PRIMARY KEY,
			distinct_flow_count INTEGER NOT NULL,
			proto_bytes TEXT NOT NULL,
			proto_packets TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS threat_alerts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER NOT NULL,
			kind TEXT NOT NULL DEFAULT 'scan',
			ip INTEGER NOT NULL,
			distinct_peers INTEGER NOT NULL DEFAULT 0,
			volume_bytes INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_threat_alerts_ts ON threat_alerts(ts)`,

		`CREATE TABLE IF NOT EXISTS app_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			refresh_interval_ms INTEGER NOT NULL,
			persist_scan_alerts INTEGER NOT NULL,
			db_flow_topk INTEGER NOT NULL DEFAULT 2000,
			topk_per_bucket INTEGER NOT NULL DEFAULT 200,
			anomaly_enabled INTEGER NOT NULL DEFAULT 0,
			anomaly_window_sec INTEGER NOT NULL DEFAULT 60,
			anomaly_peer_threshold INTEGER NOT NULL DEFAULT 500,
			anomaly_avg_packets_threshold REAL NOT NULL DEFAULT 10.0,
			volume_threshold_bytes INTEGER NOT NULL DEFAULT 524288000,
			ai_enabled INTEGER NOT NULL DEFAULT 0,
			ai_provider TEXT NOT NULL DEFAULT '',
			ai_base_url TEXT NOT NULL DEFAULT '',
			ai_api_key TEXT NOT NULL DEFAULT '',
			ai_model TEXT NOT NULL DEFAULT ''
		)`,

		`CREATE TABLE IF NOT EXISTS alert_webhooks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel TEXT NOT NULL,
			url TEXT NOT NULL,
			secret TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			long_lived INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS auth_secret (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			secret BLOB NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS chat_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_sessions_user ON chat_sessions(user_id, updated_at DESC)`,

		`CREATE TABLE IF NOT EXISTS chat_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			tool_calls TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			model TEXT NOT NULL DEFAULT '',
			elapsed_ms INTEGER NOT NULL DEFAULT 0,
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_session ON chat_messages(session_id, id)`,

		`CREATE TABLE IF NOT EXISTS ip_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			value TEXT NOT NULL,
			label TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			UNIQUE(kind, value)
		)`,

		`CREATE TABLE IF NOT EXISTS port_mappings (
			port INTEGER PRIMARY KEY,
			service TEXT NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS mcp_servers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			transport TEXT NOT NULL,
			endpoint TEXT NOT NULL DEFAULT '',
			command TEXT NOT NULL DEFAULT '',
			args TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			auth_type TEXT NOT NULL DEFAULT 'none',
			auth_username TEXT NOT NULL DEFAULT '',
			auth_password TEXT NOT NULL DEFAULT '',
			auth_token TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt, err)
		}
	}
	return nil
}

func seedPortMappingsIfEmpty(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM port_mappings`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO port_mappings(port, service) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for port, service := range wellKnownPorts {
		if _, err := stmt.Exec(port, service); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ensureIncrementalVacuum(db *sql.DB) error {
	var mode int
	if err := db.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		return fmt.Errorf("read auto_vacuum: %w", err)
	}
	if mode == 2 {
		return nil
	}
	log.Printf("store: converting database to auto_vacuum=INCREMENTAL (one-time; may take a while on a large existing database)")
	if _, err := db.Exec(`PRAGMA auto_vacuum = INCREMENTAL`); err != nil {
		return fmt.Errorf("set auto_vacuum: %w", err)
	}
	if _, err := db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	log.Printf("store: auto_vacuum conversion complete")
	return nil
}

func ensureAppConfigCapacityColumns(db *sql.DB) error {
	existing, err := columnNames(db, "app_config")
	if err != nil {
		return fmt.Errorf("read app_config columns: %w", err)
	}
	hadAnomalyWindow := existing["anomaly_window_sec"]
	hadLegacyScanColumns := existing["scan_window_sec"]

	for _, col := range []struct{ name, ddl string }{
		{"db_flow_topk", "ALTER TABLE app_config ADD COLUMN db_flow_topk INTEGER NOT NULL DEFAULT 2000"},
		{"topk_per_bucket", "ALTER TABLE app_config ADD COLUMN topk_per_bucket INTEGER NOT NULL DEFAULT 200"},
		{"anomaly_enabled", "ALTER TABLE app_config ADD COLUMN anomaly_enabled INTEGER NOT NULL DEFAULT 0"},
		{"anomaly_window_sec", "ALTER TABLE app_config ADD COLUMN anomaly_window_sec INTEGER NOT NULL DEFAULT 60"},
		{"anomaly_peer_threshold", "ALTER TABLE app_config ADD COLUMN anomaly_peer_threshold INTEGER NOT NULL DEFAULT 500"},
		{"anomaly_avg_packets_threshold", "ALTER TABLE app_config ADD COLUMN anomaly_avg_packets_threshold REAL NOT NULL DEFAULT 10.0"},
		{"volume_threshold_bytes", "ALTER TABLE app_config ADD COLUMN volume_threshold_bytes INTEGER NOT NULL DEFAULT 524288000"},
		{"ai_enabled", "ALTER TABLE app_config ADD COLUMN ai_enabled INTEGER NOT NULL DEFAULT 0"},
		{"ai_provider", "ALTER TABLE app_config ADD COLUMN ai_provider TEXT NOT NULL DEFAULT ''"},
		{"ai_base_url", "ALTER TABLE app_config ADD COLUMN ai_base_url TEXT NOT NULL DEFAULT ''"},
		{"ai_api_key", "ALTER TABLE app_config ADD COLUMN ai_api_key TEXT NOT NULL DEFAULT ''"},
		{"ai_model", "ALTER TABLE app_config ADD COLUMN ai_model TEXT NOT NULL DEFAULT ''"},
	} {
		if existing[col.name] {
			continue
		}
		log.Printf("store: adding app_config.%s column (existing database predates this setting)", col.name)
		if _, err := db.Exec(col.ddl); err != nil {
			return fmt.Errorf("add column %s: %w", col.name, err)
		}
	}

	if !hadAnomalyWindow && hadLegacyScanColumns {
		log.Printf("store: carrying forward legacy scan_window_sec/scan_dest_threshold/scan_avg_packets_threshold into the new shared anomaly_* columns")
		if _, err := db.Exec(`UPDATE app_config SET anomaly_window_sec = scan_window_sec, anomaly_peer_threshold = scan_dest_threshold, anomaly_avg_packets_threshold = scan_avg_packets_threshold WHERE id = 1`); err != nil {
			return fmt.Errorf("backfill anomaly config from legacy scan columns: %w", err)
		}
	}

	legacyColumns := []string{
		"scan_window_sec", "scan_dest_threshold", "scan_avg_packets_threshold",
		"ddos_window_sec", "ddos_src_threshold", "ddos_avg_packets_threshold",
		"wecom_enabled", "wecom_webhook_url",
		"dingtalk_enabled", "dingtalk_webhook_url", "dingtalk_secret",
		"feishu_enabled", "feishu_webhook_url",
	}
	for _, col := range legacyColumns {
		if !existing[col] {
			continue
		}
		log.Printf("store: dropping legacy app_config.%s column (superseded, no longer read or written)", col)
		if _, err := db.Exec(`ALTER TABLE app_config DROP COLUMN ` + col); err != nil {
			return fmt.Errorf("drop legacy column %s: %w", col, err)
		}
	}
	return nil
}

func ensureUserExtraColumns(db *sql.DB) error {
	existing, err := columnNames(db, "users")
	if err != nil {
		return fmt.Errorf("read users columns: %w", err)
	}
	for _, col := range []struct{ name, ddl string }{
		{"description", "ALTER TABLE users ADD COLUMN description TEXT NOT NULL DEFAULT ''"},
		{"long_lived", "ALTER TABLE users ADD COLUMN long_lived INTEGER NOT NULL DEFAULT 0"},
	} {
		if existing[col.name] {
			continue
		}
		log.Printf("store: adding users.%s column (existing database predates this setting)", col.name)
		if _, err := db.Exec(col.ddl); err != nil {
			return fmt.Errorf("add column %s: %w", col.name, err)
		}
	}
	return nil
}

func ensureChatMessagesMetadataColumns(db *sql.DB) error {
	existing, err := columnNames(db, "chat_messages")
	if err != nil {
		return fmt.Errorf("read chat_messages columns: %w", err)
	}
	for _, col := range []struct{ name, ddl string }{
		{"model", "ALTER TABLE chat_messages ADD COLUMN model TEXT NOT NULL DEFAULT ''"},
		{"elapsed_ms", "ALTER TABLE chat_messages ADD COLUMN elapsed_ms INTEGER NOT NULL DEFAULT 0"},
		{"prompt_tokens", "ALTER TABLE chat_messages ADD COLUMN prompt_tokens INTEGER NOT NULL DEFAULT 0"},
		{"completion_tokens", "ALTER TABLE chat_messages ADD COLUMN completion_tokens INTEGER NOT NULL DEFAULT 0"},
	} {
		if existing[col.name] {
			continue
		}
		log.Printf("store: adding chat_messages.%s column (existing database predates this setting)", col.name)
		if _, err := db.Exec(col.ddl); err != nil {
			return fmt.Errorf("add column %s: %w", col.name, err)
		}
	}
	return nil
}

func ensureMCPServerAuthColumns(db *sql.DB) error {
	existing, err := columnNames(db, "mcp_servers")
	if err != nil {
		return fmt.Errorf("read mcp_servers columns: %w", err)
	}
	for _, col := range []struct{ name, ddl string }{
		{"auth_type", "ALTER TABLE mcp_servers ADD COLUMN auth_type TEXT NOT NULL DEFAULT 'none'"},
		{"auth_username", "ALTER TABLE mcp_servers ADD COLUMN auth_username TEXT NOT NULL DEFAULT ''"},
		{"auth_password", "ALTER TABLE mcp_servers ADD COLUMN auth_password TEXT NOT NULL DEFAULT ''"},
		{"auth_token", "ALTER TABLE mcp_servers ADD COLUMN auth_token TEXT NOT NULL DEFAULT ''"},
	} {
		if existing[col.name] {
			continue
		}
		log.Printf("store: adding mcp_servers.%s column (existing database predates this setting)", col.name)
		if _, err := db.Exec(col.ddl); err != nil {
			return fmt.Errorf("add column %s: %w", col.name, err)
		}
	}
	return nil
}

func columnNames(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, rows.Err()
}

func tableNames(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, rows.Err()
}

func ensureThreatAlertsSchema(db *sql.DB) error {
	tables, err := tableNames(db)
	if err != nil {
		return fmt.Errorf("read table list: %w", err)
	}

	if !tables["scan_alerts"] && !tables["threat_alerts"] {
		return nil
	}
	if !tables["threat_alerts"] {
		log.Printf("store: renaming scan_alerts table to threat_alerts (existing database predates DDoS detection)")
		if _, err := db.Exec(`ALTER TABLE scan_alerts RENAME TO threat_alerts`); err != nil {
			return fmt.Errorf("rename scan_alerts: %w", err)
		}
	}

	cols, err := columnNames(db, "threat_alerts")
	if err != nil {
		return fmt.Errorf("read threat_alerts columns: %w", err)
	}
	if !cols["ip"] {
		log.Printf("store: renaming threat_alerts.src_ip to ip")
		if _, err := db.Exec(`ALTER TABLE threat_alerts RENAME COLUMN src_ip TO ip`); err != nil {
			return fmt.Errorf("rename src_ip: %w", err)
		}
	}
	if !cols["distinct_peers"] {
		log.Printf("store: renaming threat_alerts.distinct_dests to distinct_peers")
		if _, err := db.Exec(`ALTER TABLE threat_alerts RENAME COLUMN distinct_dests TO distinct_peers`); err != nil {
			return fmt.Errorf("rename distinct_dests: %w", err)
		}
	}
	if !cols["kind"] {
		log.Printf("store: adding threat_alerts.kind column (defaulting existing rows to 'scan')")
		if _, err := db.Exec(`ALTER TABLE threat_alerts ADD COLUMN kind TEXT NOT NULL DEFAULT 'scan'`); err != nil {
			return fmt.Errorf("add kind column: %w", err)
		}
	}
	if !cols["volume_bytes"] {
		log.Printf("store: adding threat_alerts.volume_bytes column (predates the volume-based anomaly signal, defaults existing rows to 0)")
		if _, err := db.Exec(`ALTER TABLE threat_alerts ADD COLUMN volume_bytes INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add volume_bytes column: %w", err)
		}
	}
	return nil
}

func (s *Store) LoadConfig() (dto ConfigDTO, ok bool, err error) {
	var persistScanAlerts, anomalyEnabled, aiEnabled int
	row := s.db.QueryRow(`SELECT refresh_interval_ms, persist_scan_alerts, db_flow_topk, topk_per_bucket,
		anomaly_enabled, anomaly_window_sec, anomaly_peer_threshold, anomaly_avg_packets_threshold, volume_threshold_bytes,
		ai_enabled, ai_provider, ai_base_url, ai_api_key, ai_model
		FROM app_config WHERE id = 1`)
	if err := row.Scan(&dto.RefreshIntervalMs, &persistScanAlerts, &dto.DBFlowTopK, &dto.TopKPerBucket,
		&anomalyEnabled, &dto.AnomalyWindowSec, &dto.AnomalyPeerThreshold, &dto.AnomalyAvgPacketsThreshold, &dto.VolumeThresholdBytes,
		&aiEnabled, &dto.AIProvider, &dto.AIBaseURL, &dto.AIAPIKey, &dto.AIModel); err != nil {
		if err == sql.ErrNoRows {
			return ConfigDTO{}, false, nil
		}
		return ConfigDTO{}, false, err
	}
	dto.PersistScanAlerts = persistScanAlerts != 0
	dto.AnomalyEnabled = anomalyEnabled != 0
	dto.AIEnabled = aiEnabled != 0
	return dto, true, nil
}

func (s *Store) SaveConfig(dto ConfigDTO) error {
	persistScanAlerts := 0
	if dto.PersistScanAlerts {
		persistScanAlerts = 1
	}
	anomalyEnabled := 0
	if dto.AnomalyEnabled {
		anomalyEnabled = 1
	}
	aiEnabled := 0
	if dto.AIEnabled {
		aiEnabled = 1
	}
	res, err := s.db.Exec(`UPDATE app_config SET
		refresh_interval_ms = ?, persist_scan_alerts = ?, db_flow_topk = ?, topk_per_bucket = ?,
		anomaly_enabled = ?, anomaly_window_sec = ?, anomaly_peer_threshold = ?, anomaly_avg_packets_threshold = ?, volume_threshold_bytes = ?,
		ai_enabled = ?, ai_provider = ?, ai_base_url = ?, ai_api_key = ?, ai_model = ?
		WHERE id = 1`,
		dto.RefreshIntervalMs, persistScanAlerts, dto.DBFlowTopK, dto.TopKPerBucket,
		anomalyEnabled, dto.AnomalyWindowSec, dto.AnomalyPeerThreshold, dto.AnomalyAvgPacketsThreshold, dto.VolumeThresholdBytes,
		aiEnabled, dto.AIProvider, dto.AIBaseURL, dto.AIAPIKey, dto.AIModel)
	if err != nil {
		return fmt.Errorf("update app_config: %w", err)
	}
	if n, rowsErr := res.RowsAffected(); rowsErr == nil && n > 0 {
		return nil
	}

	_, err = s.db.Exec(`INSERT INTO app_config(
		id, refresh_interval_ms, persist_scan_alerts, db_flow_topk, topk_per_bucket,
		anomaly_enabled, anomaly_window_sec, anomaly_peer_threshold, anomaly_avg_packets_threshold, volume_threshold_bytes,
		ai_enabled, ai_provider, ai_base_url, ai_api_key, ai_model
		) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dto.RefreshIntervalMs, persistScanAlerts, dto.DBFlowTopK, dto.TopKPerBucket,
		anomalyEnabled, dto.AnomalyWindowSec, dto.AnomalyPeerThreshold, dto.AnomalyAvgPacketsThreshold, dto.VolumeThresholdBytes,
		aiEnabled, dto.AIProvider, dto.AIBaseURL, dto.AIAPIKey, dto.AIModel)
	if err != nil {
		return fmt.Errorf("insert app_config: %w", err)
	}
	return nil
}

type WebhookRecord struct {
	ID      int    `json:"id"`
	Channel string `json:"channel"`
	URL     string `json:"url"`
	Secret  string `json:"secret,omitempty"`
	Enabled bool   `json:"enabled"`
}

func (s *Store) ListWebhooks() ([]WebhookRecord, error) {
	rows, err := s.db.Query(`SELECT id, channel, url, secret, enabled FROM alert_webhooks ORDER BY channel, id`)
	if err != nil {
		return nil, fmt.Errorf("query webhooks: %w", err)
	}
	defer rows.Close()

	out := []WebhookRecord{}
	for rows.Next() {
		var w WebhookRecord
		var enabled int
		if err := rows.Scan(&w.ID, &w.Channel, &w.URL, &w.Secret, &enabled); err != nil {
			return nil, err
		}
		w.Enabled = enabled != 0
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) ListEnabledWebhooks() ([]WebhookRecord, error) {
	rows, err := s.db.Query(`SELECT id, channel, url, secret, enabled FROM alert_webhooks WHERE enabled = 1 ORDER BY channel, id`)
	if err != nil {
		return nil, fmt.Errorf("query enabled webhooks: %w", err)
	}
	defer rows.Close()
	var out []WebhookRecord
	for rows.Next() {
		var w WebhookRecord
		var enabled int
		if err := rows.Scan(&w.ID, &w.Channel, &w.URL, &w.Secret, &enabled); err != nil {
			return nil, err
		}
		w.Enabled = enabled != 0
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) CreateWebhook(channel, url, secret string, enabled bool) (WebhookRecord, error) {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	res, err := s.db.Exec(`INSERT INTO alert_webhooks(channel, url, secret, enabled) VALUES (?, ?, ?, ?)`, channel, url, secret, enabledInt)
	if err != nil {
		return WebhookRecord{}, fmt.Errorf("insert webhook: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return WebhookRecord{}, err
	}
	return WebhookRecord{ID: int(id), Channel: channel, URL: url, Secret: secret, Enabled: enabled}, nil
}

func (s *Store) UpdateWebhook(id int, url, secret string, enabled bool) (WebhookRecord, error) {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	if _, err := s.db.Exec(`UPDATE alert_webhooks SET url = ?, secret = ?, enabled = ? WHERE id = ?`, url, secret, enabledInt, id); err != nil {
		return WebhookRecord{}, fmt.Errorf("update webhook: %w", err)
	}
	var w WebhookRecord
	var enabledOut int
	row := s.db.QueryRow(`SELECT id, channel, url, secret, enabled FROM alert_webhooks WHERE id = ?`, id)
	if err := row.Scan(&w.ID, &w.Channel, &w.URL, &w.Secret, &enabledOut); err != nil {
		return WebhookRecord{}, fmt.Errorf("read back updated webhook: %w", err)
	}
	w.Enabled = enabledOut != 0
	return w, nil
}

func (s *Store) DeleteWebhook(id int) error {
	_, err := s.db.Exec(`DELETE FROM alert_webhooks WHERE id = ?`, id)
	return err
}

type IPTagRecord struct {
	ID    int    `json:"id"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Label string `json:"label"`
}

func (s *Store) ListIPTags() ([]IPTagRecord, error) {
	rows, err := s.db.Query(`SELECT id, kind, value, label FROM ip_tags ORDER BY kind, value`)
	if err != nil {
		return nil, fmt.Errorf("query ip tags: %w", err)
	}
	defer rows.Close()

	out := []IPTagRecord{}
	for rows.Next() {
		var t IPTagRecord
		if err := rows.Scan(&t.ID, &t.Kind, &t.Value, &t.Label); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) QueryIPTags(page, pageSize int, q string) ([]IPTagRecord, int, error) {
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	where := "1=1"
	var args []any
	if q != "" {
		where += " AND (value LIKE ? OR label LIKE ?)"
		like := "%" + q + "%"
		args = append(args, like, like)
	}

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ip_tags WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count ip tags: %w", err)
	}
	rows, err := s.db.Query(`SELECT id, kind, value, label FROM ip_tags WHERE `+where+` ORDER BY kind, value LIMIT ? OFFSET ?`, append(args, pageSize, page*pageSize)...)
	if err != nil {
		return nil, 0, fmt.Errorf("query ip tags: %w", err)
	}
	defer rows.Close()
	out := []IPTagRecord{}
	for rows.Next() {
		var t IPTagRecord
		if err := rows.Scan(&t.ID, &t.Kind, &t.Value, &t.Label); err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

func (s *Store) CreateIPTag(kind, value, label string) (IPTagRecord, error) {
	res, err := s.db.Exec(`INSERT INTO ip_tags(kind, value, label, created_at) VALUES (?, ?, ?, ?)`, kind, value, label, time.Now().Unix())
	if err != nil {
		return IPTagRecord{}, fmt.Errorf("insert ip tag: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return IPTagRecord{}, err
	}
	return IPTagRecord{ID: int(id), Kind: kind, Value: value, Label: label}, nil
}

func (s *Store) UpdateIPTag(id int, label string) (IPTagRecord, error) {
	if _, err := s.db.Exec(`UPDATE ip_tags SET label = ? WHERE id = ?`, label, id); err != nil {
		return IPTagRecord{}, fmt.Errorf("update ip tag: %w", err)
	}
	var t IPTagRecord
	row := s.db.QueryRow(`SELECT id, kind, value, label FROM ip_tags WHERE id = ?`, id)
	if err := row.Scan(&t.ID, &t.Kind, &t.Value, &t.Label); err != nil {
		return IPTagRecord{}, fmt.Errorf("read back updated ip tag: %w", err)
	}
	return t, nil
}

func (s *Store) DeleteIPTag(id int) error {
	_, err := s.db.Exec(`DELETE FROM ip_tags WHERE id = ?`, id)
	return err
}

type PortMappingRecord struct {
	Port    uint16 `json:"port"`
	Service string `json:"service"`
}

func (s *Store) ListPortMappings() ([]PortMappingRecord, error) {
	rows, err := s.db.Query(`SELECT port, service FROM port_mappings ORDER BY port`)
	if err != nil {
		return nil, fmt.Errorf("query port mappings: %w", err)
	}
	defer rows.Close()

	out := []PortMappingRecord{}
	for rows.Next() {
		var m PortMappingRecord
		if err := rows.Scan(&m.Port, &m.Service); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) QueryPortMappings(page, pageSize int, q string) ([]PortMappingRecord, int, error) {
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = 50
	}

	if q == "" {
		var total int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM port_mappings`).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count port mappings: %w", err)
		}
		rows, err := s.db.Query(`SELECT port, service FROM port_mappings ORDER BY port LIMIT ? OFFSET ?`, pageSize, page*pageSize)
		if err != nil {
			return nil, 0, fmt.Errorf("query port mappings: %w", err)
		}
		defer rows.Close()
		var out []PortMappingRecord
		for rows.Next() {
			var m PortMappingRecord
			if err := rows.Scan(&m.Port, &m.Service); err != nil {
				return nil, 0, err
			}
			out = append(out, m)
		}
		return out, total, rows.Err()
	}

	rows, err := s.db.Query(`SELECT port, service FROM port_mappings ORDER BY port`)
	if err != nil {
		return nil, 0, fmt.Errorf("query port mappings: %w", err)
	}
	defer rows.Close()
	needle := strings.ToLower(q)
	var all []PortMappingRecord
	for rows.Next() {
		var m PortMappingRecord
		if err := rows.Scan(&m.Port, &m.Service); err != nil {
			return nil, 0, err
		}
		if strings.Contains(strconv.Itoa(int(m.Port)), needle) || strings.Contains(strings.ToLower(m.Service), needle) {
			all = append(all, m)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return paginateSlice(all, page, pageSize), len(all), nil
}

func (s *Store) CreatePortMapping(port uint16, service string) (PortMappingRecord, error) {
	if _, err := s.db.Exec(`INSERT INTO port_mappings(port, service) VALUES (?, ?)`, port, service); err != nil {
		return PortMappingRecord{}, fmt.Errorf("insert port mapping: %w", err)
	}
	return PortMappingRecord{Port: port, Service: service}, nil
}

func (s *Store) UpdatePortMapping(port uint16, service string) (PortMappingRecord, error) {
	res, err := s.db.Exec(`UPDATE port_mappings SET service = ? WHERE port = ?`, service, port)
	if err != nil {
		return PortMappingRecord{}, fmt.Errorf("update port mapping: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return PortMappingRecord{}, fmt.Errorf("port %d not found", port)
	}
	return PortMappingRecord{Port: port, Service: service}, nil
}

func (s *Store) DeletePortMapping(port uint16) error {
	_, err := s.db.Exec(`DELETE FROM port_mappings WHERE port = ?`, port)
	return err
}

type MCPServerRecord struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Transport    string `json:"transport"`
	Endpoint     string `json:"endpoint,omitempty"`
	Command      string `json:"command,omitempty"`
	Args         string `json:"args,omitempty"`
	Enabled      bool   `json:"enabled"`
	AuthType     string `json:"authType"`
	AuthUsername string `json:"authUsername,omitempty"`
	AuthPassword string `json:"authPassword,omitempty"`
	AuthToken    string `json:"authToken,omitempty"`
}

func (s *Store) ListMCPServers() ([]MCPServerRecord, error) {
	rows, err := s.db.Query(`SELECT id, name, transport, endpoint, command, args, enabled, auth_type, auth_username, auth_password, auth_token FROM mcp_servers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query mcp servers: %w", err)
	}
	defer rows.Close()

	out := []MCPServerRecord{}
	for rows.Next() {
		var m MCPServerRecord
		var enabled int
		if err := rows.Scan(&m.ID, &m.Name, &m.Transport, &m.Endpoint, &m.Command, &m.Args, &enabled, &m.AuthType, &m.AuthUsername, &m.AuthPassword, &m.AuthToken); err != nil {
			return nil, err
		}
		m.Enabled = enabled != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CreateMCPServer(name, transport, endpoint, command, args string, enabled bool, authType, authUsername, authPassword, authToken string) (MCPServerRecord, error) {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	res, err := s.db.Exec(`INSERT INTO mcp_servers(name, transport, endpoint, command, args, enabled, auth_type, auth_username, auth_password, auth_token, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name, transport, endpoint, command, args, enabledInt, authType, authUsername, authPassword, authToken, time.Now().Unix())
	if err != nil {
		return MCPServerRecord{}, fmt.Errorf("insert mcp server: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return MCPServerRecord{}, err
	}
	return MCPServerRecord{ID: int(id), Name: name, Transport: transport, Endpoint: endpoint, Command: command, Args: args, Enabled: enabled, AuthType: authType, AuthUsername: authUsername, AuthPassword: authPassword, AuthToken: authToken}, nil
}

func (s *Store) UpdateMCPServer(id int, name, endpoint, command, args string, enabled bool, authType, authUsername, authPassword, authToken string) (MCPServerRecord, error) {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	if _, err := s.db.Exec(`UPDATE mcp_servers SET name = ?, endpoint = ?, command = ?, args = ?, enabled = ?, auth_type = ?, auth_username = ?, auth_password = ?, auth_token = ? WHERE id = ?`,
		name, endpoint, command, args, enabledInt, authType, authUsername, authPassword, authToken, id); err != nil {
		return MCPServerRecord{}, fmt.Errorf("update mcp server: %w", err)
	}
	var m MCPServerRecord
	var enabledOut int
	row := s.db.QueryRow(`SELECT id, name, transport, endpoint, command, args, enabled, auth_type, auth_username, auth_password, auth_token FROM mcp_servers WHERE id = ?`, id)
	if err := row.Scan(&m.ID, &m.Name, &m.Transport, &m.Endpoint, &m.Command, &m.Args, &enabledOut, &m.AuthType, &m.AuthUsername, &m.AuthPassword, &m.AuthToken); err != nil {
		return MCPServerRecord{}, fmt.Errorf("read back updated mcp server: %w", err)
	}
	m.Enabled = enabledOut != 0
	return m, nil
}

func (s *Store) DeleteMCPServer(id int) error {
	_, err := s.db.Exec(`DELETE FROM mcp_servers WHERE id = ?`, id)
	return err
}

type ChatSession struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ChatMessage struct {
	ID               int       `json:"id"`
	Role             string    `json:"role"`
	Content          string    `json:"content"`
	ToolCalls        []string  `json:"toolCalls,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	Model            string    `json:"model,omitempty"`
	ElapsedMs        int64     `json:"elapsedMs,omitempty"`
	PromptTokens     int       `json:"promptTokens,omitempty"`
	CompletionTokens int       `json:"completionTokens,omitempty"`
}

func (s *Store) CreateChatSession(userID int) (ChatSession, error) {
	now := time.Now()
	res, err := s.db.Exec(`INSERT INTO chat_sessions(user_id, title, created_at, updated_at) VALUES (?, '', ?, ?)`,
		userID, now.Unix(), now.Unix())
	if err != nil {
		return ChatSession{}, fmt.Errorf("insert chat session: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ChatSession{}, err
	}
	return ChatSession{ID: int(id), Title: "", CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) ListChatSessions(userID int) ([]ChatSession, error) {
	rows, err := s.db.Query(`SELECT id, title, created_at, updated_at FROM chat_sessions WHERE user_id = ? ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("query chat sessions: %w", err)
	}
	defer rows.Close()
	out := []ChatSession{}
	for rows.Next() {
		var cs ChatSession
		var createdAt, updatedAt int64
		if err := rows.Scan(&cs.ID, &cs.Title, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		cs.CreatedAt = time.Unix(createdAt, 0)
		cs.UpdatedAt = time.Unix(updatedAt, 0)
		out = append(out, cs)
	}
	return out, rows.Err()
}

func (s *Store) ChatSessionOwner(sessionID int) (userID int, ok bool, err error) {
	row := s.db.QueryRow(`SELECT user_id FROM chat_sessions WHERE id = ?`, sessionID)
	if err := row.Scan(&userID); err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, err
	}
	return userID, true, nil
}

func (s *Store) DeleteChatSession(sessionID int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM chat_messages WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete chat messages: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM chat_sessions WHERE id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete chat session: %w", err)
	}
	return tx.Commit()
}

func (s *Store) TouchChatSession(sessionID int, firstUserMessage string) error {
	const maxTitleRunes = 40

	title := firstUserMessage
	runes := []rune(title)
	if len(runes) > maxTitleRunes {
		title = string(runes[:maxTitleRunes]) + "..."
	}
	_, err := s.db.Exec(`UPDATE chat_sessions SET updated_at = ?, title = CASE WHEN title = '' THEN ? ELSE title END WHERE id = ?`,
		time.Now().Unix(), title, sessionID)
	return err
}

func (s *Store) AppendChatMessage(sessionID int, role, content string, toolCalls []string, model string, elapsedMs int64, promptTokens, completionTokens int) (ChatMessage, error) {
	toolCallsJSON, err := json.Marshal(toolCalls)
	if err != nil {
		return ChatMessage{}, fmt.Errorf("marshal tool calls: %w", err)
	}
	now := time.Now()
	res, err := s.db.Exec(`INSERT INTO chat_messages(session_id, role, content, tool_calls, created_at, model, elapsed_ms, prompt_tokens, completion_tokens)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, role, content, string(toolCallsJSON), now.Unix(), model, elapsedMs, promptTokens, completionTokens)
	if err != nil {
		return ChatMessage{}, fmt.Errorf("insert chat message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ChatMessage{}, err
	}
	return ChatMessage{
		ID: int(id), Role: role, Content: content, ToolCalls: toolCalls, CreatedAt: now,
		Model: model, ElapsedMs: elapsedMs, PromptTokens: promptTokens, CompletionTokens: completionTokens,
	}, nil
}

func (s *Store) ListChatMessages(sessionID int) ([]ChatMessage, error) {
	rows, err := s.db.Query(`SELECT id, role, content, tool_calls, created_at, model, elapsed_ms, prompt_tokens, completion_tokens
		FROM chat_messages WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query chat messages: %w", err)
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var cm ChatMessage
		var toolCallsJSON string
		var createdAt int64
		if err := rows.Scan(&cm.ID, &cm.Role, &cm.Content, &toolCallsJSON, &createdAt,
			&cm.Model, &cm.ElapsedMs, &cm.PromptTokens, &cm.CompletionTokens); err != nil {
			return nil, err
		}
		if toolCallsJSON != "" {
			if err := json.Unmarshal([]byte(toolCallsJSON), &cm.ToolCalls); err != nil {
				return nil, fmt.Errorf("unmarshal tool calls: %w", err)
			}
		}
		cm.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, cm)
	}
	return out, rows.Err()
}

func (s *Store) EnsureAuthSecret() ([]byte, error) {
	var secret []byte
	err := s.db.QueryRow(`SELECT secret FROM auth_secret WHERE id = 1`).Scan(&secret)
	if err == nil {
		return secret, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	secret = make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate auth secret: %w", err)
	}
	if _, err := s.db.Exec(`INSERT INTO auth_secret(id, secret) VALUES (1, ?)`, secret); err != nil {
		return nil, fmt.Errorf("store auth secret: %w", err)
	}
	return secret, nil
}

type User struct {
	ID           int
	Username     string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
	Description  string
	LongLived    bool
}

type BootstrapResult struct {
	AdminUsername     string
	AdminPassword     string
	DashboardUsername string
	DashboardPassword string
}

func (s *Store) BootstrapAdminIfEmpty() (result BootstrapResult, created bool, err error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return BootstrapResult{}, false, err
	}
	if count > 0 {
		return BootstrapResult{}, false, nil
	}

	adminPassword := generateRandomPassword()
	adminHash, err := hashPassword(adminPassword)
	if err != nil {
		return BootstrapResult{}, false, fmt.Errorf("hash bootstrap admin password: %w", err)
	}
	if _, err := s.db.Exec(`INSERT INTO users(username, password_hash, role, created_at) VALUES (?, ?, 'admin', ?)`,
		"admin", adminHash, time.Now().Unix()); err != nil {
		return BootstrapResult{}, false, fmt.Errorf("insert bootstrap admin: %w", err)
	}

	dashboardPassword := generateRandomPassword()
	dashboardHash, err := hashPassword(dashboardPassword)
	if err != nil {
		return BootstrapResult{}, false, fmt.Errorf("hash bootstrap dashboard password: %w", err)
	}
	if _, err := s.db.Exec(`INSERT INTO users(username, password_hash, role, created_at, description, long_lived) VALUES (?, ?, 'normal', ?, ?, 1)`,
		"dashboard", dashboardHash, time.Now().Unix(), "自动创建，专用于大屏投放/自动登录场景，会话长期有效"); err != nil {
		return BootstrapResult{}, false, fmt.Errorf("insert bootstrap dashboard account: %w", err)
	}

	return BootstrapResult{
		AdminUsername:     "admin",
		AdminPassword:     adminPassword,
		DashboardUsername: "dashboard",
		DashboardPassword: dashboardPassword,
	}, true, nil
}

func scanUserRow(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var createdAt int64
	var longLived int
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &createdAt, &u.Description, &longLived); err != nil {
		return User{}, err
	}
	u.CreatedAt = time.Unix(createdAt, 0)
	u.LongLived = longLived != 0
	return u, nil
}

func (s *Store) GetUserByUsername(username string) (User, error) {
	row := s.db.QueryRow(`SELECT id, username, password_hash, role, created_at, description, long_lived FROM users WHERE username = ?`, username)
	return scanUserRow(row)
}

func (s *Store) GetUserByID(id int) (User, error) {
	row := s.db.QueryRow(`SELECT id, username, password_hash, role, created_at, description, long_lived FROM users WHERE id = ?`, id)
	return scanUserRow(row)
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, password_hash, role, created_at, description, long_lived FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CreateUser(username, passwordHash, role, description string, longLived bool) (User, error) {
	longLivedInt := 0
	if longLived {
		longLivedInt = 1
	}
	res, err := s.db.Exec(`INSERT INTO users(username, password_hash, role, created_at, description, long_lived) VALUES (?, ?, ?, ?, ?, ?)`,
		username, passwordHash, role, time.Now().Unix(), description, longLivedInt)
	if err != nil {
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return s.GetUserByID(int(id))
}

func (s *Store) UpdateUser(id int, role, newPasswordHash, description string, longLived bool) error {
	longLivedInt := 0
	if longLived {
		longLivedInt = 1
	}
	if newPasswordHash != "" {
		_, err := s.db.Exec(`UPDATE users SET role = ?, password_hash = ?, description = ?, long_lived = ? WHERE id = ?`,
			role, newPasswordHash, description, longLivedInt, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE users SET role = ?, description = ?, long_lived = ? WHERE id = ?`, role, description, longLivedInt, id)
	return err
}

func (s *Store) DeleteUser(id int) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *Store) Enqueue(snap tickSnapshot) {
	if s == nil {
		return
	}
	select {
	case s.writeCh <- snap:
	default:
		log.Printf("store: write queue full, dropping tick %s", snap.start.Format(time.RFC3339))
	}
}

func (s *Store) writeLoop() {
	for {
		select {
		case snap := <-s.writeCh:
			if err := s.writeTick(snap); err != nil {
				log.Printf("store: write tick failed: %v", err)
			}
		case <-s.closeCh:
			return
		}
	}
}

func (s *Store) writeTick(snap tickSnapshot) error {
	ts := snap.start.Unix()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	protoBytesJSON, err := marshalProtoMap(snap.protoBytes)
	if err != nil {
		return fmt.Errorf("marshal protoBytes: %w", err)
	}
	protoPacketsJSON, err := marshalProtoMap(snap.protoPackets)
	if err != nil {
		return fmt.Errorf("marshal protoPackets: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO bucket_summary(ts, distinct_flow_count, proto_bytes, proto_packets) VALUES (?, ?, ?, ?)`,
		ts, snap.distinctFlowCount, protoBytesJSON, protoPacketsJSON); err != nil {
		return fmt.Errorf("insert bucket_summary: %w", err)
	}

	if len(snap.scanAlerts) > 0 {
		alertStmt, err := tx.Prepare(`INSERT INTO threat_alerts(ts, kind, ip, distinct_peers, volume_bytes) VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer alertStmt.Close()
		for _, a := range snap.scanAlerts {
			ip, err := ipToUint32(a.IP)
			if err != nil {
				continue
			}
			if _, err := alertStmt.Exec(ts, string(a.Kind), ip, a.DistinctPeers, a.VolumeBytes); err != nil {
				return fmt.Errorf("insert threat_alerts: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bucket_summary/threat_alerts: %w", err)
	}

	if err := s.ts.writeTick(ts, snap.flows, snap.ips, snap.ports); err != nil {
		return fmt.Errorf("write time-series tick: %w", err)
	}
	return nil
}

func marshalProtoMap(m map[uint8]uint64) (string, error) {
	strMap := make(map[string]uint64, len(m))
	for k, v := range m {
		strMap[strconv.Itoa(int(k))] = v
	}
	b, err := json.Marshal(strMap)
	return string(b), err
}

func unmarshalProtoMap(s string) (map[uint8]uint64, error) {
	var strMap map[string]uint64
	if err := json.Unmarshal([]byte(s), &strMap); err != nil {
		return nil, err
	}
	out := make(map[uint8]uint64, len(strMap))
	for k, v := range strMap {
		n, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		out[uint8(n)] = v
	}
	return out, nil
}

func ipToUint32(s string) (uint32, error) {
	ip := net.ParseIP(s)
	if ip == nil {
		return 0, fmt.Errorf("invalid IP %q", s)
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return 0, fmt.Errorf("not an IPv4 address: %q", s)
	}
	return binary.LittleEndian.Uint32(ip4), nil
}

func (s *Store) retentionLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	s.pruneOnce()
	for {
		select {
		case <-ticker.C:
			s.pruneOnce()
		case <-s.closeCh:
			return
		}
	}
}

func (s *Store) pruneOnce() {
	cutoff := time.Now().Add(-s.retention).Unix()

	tables := []string{"bucket_summary", "threat_alerts"}
	if existing, err := tableNames(s.db); err == nil {
		for _, legacy := range []string{"flow_samples", "ip_samples", "port_samples"} {
			if existing[legacy] {
				tables = append(tables, legacy)
			}
		}
	}
	for _, t := range tables {
		if _, err := s.db.Exec(`DELETE FROM `+t+` WHERE ts < ?`, cutoff); err != nil {
			log.Printf("store: prune %s failed: %v", t, err)
		}
	}
	if _, err := s.db.Exec(`PRAGMA incremental_vacuum`); err != nil {
		log.Printf("store: incremental_vacuum failed: %v", err)
	}
	s.dropEmptyLegacySampleTablesOnce()

	if err := s.ts.prune(time.Now().Add(-s.retention)); err != nil {
		log.Printf("store: prune time-series store failed: %v", err)
	}
}

// dropEmptyLegacySampleTablesOnce reclaims the old SQLite
// flow_samples/ip_samples/port_samples tables' space once their rows have
// fully aged out under the retention cutoff above -- these tables predate
// the DuckDB-backed tsStore and are no longer written to or queried, they
// just need to finish draining once per existing installation.
func (s *Store) dropEmptyLegacySampleTablesOnce() {
	for _, t := range []string{"flow_samples", "ip_samples", "port_samples"} {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + t).Scan(&count); err != nil {
			continue
		}
		if count > 0 {
			continue
		}
		log.Printf("store: legacy %s table has fully aged out, dropping it to reclaim space", t)
		if _, err := s.db.Exec(`DROP TABLE ` + t); err != nil {
			log.Printf("store: drop legacy table %s failed: %v", t, err)
			continue
		}
		if _, err := s.db.Exec(`VACUUM`); err != nil {
			log.Printf("store: vacuum after dropping %s failed: %v", t, err)
		}
	}
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	close(s.closeCh)
	if s.ts != nil {
		if err := s.ts.Close(); err != nil {
			log.Printf("store: close time-series store: %v", err)
		}
	}
	return s.db.Close()
}

func (s *Store) queryBucketSummary(cutoff, until int64) (protoBytes, protoPackets map[uint8]uint64, latestDistinctFlowCount int, err error) {
	protoBytes = map[uint8]uint64{}
	protoPackets = map[uint8]uint64{}
	rows, err := s.db.Query(`SELECT proto_bytes, proto_packets, distinct_flow_count FROM bucket_summary WHERE ts >= ? AND ts <= ? ORDER BY ts DESC`, cutoff, until)
	if err != nil {
		return nil, nil, 0, err
	}
	defer rows.Close()
	first := true
	for rows.Next() {
		var pbJSON, ppJSON string
		var dfc int
		if err := rows.Scan(&pbJSON, &ppJSON, &dfc); err != nil {
			return nil, nil, 0, err
		}
		if first {
			latestDistinctFlowCount = dfc
			first = false
		}
		pb, err := unmarshalProtoMap(pbJSON)
		if err != nil {
			return nil, nil, 0, err
		}
		pp, err := unmarshalProtoMap(ppJSON)
		if err != nil {
			return nil, nil, 0, err
		}
		for k, v := range pb {
			protoBytes[k] += v
		}
		for k, v := range pp {
			protoPackets[k] += v
		}
	}
	return protoBytes, protoPackets, latestDistinctFlowCount, rows.Err()
}

func (s *Store) queryTopFlows(cutoff, until int64, limit int) ([]FlowStat, error) {
	rows, _, err := s.ts.queryFlowsLimited(cutoff, until, nil, limit, 0, false)
	if err != nil {
		return nil, fmt.Errorf("query flows: %w", err)
	}
	out := make([]FlowStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, FlowStat{
			SrcIP: ipString(r.Key.SrcIP), SrcPort: r.Key.SrcPort,
			DstIP: ipString(r.Key.DstIP), DstPort: r.Key.DstPort,
			Proto: protoName(r.Key.Proto), Service: serviceName(r.Key.DstPort),
			Domain:  r.Domain,
			Packets: r.Packets, Bytes: r.Bytes,
		})
	}
	return out, nil
}

func (s *Store) queryTopIPs(cutoff, until int64, limit int) ([]IPStat, error) {
	rows, _, err := s.ts.queryIPsLimited(cutoff, until, limit, 0, false)
	if err != nil {
		return nil, fmt.Errorf("query ips: %w", err)
	}
	out := make([]IPStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, IPStat{IP: ipString(r.Key), Packets: r.Packets, Bytes: r.Bytes})
	}
	return out, nil
}

func (s *Store) queryTopPorts(cutoff, until int64, limit int) ([]PortStat, error) {
	rows, _, err := s.ts.queryPortsLimited(cutoff, until, limit, 0, false)
	if err != nil {
		return nil, fmt.Errorf("query ports: %w", err)
	}
	out := make([]PortStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, PortStat{Port: r.Key.Port, Proto: protoName(r.Key.Proto), Service: serviceName(r.Key.Port), Packets: r.Packets, Bytes: r.Bytes})
	}
	return out, nil
}

func (s *Store) QueryIPsPaged(from, to time.Time, page, pageSize int, filter string) ([]IPStat, int, error) {
	cutoff, upper := from.Unix(), to.Unix()
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = 50
	}

	var rows []aggRow[uint32]
	var total int
	var err error
	if filter == "" {
		rows, total, err = s.ts.queryIPsLimited(cutoff, upper, pageSize, page*pageSize, true)
	} else {
		// ip is a packed-integer column, can't push a substring LIKE down
		// to SQL -- pull everything, filter/sort/paginate in Go, same as
		// QueryThreatAlertsRange does for the same underlying reason.
		var all []aggRow[uint32]
		all, err = s.ts.queryIPs(cutoff, upper)
		if err == nil {
			var filtered []aggRow[uint32]
			for _, r := range all {
				if strings.Contains(ipString(r.Key), filter) {
					filtered = append(filtered, r)
				}
			}
			sort.Slice(filtered, func(i, j int) bool { return filtered[i].Bytes > filtered[j].Bytes })
			total = len(filtered)
			rows = limitOffsetSlice(filtered, pageSize, page*pageSize)
		}
	}
	if err != nil {
		return nil, 0, fmt.Errorf("query ips: %w", err)
	}

	out := make([]IPStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, IPStat{IP: ipString(r.Key), Packets: r.Packets, Bytes: r.Bytes})
	}
	return out, total, nil
}

func (s *Store) QueryPortsPaged(from, to time.Time, page, pageSize int, filter string) ([]PortStat, int, error) {
	cutoff, upper := from.Unix(), to.Unix()
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = 50
	}

	toStat := func(r aggRow[tsPortKey]) PortStat {
		return PortStat{Port: r.Key.Port, Proto: protoName(r.Key.Proto), Service: serviceName(r.Key.Port), Packets: r.Packets, Bytes: r.Bytes}
	}

	var pageRows []aggRow[tsPortKey]
	var total int
	var err error
	if filter == "" {
		pageRows, total, err = s.ts.queryPortsLimited(cutoff, upper, pageSize, page*pageSize, true)
	} else {
		// filter matches against the formatted "proto/port service" text,
		// not a column DuckDB can push a LIKE into directly -- pull
		// everything, filter/sort/paginate in Go.
		var all []aggRow[tsPortKey]
		all, err = s.ts.queryPorts(cutoff, upper)
		if err == nil {
			var filtered []aggRow[tsPortKey]
			needle := strings.ToLower(filter)
			for _, r := range all {
				ps := toStat(r)
				haystack := strings.ToLower(ps.Proto + "/" + strconv.Itoa(int(ps.Port)) + " " + ps.Service)
				if strings.Contains(haystack, needle) {
					filtered = append(filtered, r)
				}
			}
			sort.Slice(filtered, func(i, j int) bool { return filtered[i].Bytes > filtered[j].Bytes })
			total = len(filtered)
			pageRows = limitOffsetSlice(filtered, pageSize, page*pageSize)
		}
	}
	if err != nil {
		return nil, 0, fmt.Errorf("query ports: %w", err)
	}

	out := make([]PortStat, 0, len(pageRows))
	for _, r := range pageRows {
		out = append(out, toStat(r))
	}
	return out, total, nil
}

func (s *Store) QueryServiceCategories(from, to time.Time) ([]CategoryStat, error) {
	cutoff, upper := from.Unix(), to.Unix()
	rows, err := s.ts.queryPorts(cutoff, upper)
	if err != nil {
		return nil, fmt.Errorf("query service categories: %w", err)
	}

	portTotals := map[uint16]portAgg{}
	for _, r := range rows {
		agg := portTotals[r.Key.Port]
		agg.Packets += r.Packets
		agg.Bytes += r.Bytes
		portTotals[r.Key.Port] = agg
	}
	return buildCategoryStats(portTotals), nil
}

func paginateSlice[T any](items []T, page, pageSize int) []T {
	start := page * pageSize
	if start >= len(items) {
		return nil
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func (s *Store) queryTopDomains(cutoff, until int64, limit int) ([]DomainStat, error) {
	rows, _, err := s.ts.queryDomainsLimited(cutoff, until, "", limit, 0, false)
	if err != nil {
		return nil, fmt.Errorf("query domains: %w", err)
	}
	out := make([]DomainStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, DomainStat{Domain: r.Key, Packets: r.Packets, Bytes: r.Bytes})
	}
	return out, nil
}

func (s *Store) QueryDomainsPaged(from, to time.Time, page, pageSize int, filter string) ([]DomainStat, int, error) {
	cutoff, upper := from.Unix(), to.Unix()
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = 50
	}

	rows, total, err := s.ts.queryDomainsLimited(cutoff, upper, filter, pageSize, page*pageSize, true)
	if err != nil {
		return nil, 0, fmt.Errorf("query domains: %w", err)
	}
	out := make([]DomainStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, DomainStat{Domain: r.Key, Packets: r.Packets, Bytes: r.Bytes})
	}
	return out, total, nil
}

func (s *Store) QueryReportRange(from, to time.Time, limit int) (Report, error) {
	cutoff, until := from.Unix(), to.Unix()

	protoBytes, protoPackets, latestDistinctFlowCount, err := s.queryBucketSummary(cutoff, until)
	if err != nil {
		return Report{}, fmt.Errorf("query bucket summary: %w", err)
	}
	var totalPackets, totalBytes uint64
	for _, v := range protoPackets {
		totalPackets += v
	}
	for _, v := range protoBytes {
		totalBytes += v
	}

	topFlows, err := s.queryTopFlows(cutoff, until, limit)
	if err != nil {
		return Report{}, fmt.Errorf("query top flows: %w", err)
	}
	topIPs, err := s.queryTopIPs(cutoff, until, limit)
	if err != nil {
		return Report{}, fmt.Errorf("query top ips: %w", err)
	}
	topPorts, err := s.queryTopPorts(cutoff, until, limit)
	if err != nil {
		return Report{}, fmt.Errorf("query top ports: %w", err)
	}
	topDomains, err := s.queryTopDomains(cutoff, until, limit)
	if err != nil {
		return Report{}, fmt.Errorf("query top domains: %w", err)
	}

	return Report{
		GeneratedAt:    time.Now(),
		ActiveFlowsNow: latestDistinctFlowCount,
		TotalPackets:   totalPackets,
		TotalBytes:     totalBytes,
		PossibleTicks:  0,
		Protocols:      rankProtocols(protoPackets, protoBytes),
		TopFlows:       topFlows,
		TopIPs:         topIPs,
		TopPorts:       topPorts,
		TopDomains:     topDomains,
	}, nil
}

func (s *Store) QueryTimeSeries(from, to time.Time) (Timeseries, error) {
	slotSize := time.Minute
	if to.Sub(from) > time.Hour {
		slotSize = time.Hour
	}
	cutoff, upper := from.Unix(), to.Unix()

	rows, err := s.db.Query(`SELECT ts, proto_bytes FROM bucket_summary WHERE ts >= ? AND ts <= ? ORDER BY ts`, cutoff, upper)
	if err != nil {
		return Timeseries{}, err
	}
	defer rows.Close()

	slotBytes := map[time.Time]map[string]uint64{}
	var order []time.Time
	for rows.Next() {
		var ts int64
		var pbJSON string
		if err := rows.Scan(&ts, &pbJSON); err != nil {
			return Timeseries{}, err
		}
		pb, err := unmarshalProtoMap(pbJSON)
		if err != nil {
			return Timeseries{}, err
		}
		slot := time.Unix(ts, 0).Truncate(slotSize)
		byProto, ok := slotBytes[slot]
		if !ok {
			byProto = map[string]uint64{}
			slotBytes[slot] = byProto
			order = append(order, slot)
		}
		for proto, v := range pb {
			byProto[protoName(proto)] += v
		}
	}
	if err := rows.Err(); err != nil {
		return Timeseries{}, err
	}

	sort.Slice(order, func(i, j int) bool { return order[i].Before(order[j]) })
	points := make([]TimeseriesPoint, 0, len(order))
	for _, slot := range order {
		points = append(points, TimeseriesPoint{Time: slot, Bytes: slotBytes[slot]})
	}
	return Timeseries{Points: points}, nil
}

// QueryFlowRate mirrors the math the in-memory aggregator used to do in
// flowRateRecent: distinct_flow_count is written to bucket_summary every
// tick, so the per-second rate between consecutive points is just a
// division over the real elapsed time between them.
func (s *Store) QueryFlowRate(from, to time.Time) (FlowRate, error) {
	cutoff, upper := from.Unix(), to.Unix()
	rows, err := s.db.Query(`SELECT ts, distinct_flow_count FROM bucket_summary WHERE ts >= ? AND ts <= ? ORDER BY ts`, cutoff, upper)
	if err != nil {
		return FlowRate{}, err
	}
	defer rows.Close()

	points := make([]FlowRatePoint, 0)
	var prevTime time.Time
	for rows.Next() {
		var ts int64
		var count int
		if err := rows.Scan(&ts, &count); err != nil {
			return FlowRate{}, err
		}
		t := time.Unix(ts, 0)
		perSec := 0.0
		if !prevTime.IsZero() {
			if elapsed := t.Sub(prevTime).Seconds(); elapsed > 0 {
				perSec = float64(count) / elapsed
			}
		}
		points = append(points, FlowRatePoint{Time: t, Count: count, PerSec: perSec})
		prevTime = t
	}
	if err := rows.Err(); err != nil {
		return FlowRate{}, err
	}
	return FlowRate{Points: points}, nil
}

// QueryTopIPsInRange is a thin wrapper around tsStore's already-validated
// bounded-top-K query, used by /api/geo to pick world-map candidate IPs.
func (s *Store) QueryTopIPsInRange(from, to time.Time, limit int) ([]IPStat, error) {
	rows, _, err := s.ts.queryIPsLimited(from.Unix(), to.Unix(), limit, 0, false)
	if err != nil {
		return nil, fmt.Errorf("query top ips: %w", err)
	}
	out := make([]IPStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, IPStat{IP: ipString(r.Key), Packets: r.Packets, Bytes: r.Bytes})
	}
	return out, nil
}

type FlowFilter struct {
	IP string
}

func (s *Store) QueryFlowsPaged(from, to time.Time, page, pageSize int, filter FlowFilter) ([]FlowStat, int, error) {
	cutoff, upper := from.Unix(), to.Unix()
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = 50
	}

	var ipFilter *uint32
	if filter.IP != "" {
		ipNum, err := ipToUint32(filter.IP)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid ip filter: %w", err)
		}
		ipFilter = &ipNum
	}

	rows, total, err := s.ts.queryFlowsLimited(cutoff, upper, ipFilter, pageSize, page*pageSize, true)
	if err != nil {
		return nil, 0, fmt.Errorf("query flows: %w", err)
	}
	out := make([]FlowStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, FlowStat{
			SrcIP: ipString(r.Key.SrcIP), SrcPort: r.Key.SrcPort,
			DstIP: ipString(r.Key.DstIP), DstPort: r.Key.DstPort,
			Proto: protoName(r.Key.Proto), Service: serviceName(r.Key.DstPort),
			Domain:  r.Domain,
			Packets: r.Packets, Bytes: r.Bytes,
		})
	}
	return out, total, nil
}

type ThreatAlertRecord struct {
	Time          time.Time `json:"time"`
	Kind          string    `json:"kind"`
	IP            string    `json:"ip"`
	Label         string    `json:"label,omitempty"`
	DistinctPeers int       `json:"distinctPeers,omitempty"`
	VolumeBytes   uint64    `json:"volumeBytes,omitempty"`
}

func (s *Store) QueryThreatAlerts(page, pageSize int, ipFilter, kind string) ([]ThreatAlertRecord, int, error) {
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = 50
	}

	where := "1=1"
	var args []any
	if kind != "" {
		where += " AND kind = ?"
		args = append(args, kind)
	}

	if ipFilter == "" {
		var total int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM threat_alerts WHERE `+where, args...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count threat alerts: %w", err)
		}
		q := `SELECT ts, kind, ip, distinct_peers, volume_bytes FROM threat_alerts WHERE ` + where + ` ORDER BY ts DESC LIMIT ? OFFSET ?`
		qArgs := append(append([]any{}, args...), pageSize, page*pageSize)
		rows, err := s.db.Query(q, qArgs...)
		if err != nil {
			return nil, 0, fmt.Errorf("query threat alerts: %w", err)
		}
		defer rows.Close()
		out, err := scanThreatAlertRows(rows)
		return out, total, err
	}

	q := `SELECT ts, kind, ip, distinct_peers, volume_bytes FROM threat_alerts WHERE ` + where + ` ORDER BY ts DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query threat alerts: %w", err)
	}
	defer rows.Close()
	all, err := scanThreatAlertRows(rows)
	if err != nil {
		return nil, 0, err
	}
	var matched []ThreatAlertRecord
	for _, a := range all {
		if strings.Contains(a.IP, ipFilter) {
			matched = append(matched, a)
		}
	}
	return paginateSlice(matched, page, pageSize), len(matched), nil
}

// QueryThreatAlertsRange's ip column is stored as an INTEGER (packed
// uint32, see threat_alerts' schema), so a fuzzy filter can't be pushed
// down as a SQL LIKE the way a TEXT column would -- same constraint and
// same fix as QueryIPsPaged: pull every matching row for the time range,
// filter by the string-formatted IP in Go, then paginate the filtered
// slice. Only taken when filter is non-empty; the empty-filter path stays
// on the cheap SQL-side COUNT+LIMIT/OFFSET query.
func (s *Store) QueryThreatAlertsRange(from, to time.Time, page, pageSize int, filter string) ([]ThreatAlertRecord, int, error) {
	cutoff, until := from.Unix(), to.Unix()

	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = 50
	}

	if filter == "" {
		var total int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM threat_alerts WHERE ts >= ? AND ts <= ?`, cutoff, until).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count threat alerts: %w", err)
		}
		rows, err := s.db.Query(`SELECT ts, kind, ip, distinct_peers, volume_bytes FROM threat_alerts WHERE ts >= ? AND ts <= ? ORDER BY ts DESC LIMIT ? OFFSET ?`,
			cutoff, until, pageSize, page*pageSize)
		if err != nil {
			return nil, 0, fmt.Errorf("query threat alerts: %w", err)
		}
		defer rows.Close()
		out, err := scanThreatAlertRows(rows)
		return out, total, err
	}

	rows, err := s.db.Query(`SELECT ts, kind, ip, distinct_peers, volume_bytes FROM threat_alerts WHERE ts >= ? AND ts <= ? ORDER BY ts DESC`, cutoff, until)
	if err != nil {
		return nil, 0, fmt.Errorf("query threat alerts: %w", err)
	}
	defer rows.Close()
	all, err := scanThreatAlertRows(rows)
	if err != nil {
		return nil, 0, err
	}
	var matched []ThreatAlertRecord
	for _, a := range all {
		if strings.Contains(a.IP, filter) {
			matched = append(matched, a)
		}
	}
	return paginateSlice(matched, page, pageSize), len(matched), nil
}

func scanThreatAlertRows(rows *sql.Rows) ([]ThreatAlertRecord, error) {
	var out []ThreatAlertRecord
	for rows.Next() {
		var ts int64
		var kind string
		var ip uint32
		var dp int
		var vb uint64
		if err := rows.Scan(&ts, &kind, &ip, &dp, &vb); err != nil {
			return nil, err
		}
		out = append(out, ThreatAlertRecord{Time: time.Unix(ts, 0), Kind: kind, IP: ipString(ip), DistinctPeers: dp, VolumeBytes: vb})
	}
	return out, rows.Err()
}
