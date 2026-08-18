package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	duckdb "github.com/marcboeker/go-duckdb"
)

const (
	tblFlow = "flow_samples"
	tblIP   = "ip_samples"
	tblPort = "port_samples"
)

// distributedTopKMargin bounds how many rows each source (sealed Parquet
// files, hot buffer) contributes when a query needs to merge across
// sources. Fetching each source's own top-(limit+offset+margin) instead
// of every matching row is an approximate "distributed top-K": exact for
// flow 5-tuples (a flow's ephemeral port makes the same key spanning two
// sources essentially impossible), a deliberate approximation for
// ip/port/domain (the same key legitimately CAN repeat across sources,
// e.g. a long-lived IP active both before and after a period boundary) --
// traded for avoiding pulling the full row set into Go, which was the
// actual production slowdown this was built to fix.
const distributedTopKMargin = 3000

// maxFetchK bounds how deep any *Limited query is allowed to sort ("jump
// to last page" turns into a large offset, which turns directly into a
// large fetchK, which forces an almost-unbounded ORDER BY over the whole
// source instead of the intended bounded top-K/heap-sort). Confirmed in
// production: offset near 12.5M on flow_samples produced fetchK≈20M and
// an 83s query. The frontend now keeps its own pagination sequential-only
// for the one view (Flows) where this is reachable through the UI, but
// this is the backend-side guarantee for any caller (a crafted URL/API
// request) that bypasses that -- rejected fast with a clear error instead
// of silently running the expensive query. Generous enough that no
// realistic click-through-every-page session would ever hit it.
const maxFetchK = 50_000

var errFetchTooDeep = errors.New("requested page is too far into the result set for this range; narrow the time range or filter, or page forward sequentially")

var tsTables = []string{tblFlow, tblIP, tblPort}

var tsTableDDL = map[string]string{
	tblFlow: `CREATE TABLE flow_samples (ts BIGINT, src_ip BIGINT, dst_ip BIGINT, src_port BIGINT, dst_port BIGINT, proto BIGINT, packets BIGINT, bytes BIGINT, domain VARCHAR)`,
	tblIP:   `CREATE TABLE ip_samples (ts BIGINT, ip BIGINT, packets BIGINT, bytes BIGINT)`,
	tblPort: `CREATE TABLE port_samples (ts BIGINT, proto BIGINT, port BIGINT, packets BIGINT, bytes BIGINT)`,
}

// tsStore holds the high-volume flow_samples/ip_samples/port_samples
// tables in DuckDB instead of SQLite: a small "hot" buffer for the
// current period is written via the Appender API, then sealed out to
// Parquet files at each period boundary. Retention removes whole sealed
// files (real OS-level deletion) instead of relying on DuckDB's
// DELETE/VACUUM, which does not reclaim space within a shared database
// file (confirmed by benchmarking -- see project memory).
type tsStore struct {
	dir    string
	period time.Duration

	mu          sync.RWMutex
	periodStart time.Time
	hotDB       *sql.DB
	hotConn     *sql.Conn
	appenders   map[string]*duckdb.Appender

	totalMu    sync.Mutex
	totalCache map[string]totalCacheEntry

	flowFileMu    sync.Mutex
	flowFileCache map[string]flowFileCacheEntry
}

// totalCountCacheTTL bounds how long a *Limited query's wantTotal count is
// reused across calls with the same [cutoff,until,filter]. Paging through
// the same range/filter (the common case: a user clicking through result
// pages) would otherwise re-run a full GROUP BY scan over every sealed
// Parquet file on every single page turn, even though the total can't have
// changed. A short TTL trades a little staleness (new data written or aged
// out mid-window) for skipping that duplicate scan.
const totalCountCacheTTL = 30 * time.Second

type totalCacheEntry struct {
	total    int
	computed time.Time
}

// cachedTotal returns the cached total for key if still fresh, otherwise
// calls compute, caches, and returns the fresh result.
func (t *tsStore) cachedTotal(key string, compute func() (int, error)) (int, error) {
	t.totalMu.Lock()
	if e, ok := t.totalCache[key]; ok && time.Since(e.computed) < totalCountCacheTTL {
		t.totalMu.Unlock()
		return e.total, nil
	}
	t.totalMu.Unlock()

	total, err := compute()
	if err != nil {
		return 0, err
	}

	t.totalMu.Lock()
	t.totalCache[key] = totalCacheEntry{total: total, computed: time.Now()}
	t.totalMu.Unlock()
	return total, nil
}

// totalCacheBucketSeconds buckets cutoff/until into the cache key instead of
// matching them exactly. A "last N days" window is re-resolved to an
// absolute [now-N days, now] pair on every single request (see parseRange),
// so cutoff/until drift by a few seconds on every request even when the
// user is just paging through the same nominal range -- an exact-match key
// would never hit the cache at all. Bucketing to <=totalCountCacheTTL keeps
// same-nominal-range requests landing in the same bucket while still
// expiring within one TTL window.
const totalCacheBucketSeconds = 15

func flowTotalCacheKey(cutoff, until int64, ipFilter *uint32) string {
	ipKey := "nil"
	if ipFilter != nil {
		ipKey = strconv.FormatUint(uint64(*ipFilter), 10)
	}
	return fmt.Sprintf("flow|%d|%d|%s", cutoff/totalCacheBucketSeconds, until/totalCacheBucketSeconds, ipKey)
}

var duckDBMemoryLimitOnce sync.Once
var duckDBMemoryLimitPragma string

// duckDBMemoryLimit returns the "<N>MB" PRAGMA memory_limit value applied
// to every DuckDB connection this process opens: 80% of the host's
// available memory at startup (read once from /proc/meminfo, cached for
// the process lifetime). This is a safety net, not a performance feature
// -- without it, a query or seal-time aggregation over a large enough
// dataset grows DuckDB's working set unbounded and can take the whole
// process down (confirmed by benchmarking a 500M-row aggregation with no
// limit set: it forced disk spill and the process was killed). Falls back
// to no limit (empty string, DuckDB's own default) if /proc/meminfo can't
// be read.
func duckDBMemoryLimit() string {
	duckDBMemoryLimitOnce.Do(func() {
		available, err := readMemAvailableBytes()
		if err != nil || available <= 0 {
			log.Printf("tsstore: could not detect available system memory, DuckDB memory_limit left unset: %v", err)
			return
		}
		budgetMB := available * 8 / 10 / (1024 * 1024)
		duckDBMemoryLimitPragma = fmt.Sprintf("%dMB", budgetMB)
		log.Printf("tsstore: DuckDB memory_limit set to %s (80%% of %dMB detected available)", duckDBMemoryLimitPragma, available/(1024*1024))
	})
	return duckDBMemoryLimitPragma
}

func readMemAvailableBytes() (int64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("malformed MemAvailable line: %q", line)
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return kb * 1024, nil
	}
	return 0, fmt.Errorf("MemAvailable not found in /proc/meminfo")
}

// applyMemoryLimit runs the memory_limit pragma against db, logging (not
// failing) on error since a missing pragma should degrade to "no limit",
// not break startup/queries outright.
func applyMemoryLimit(db *sql.DB) {
	limit := duckDBMemoryLimit()
	if limit == "" {
		return
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA memory_limit='%s'`, limit)); err != nil {
		log.Printf("tsstore: failed to apply memory_limit pragma: %v", err)
	}
}

// openScratchDuckDB opens a short-lived in-memory DuckDB connection with
// the process-wide memory_limit pragma applied. Every ad-hoc scratch
// connection used to query sealed Parquet files should go through this
// instead of a bare sql.Open, so none of them can grow unbounded.
func openScratchDuckDB() (*sql.DB, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, err
	}
	applyMemoryLimit(db)
	return db, nil
}

// flowFileCacheCap bounds how many rows (top-N by bytes) get cached per
// sealed flow_samples file. Once a file is sealed it's immutable, so its
// cached top-N never needs to change -- only new files ever add a cache
// entry. A query can only trust the cache for a file when its own
// fetchK (see distributedTopKMargin) fits within this cap; deeper
// pagination than that falls back to a live query for that file, same as
// today's behavior, just scoped to fewer files.
const flowFileCacheCap = 10000

type flowFileCacheEntry struct {
	start, end int64
	rows       []flowAggRow
	total      int
}

// computeFlowFileTopK runs the same GROUP BY .. ORDER BY .. LIMIT query
// used elsewhere in this file, but scoped to a single sealed Parquet file,
// plus a separate exact distinct-group COUNT for that same file. Both are
// safe to cache indefinitely once computed, since a sealed file's content
// never changes.
func computeFlowFileTopK(path string, start, end int64) (flowFileCacheEntry, error) {
	db, err := openScratchDuckDB()
	if err != nil {
		return flowFileCacheEntry{}, fmt.Errorf("open scratch duckdb: %w", err)
	}
	defer db.Close()

	source := "read_parquet('" + strings.ReplaceAll(filepath.ToSlash(path), "'", "''") + "')"
	q := fmt.Sprintf(`SELECT src_ip, src_port, dst_ip, dst_port, proto, SUM(packets), SUM(bytes), MAX(domain)
		FROM %s GROUP BY src_ip, src_port, dst_ip, dst_port, proto
		ORDER BY SUM(bytes) DESC LIMIT ?`, source)
	rows, err := db.Query(q, flowFileCacheCap)
	if err != nil {
		return flowFileCacheEntry{}, fmt.Errorf("query file top-k: %w", err)
	}
	var out []flowAggRow
	for rows.Next() {
		var srcIP, srcPort, dstIP, dstPort, proto, packets, bytes int64
		var domain sql.NullString
		if err := rows.Scan(&srcIP, &srcPort, &dstIP, &dstPort, &proto, &packets, &bytes, &domain); err != nil {
			rows.Close()
			return flowFileCacheEntry{}, err
		}
		out = append(out, flowAggRow{
			Key: flowKey{
				SrcIP: uint32(srcIP), DstIP: uint32(dstIP),
				SrcPort: uint16(srcPort), DstPort: uint16(dstPort),
				Proto: uint8(proto),
			},
			Packets: uint64(packets), Bytes: uint64(bytes), Domain: domain.String,
		})
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return flowFileCacheEntry{}, rowsErr
	}

	var total int
	countQ := fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT 1 FROM %s GROUP BY src_ip, src_port, dst_ip, dst_port, proto)`, source)
	if err := db.QueryRow(countQ).Scan(&total); err != nil {
		return flowFileCacheEntry{}, fmt.Errorf("count file groups: %w", err)
	}

	return flowFileCacheEntry{start: start, end: end, rows: out, total: total}, nil
}

// getFlowFileTopK returns the cached top-K/count for a sealed flow_samples
// file, computing and caching it on first use if not already present
// (e.g. right after a restart, before sealAndRotate's eager population has
// run for older files). Concurrent callers racing to compute the same
// uncached file will just redo the (bounded, single-file) work twice
// rather than block each other -- cheap enough not to need a singleflight.
func (t *tsStore) getFlowFileTopK(path string, start, end int64) (flowFileCacheEntry, error) {
	t.flowFileMu.Lock()
	if e, ok := t.flowFileCache[path]; ok {
		t.flowFileMu.Unlock()
		return e, nil
	}
	t.flowFileMu.Unlock()

	entry, err := computeFlowFileTopK(path, start, end)
	if err != nil {
		return flowFileCacheEntry{}, err
	}

	t.flowFileMu.Lock()
	t.flowFileCache[path] = entry
	t.flowFileMu.Unlock()
	return entry, nil
}

// warmFlowFileCache eagerly builds the per-file Top-K cache for every
// flow_samples sealed file already on disk, run once in the background
// right after startup. Without this, a fresh restart leaves every
// pre-existing file's cache cold, and the FIRST query touching a wide
// historical range pays the full rebuild cost inline -- confirmed in
// production: 142 pre-existing files, 42s on the first 15-day query
// (~300ms/file, built one at a time, sequentially, inside that request).
// sealAndRotate's own eager build only covers files sealed AFTER this
// process started, so it never helps with files that already existed at
// startup -- this is the other half of that mechanism. Concurrency is
// bounded so warmup itself doesn't become a resource spike stacked on top
// of live traffic.
func (t *tsStore) warmFlowFileCache() {
	dir := filepath.Join(t.dir, "sealed", tblFlow)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("tsstore: flow file cache warmup: read sealed dir failed: %v", err)
		}
		return
	}

	type warmJob struct {
		path       string
		start, end int64
	}
	periodSec := int64(t.period / time.Second)
	var jobs []warmJob
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		start, end, ok := parseSealedRange(e.Name(), periodSec)
		if !ok {
			continue
		}
		jobs = append(jobs, warmJob{path: filepath.Join(dir, e.Name()), start: start, end: end})
	}
	if len(jobs) == 0 {
		return
	}

	concurrency := runtime.NumCPU()
	if concurrency > 8 {
		concurrency = 8
	}
	if concurrency < 1 {
		concurrency = 1
	}

	warmStart := time.Now()
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var failed int64
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j warmJob) {
			defer wg.Done()
			defer func() { <-sem }()
			if _, err := t.getFlowFileTopK(j.path, j.start, j.end); err != nil {
				atomic.AddInt64(&failed, 1)
				log.Printf("tsstore: flow file cache warmup failed for %s (will retry lazily on next query): %v", j.path, err)
			}
		}(j)
	}
	wg.Wait()
	log.Printf("tsstore: flow file cache warmup done: %d file(s), %d failed, took %v", len(jobs), atomic.LoadInt64(&failed), time.Since(warmStart))
}

func newTSStore(dir string, period time.Duration) (*tsStore, error) {
	if period <= 0 {
		period = time.Hour
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create tsdata dir: %w", err)
	}
	for _, table := range tsTables {
		if err := os.MkdirAll(filepath.Join(dir, "sealed", table), 0o755); err != nil {
			return nil, fmt.Errorf("create sealed dir for %s: %w", table, err)
		}
	}

	t := &tsStore{
		dir: dir, period: period,
		totalCache:    make(map[string]totalCacheEntry),
		flowFileCache: make(map[string]flowFileCacheEntry),
	}

	if _, err := os.Stat(t.hotPath()); err == nil {
		log.Printf("tsstore: found a hot buffer left over from a previous run, sealing it before starting fresh")
		if err := t.recoverLeftoverHot(); err != nil {
			return nil, fmt.Errorf("recover leftover hot buffer: %w", err)
		}
	}

	if err := t.openFreshHot(time.Now().Truncate(period)); err != nil {
		return nil, fmt.Errorf("open hot buffer: %w", err)
	}

	go t.warmFlowFileCache()
	return t, nil
}

func (t *tsStore) hotPath() string {
	return filepath.Join(t.dir, "hot.duckdb")
}

// recoverLeftoverHot handles both a crash and a clean shutdown the same
// way: hot.duckdb is a normal persistent DuckDB file, so whatever it
// still holds from the previous run is sealed to Parquet (labeled with
// the earliest timestamp found in it) before a fresh hot buffer is
// opened. This keeps write/seal/recover to a single code path instead of
// having separate "seal on graceful close" and "recover after crash"
// logic.
func (t *tsStore) recoverLeftoverHot() error {
	path := t.hotPath()
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return fmt.Errorf("open leftover hot buffer: %w", err)
	}
	applyMemoryLimit(db)

	var minTS, maxTS sql.NullInt64
	for _, table := range tsTables {
		var lo, hi sql.NullInt64
		if err := db.QueryRow(`SELECT MIN(ts), MAX(ts) FROM ` + table).Scan(&lo, &hi); err != nil {
			continue
		}
		if lo.Valid && (!minTS.Valid || lo.Int64 < minTS.Int64) {
			minTS = lo
		}
		if hi.Valid && (!maxTS.Valid || hi.Int64 > maxTS.Int64) {
			maxTS = hi
		}
	}

	if !minTS.Valid {
		db.Close()
		return os.Remove(path)
	}
	if !maxTS.Valid {
		maxTS = minTS
	}

	// Encode the real [min,max] data range (not the nominal full-period
	// width) in the filename, since a leftover buffer recovered mid-period
	// (a restart) usually only holds a fraction of a period -- treating it
	// as if it spanned the full period would keep it "in play" for
	// overlap checks for far longer than its actual data justifies.
	for _, table := range tsTables {
		sealPath := filepath.Join(t.dir, "sealed", table, fmt.Sprintf("%d-%d_recovered.parquet", minTS.Int64, maxTS.Int64))
		if _, err := db.Exec(fmt.Sprintf(`COPY %s TO '%s' (FORMAT PARQUET)`, table, sealPath)); err != nil {
			log.Printf("tsstore: recovering %s from leftover hot buffer failed, that period's data is lost: %v", table, err)
		}
	}
	db.Close()
	return os.Remove(path)
}

// openFreshHot creates a brand-new hot.duckdb with an empty schema and
// opens the per-table Appenders. Callers must hold t.mu for writing,
// except during newTSStore's construction, before t is visible to other
// goroutines.
func (t *tsStore) openFreshHot(periodStart time.Time) error {
	db, err := sql.Open("duckdb", t.hotPath())
	if err != nil {
		return fmt.Errorf("open hot buffer: %w", err)
	}
	applyMemoryLimit(db)
	for _, table := range tsTables {
		if _, err := db.Exec(tsTableDDL[table]); err != nil {
			db.Close()
			return fmt.Errorf("create %s: %w", table, err)
		}
	}
	db.SetMaxOpenConns(4)

	conn, err := db.Conn(context.Background())
	if err != nil {
		db.Close()
		return fmt.Errorf("acquire appender conn: %w", err)
	}

	appenders := map[string]*duckdb.Appender{}
	if err := conn.Raw(func(driverConn any) error {
		for _, table := range tsTables {
			app, err := duckdb.NewAppenderFromConn(driverConn.(driver.Conn), "", table)
			if err != nil {
				return fmt.Errorf("create appender for %s: %w", table, err)
			}
			appenders[table] = app
		}
		return nil
	}); err != nil {
		conn.Close()
		db.Close()
		return err
	}

	t.hotDB = db
	t.hotConn = conn
	t.appenders = appenders
	t.periodStart = periodStart
	return nil
}

func (t *tsStore) writeTick(ts int64, flows []flowSample, ips map[uint32]xdpflowFlowStats, ports map[portKey]xdpflowFlowStats) error {
	newStart := time.Unix(ts, 0).Truncate(t.period)
	if newStart.After(t.periodStart) {
		if err := t.sealAndRotate(newStart); err != nil {
			return fmt.Errorf("seal period: %w", err)
		}
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	flowApp := t.appenders[tblFlow]
	for _, f := range flows {
		var domain any
		if f.domain != "" {
			domain = f.domain
		}
		if err := flowApp.AppendRow(ts, int64(f.key.Saddr), int64(f.key.Daddr), int64(ntohs(f.key.Sport)), int64(ntohs(f.key.Dport)), int64(f.key.Proto), int64(f.packets), int64(f.bytes), domain); err != nil {
			return fmt.Errorf("append flow_samples: %w", err)
		}
	}
	if err := flowApp.Flush(); err != nil {
		return fmt.Errorf("flush flow_samples: %w", err)
	}

	ipApp := t.appenders[tblIP]
	for ip, stat := range ips {
		if err := ipApp.AppendRow(ts, int64(ip), int64(stat.Packets), int64(stat.Bytes)); err != nil {
			return fmt.Errorf("append ip_samples: %w", err)
		}
	}
	if err := ipApp.Flush(); err != nil {
		return fmt.Errorf("flush ip_samples: %w", err)
	}

	portApp := t.appenders[tblPort]
	for pk, stat := range ports {
		if err := portApp.AppendRow(ts, int64(pk.proto), int64(ntohs(pk.port)), int64(stat.Packets), int64(stat.Bytes)); err != nil {
			return fmt.Errorf("append port_samples: %w", err)
		}
	}
	if err := portApp.Flush(); err != nil {
		return fmt.Errorf("flush port_samples: %w", err)
	}

	return nil
}

func (t *tsStore) sealAndRotate(newPeriodStart time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !newPeriodStart.After(t.periodStart) {
		return nil
	}

	start := t.periodStart.Unix()
	for _, table := range tsTables {
		if err := t.appenders[table].Close(); err != nil {
			return fmt.Errorf("close appender %s: %w", table, err)
		}
	}
	var flowSealPath string
	var flowStart, flowEnd int64
	for _, table := range tsTables {
		end := start
		var maxTS sql.NullInt64
		if err := t.hotDB.QueryRow(`SELECT MAX(ts) FROM ` + table).Scan(&maxTS); err == nil && maxTS.Valid {
			end = maxTS.Int64
		}
		sealPath := filepath.Join(t.dir, "sealed", table, fmt.Sprintf("%d-%d.parquet", start, end))
		if _, err := t.hotDB.Exec(fmt.Sprintf(`COPY %s TO '%s' (FORMAT PARQUET)`, table, sealPath)); err != nil {
			return fmt.Errorf("seal %s: %w", table, err)
		}
		if table == tblFlow {
			flowSealPath, flowStart, flowEnd = sealPath, start, end
		}
	}

	// Populate the new file's Top-K cache in the background rather than
	// inline here: sealAndRotate holds t.mu for writing, blocking new
	// ticks, and this file's top-K is only needed the next time a query
	// actually reaches it -- getFlowFileTopK computes it lazily on demand
	// if this goroutine hasn't finished (or failed) by then, so this is
	// purely a best-effort head start, never a correctness dependency.
	if flowSealPath != "" {
		go func(path string, s, e int64) {
			if _, err := t.getFlowFileTopK(path, s, e); err != nil {
				log.Printf("tsstore: background flow file top-k cache build failed for %s (will retry lazily on next query): %v", path, err)
			}
		}(flowSealPath, flowStart, flowEnd)
	}

	if err := t.hotConn.Close(); err != nil {
		log.Printf("tsstore: close hot appender conn: %v", err)
	}
	if err := t.hotDB.Close(); err != nil {
		log.Printf("tsstore: close hot db: %v", err)
	}
	if err := os.Remove(t.hotPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove sealed hot buffer: %w", err)
	}

	return t.openFreshHot(newPeriodStart)
}

func (t *tsStore) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, table := range tsTables {
		if app, ok := t.appenders[table]; ok {
			if err := app.Close(); err != nil {
				log.Printf("tsstore: close appender %s: %v", table, err)
			}
		}
	}
	if t.hotConn != nil {
		if err := t.hotConn.Close(); err != nil {
			log.Printf("tsstore: close hot conn: %v", err)
		}
	}
	if t.hotDB != nil {
		return t.hotDB.Close()
	}
	return nil
}

func (t *tsStore) prune(cutoff time.Time) error {
	cutoffUnix := cutoff.Unix()
	periodSec := int64(t.period / time.Second)
	for _, table := range tsTables {
		dir := filepath.Join(t.dir, "sealed", table)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read sealed dir %s: %w", table, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			_, end, ok := parseSealedRange(e.Name(), periodSec)
			if !ok {
				continue
			}
			if end <= cutoffUnix {
				path := filepath.Join(dir, e.Name())
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					log.Printf("tsstore: prune %s failed: %v", path, err)
				}
				if table == tblFlow {
					t.flowFileMu.Lock()
					delete(t.flowFileCache, path)
					t.flowFileMu.Unlock()
				}
			}
		}
	}
	return nil
}

// DiskUsageBytes sums the current hot buffer plus every sealed Parquet
// file, so callers reporting on-disk size (the 系统监控 page) see the
// real total instead of just the SQLite file.
func (t *tsStore) DiskUsageBytes() int64 {
	var total int64
	if fi, err := os.Stat(t.hotPath()); err == nil {
		total += fi.Size()
	}
	filepath.WalkDir(filepath.Join(t.dir, "sealed"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total
}

// parseSealedRange parses a sealed filename's [start,end] data range.
// Current format is "<start>-<end>.parquet" or "<start>-<end>_recovered.parquet",
// encoding the real span of ts values inside the file (see sealAndRotate/
// recoverLeftoverHot). Falls back to the legacy "<start>.parquet" format
// (no encoded end -- files sealed before this range-encoding existed),
// assuming a full nominal period width for those, so already-sealed files
// don't need migrating and keep working under the old, less precise
// overlap behavior until they age out.
func parseSealedRange(name string, periodSec int64) (start, end int64, ok bool) {
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, 0, false
	}
	startVal, err := strconv.ParseInt(name[:i], 10, 64)
	if err != nil {
		return 0, 0, false
	}

	if i < len(name) && name[i] == '-' {
		j := i + 1
		for j < len(name) && name[j] >= '0' && name[j] <= '9' {
			j++
		}
		if j > i+1 {
			if endVal, err := strconv.ParseInt(name[i+1:j], 10, 64); err == nil {
				return startVal, endVal, true
			}
		}
	}

	return startVal, startVal + periodSec, true
}

func (t *tsStore) listSealedFiles(table string, cutoff, until int64) ([]string, error) {
	dir := filepath.Join(t.dir, "sealed", table)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	periodSec := int64(t.period / time.Second)
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		start, end, ok := parseSealedRange(e.Name(), periodSec)
		if !ok {
			continue
		}
		if start <= until && end >= cutoff {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

func parquetFileList(files []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range files {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('\'')
		b.WriteString(strings.ReplaceAll(filepath.ToSlash(f), "'", "''"))
		b.WriteByte('\'')
	}
	b.WriteByte(']')
	return b.String()
}

// querySealedAgg runs buildSQL against a read_parquet() view of whatever
// sealed files overlap [cutoff, until] for table, on a short-lived
// in-memory DuckDB connection. Returns (nil, nil) when no sealed files
// overlap the window -- the important fast path that avoids ever
// touching historical Parquet files for a window entirely inside the
// current hot period.
func querySealedAgg[T any](t *tsStore, table string, buildSQL func(source string) string, cutoff, until int64, scan func(*sql.Rows) ([]T, error)) ([]T, error) {
	files, err := t.listSealedFiles(table, cutoff, until)
	if err != nil {
		return nil, fmt.Errorf("list sealed files for %s: %w", table, err)
	}
	if len(files) == 0 {
		return nil, nil
	}

	scratch, err := openScratchDuckDB()
	if err != nil {
		return nil, fmt.Errorf("open scratch duckdb: %w", err)
	}
	defer scratch.Close()

	rows, err := scratch.Query(buildSQL("read_parquet("+parquetFileList(files)+")"), cutoff, until)
	if err != nil {
		return nil, fmt.Errorf("query parquet: %w", err)
	}
	return scan(rows)
}

// aggRow is the common (group key, summed packets/bytes) shape shared by
// the ip_samples/port_samples/domain aggregations. flow_samples carries
// an extra MAX(domain) column and gets its own flowAggRow/flowKey below.
type aggRow[K comparable] struct {
	Key     K
	Packets uint64
	Bytes   uint64
}

func mergeSimpleAgg[K comparable](a, b []aggRow[K]) []aggRow[K] {
	acc := map[K]*aggRow[K]{}
	add := func(rows []aggRow[K]) {
		for _, r := range rows {
			if existing, ok := acc[r.Key]; ok {
				existing.Packets += r.Packets
				existing.Bytes += r.Bytes
			} else {
				cp := r
				acc[r.Key] = &cp
			}
		}
	}
	add(a)
	add(b)
	out := make([]aggRow[K], 0, len(acc))
	for _, v := range acc {
		out = append(out, *v)
	}
	return out
}

type flowKey struct {
	SrcIP, DstIP     uint32
	SrcPort, DstPort uint16
	Proto            uint8
}

type flowAggRow struct {
	Key     flowKey
	Packets uint64
	Bytes   uint64
	Domain  string
}

func mergeFlowAgg(a, b []flowAggRow) []flowAggRow {
	acc := map[flowKey]*flowAggRow{}
	add := func(rows []flowAggRow) {
		for _, r := range rows {
			if existing, ok := acc[r.Key]; ok {
				existing.Packets += r.Packets
				existing.Bytes += r.Bytes
				if r.Domain > existing.Domain {
					existing.Domain = r.Domain
				}
			} else {
				cp := r
				acc[r.Key] = &cp
			}
		}
	}
	add(a)
	add(b)
	out := make([]flowAggRow, 0, len(acc))
	for _, v := range acc {
		out = append(out, *v)
	}
	return out
}

type tsPortKey struct {
	Proto uint8
	Port  uint16
}

func (t *tsStore) queryIPs(cutoff, until int64) ([]aggRow[uint32], error) {
	buildSQL := func(source string) string {
		return fmt.Sprintf(`SELECT ip, SUM(packets), SUM(bytes) FROM %s WHERE ts >= ? AND ts <= ? GROUP BY ip`, source)
	}
	scan := func(rows *sql.Rows) ([]aggRow[uint32], error) {
		defer rows.Close()
		var out []aggRow[uint32]
		for rows.Next() {
			var ip, packets, bytes int64
			if err := rows.Scan(&ip, &packets, &bytes); err != nil {
				return nil, err
			}
			out = append(out, aggRow[uint32]{Key: uint32(ip), Packets: uint64(packets), Bytes: uint64(bytes)})
		}
		return out, rows.Err()
	}

	sealed, err := querySealedAgg(t, tblIP, buildSQL, cutoff, until, scan)
	if err != nil {
		return nil, fmt.Errorf("query sealed ip_samples: %w", err)
	}

	t.mu.RLock()
	hotRows, err := t.hotDB.Query(buildSQL(tblIP), cutoff, until)
	if err != nil {
		t.mu.RUnlock()
		return nil, fmt.Errorf("query hot ip_samples: %w", err)
	}
	hot, err := scan(hotRows)
	t.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("scan hot ip_samples: %w", err)
	}

	return mergeSimpleAgg(sealed, hot), nil
}

func (t *tsStore) queryPorts(cutoff, until int64) ([]aggRow[tsPortKey], error) {
	buildSQL := func(source string) string {
		return fmt.Sprintf(`SELECT proto, port, SUM(packets), SUM(bytes) FROM %s WHERE ts >= ? AND ts <= ? GROUP BY proto, port`, source)
	}
	scan := func(rows *sql.Rows) ([]aggRow[tsPortKey], error) {
		defer rows.Close()
		var out []aggRow[tsPortKey]
		for rows.Next() {
			var proto, port, packets, bytes int64
			if err := rows.Scan(&proto, &port, &packets, &bytes); err != nil {
				return nil, err
			}
			out = append(out, aggRow[tsPortKey]{Key: tsPortKey{Proto: uint8(proto), Port: uint16(port)}, Packets: uint64(packets), Bytes: uint64(bytes)})
		}
		return out, rows.Err()
	}

	sealed, err := querySealedAgg(t, tblPort, buildSQL, cutoff, until, scan)
	if err != nil {
		return nil, fmt.Errorf("query sealed port_samples: %w", err)
	}

	t.mu.RLock()
	hotRows, err := t.hotDB.Query(buildSQL(tblPort), cutoff, until)
	if err != nil {
		t.mu.RUnlock()
		return nil, fmt.Errorf("query hot port_samples: %w", err)
	}
	hot, err := scan(hotRows)
	t.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("scan hot port_samples: %w", err)
	}

	return mergeSimpleAgg(sealed, hot), nil
}

// The *Limited variants below serve queryTopX (limit=N, offset=0) and
// QueryXPaged (limit=pageSize, offset=page*pageSize) alike. When the query
// window is entirely inside the still-open hot period (the overwhelmingly
// common case: any "last N minutes" dashboard or AI-chat query), sorting
// and limiting is pushed straight into the hot buffer's SQL, so only the
// requested page of rows ever crosses the cgo boundary. When sealed
// Parquet files are also involved (e.g. the window reaches back across a
// period boundary, or -- in practice, more often -- a recent restart
// sealed a small "_recovered" fragment), each source is queried for its
// own bounded top-K (see distributedTopKMargin) and the results merged in
// Go, instead of pulling every matching group from every source.

func (t *tsStore) queryFlowsLimited(cutoff, until int64, ipFilter *uint32, limit, offset int, wantTotal bool) ([]flowAggRow, int, error) {
	if limit+offset > maxFetchK {
		return nil, 0, errFetchTooDeep
	}
	callStart := time.Now()
	files, err := t.listSealedFiles(tblFlow, cutoff, until)
	if err != nil {
		return nil, 0, fmt.Errorf("list sealed files for %s: %w", tblFlow, err)
	}

	where := "ts >= ? AND ts <= ?"
	if ipFilter != nil {
		where += fmt.Sprintf(" AND (src_ip = %d OR dst_ip = %d)", *ipFilter, *ipFilter)
	}
	countSQL := func(source string) string {
		return fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT 1 FROM %s WHERE %s GROUP BY src_ip, src_port, dst_ip, dst_port, proto)`, source, where)
	}

	if len(files) == 0 {
		// COUNT(*) OVER() deliberately kept out of the row query below: it's
		// a global window function, so DuckDB must fully materialize the
		// GROUP BY before it can compute it, which blocks the bounded
		// top-N/heap-sort optimization for ORDER BY ... LIMIT. On flow_samples
		// specifically (near-1:1 row-to-group cardinality thanks to ephemeral
		// source ports) that turned an 8s query into 1.6s in testing. The
		// total, when actually needed, is fetched via its own lightweight
		// query instead.
		q := fmt.Sprintf(`SELECT src_ip, src_port, dst_ip, dst_port, proto, SUM(packets), SUM(bytes), MAX(domain)
			FROM %s WHERE %s GROUP BY src_ip, src_port, dst_ip, dst_port, proto
			ORDER BY SUM(bytes) DESC LIMIT ? OFFSET ?`, tblFlow, where)

		lockStart := time.Now()
		t.mu.RLock()
		lockWait := time.Since(lockStart)
		defer t.mu.RUnlock()
		queryStart := time.Now()
		rows, err := t.hotDB.Query(q, cutoff, until, limit, offset)
		if err != nil {
			return nil, 0, fmt.Errorf("query hot flow_samples: %w", err)
		}
		var out []flowAggRow
		for rows.Next() {
			var srcIP, srcPort, dstIP, dstPort, proto, packets, bytes int64
			var domain sql.NullString
			if err := rows.Scan(&srcIP, &srcPort, &dstIP, &dstPort, &proto, &packets, &bytes, &domain); err != nil {
				rows.Close()
				return nil, 0, err
			}
			out = append(out, flowAggRow{
				Key: flowKey{
					SrcIP: uint32(srcIP), DstIP: uint32(dstIP),
					SrcPort: uint16(srcPort), DstPort: uint16(dstPort),
					Proto: uint8(proto),
				},
				Packets: uint64(packets), Bytes: uint64(bytes), Domain: domain.String,
			})
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return nil, 0, rowsErr
		}

		total := 0
		if wantTotal {
			total, err = t.cachedTotal(flowTotalCacheKey(cutoff, until, ipFilter), func() (int, error) {
				var n int
				if err := t.hotDB.QueryRow(countSQL(tblFlow), cutoff, until).Scan(&n); err != nil {
					return 0, fmt.Errorf("count hot flow_samples: %w", err)
				}
				return n, nil
			})
			if err != nil {
				return nil, 0, err
			}
		}
		log.Printf("tsstore: queryFlowsLimited FAST path window=%ds lockWait=%v query+scan=%v total=%d rows=%d overall=%v",
			(until-cutoff), lockWait, time.Since(queryStart), total, len(out), time.Since(callStart))
		return out, total, nil
	}

	slowStart := time.Now()

	buildSQL := func(source string) string {
		return fmt.Sprintf(`SELECT src_ip, src_port, dst_ip, dst_port, proto, SUM(packets), SUM(bytes), MAX(domain)
			FROM %s WHERE %s GROUP BY src_ip, src_port, dst_ip, dst_port, proto
			ORDER BY SUM(bytes) DESC LIMIT ?`, source, where)
	}
	scan := func(rows *sql.Rows) ([]flowAggRow, error) {
		defer rows.Close()
		var out []flowAggRow
		for rows.Next() {
			var srcIP, srcPort, dstIP, dstPort, proto, packets, bytes int64
			var domain sql.NullString
			if err := rows.Scan(&srcIP, &srcPort, &dstIP, &dstPort, &proto, &packets, &bytes, &domain); err != nil {
				return nil, err
			}
			out = append(out, flowAggRow{
				Key: flowKey{
					SrcIP: uint32(srcIP), DstIP: uint32(dstIP),
					SrcPort: uint16(srcPort), DstPort: uint16(dstPort),
					Proto: uint8(proto),
				},
				Packets: uint64(packets), Bytes: uint64(bytes), Domain: domain.String,
			})
		}
		return out, rows.Err()
	}

	fetchK := limit + offset + distributedTopKMargin

	// When there's no IP filter, split sealed files into ones we can trust
	// the per-file cache for (fully inside [cutoff,until], and the cache
	// holds enough rows to cover fetchK) versus ones that still need a
	// live, ts-filtered query -- a file only partially overlapping the
	// requested window (typically at most the first/last file touched)
	// can't safely use a cache built from that file's FULL range, since
	// the cached SUM(bytes) per key would include ticks outside the
	// requested window. An IP filter disables the cache entirely: it's
	// built unfiltered (top-by-bytes), so a specific IP's flows may not be
	// in it at all even though they're in the file -- see project notes
	// on why filtered search can't safely use this cache.
	var sealedRows []flowAggRow
	var liveFiles []string
	var cachedSealedTotal int
	if ipFilter == nil {
		periodSec := int64(t.period / time.Second)
		for _, f := range files {
			start, end, ok := parseSealedRange(filepath.Base(f), periodSec)
			fullyContained := ok && start >= cutoff && end <= until
			if fullyContained && fetchK <= flowFileCacheCap {
				entry, err := t.getFlowFileTopK(f, start, end)
				if err != nil {
					return nil, 0, fmt.Errorf("get cached flow file top-k: %w", err)
				}
				sealedRows = append(sealedRows, entry.rows...)
				cachedSealedTotal += entry.total
				continue
			}
			liveFiles = append(liveFiles, f)
		}
	} else {
		liveFiles = files
	}

	if len(liveFiles) > 0 {
		scratch, err := openScratchDuckDB()
		if err != nil {
			return nil, 0, fmt.Errorf("open scratch duckdb: %w", err)
		}
		liveSource := "read_parquet(" + parquetFileList(liveFiles) + ")"
		rowsSQL, err := scratch.Query(buildSQL(liveSource), cutoff, until, fetchK)
		if err != nil {
			scratch.Close()
			return nil, 0, fmt.Errorf("query sealed flow_samples: %w", err)
		}
		liveRows, err := scan(rowsSQL)
		scratch.Close()
		if err != nil {
			return nil, 0, fmt.Errorf("scan sealed flow_samples: %w", err)
		}
		sealedRows = append(sealedRows, liveRows...)
	}

	t.mu.RLock()
	hotRowsSQL, err := t.hotDB.Query(buildSQL(tblFlow), cutoff, until, fetchK)
	if err != nil {
		t.mu.RUnlock()
		return nil, 0, fmt.Errorf("query hot flow_samples: %w", err)
	}
	hotRows, err := scan(hotRowsSQL)
	if err != nil {
		t.mu.RUnlock()
		return nil, 0, fmt.Errorf("scan hot flow_samples: %w", err)
	}
	t.mu.RUnlock()

	total := 0
	if wantTotal {
		total, err = t.cachedTotal(flowTotalCacheKey(cutoff, until, ipFilter), func() (int, error) {
			sum := cachedSealedTotal
			if len(liveFiles) > 0 {
				scratch2, err := openScratchDuckDB()
				if err != nil {
					return 0, fmt.Errorf("open scratch duckdb for count: %w", err)
				}
				defer scratch2.Close()
				liveSource := "read_parquet(" + parquetFileList(liveFiles) + ")"
				var liveTotal int
				if err := scratch2.QueryRow(countSQL(liveSource), cutoff, until).Scan(&liveTotal); err != nil {
					return 0, fmt.Errorf("count sealed flow_samples: %w", err)
				}
				sum += liveTotal
			}
			t.mu.RLock()
			defer t.mu.RUnlock()
			var hotTotal int
			if err := t.hotDB.QueryRow(countSQL(tblFlow), cutoff, until).Scan(&hotTotal); err != nil {
				return 0, fmt.Errorf("count hot flow_samples: %w", err)
			}
			return sum + hotTotal, nil
		})
		if err != nil {
			return nil, 0, err
		}
	}

	merged := mergeFlowAgg(sealedRows, hotRows)
	sort.Slice(merged, func(i, j int) bool { return merged[i].Bytes > merged[j].Bytes })
	log.Printf("tsstore: queryFlowsLimited SLOW path (%d sealed file(s), cacheHits=%d liveFiles=%d, fetchK=%d) window=%ds sealedRows=%d hotRows=%d merged=%d total=%d took=%v",
		len(files), len(files)-len(liveFiles), len(liveFiles), fetchK, until-cutoff, len(sealedRows), len(hotRows), len(merged), total, time.Since(slowStart))
	return limitOffsetSlice(merged, limit, offset), total, nil
}

func (t *tsStore) queryIPsLimited(cutoff, until int64, limit, offset int, wantTotal bool) ([]aggRow[uint32], int, error) {
	if limit+offset > maxFetchK {
		return nil, 0, errFetchTooDeep
	}
	files, err := t.listSealedFiles(tblIP, cutoff, until)
	if err != nil {
		return nil, 0, fmt.Errorf("list sealed files for %s: %w", tblIP, err)
	}

	countSQL := func(source string) string {
		return fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT 1 FROM %s WHERE ts >= ? AND ts <= ? GROUP BY ip)`, source)
	}

	if len(files) == 0 {
		t.mu.RLock()
		defer t.mu.RUnlock()
		rows, err := t.hotDB.Query(`SELECT ip, SUM(packets), SUM(bytes) FROM ip_samples
			WHERE ts >= ? AND ts <= ? GROUP BY ip ORDER BY SUM(bytes) DESC LIMIT ? OFFSET ?`, cutoff, until, limit, offset)
		if err != nil {
			return nil, 0, fmt.Errorf("query hot ip_samples: %w", err)
		}
		var out []aggRow[uint32]
		for rows.Next() {
			var ip, packets, bytes int64
			if err := rows.Scan(&ip, &packets, &bytes); err != nil {
				rows.Close()
				return nil, 0, err
			}
			out = append(out, aggRow[uint32]{Key: uint32(ip), Packets: uint64(packets), Bytes: uint64(bytes)})
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return nil, 0, rowsErr
		}
		total := 0
		if wantTotal {
			if err := t.hotDB.QueryRow(countSQL(tblIP), cutoff, until).Scan(&total); err != nil {
				return nil, 0, fmt.Errorf("count hot ip_samples: %w", err)
			}
		}
		return out, total, nil
	}

	buildSQL := func(source string) string {
		return fmt.Sprintf(`SELECT ip, SUM(packets), SUM(bytes) FROM %s WHERE ts >= ? AND ts <= ? GROUP BY ip ORDER BY SUM(bytes) DESC LIMIT ?`, source)
	}
	scan := func(rows *sql.Rows) ([]aggRow[uint32], error) {
		defer rows.Close()
		var out []aggRow[uint32]
		for rows.Next() {
			var ip, packets, bytes int64
			if err := rows.Scan(&ip, &packets, &bytes); err != nil {
				return nil, err
			}
			out = append(out, aggRow[uint32]{Key: uint32(ip), Packets: uint64(packets), Bytes: uint64(bytes)})
		}
		return out, rows.Err()
	}

	fetchK := limit + offset + distributedTopKMargin

	scratch, err := openScratchDuckDB()
	if err != nil {
		return nil, 0, fmt.Errorf("open scratch duckdb: %w", err)
	}
	sealedSource := "read_parquet(" + parquetFileList(files) + ")"
	sealedRowsSQL, err := scratch.Query(buildSQL(sealedSource), cutoff, until, fetchK)
	if err != nil {
		scratch.Close()
		return nil, 0, fmt.Errorf("query sealed ip_samples: %w", err)
	}
	sealedRows, err := scan(sealedRowsSQL)
	if err != nil {
		scratch.Close()
		return nil, 0, fmt.Errorf("scan sealed ip_samples: %w", err)
	}
	scratch.Close()

	t.mu.RLock()
	hotRowsSQL, err := t.hotDB.Query(buildSQL(tblIP), cutoff, until, fetchK)
	if err != nil {
		t.mu.RUnlock()
		return nil, 0, fmt.Errorf("query hot ip_samples: %w", err)
	}
	hotRows, err := scan(hotRowsSQL)
	if err != nil {
		t.mu.RUnlock()
		return nil, 0, fmt.Errorf("scan hot ip_samples: %w", err)
	}

	total := 0
	if wantTotal {
		// A naive sealedTotal+hotTotal double-counts any ip that appears in
		// both sources (near-certain for long-lived/well-known hosts) --
		// see project notes on the resulting "phantom last pages" bug. Fix:
		// pull hot's full distinct key set (bounded, low-cardinality
		// dimension) and exclude it from the sealed count, so the two
		// counts are disjoint and sum to the true total.
		hotIPRows, err := t.hotDB.Query(`SELECT DISTINCT ip FROM ip_samples WHERE ts >= ? AND ts <= ?`, cutoff, until)
		if err != nil {
			t.mu.RUnlock()
			return nil, 0, fmt.Errorf("list hot distinct ips: %w", err)
		}
		var hotIPs []uint32
		for hotIPRows.Next() {
			var ip int64
			if err := hotIPRows.Scan(&ip); err != nil {
				hotIPRows.Close()
				t.mu.RUnlock()
				return nil, 0, err
			}
			hotIPs = append(hotIPs, uint32(ip))
		}
		rowsErr := hotIPRows.Err()
		hotIPRows.Close()
		if rowsErr != nil {
			t.mu.RUnlock()
			return nil, 0, rowsErr
		}
		t.mu.RUnlock()

		scratch2, err := openScratchDuckDB()
		if err != nil {
			return nil, 0, fmt.Errorf("open scratch duckdb for count: %w", err)
		}
		sealedExclusive := countSQL(sealedSource)
		if len(hotIPs) > 0 {
			var excl strings.Builder
			excl.WriteString(" AND ip NOT IN (")
			for i, ip := range hotIPs {
				if i > 0 {
					excl.WriteString(", ")
				}
				excl.WriteString(strconv.FormatInt(int64(ip), 10))
			}
			excl.WriteByte(')')
			sealedExclusive = fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT 1 FROM %s WHERE ts >= ? AND ts <= ?%s GROUP BY ip)`, sealedSource, excl.String())
		}
		var sealedOnlyTotal int
		if err := scratch2.QueryRow(sealedExclusive, cutoff, until).Scan(&sealedOnlyTotal); err != nil {
			scratch2.Close()
			return nil, 0, fmt.Errorf("count sealed-only ip_samples: %w", err)
		}
		scratch2.Close()
		total = sealedOnlyTotal + len(hotIPs)
		log.Printf("tsstore: queryIPsLimited exact total: sealedOnly=%d hotDistinct=%d total=%d", sealedOnlyTotal, len(hotIPs), total)
	} else {
		t.mu.RUnlock()
	}

	merged := mergeSimpleAgg(sealedRows, hotRows)
	sort.Slice(merged, func(i, j int) bool { return merged[i].Bytes > merged[j].Bytes })
	return limitOffsetSlice(merged, limit, offset), total, nil
}

func (t *tsStore) queryPortsLimited(cutoff, until int64, limit, offset int, wantTotal bool) ([]aggRow[tsPortKey], int, error) {
	if limit+offset > maxFetchK {
		return nil, 0, errFetchTooDeep
	}
	files, err := t.listSealedFiles(tblPort, cutoff, until)
	if err != nil {
		return nil, 0, fmt.Errorf("list sealed files for %s: %w", tblPort, err)
	}

	countSQL := func(source string) string {
		return fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT 1 FROM %s WHERE ts >= ? AND ts <= ? GROUP BY proto, port)`, source)
	}

	if len(files) == 0 {
		t.mu.RLock()
		defer t.mu.RUnlock()
		rows, err := t.hotDB.Query(`SELECT proto, port, SUM(packets), SUM(bytes) FROM port_samples
			WHERE ts >= ? AND ts <= ? GROUP BY proto, port ORDER BY SUM(bytes) DESC LIMIT ? OFFSET ?`, cutoff, until, limit, offset)
		if err != nil {
			return nil, 0, fmt.Errorf("query hot port_samples: %w", err)
		}
		var out []aggRow[tsPortKey]
		for rows.Next() {
			var proto, port, packets, bytes int64
			if err := rows.Scan(&proto, &port, &packets, &bytes); err != nil {
				rows.Close()
				return nil, 0, err
			}
			out = append(out, aggRow[tsPortKey]{Key: tsPortKey{Proto: uint8(proto), Port: uint16(port)}, Packets: uint64(packets), Bytes: uint64(bytes)})
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return nil, 0, rowsErr
		}
		total := 0
		if wantTotal {
			if err := t.hotDB.QueryRow(countSQL(tblPort), cutoff, until).Scan(&total); err != nil {
				return nil, 0, fmt.Errorf("count hot port_samples: %w", err)
			}
		}
		return out, total, nil
	}

	buildSQL := func(source string) string {
		return fmt.Sprintf(`SELECT proto, port, SUM(packets), SUM(bytes) FROM %s WHERE ts >= ? AND ts <= ? GROUP BY proto, port ORDER BY SUM(bytes) DESC LIMIT ?`, source)
	}
	scan := func(rows *sql.Rows) ([]aggRow[tsPortKey], error) {
		defer rows.Close()
		var out []aggRow[tsPortKey]
		for rows.Next() {
			var proto, port, packets, bytes int64
			if err := rows.Scan(&proto, &port, &packets, &bytes); err != nil {
				return nil, err
			}
			out = append(out, aggRow[tsPortKey]{Key: tsPortKey{Proto: uint8(proto), Port: uint16(port)}, Packets: uint64(packets), Bytes: uint64(bytes)})
		}
		return out, rows.Err()
	}

	fetchK := limit + offset + distributedTopKMargin

	scratch, err := openScratchDuckDB()
	if err != nil {
		return nil, 0, fmt.Errorf("open scratch duckdb: %w", err)
	}
	sealedSource := "read_parquet(" + parquetFileList(files) + ")"
	sealedRowsSQL, err := scratch.Query(buildSQL(sealedSource), cutoff, until, fetchK)
	if err != nil {
		scratch.Close()
		return nil, 0, fmt.Errorf("query sealed port_samples: %w", err)
	}
	sealedRows, err := scan(sealedRowsSQL)
	if err != nil {
		scratch.Close()
		return nil, 0, fmt.Errorf("scan sealed port_samples: %w", err)
	}
	scratch.Close()

	t.mu.RLock()
	hotRowsSQL, err := t.hotDB.Query(buildSQL(tblPort), cutoff, until, fetchK)
	if err != nil {
		t.mu.RUnlock()
		return nil, 0, fmt.Errorf("query hot port_samples: %w", err)
	}
	hotRows, err := scan(hotRowsSQL)
	if err != nil {
		t.mu.RUnlock()
		return nil, 0, fmt.Errorf("scan hot port_samples: %w", err)
	}

	total := 0
	if wantTotal {
		// See queryIPsLimited for why this can't be a naive sealedTotal+
		// hotTotal sum: pull hot's full distinct (proto,port) set and
		// exclude it from the sealed count so the two totals are disjoint.
		hotPortRows, err := t.hotDB.Query(`SELECT DISTINCT proto, port FROM port_samples WHERE ts >= ? AND ts <= ?`, cutoff, until)
		if err != nil {
			t.mu.RUnlock()
			return nil, 0, fmt.Errorf("list hot distinct ports: %w", err)
		}
		var hotKeys []tsPortKey
		for hotPortRows.Next() {
			var proto, port int64
			if err := hotPortRows.Scan(&proto, &port); err != nil {
				hotPortRows.Close()
				t.mu.RUnlock()
				return nil, 0, err
			}
			hotKeys = append(hotKeys, tsPortKey{Proto: uint8(proto), Port: uint16(port)})
		}
		rowsErr := hotPortRows.Err()
		hotPortRows.Close()
		if rowsErr != nil {
			t.mu.RUnlock()
			return nil, 0, rowsErr
		}
		t.mu.RUnlock()

		scratch2, err := openScratchDuckDB()
		if err != nil {
			return nil, 0, fmt.Errorf("open scratch duckdb for count: %w", err)
		}
		sealedExclusive := countSQL(sealedSource)
		if len(hotKeys) > 0 {
			var excl strings.Builder
			excl.WriteString(" AND (proto, port) NOT IN (")
			for i, k := range hotKeys {
				if i > 0 {
					excl.WriteString(", ")
				}
				fmt.Fprintf(&excl, "(%d, %d)", k.Proto, k.Port)
			}
			excl.WriteByte(')')
			sealedExclusive = fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT 1 FROM %s WHERE ts >= ? AND ts <= ?%s GROUP BY proto, port)`, sealedSource, excl.String())
		}
		var sealedOnlyTotal int
		if err := scratch2.QueryRow(sealedExclusive, cutoff, until).Scan(&sealedOnlyTotal); err != nil {
			scratch2.Close()
			return nil, 0, fmt.Errorf("count sealed-only port_samples: %w", err)
		}
		scratch2.Close()
		total = sealedOnlyTotal + len(hotKeys)
		log.Printf("tsstore: queryPortsLimited exact total: sealedOnly=%d hotDistinct=%d total=%d", sealedOnlyTotal, len(hotKeys), total)
	} else {
		t.mu.RUnlock()
	}

	merged := mergeSimpleAgg(sealedRows, hotRows)
	sort.Slice(merged, func(i, j int) bool { return merged[i].Bytes > merged[j].Bytes })
	return limitOffsetSlice(merged, limit, offset), total, nil
}

// queryDomainsLimited supports a LIKE filter pushed straight into SQL
// (unlike the IP/port cases, domain is a plain VARCHAR column so this
// doesn't hit the packed-integer LIKE limitation), so this stays on the
// SQL-pushdown fast path regardless of whether filter is set.
func (t *tsStore) queryDomainsLimited(cutoff, until int64, filter string, limit, offset int, wantTotal bool) ([]aggRow[string], int, error) {
	if limit+offset > maxFetchK {
		return nil, 0, errFetchTooDeep
	}
	files, err := t.listSealedFiles(tblFlow, cutoff, until)
	if err != nil {
		return nil, 0, fmt.Errorf("list sealed files for %s (domains): %w", tblFlow, err)
	}

	where := "ts >= ? AND ts <= ? AND domain IS NOT NULL AND domain != ''"
	if filter != "" {
		where += " AND domain LIKE ?"
	}
	countSQL := func(source string) string {
		return fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT 1 FROM %s WHERE %s GROUP BY domain)`, source, where)
	}
	countArgs := func(cutoff, until int64) []any {
		a := []any{cutoff, until}
		if filter != "" {
			a = append(a, "%"+filter+"%")
		}
		return a
	}

	if len(files) == 0 {
		args := append(countArgs(cutoff, until), limit, offset)

		q := `SELECT domain, SUM(packets), SUM(bytes) FROM flow_samples
			WHERE ` + where + ` GROUP BY domain ORDER BY SUM(bytes) DESC LIMIT ? OFFSET ?`

		t.mu.RLock()
		defer t.mu.RUnlock()
		rows, err := t.hotDB.Query(q, args...)
		if err != nil {
			return nil, 0, fmt.Errorf("query hot flow_samples (domains): %w", err)
		}
		var out []aggRow[string]
		for rows.Next() {
			var domain string
			var packets, bytes int64
			if err := rows.Scan(&domain, &packets, &bytes); err != nil {
				rows.Close()
				return nil, 0, err
			}
			out = append(out, aggRow[string]{Key: domain, Packets: uint64(packets), Bytes: uint64(bytes)})
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return nil, 0, rowsErr
		}
		total := 0
		if wantTotal {
			if err := t.hotDB.QueryRow(countSQL(tblFlow), countArgs(cutoff, until)...).Scan(&total); err != nil {
				return nil, 0, fmt.Errorf("count hot flow_samples (domains): %w", err)
			}
		}
		return out, total, nil
	}

	buildSQL := func(source string) string {
		return fmt.Sprintf(`SELECT domain, SUM(packets), SUM(bytes) FROM %s WHERE %s GROUP BY domain ORDER BY SUM(bytes) DESC LIMIT ?`, source, where)
	}
	buildArgs := func(cutoff, until int64, fetchK int) []any {
		return append(countArgs(cutoff, until), fetchK)
	}
	scan := func(rows *sql.Rows) ([]aggRow[string], error) {
		defer rows.Close()
		var out []aggRow[string]
		for rows.Next() {
			var domain string
			var packets, bytes int64
			if err := rows.Scan(&domain, &packets, &bytes); err != nil {
				return nil, err
			}
			out = append(out, aggRow[string]{Key: domain, Packets: uint64(packets), Bytes: uint64(bytes)})
		}
		return out, rows.Err()
	}

	fetchK := limit + offset + distributedTopKMargin

	scratch, err := openScratchDuckDB()
	if err != nil {
		return nil, 0, fmt.Errorf("open scratch duckdb: %w", err)
	}
	sealedSource := "read_parquet(" + parquetFileList(files) + ")"
	sealedRowsSQL, err := scratch.Query(buildSQL(sealedSource), buildArgs(cutoff, until, fetchK)...)
	if err != nil {
		scratch.Close()
		return nil, 0, fmt.Errorf("query sealed flow_samples (domains): %w", err)
	}
	sealedRows, err := scan(sealedRowsSQL)
	if err != nil {
		scratch.Close()
		return nil, 0, fmt.Errorf("scan sealed flow_samples (domains): %w", err)
	}
	scratch.Close()

	t.mu.RLock()
	hotRowsSQL, err := t.hotDB.Query(buildSQL(tblFlow), buildArgs(cutoff, until, fetchK)...)
	if err != nil {
		t.mu.RUnlock()
		return nil, 0, fmt.Errorf("query hot flow_samples (domains): %w", err)
	}
	hotRows, err := scan(hotRowsSQL)
	if err != nil {
		t.mu.RUnlock()
		return nil, 0, fmt.Errorf("scan hot flow_samples (domains): %w", err)
	}

	total := 0
	if wantTotal {
		// See queryIPsLimited for why this can't be a naive sealedTotal+
		// hotTotal sum: pull hot's full distinct domain set and exclude it
		// from the sealed count so the two totals are disjoint.
		hotDomainRows, err := t.hotDB.Query(`SELECT DISTINCT domain FROM `+tblFlow+` WHERE `+where, countArgs(cutoff, until)...)
		if err != nil {
			t.mu.RUnlock()
			return nil, 0, fmt.Errorf("list hot distinct domains: %w", err)
		}
		var hotDomains []string
		for hotDomainRows.Next() {
			var d string
			if err := hotDomainRows.Scan(&d); err != nil {
				hotDomainRows.Close()
				t.mu.RUnlock()
				return nil, 0, err
			}
			hotDomains = append(hotDomains, d)
		}
		rowsErr := hotDomainRows.Err()
		hotDomainRows.Close()
		if rowsErr != nil {
			t.mu.RUnlock()
			return nil, 0, rowsErr
		}
		t.mu.RUnlock()

		scratch2, err := openScratchDuckDB()
		if err != nil {
			return nil, 0, fmt.Errorf("open scratch duckdb for count: %w", err)
		}
		sealedExclusive := countSQL(sealedSource)
		if len(hotDomains) > 0 {
			var excl strings.Builder
			excl.WriteString(" AND domain NOT IN (")
			for i, d := range hotDomains {
				if i > 0 {
					excl.WriteString(", ")
				}
				excl.WriteByte('\'')
				excl.WriteString(strings.ReplaceAll(d, "'", "''"))
				excl.WriteByte('\'')
			}
			excl.WriteByte(')')
			sealedExclusive = fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT 1 FROM %s WHERE %s%s GROUP BY domain)`, sealedSource, where, excl.String())
		}
		var sealedOnlyTotal int
		if err := scratch2.QueryRow(sealedExclusive, countArgs(cutoff, until)...).Scan(&sealedOnlyTotal); err != nil {
			scratch2.Close()
			return nil, 0, fmt.Errorf("count sealed-only flow_samples (domains): %w", err)
		}
		scratch2.Close()
		total = sealedOnlyTotal + len(hotDomains)
		log.Printf("tsstore: queryDomainsLimited exact total: sealedOnly=%d hotDistinct=%d total=%d", sealedOnlyTotal, len(hotDomains), total)
	} else {
		t.mu.RUnlock()
	}

	merged := mergeSimpleAgg(sealedRows, hotRows)
	sort.Slice(merged, func(i, j int) bool { return merged[i].Bytes > merged[j].Bytes })
	return limitOffsetSlice(merged, limit, offset), total, nil
}

func limitOffsetSlice[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

// isPrivateIPv4SQL returns a SQL predicate testing whether column is an
// RFC1918 private address, matching Go's net.IP.IsPrivate() for IPv4
// exactly (10/8, 172.16/12, 192.168/16). Written as integer bit ops
// (not an inet/CIDR function) since the DuckDB build here only bundles
// the "json" extension. Relies on the same little-endian uint32 packing
// ipString/ipToUint32 use elsewhere, so `column & 255` is always the
// first octet.
func isPrivateIPv4SQL(column string) string {
	return fmt.Sprintf(
		`((%s & 255) = 10 OR ((%s & 255) = 172 AND ((%s >> 8) & 255) BETWEEN 16 AND 31) OR ((%s & 255) = 192 AND ((%s >> 8) & 255) = 168))`,
		column, column, column, column, column,
	)
}

type edgePairKey struct{ A, B uint32 }

// queryTopologyEdges returns the topologyMaxEdges heaviest private-to-
// private IP pairs (src/dst canonicalized so A<=B, matching topology.go's
// existing swap-if-needed logic) in [cutoff,until]. The private-IP filter
// is pushed into SQL rather than applied after an overall top-K fetch:
// internal traffic is typically a small fraction of total bytes, so
// filtering post-hoc after only fetching the overall heaviest flows could
// genuinely miss the true internal top edges -- unlike the distributed
// top-K approximation used elsewhere in this file, that's not an
// acceptable tradeoff here, so this always evaluates the full private-
// private data before ranking, on both the fast and slow paths.
func (t *tsStore) queryTopologyEdges(cutoff, until int64) ([]aggRow[edgePairKey], error) {
	files, err := t.listSealedFiles(tblFlow, cutoff, until)
	if err != nil {
		return nil, fmt.Errorf("list sealed files for %s (topology): %w", tblFlow, err)
	}

	privateFilter := isPrivateIPv4SQL("src_ip") + " AND " + isPrivateIPv4SQL("dst_ip")
	buildSQL := func(source string, withLimit bool) string {
		q := fmt.Sprintf(`SELECT LEAST(src_ip,dst_ip), GREATEST(src_ip,dst_ip), SUM(packets), SUM(bytes)
			FROM %s WHERE ts >= ? AND ts <= ? AND %s
			GROUP BY LEAST(src_ip,dst_ip), GREATEST(src_ip,dst_ip)
			ORDER BY SUM(bytes) DESC`, source, privateFilter)
		if withLimit {
			q += " LIMIT ?"
		}
		return q
	}
	scan := func(rows *sql.Rows) ([]aggRow[edgePairKey], error) {
		defer rows.Close()
		var out []aggRow[edgePairKey]
		for rows.Next() {
			var a, b, packets, bytes int64
			if err := rows.Scan(&a, &b, &packets, &bytes); err != nil {
				return nil, err
			}
			out = append(out, aggRow[edgePairKey]{
				Key:     edgePairKey{A: uint32(a), B: uint32(b)},
				Packets: uint64(packets), Bytes: uint64(bytes),
			})
		}
		return out, rows.Err()
	}

	if len(files) == 0 {
		t.mu.RLock()
		rows, err := t.hotDB.Query(buildSQL(tblFlow, true), cutoff, until, topologyMaxEdges)
		if err != nil {
			t.mu.RUnlock()
			return nil, fmt.Errorf("query hot flow_samples (topology): %w", err)
		}
		hot, err := scan(rows)
		t.mu.RUnlock()
		if err != nil {
			return nil, fmt.Errorf("scan hot flow_samples (topology): %w", err)
		}
		return hot, nil
	}

	fetchK := topologyMaxEdges + distributedTopKMargin

	scratch, err := openScratchDuckDB()
	if err != nil {
		return nil, fmt.Errorf("open scratch duckdb: %w", err)
	}
	sealedSQL := buildSQL("read_parquet("+parquetFileList(files)+")", true)
	sealedRowsSQL, err := scratch.Query(sealedSQL, cutoff, until, fetchK)
	if err != nil {
		scratch.Close()
		return nil, fmt.Errorf("query sealed flow_samples (topology): %w", err)
	}
	sealedRows, err := scan(sealedRowsSQL)
	scratch.Close()
	if err != nil {
		return nil, fmt.Errorf("scan sealed flow_samples (topology): %w", err)
	}

	t.mu.RLock()
	hotRowsSQL, err := t.hotDB.Query(buildSQL(tblFlow, true), cutoff, until, fetchK)
	if err != nil {
		t.mu.RUnlock()
		return nil, fmt.Errorf("query hot flow_samples (topology): %w", err)
	}
	hotRows, err := scan(hotRowsSQL)
	t.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("scan hot flow_samples (topology): %w", err)
	}

	merged := mergeSimpleAgg(sealedRows, hotRows)
	sort.Slice(merged, func(i, j int) bool { return merged[i].Bytes > merged[j].Bytes })
	if len(merged) > topologyMaxEdges {
		merged = merged[:topologyMaxEdges]
	}
	return merged, nil
}

// queryTopologyNodeTotals sums each ip's total bytes/packets across every
// private-to-private flow it appears in (as either src or dst), restricted
// to the given ip set -- topology.go's node totals reflect ALL of a
// node's private-private traffic, not just whichever edges made the
// top-topologyMaxEdges cut, so this is a second, separate aggregation.
func (t *tsStore) queryTopologyNodeTotals(cutoff, until int64, ips []uint32) (map[uint32]aggRow[uint32], error) {
	if len(ips) == 0 {
		return map[uint32]aggRow[uint32]{}, nil
	}
	var inList strings.Builder
	inList.WriteByte('(')
	for i, ip := range ips {
		if i > 0 {
			inList.WriteString(", ")
		}
		inList.WriteString(strconv.FormatInt(int64(ip), 10))
	}
	inList.WriteByte(')')

	privateFilter := isPrivateIPv4SQL("src_ip") + " AND " + isPrivateIPv4SQL("dst_ip")
	buildSQL := func(source string) string {
		return fmt.Sprintf(`SELECT ip, SUM(packets), SUM(bytes) FROM (
			SELECT src_ip AS ip, packets, bytes FROM %[1]s WHERE ts >= ? AND ts <= ? AND %[2]s AND src_ip IN %[3]s
			UNION ALL
			SELECT dst_ip AS ip, packets, bytes FROM %[1]s WHERE ts >= ? AND ts <= ? AND %[2]s AND dst_ip IN %[3]s
		) GROUP BY ip`, source, privateFilter, inList.String())
	}
	scan := func(rows *sql.Rows) (map[uint32]aggRow[uint32], error) {
		defer rows.Close()
		out := map[uint32]aggRow[uint32]{}
		for rows.Next() {
			var ip, packets, bytes int64
			if err := rows.Scan(&ip, &packets, &bytes); err != nil {
				return nil, err
			}
			out[uint32(ip)] = aggRow[uint32]{Key: uint32(ip), Packets: uint64(packets), Bytes: uint64(bytes)}
		}
		return out, rows.Err()
	}

	files, err := t.listSealedFiles(tblFlow, cutoff, until)
	if err != nil {
		return nil, fmt.Errorf("list sealed files for %s (topology nodes): %w", tblFlow, err)
	}

	totals := map[uint32]aggRow[uint32]{}
	mergeInto := func(src map[uint32]aggRow[uint32]) {
		for ip, r := range src {
			e := totals[ip]
			e.Key = ip
			e.Packets += r.Packets
			e.Bytes += r.Bytes
			totals[ip] = e
		}
	}

	if len(files) > 0 {
		scratch, err := openScratchDuckDB()
		if err != nil {
			return nil, fmt.Errorf("open scratch duckdb: %w", err)
		}
		sealedRowsSQL, err := scratch.Query(buildSQL("read_parquet("+parquetFileList(files)+")"), cutoff, until, cutoff, until)
		if err != nil {
			scratch.Close()
			return nil, fmt.Errorf("query sealed flow_samples (topology nodes): %w", err)
		}
		sealed, err := scan(sealedRowsSQL)
		scratch.Close()
		if err != nil {
			return nil, fmt.Errorf("scan sealed flow_samples (topology nodes): %w", err)
		}
		mergeInto(sealed)
	}

	t.mu.RLock()
	hotRowsSQL, err := t.hotDB.Query(buildSQL(tblFlow), cutoff, until, cutoff, until)
	if err != nil {
		t.mu.RUnlock()
		return nil, fmt.Errorf("query hot flow_samples (topology nodes): %w", err)
	}
	hot, err := scan(hotRowsSQL)
	t.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("scan hot flow_samples (topology nodes): %w", err)
	}
	mergeInto(hot)

	return totals, nil
}
