//go:build 386 || amd64 || arm || arm64 || loong64 || mips64le || mipsle || ppc64le || riscv64 || wasm

package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"structs"

	"github.com/cilium/ebpf"
)

type xdpflowFlowKey struct {
	_     structs.HostLayout
	Saddr uint32
	Daddr uint32
	Sport uint16
	Dport uint16
	Proto uint8
	Pad   [3]uint8
}

type xdpflowFlowStats struct {
	_       structs.HostLayout
	Packets uint64
	Bytes   uint64
}

const (
	xdpflowMapFlowStatsMap  = "flow_stats_map"
	xdpflowMapHttpEvents    = "http_events"
	xdpflowMapSniEvents     = "sni_events"
	xdpflowProgXdpFlowCount = "xdp_flow_count"
)

func loadXdpflow() (*ebpf.CollectionSpec, error) {
	reader := bytes.NewReader(_XdpflowBytes)
	spec, err := ebpf.LoadCollectionSpecFromReader(reader)
	if err != nil {
		return nil, fmt.Errorf("can't load xdpflow: %w", err)
	}

	return spec, err
}

func loadXdpflowObjects(obj any, opts *ebpf.CollectionOptions) error {
	spec, err := loadXdpflow()
	if err != nil {
		return err
	}

	return spec.LoadAndAssign(obj, opts)
}

type xdpflowSpecs struct {
	xdpflowProgramSpecs
	xdpflowMapSpecs
	xdpflowVariableSpecs
}

type xdpflowProgramSpecs struct {
	XdpFlowCount *ebpf.ProgramSpec `ebpf:"xdp_flow_count"`
}

type xdpflowMapSpecs struct {
	FlowStatsMap *ebpf.MapSpec `ebpf:"flow_stats_map"`
	HttpEvents   *ebpf.MapSpec `ebpf:"http_events"`
	SniEvents    *ebpf.MapSpec `ebpf:"sni_events"`
}

type xdpflowVariableSpecs struct {
}

type xdpflowObjects struct {
	xdpflowPrograms
	xdpflowMaps
	xdpflowVariables
}

func (o *xdpflowObjects) Close() error {
	return _XdpflowClose(
		&o.xdpflowPrograms,
		&o.xdpflowMaps,
	)
}

type xdpflowMaps struct {
	FlowStatsMap *ebpf.Map `ebpf:"flow_stats_map"`
	HttpEvents   *ebpf.Map `ebpf:"http_events"`
	SniEvents    *ebpf.Map `ebpf:"sni_events"`
}

func (m *xdpflowMaps) Close() error {
	return _XdpflowClose(
		m.FlowStatsMap,
		m.HttpEvents,
		m.SniEvents,
	)
}

type xdpflowVariables struct {
}

type xdpflowPrograms struct {
	XdpFlowCount *ebpf.Program `ebpf:"xdp_flow_count"`
}

func (p *xdpflowPrograms) Close() error {
	return _XdpflowClose(
		p.XdpFlowCount,
	)
}

func _XdpflowClose(closers ...io.Closer) error {
	for _, closer := range closers {
		if err := closer.Close(); err != nil {
			return err
		}
	}
	return nil
}

//go:embed xdpflow_bpfel.o
var _XdpflowBytes []byte
