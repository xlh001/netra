package main

import (
	"fmt"
	"time"
)

const topologyMaxEdges = 18

type TopologyNode struct {
	IP      string `json:"ip"`
	Label   string `json:"label,omitempty"`
	Bytes   uint64 `json:"bytes"`
	Packets uint64 `json:"packets"`
}

type TopologyEdge struct {
	Src     string `json:"src"`
	Dst     string `json:"dst"`
	Bytes   uint64 `json:"bytes"`
	Packets uint64 `json:"packets"`
}

type Topology struct {
	Window string         `json:"window"`
	Nodes  []TopologyNode `json:"nodes"`
	Edges  []TopologyEdge `json:"edges"`
}

// QueryTopology returns edges/nodes for private-to-private IP pairs only,
// with each node's totals covering every private-private flow it
// participates in (not just the edges that made the top-topologyMaxEdges
// cut), sourced from tsStore.queryTopologyEdges/queryTopologyNodeTotals.
func (s *Store) QueryTopology(from, to time.Time) (Topology, error) {
	cutoff, until := from.Unix(), to.Unix()

	edgeRows, err := s.ts.queryTopologyEdges(cutoff, until)
	if err != nil {
		return Topology{}, fmt.Errorf("query topology edges: %w", err)
	}

	ipSet := map[uint32]bool{}
	var ips []uint32
	for _, e := range edgeRows {
		for _, ip := range [2]uint32{e.Key.A, e.Key.B} {
			if !ipSet[ip] {
				ipSet[ip] = true
				ips = append(ips, ip)
			}
		}
	}

	nodeTotals, err := s.ts.queryTopologyNodeTotals(cutoff, until, ips)
	if err != nil {
		return Topology{}, fmt.Errorf("query topology node totals: %w", err)
	}

	seen := map[uint32]bool{}
	nodes := []TopologyNode{}
	edges := []TopologyEdge{}
	for _, e := range edgeRows {
		edges = append(edges, TopologyEdge{
			Src: ipString(e.Key.A), Dst: ipString(e.Key.B),
			Bytes: e.Bytes, Packets: e.Packets,
		})
		for _, ip := range [2]uint32{e.Key.A, e.Key.B} {
			if seen[ip] {
				continue
			}
			seen[ip] = true
			nt := nodeTotals[ip]
			nodes = append(nodes, TopologyNode{IP: ipString(ip), Bytes: nt.Bytes, Packets: nt.Packets})
		}
	}

	return Topology{Nodes: nodes, Edges: edges}, nil
}
