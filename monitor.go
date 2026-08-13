package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type monitor struct {
	mu           sync.Mutex
	lastCPU      cpuSample
	haveLastCPU  bool
	cpuPercent   float64
	processStart time.Time
	dbPath       string
	store        *Store
}

func newMonitor(dbPath string, store *Store) *monitor {
	m := &monitor{processStart: time.Now(), dbPath: dbPath, store: store}
	if s, err := readCPUSample(); err == nil {
		m.lastCPU = s
		m.haveLastCPU = true
	}
	return m
}

func (m *monitor) run() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		cur, err := readCPUSample()
		if err != nil {
			continue
		}
		m.mu.Lock()
		if m.haveLastCPU {
			m.cpuPercent = cpuPercentBetween(m.lastCPU, cur)
		}
		m.lastCPU = cur
		m.haveLastCPU = true
		m.mu.Unlock()
	}
}

type cpuSample struct {
	idle  uint64
	total uint64
}

func readCPUSample() (cpuSample, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSample{}, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var vals [8]uint64
		for i, fstr := range fields {
			if i >= len(vals) {
				break
			}
			v, _ := strconv.ParseUint(fstr, 10, 64)
			vals[i] = v
		}
		user, nice, system, idle, iowait, irq, softirq, steal := vals[0], vals[1], vals[2], vals[3], vals[4], vals[5], vals[6], vals[7]
		return cpuSample{idle: idle + iowait, total: user + nice + system + idle + iowait + irq + softirq + steal}, nil
	}
	return cpuSample{}, fmt.Errorf("no aggregate cpu line in /proc/stat")
}

func cpuPercentBetween(prev, cur cpuSample) float64 {
	totalDelta := float64(cur.total - prev.total)
	if totalDelta <= 0 {
		return 0
	}
	idleDelta := float64(cur.idle - prev.idle)
	used := (totalDelta - idleDelta) / totalDelta * 100
	switch {
	case used < 0:
		return 0
	case used > 100:
		return 100
	default:
		return used
	}
}

func readMemInfo() (totalBytes, availableBytes uint64, err error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		var target *uint64
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			target = &totalBytes
		case strings.HasPrefix(line, "MemAvailable:"):
			target = &availableBytes
		default:
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		*target = kb * 1024
	}
	return totalBytes, availableBytes, scanner.Err()
}

func readLoadAvg() (load1, load5, load15 float64, err error) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("unexpected /proc/loadavg format")
	}
	load1, _ = strconv.ParseFloat(fields[0], 64)
	load5, _ = strconv.ParseFloat(fields[1], 64)
	load15, _ = strconv.ParseFloat(fields[2], 64)
	return load1, load5, load15, nil
}

func readDiskUsage(path string) (totalBytes, usedBytes uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	if free > total {
		free = total
	}
	return total, total - free, nil
}

type MonitorSnapshot struct {
	NumCPU          int     `json:"numCPU"`
	CPUPercent      float64 `json:"cpuPercent"`
	LoadAvg1        float64 `json:"loadAvg1"`
	LoadAvg5        float64 `json:"loadAvg5"`
	LoadAvg15       float64 `json:"loadAvg15"`
	MemTotalBytes   uint64  `json:"memTotalBytes"`
	MemUsedBytes    uint64  `json:"memUsedBytes"`
	MemUsedPercent  float64 `json:"memUsedPercent"`
	DiskTotalBytes  uint64  `json:"diskTotalBytes"`
	DiskUsedBytes   uint64  `json:"diskUsedBytes"`
	DiskUsedPercent float64 `json:"diskUsedPercent"`

	Goroutines       int    `json:"goroutines"`
	HeapAllocBytes   uint64 `json:"heapAllocBytes"`
	HeapSysBytes     uint64 `json:"heapSysBytes"`
	NumGC            uint32 `json:"numGC"`
	ProcessUptimeSec int64  `json:"processUptimeSec"`

	PersistenceEnabled bool  `json:"persistenceEnabled"`
	DBFileSizeBytes    int64 `json:"dbFileSizeBytes,omitempty"`
	DBWALSizeBytes     int64 `json:"dbWalSizeBytes,omitempty"`
	TSStoreSizeBytes   int64 `json:"tsStoreSizeBytes,omitempty"`
	DBOpenConns        int   `json:"dbOpenConns,omitempty"`
	DBInUseConns       int   `json:"dbInUseConns,omitempty"`
	DBIdleConns        int   `json:"dbIdleConns,omitempty"`
	DBWaitCount        int64 `json:"dbWaitCount,omitempty"`

	ReadFailuresRecent int `json:"readFailuresRecent"`
}

func (m *monitor) snapshot(agg *aggregator) MonitorSnapshot {
	m.mu.Lock()
	cpuPercent := m.cpuPercent
	m.mu.Unlock()

	s := MonitorSnapshot{
		NumCPU:             runtime.NumCPU(),
		CPUPercent:         cpuPercent,
		Goroutines:         runtime.NumGoroutine(),
		ProcessUptimeSec:   int64(time.Since(m.processStart).Seconds()),
		ReadFailuresRecent: agg.recentReadFailures(),
	}

	if load1, load5, load15, err := readLoadAvg(); err == nil {
		s.LoadAvg1, s.LoadAvg5, s.LoadAvg15 = load1, load5, load15
	}

	if total, avail, err := readMemInfo(); err == nil {
		s.MemTotalBytes = total
		s.MemUsedBytes = total - avail
		if total > 0 {
			s.MemUsedPercent = float64(s.MemUsedBytes) / float64(total) * 100
		}
	}

	diskPath := "."
	if m.dbPath != "" {
		diskPath = m.dbPath
	}
	if total, used, err := readDiskUsage(diskPath); err == nil {
		s.DiskTotalBytes = total
		s.DiskUsedBytes = used
		if total > 0 {
			s.DiskUsedPercent = float64(used) / float64(total) * 100
		}
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	s.HeapAllocBytes = mem.HeapAlloc
	s.HeapSysBytes = mem.HeapSys
	s.NumGC = mem.NumGC

	if m.store != nil {
		s.PersistenceEnabled = true
		if info, err := os.Stat(m.dbPath); err == nil {
			s.DBFileSizeBytes = info.Size()
		}
		if info, err := os.Stat(m.dbPath + "-wal"); err == nil {
			s.DBWALSizeBytes = info.Size()
		}
		if m.store.ts != nil {
			s.TSStoreSizeBytes = m.store.ts.DiskUsageBytes()
		}
		dbStats := m.store.db.Stats()
		s.DBOpenConns = dbStats.OpenConnections
		s.DBInUseConns = dbStats.InUse
		s.DBIdleConns = dbStats.Idle
		s.DBWaitCount = dbStats.WaitCount
	}

	return s
}
