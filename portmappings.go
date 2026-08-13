package main

import "sync"

type portMappingCache struct {
	mu sync.RWMutex
	m  map[uint16]string
}

func newPortMappingCache() *portMappingCache {
	m := make(map[uint16]string, len(wellKnownPorts))
	for port, service := range wellKnownPorts {
		m[port] = service
	}
	return &portMappingCache{m: m}
}

func (c *portMappingCache) rebuild(records []PortMappingRecord) {
	m := make(map[uint16]string, len(records))
	for _, r := range records {
		m[r.Port] = r.Service
	}
	c.mu.Lock()
	c.m = m
	c.mu.Unlock()
}

func (c *portMappingCache) lookup(port uint16) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.m[port]
}
