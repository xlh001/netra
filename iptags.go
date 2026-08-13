package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"sync"
)

type ipTagCache struct {
	mu    sync.RWMutex
	exact map[uint32]string
	cidrs []cidrTagEntry
}

type cidrTagEntry struct {
	network *net.IPNet
	label   string
	ones    int
}

func newIPTagCache() *ipTagCache {
	return &ipTagCache{exact: map[uint32]string{}}
}

func (c *ipTagCache) rebuild(records []IPTagRecord) {
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

func (c *ipTagCache) lookup(ip uint32) string {
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

func resolveIPTag(tags *ipTagCache, ipStr string) string {
	if tags == nil {
		return ""
	}
	ip, err := ipToUint32(ipStr)
	if err != nil {
		return ""
	}
	return tags.lookup(ip)
}

func annotateIPTagsReport(tags *ipTagCache, report *Report) {
	for i := range report.TopIPs {
		report.TopIPs[i].Label = resolveIPTag(tags, report.TopIPs[i].IP)
	}
	for i := range report.TopFlows {
		report.TopFlows[i].SrcLabel = resolveIPTag(tags, report.TopFlows[i].SrcIP)
		report.TopFlows[i].DstLabel = resolveIPTag(tags, report.TopFlows[i].DstIP)
	}
	for i := range report.ScanAlerts {
		report.ScanAlerts[i].Label = resolveIPTag(tags, report.ScanAlerts[i].IP)
	}
}

func annotateIPTagsFlows(tags *ipTagCache, flows []FlowStat) {
	for i := range flows {
		flows[i].SrcLabel = resolveIPTag(tags, flows[i].SrcIP)
		flows[i].DstLabel = resolveIPTag(tags, flows[i].DstIP)
	}
}

func annotateIPTagsIPs(tags *ipTagCache, ips []IPStat) {
	for i := range ips {
		ips[i].Label = resolveIPTag(tags, ips[i].IP)
	}
}

func annotateIPTagsTopology(tags *ipTagCache, topo *Topology) {
	for i := range topo.Nodes {
		topo.Nodes[i].Label = resolveIPTag(tags, topo.Nodes[i].IP)
	}
}

func annotateIPTagsAlerts(tags *ipTagCache, alerts []ThreatAlertRecord) {
	for i := range alerts {
		alerts[i].Label = resolveIPTag(tags, alerts[i].IP)
	}
}

func formatIPLabel(ip, label string) string {
	if label == "" {
		return ip
	}
	return fmt.Sprintf("%s (%s)", label, ip)
}
