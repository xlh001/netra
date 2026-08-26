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

type ifaceConfig struct {
	name           string
	promiscByNetra bool
}

type ifaceCounterSample struct {
	rxPackets uint64
	rxBytes   uint64
	at        time.Time
}

type ifaceRate struct {
	rxPPS float64
	rxBPS float64
}

type monitor struct {
	mu           sync.Mutex
	lastCPU      cpuSample
	haveLastCPU  bool
	cpuPercent   float64
	processStart time.Time
	dbPath       string
	store        *Store

	ifaces      []ifaceConfig
	genericMode bool

	lastIfaceSamples map[string]ifaceCounterSample
	ifaceRates       map[string]ifaceRate
}

func newMonitor(dbPath string, store *Store, ifaces []ifaceConfig, genericMode bool) *monitor {
	m := &monitor{processStart: time.Now(), dbPath: dbPath, store: store, ifaces: ifaces, genericMode: genericMode}
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
		if err == nil {
			m.mu.Lock()
			if m.haveLastCPU {
				m.cpuPercent = cpuPercentBetween(m.lastCPU, cur)
			}
			m.lastCPU = cur
			m.haveLastCPU = true
			m.mu.Unlock()
		}
		m.sampleIfaceCounters()
	}
}

func (m *monitor) sampleIfaceCounters() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ifc := range m.ifaces {
		rxPackets, rxBytes, err := readIfaceCounters(ifc.name)
		if err != nil {
			continue
		}
		cur := ifaceCounterSample{rxPackets: rxPackets, rxBytes: rxBytes, at: now}
		if prev, ok := m.lastIfaceSamples[ifc.name]; ok && cur.rxPackets >= prev.rxPackets && cur.rxBytes >= prev.rxBytes {
			dt := cur.at.Sub(prev.at).Seconds()
			if dt > 0 {
				if m.ifaceRates == nil {
					m.ifaceRates = map[string]ifaceRate{}
				}
				m.ifaceRates[ifc.name] = ifaceRate{
					rxPPS: float64(cur.rxPackets-prev.rxPackets) / dt,
					rxBPS: float64(cur.rxBytes-prev.rxBytes) * 8 / dt,
				}
			}
		}
		if m.lastIfaceSamples == nil {
			m.lastIfaceSamples = map[string]ifaceCounterSample{}
		}
		m.lastIfaceSamples[ifc.name] = cur
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

func readSysfsUint64(path string) (uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
}

func readIfaceCounters(name string) (rxPackets, rxBytes uint64, err error) {
	rxPackets, err = readSysfsUint64(fmt.Sprintf("/sys/class/net/%s/statistics/rx_packets", name))
	if err != nil {
		return 0, 0, err
	}
	rxBytes, err = readSysfsUint64(fmt.Sprintf("/sys/class/net/%s/statistics/rx_bytes", name))
	if err != nil {
		return 0, 0, err
	}
	return rxPackets, rxBytes, nil
}

func readIfaceCarrier(name string) bool {
	v, err := readSysfsUint64(fmt.Sprintf("/sys/class/net/%s/carrier", name))
	return err == nil && v == 1
}

func readIfaceSpeedMbps(name string) int64 {
	b, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/speed", name))
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

type IfaceStatus struct {
	Name                  string  `json:"name"`
	PromiscEnabledByNetra bool    `json:"promiscEnabledByNetra"`
	CarrierUp             bool    `json:"carrierUp"`
	SpeedMbps             int64   `json:"speedMbps,omitempty"`
	RxPPS                 float64 `json:"rxPPS"`
	RxBPS                 float64 `json:"rxBPS"`
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
	ActiveFlows        int `json:"activeFlows"`

	KafkaEnabled           bool  `json:"kafkaEnabled"`
	KafkaQueueBytes        int64 `json:"kafkaQueueBytes"`
	KafkaDroppedTicksTotal int64 `json:"kafkaDroppedTicksTotal"`
	KafkaWriteErrorsTotal  int64 `json:"kafkaWriteErrorsTotal"`

	ThreatAlertsScanTotal   int64 `json:"threatAlertsScanTotal"`
	ThreatAlertsDDoSTotal   int64 `json:"threatAlertsDdosTotal"`
	ThreatAlertsVolumeTotal int64 `json:"threatAlertsVolumeTotal"`

	MCPServersConnected int `json:"mcpServersConnected"`
	MCPServersTotal     int `json:"mcpServersTotal"`

	XDPGenericMode bool          `json:"xdpGenericMode"`
	Ifaces         []IfaceStatus `json:"ifaces"`
}

func (m *monitor) snapshot(agg *aggregator, kafkaExp *kafkaExporter, mcpMgr *mcpManager) MonitorSnapshot {
	m.mu.Lock()
	cpuPercent := m.cpuPercent
	ifaces := m.ifaces
	rates := m.ifaceRates
	genericMode := m.genericMode
	m.mu.Unlock()

	s := MonitorSnapshot{
		NumCPU:             runtime.NumCPU(),
		CPUPercent:         cpuPercent,
		Goroutines:         runtime.NumGoroutine(),
		ProcessUptimeSec:   int64(time.Since(m.processStart).Seconds()),
		ReadFailuresRecent: agg.recentReadFailures(),
		ActiveFlows:        agg.activeFlowCount(),
	}

	s.ThreatAlertsScanTotal, s.ThreatAlertsDDoSTotal, s.ThreatAlertsVolumeTotal = agg.alertTotals()
	s.KafkaEnabled = kafkaExp.enabled()
	s.KafkaQueueBytes, s.KafkaDroppedTicksTotal, s.KafkaWriteErrorsTotal = kafkaExp.stats()
	s.MCPServersConnected, s.MCPServersTotal = mcpMgr.connectedCounts()

	s.XDPGenericMode = genericMode
	s.Ifaces = make([]IfaceStatus, 0, len(ifaces))
	for _, ifc := range ifaces {
		st := IfaceStatus{
			Name:                  ifc.name,
			PromiscEnabledByNetra: ifc.promiscByNetra,
			CarrierUp:             readIfaceCarrier(ifc.name),
			SpeedMbps:             readIfaceSpeedMbps(ifc.name),
		}
		if rate, ok := rates[ifc.name]; ok {
			st.RxPPS = rate.rxPPS
			st.RxBPS = rate.rxBPS
		}
		s.Ifaces = append(s.Ifaces, st)
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
