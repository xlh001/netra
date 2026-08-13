package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
)

type kafkaExporter struct {
	mu     sync.RWMutex
	writer *kafkago.Writer
}

type kafkaExportPayload struct {
	Timestamp    time.Time         `json:"timestamp"`
	ProtoPackets map[string]uint64 `json:"protoPackets"`
	ProtoBytes   map[string]uint64 `json:"protoBytes"`
	Flows        []FlowStat        `json:"flows"`
}

func newKafkaWriter(cfg ConfigDTO) (*kafkago.Writer, error) {
	if !cfg.KafkaEnabled {
		return nil, nil
	}
	brokers := splitKafkaBrokers(cfg.KafkaBrokers)
	if len(brokers) == 0 || cfg.KafkaTopic == "" {
		return nil, fmt.Errorf("kafka enabled but brokers/topic not configured")
	}

	var transport *kafkago.Transport
	if cfg.KafkaSASLUsername != "" || cfg.KafkaTLS {
		transport = &kafkago.Transport{}
		if cfg.KafkaSASLUsername != "" {
			transport.SASL = plain.Mechanism{Username: cfg.KafkaSASLUsername, Password: cfg.KafkaSASLPassword}
		}
		if cfg.KafkaTLS {
			transport.TLS = &tls.Config{}
		}
	}

	return &kafkago.Writer{
		Addr:      kafkago.TCP(brokers...),
		Topic:     cfg.KafkaTopic,
		Balancer:  &kafkago.LeastBytes{},
		Transport: transport,

		Async:        true,
		BatchTimeout: 1 * time.Second,
	}, nil
}

func splitKafkaBrokers(s string) []string {
	var out []string
	for _, b := range strings.Split(s, ",") {
		b = strings.TrimSpace(b)
		if b != "" {
			out = append(out, b)
		}
	}
	return out
}

func applyKafkaConfig(exp *kafkaExporter, cfg *Config) {
	v := cfg.Snapshot()
	writer, err := newKafkaWriter(v)
	if err != nil {
		log.Printf("kafka: %v -- export disabled", err)
		writer = nil
	}

	exp.mu.Lock()
	old := exp.writer
	exp.writer = writer
	exp.mu.Unlock()

	if old != nil {
		if err := old.Close(); err != nil {
			log.Printf("kafka: closing previous writer: %v", err)
		}
	}
}

func (exp *kafkaExporter) Publish(snap tickSnapshot) {
	exp.mu.RLock()
	writer := exp.writer
	exp.mu.RUnlock()
	if writer == nil {
		return
	}

	payload := kafkaExportPayload{
		Timestamp:    snap.start,
		ProtoPackets: protoMapByName(snap.protoPackets),
		ProtoBytes:   protoMapByName(snap.protoBytes),
		Flows:        flowSamplesToStats(snap.flows),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("kafka: encode payload: %v", err)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := writer.WriteMessages(ctx, kafkago.Message{Value: body}); err != nil {
			log.Printf("kafka: publish failed: %v", err)
		}
	}()
}

func (exp *kafkaExporter) Close() {
	exp.mu.Lock()
	defer exp.mu.Unlock()
	if exp.writer != nil {
		exp.writer.Close()
		exp.writer = nil
	}
}

func protoMapByName(m map[uint8]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(m))
	for k, v := range m {
		out[protoName(k)] = v
	}
	return out
}

func flowSamplesToStats(samples []flowSample) []FlowStat {
	out := make([]FlowStat, 0, len(samples))
	for _, s := range samples {
		out = append(out, FlowStat{
			SrcIP: ipString(s.key.Saddr), SrcPort: s.key.Sport,
			DstIP: ipString(s.key.Daddr), DstPort: s.key.Dport,
			Proto: protoName(s.key.Proto), Service: serviceName(s.key.Dport),
			Domain:  s.domain,
			Packets: s.packets, Bytes: s.bytes,
		})
	}
	return out
}
