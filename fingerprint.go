package main

import (
	"log"
	"sync"
	"sync/atomic"
)

type ServiceFingerprint struct {
	Port  int    `json:"port"`
	Value string `json:"value"`
}

type ServiceFingerprintRow struct {
	IP    uint32
	Port  uint16
	Value string
}

type fingerprintKey struct {
	ip   uint32
	port uint16
}

const serviceFingerprintWriteQueueSlots = 256

type serviceFingerprintManager struct {
	mu      sync.Mutex
	cache   map[fingerprintKey]string
	store   *Store
	writeCh chan ServiceFingerprintRow
	dropped uint64
}

func newServiceFingerprintManager(store *Store) *serviceFingerprintManager {
	m := &serviceFingerprintManager{
		cache:   map[fingerprintKey]string{},
		store:   store,
		writeCh: make(chan ServiceFingerprintRow, serviceFingerprintWriteQueueSlots),
	}
	existing, err := store.LoadServiceFingerprints()
	if err != nil {
		log.Printf("service fingerprint: load cache failed: %v", err)
	}
	for _, f := range existing {
		m.cache[fingerprintKey{ip: f.IP, port: f.Port}] = f.Value
	}
	go m.writeLoop()
	return m
}

func (m *serviceFingerprintManager) writeLoop() {
	for row := range m.writeCh {
		if err := m.store.UpsertServiceFingerprint(row.IP, row.Port, row.Value); err != nil {
			log.Printf("service fingerprint: persist failed: %v", err)
		}
	}
}

func (m *serviceFingerprintManager) observe(ip uint32, port uint16, value string) {
	if value == "" {
		return
	}
	key := fingerprintKey{ip: ip, port: port}

	m.mu.Lock()
	if m.cache[key] == value {
		m.mu.Unlock()
		return
	}
	m.cache[key] = value
	m.mu.Unlock()

	select {
	case m.writeCh <- ServiceFingerprintRow{IP: ip, Port: port, Value: value}:
	default:
		atomic.AddUint64(&m.dropped, 1)
	}
}

func annotateFingerprintsFlows(store *Store, flows []FlowStat) {
	nums := make([]uint32, 0, len(flows))
	for _, f := range flows {
		ip := f.DstIP
		if f.SvcOnSrc {
			ip = f.SrcIP
		}
		if n, err := ipToUint32(ip); err == nil {
			nums = append(nums, n)
		}
	}
	byIP, err := store.QueryServiceFingerprintsForIPs(nums)
	if err != nil || len(byIP) == 0 {
		return
	}
	for i := range flows {
		ip, port := flows[i].DstIP, flows[i].DstPort
		if flows[i].SvcOnSrc {
			ip, port = flows[i].SrcIP, flows[i].SrcPort
		}
		n, err := ipToUint32(ip)
		if err != nil {
			continue
		}
		for _, f := range byIP[n] {
			if uint16(f.Port) == port {
				flows[i].Fingerprint = f.Value
				break
			}
		}
	}
}

func annotateFingerprintsIPs(store *Store, ips []IPStat) {
	nums := make([]uint32, 0, len(ips))
	for _, s := range ips {
		if n, err := ipToUint32(s.IP); err == nil {
			nums = append(nums, n)
		}
	}
	byIP, err := store.QueryServiceFingerprintsForIPs(nums)
	if err != nil || len(byIP) == 0 {
		return
	}
	for i := range ips {
		if n, err := ipToUint32(ips[i].IP); err == nil {
			ips[i].Fingerprints = byIP[n]
		}
	}
}
