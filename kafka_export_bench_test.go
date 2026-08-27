package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func genSyntheticFlows(n int) []flowSample {
	r := rand.New(rand.NewSource(42))
	protos := []uint8{6, 17, 1, 112, 89, 47}
	out := make([]flowSample, n)
	for i := 0; i < n; i++ {
		out[i] = flowSample{
			key: xdpflowFlowKey{
				Saddr: r.Uint32(),
				Daddr: r.Uint32(),
				Sport: uint16(1024 + r.Intn(64000)),
				Dport: uint16(1 + r.Intn(65000)),
				Proto: protos[r.Intn(len(protos))],
			},
			packets: uint64(1 + r.Intn(100000)),
			bytes:   uint64(1 + r.Intn(100000000)),
		}
	}
	return out
}

var benchFlowCounts = []int{2000, 10000, 50000, 100000}

func flowsToRecords(flows []FlowStat, ts time.Time) []kafkaFlowRecord {
	records := make([]kafkaFlowRecord, len(flows))
	for i, f := range flows {
		records[i] = kafkaFlowRecord{Timestamp: ts, FlowStat: f}
	}
	return records
}

func BenchmarkKafkaExportFull(b *testing.B) {
	for _, n := range benchFlowCounts {
		samples := genSyntheticFlows(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			var lastLen int
			for i := 0; i < b.N; i++ {
				records := flowsToRecords(flowSamplesToStats(samples), time.Now())
				body, err := json.Marshal(records)
				if err != nil {
					b.Fatal(err)
				}
				lastLen = len(body)
			}
			b.ReportMetric(float64(lastLen), "payload_bytes")
		})
	}
}

func BenchmarkKafkaExportConvertOnly(b *testing.B) {
	for _, n := range benchFlowCounts {
		samples := genSyntheticFlows(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = flowSamplesToStats(samples)
			}
		})
	}
}

func BenchmarkKafkaExportMarshalOnly(b *testing.B) {
	for _, n := range benchFlowCounts {
		records := flowsToRecords(flowSamplesToStats(genSyntheticFlows(n)), time.Now())
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := json.Marshal(records); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
