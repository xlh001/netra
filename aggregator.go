package main

import (
	"sort"
	"sync"
	"time"
)

const defaultTopKPerBucket = 200

const defaultDBFlowTopK = 2000

const maxRetention = 1 * time.Hour

const (
	defaultAnomalyWindow              = 1 * time.Minute
	defaultAnomalyPeerThreshold       = 500
	defaultAnomalyAvgPacketsThreshold = 10.0
)

const defaultVolumeThresholdBytes = 500 * 1024 * 1024

type portKey struct {
	proto uint8
	port  uint16
}

type bucket struct {
	start             time.Time
	protoPackets      map[uint8]uint64
	protoBytes        map[uint8]uint64
	flows             map[xdpflowFlowKey]xdpflowFlowStats
	ips               map[uint32]xdpflowFlowStats
	ports             map[portKey]xdpflowFlowStats
	distinctFlowCount int
}

type scanDestEntry struct {
	lastSeen time.Time
	packets  uint64
}

type volumeSample struct {
	ts      time.Time
	bytes   uint64
	packets uint64
}

type aggregator struct {
	mu           sync.RWMutex
	buckets      []bucket
	domainByFlow map[xdpflowFlowKey]domainEntry
	readFailures []time.Time
	interval     time.Duration
	scanDests    map[uint32]map[uint32]scanDestEntry
	ddosSrcs     map[uint32]map[uint32]scanDestEntry
	volumeTotals map[uint32][]volumeSample

	anomalyEnabled             bool
	anomalyWindow              time.Duration
	anomalyPeerThreshold       int
	anomalyAvgPacketsThreshold float64

	volumeThresholdBytes uint64

	topKPerBucket int
	dbFlowTopK    int
}

func (a *aggregator) recordReadFailure(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.readFailures = append(a.readFailures, now)
	cutoff := now.Add(-maxRetention)
	i := 0
	for i < len(a.readFailures) && a.readFailures[i].Before(cutoff) {
		i++
	}
	a.readFailures = a.readFailures[i:]
}

func (a *aggregator) recentReadFailures() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.readFailures)
}

type domainEntry struct {
	hostname string
	seenAt   time.Time
}

func newAggregator(interval time.Duration) *aggregator {
	return &aggregator{
		domainByFlow:               map[xdpflowFlowKey]domainEntry{},
		interval:                   interval,
		scanDests:                  map[uint32]map[uint32]scanDestEntry{},
		ddosSrcs:                   map[uint32]map[uint32]scanDestEntry{},
		volumeTotals:               map[uint32][]volumeSample{},
		anomalyWindow:              defaultAnomalyWindow,
		anomalyPeerThreshold:       defaultAnomalyPeerThreshold,
		anomalyAvgPacketsThreshold: defaultAnomalyAvgPacketsThreshold,
		volumeThresholdBytes:       defaultVolumeThresholdBytes,
		topKPerBucket:              defaultTopKPerBucket,
		dbFlowTopK:                 defaultDBFlowTopK,
	}
}

func (a *aggregator) UpdateAnomalyConfig(enabled bool, window time.Duration, peerThreshold int, avgPacketsThreshold float64, volumeThresholdBytes uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	wasEnabled := a.anomalyEnabled
	a.anomalyEnabled = enabled
	a.anomalyWindow = window
	a.anomalyPeerThreshold = peerThreshold
	a.anomalyAvgPacketsThreshold = avgPacketsThreshold
	a.volumeThresholdBytes = volumeThresholdBytes
	if wasEnabled && !enabled {
		a.scanDests = map[uint32]map[uint32]scanDestEntry{}
		a.ddosSrcs = map[uint32]map[uint32]scanDestEntry{}
		a.volumeTotals = map[uint32][]volumeSample{}
	}
}

func (a *aggregator) UpdateCapacityConfig(dbFlowTopK, topKPerBucket int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dbFlowTopK = dbFlowTopK
	a.topKPerBucket = topKPerBucket
}

func (a *aggregator) recordDomain(key xdpflowFlowKey, hostname string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.domainByFlow[key] = domainEntry{hostname: hostname, seenAt: time.Now()}
}

func (a *aggregator) cleanupDomainsLocked(cutoff time.Time) {
	for k, e := range a.domainByFlow {
		if e.seenAt.Before(cutoff) {
			delete(a.domainByFlow, k)
		}
	}
}

func (a *aggregator) recordAnomalyCandidates(now time.Time, cur, prev map[xdpflowFlowKey]xdpflowFlowStats) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.anomalyEnabled {
		return
	}

	for k, s := range cur {
		dests := a.scanDests[k.Saddr]
		if dests == nil {
			dests = map[uint32]scanDestEntry{}
			a.scanDests[k.Saddr] = dests
		}

		e := dests[k.Daddr]
		e.lastSeen = now
		e.packets = s.Packets
		dests[k.Daddr] = e

		srcs := a.ddosSrcs[k.Daddr]
		if srcs == nil {
			srcs = map[uint32]scanDestEntry{}
			a.ddosSrcs[k.Daddr] = srcs
		}
		se := srcs[k.Saddr]
		se.lastSeen = now
		se.packets = s.Packets
		srcs[k.Saddr] = se

		var dp, db uint64
		if p, ok := prev[k]; ok && s.Packets >= p.Packets && s.Bytes >= p.Bytes {
			dp = s.Packets - p.Packets
			db = s.Bytes - p.Bytes
		} else {
			dp = s.Packets
			db = s.Bytes
		}
		if dp == 0 && db == 0 {
			continue
		}
		a.volumeTotals[k.Saddr] = append(a.volumeTotals[k.Saddr], volumeSample{ts: now, bytes: db, packets: dp})
		a.volumeTotals[k.Daddr] = append(a.volumeTotals[k.Daddr], volumeSample{ts: now, bytes: db, packets: dp})
	}

	cutoff := now.Add(-a.anomalyWindow)
	for src, dests := range a.scanDests {
		for dst, e := range dests {
			if e.lastSeen.Before(cutoff) {
				delete(dests, dst)
			}
		}
		if len(dests) == 0 {
			delete(a.scanDests, src)
		}
	}
	for dst, srcs := range a.ddosSrcs {
		for src, e := range srcs {
			if e.lastSeen.Before(cutoff) {
				delete(srcs, src)
			}
		}
		if len(srcs) == 0 {
			delete(a.ddosSrcs, dst)
		}
	}
	for ip, samples := range a.volumeTotals {
		i := 0
		for i < len(samples) && samples[i].ts.Before(cutoff) {
			i++
		}
		if i == len(samples) {
			delete(a.volumeTotals, ip)
		} else if i > 0 {
			a.volumeTotals[ip] = samples[i:]
		}
	}
}

type AlertKind string

const (
	AlertKindScan   AlertKind = "scan"
	AlertKindDDoS   AlertKind = "ddos"
	AlertKindVolume AlertKind = "volume"
)

type ThreatAlert struct {
	Kind          AlertKind `json:"kind"`
	IP            string    `json:"ip"`
	Label         string    `json:"label,omitempty"`
	DistinctPeers int       `json:"distinctPeers,omitempty"`
	VolumeBytes   uint64    `json:"volumeBytes,omitempty"`
}

func (a *aggregator) threatAlerts() []ThreatAlert {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.threatAlertsLocked()
}

func (a *aggregator) threatAlertsLocked() []ThreatAlert {
	if !a.anomalyEnabled {
		return nil
	}
	var peerAlerts []ThreatAlert
	for src, dests := range a.scanDests {
		if len(dests) < a.anomalyPeerThreshold {
			continue
		}
		var totalPackets uint64
		for _, e := range dests {
			totalPackets += e.packets
		}
		avgPackets := float64(totalPackets) / float64(len(dests))
		if avgPackets >= a.anomalyAvgPacketsThreshold {
			continue
		}
		peerAlerts = append(peerAlerts, ThreatAlert{Kind: AlertKindScan, IP: ipString(src), DistinctPeers: len(dests)})
	}
	for dst, srcs := range a.ddosSrcs {
		if len(srcs) < a.anomalyPeerThreshold {
			continue
		}
		var totalPackets uint64
		for _, e := range srcs {
			totalPackets += e.packets
		}
		avgPackets := float64(totalPackets) / float64(len(srcs))
		if avgPackets >= a.anomalyAvgPacketsThreshold {
			continue
		}
		peerAlerts = append(peerAlerts, ThreatAlert{Kind: AlertKindDDoS, IP: ipString(dst), DistinctPeers: len(srcs)})
	}
	sort.Slice(peerAlerts, func(i, j int) bool { return peerAlerts[i].DistinctPeers > peerAlerts[j].DistinctPeers })

	var volumeAlerts []ThreatAlert
	for ip, samples := range a.volumeTotals {
		var totalBytes uint64
		for _, s := range samples {
			totalBytes += s.bytes
		}
		if totalBytes < a.volumeThresholdBytes {
			continue
		}
		volumeAlerts = append(volumeAlerts, ThreatAlert{Kind: AlertKindVolume, IP: ipString(ip), VolumeBytes: totalBytes})
	}
	sort.Slice(volumeAlerts, func(i, j int) bool { return volumeAlerts[i].VolumeBytes > volumeAlerts[j].VolumeBytes })

	return append(peerAlerts, volumeAlerts...)
}

func (a *aggregator) push(now time.Time, cur, prev map[xdpflowFlowKey]xdpflowFlowStats) tickSnapshot {

	a.mu.Lock()
	dbFlowTopK := a.dbFlowTopK
	topKPerBucket := a.topKPerBucket
	a.mu.Unlock()

	protoPackets := map[uint8]uint64{}
	protoBytes := map[uint8]uint64{}
	ipTotals := map[uint32]xdpflowFlowStats{}
	portTotals := map[portKey]xdpflowFlowStats{}

	type flowDelta struct {
		key     xdpflowFlowKey
		packets uint64
		bytes   uint64
	}
	activeFlows := make([]flowDelta, 0, len(cur))

	for k, s := range cur {
		var dp, db uint64
		if p, ok := prev[k]; ok && s.Packets >= p.Packets && s.Bytes >= p.Bytes {
			dp = s.Packets - p.Packets
			db = s.Bytes - p.Bytes
		} else {

			dp = s.Packets
			db = s.Bytes
		}
		if dp == 0 && db == 0 {
			continue
		}
		activeFlows = append(activeFlows, flowDelta{k, dp, db})

		protoPackets[k.Proto] += dp
		protoBytes[k.Proto] += db

		addStats(ipTotals, k.Saddr, dp, db)
		addStats(ipTotals, k.Daddr, dp, db)

		addPortStats(portTotals, portKey{proto: k.Proto, port: k.Dport}, dp, db)
	}

	distinctFlowCount := len(activeFlows)

	sort.Slice(activeFlows, func(i, j int) bool { return activeFlows[i].bytes > activeFlows[j].bytes })
	dbCut := len(activeFlows)
	if dbCut > dbFlowTopK {
		dbCut = dbFlowTopK
	}
	dbFlows := activeFlows[:dbCut]
	memCut := len(activeFlows)
	if memCut > topKPerBucket {
		memCut = topKPerBucket
	}
	activeFlows = activeFlows[:memCut]
	flows := make(map[xdpflowFlowKey]xdpflowFlowStats, len(activeFlows))
	for _, f := range activeFlows {
		flows[f.key] = xdpflowFlowStats{Packets: f.packets, Bytes: f.bytes}
	}

	ips := topKUint32(ipTotals, topKPerBucket)
	ports := topKPort(portTotals, topKPerBucket)

	b := bucket{
		start:             now,
		protoPackets:      protoPackets,
		protoBytes:        protoBytes,
		flows:             flows,
		ips:               ips,
		ports:             ports,
		distinctFlowCount: distinctFlowCount,
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	dbFlowSamples := make([]flowSample, len(dbFlows))
	for i, f := range dbFlows {
		dbFlowSamples[i] = flowSample{key: f.key, packets: f.packets, bytes: f.bytes, domain: a.domainByFlow[f.key].hostname}
	}

	a.buckets = append(a.buckets, b)
	cutoff := now.Add(-maxRetention)
	i := 0
	for i < len(a.buckets) && a.buckets[i].start.Before(cutoff) {
		i++
	}
	if i > 0 {
		a.buckets = a.buckets[i:]
	}
	a.cleanupDomainsLocked(cutoff)

	return tickSnapshot{
		start:             now,
		protoPackets:      protoPackets,
		protoBytes:        protoBytes,
		flows:             dbFlowSamples,
		ips:               ips,
		ports:             ports,
		distinctFlowCount: distinctFlowCount,
	}
}

func addStats(m map[uint32]xdpflowFlowStats, key uint32, dp, db uint64) {
	s := m[key]
	s.Packets += dp
	s.Bytes += db
	m[key] = s
}

func addPortStats(m map[portKey]xdpflowFlowStats, key portKey, dp, db uint64) {
	s := m[key]
	s.Packets += dp
	s.Bytes += db
	m[key] = s
}

func topKUint32(m map[uint32]xdpflowFlowStats, k int) map[uint32]xdpflowFlowStats {
	type kv struct {
		key   uint32
		stats xdpflowFlowStats
	}
	items := make([]kv, 0, len(m))
	for key, s := range m {
		items = append(items, kv{key, s})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].stats.Bytes > items[j].stats.Bytes })
	if len(items) > k {
		items = items[:k]
	}
	out := make(map[uint32]xdpflowFlowStats, len(items))
	for _, it := range items {
		out[it.key] = it.stats
	}
	return out
}

func topKPort(m map[portKey]xdpflowFlowStats, k int) map[portKey]xdpflowFlowStats {
	type kv struct {
		key   portKey
		stats xdpflowFlowStats
	}
	items := make([]kv, 0, len(m))
	for key, s := range m {
		items = append(items, kv{key, s})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].stats.Bytes > items[j].stats.Bytes })
	if len(items) > k {
		items = items[:k]
	}
	out := make(map[portKey]xdpflowFlowStats, len(items))
	for _, it := range items {
		out[it.key] = it.stats
	}
	return out
}

type Report struct {
	Window         string        `json:"window"`
	GeneratedAt    time.Time     `json:"generatedAt"`
	ActiveFlowsNow int           `json:"activeFlowsNow"`
	TotalPackets   uint64        `json:"totalPackets"`
	TotalBytes     uint64        `json:"totalBytes"`
	ReadFailures   int           `json:"readFailures,omitempty"`
	PossibleTicks  int           `json:"possibleTicks"`
	ScanAlerts     []ThreatAlert `json:"scanAlerts,omitempty"`
	Protocols      []ProtoStat   `json:"protocols"`
	TopFlows       []FlowStat    `json:"topFlows"`
	TopIPs         []IPStat      `json:"topIPs"`
	TopPorts       []PortStat    `json:"topPorts"`
	TopDomains     []DomainStat  `json:"topDomains"`
}

type ProtoStat struct {
	Proto   string `json:"proto"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

type FlowStat struct {
	SrcIP      string `json:"srcIP"`
	SrcPort    uint16 `json:"srcPort"`
	SrcLabel   string `json:"srcLabel,omitempty"`
	SrcCountry string `json:"srcCountry,omitempty"`
	DstIP      string `json:"dstIP"`
	DstPort    uint16 `json:"dstPort"`
	DstLabel   string `json:"dstLabel,omitempty"`
	DstCountry string `json:"dstCountry,omitempty"`
	Proto      string `json:"proto"`
	Service    string `json:"service,omitempty"`
	Domain     string `json:"domain,omitempty"`
	Packets    uint64 `json:"packets"`
	Bytes      uint64 `json:"bytes"`
}

type IPStat struct {
	IP      string `json:"ip"`
	Label   string `json:"label,omitempty"`
	Country string `json:"country,omitempty"`
	Org     string `json:"org,omitempty"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

type PortStat struct {
	Port    uint16 `json:"port"`
	Proto   string `json:"proto"`
	Service string `json:"service,omitempty"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

type ServiceStat struct {
	Service string `json:"service"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

type CategoryStat struct {
	Category string        `json:"category"`
	Packets  uint64        `json:"packets"`
	Bytes    uint64        `json:"bytes"`
	Services []ServiceStat `json:"services"`
}

func rankProtocols(packets, bytes map[uint8]uint64) []ProtoStat {
	type kv struct {
		proto uint8
		bytes uint64
	}
	items := make([]kv, 0, len(bytes))
	for p, b := range bytes {
		items = append(items, kv{p, b})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].bytes > items[j].bytes })
	out := make([]ProtoStat, 0, len(items))
	for _, it := range items {
		out = append(out, ProtoStat{Proto: protoName(it.proto), Packets: packets[it.proto], Bytes: it.bytes})
	}
	return out
}

type DomainStat struct {
	Domain  string `json:"domain"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

type TimeseriesPoint struct {
	Time  time.Time         `json:"time"`
	Bytes map[string]uint64 `json:"bytes"`
}

type Timeseries struct {
	Window string            `json:"window"`
	Points []TimeseriesPoint `json:"points"`
}

type FlowRatePoint struct {
	Time   time.Time `json:"time"`
	Count  int       `json:"count"`
	PerSec float64   `json:"perSec"`
}

type FlowRate struct {
	Points []FlowRatePoint `json:"points"`
}
