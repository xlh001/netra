package main

import (
	"encoding/binary"
	"net"
	"sort"
	"sync"
)

type iocCache struct {
	mu    sync.RWMutex
	exact map[uint32]string
	cidrs []cidrTagEntry
}

func newIOCCache() *iocCache {
	return &iocCache{exact: map[uint32]string{}}
}

func (c *iocCache) rebuild(records []IOCEntryRecord) {
	exact := map[uint32]string{}
	var cidrs []cidrTagEntry
	for _, r := range records {
		if r.Kind == "cidr" {
			_, network, err := net.ParseCIDR(r.Value)
			if err != nil {
				continue
			}
			ones, _ := network.Mask.Size()
			cidrs = append(cidrs, cidrTagEntry{network: network, label: r.Label, ones: ones})
			continue
		}
		ip, err := ipToUint32(r.Value)
		if err != nil {
			continue
		}
		exact[ip] = r.Label
	}
	sort.Slice(cidrs, func(i, j int) bool { return cidrs[i].ones > cidrs[j].ones })

	c.mu.Lock()
	c.exact = exact
	c.cidrs = cidrs
	c.mu.Unlock()
}

func annotateIOCAlerts(ioc *iocCache, alerts []ThreatAlertRecord) {
	if ioc == nil {
		return
	}
	for i := range alerts {
		if alerts[i].Kind != string(AlertKindIOC) {
			continue
		}
		ip, err := ipToUint32(alerts[i].IP)
		if err != nil {
			continue
		}
		if label := ioc.lookup(ip); label != "" {
			alerts[i].Label = label
		}
	}
}

func (c *iocCache) lookup(ip uint32) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if label, ok := c.exact[ip]; ok {
		return label
	}
	if len(c.cidrs) == 0 {
		return ""
	}
	addr := make(net.IP, 4)
	binary.LittleEndian.PutUint32(addr, ip)
	for _, ct := range c.cidrs {
		if ct.network.Contains(addr) {
			return ct.label
		}
	}
	return ""
}
