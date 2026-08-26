package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
)

const kafkaWriteQueueSlots = 1024

const kafkaQueueMemoryBudget = 64 * 1024 * 1024

const estimatedBytesPerFlow = 130

type kafkaQueueItem struct {
	snap     tickSnapshot
	estBytes int64
}

type kafkaExporter struct {
	mu          sync.RWMutex
	writer      *kafkago.Writer
	queueCh     chan kafkaQueueItem
	closeCh     chan struct{}
	queuedBytes int64
}

type kafkaExportPayload struct {
	Timestamp time.Time  `json:"timestamp"`
	Flows     []FlowStat `json:"flows"`
}

func newKafkaExporter() *kafkaExporter {
	exp := &kafkaExporter{
		queueCh: make(chan kafkaQueueItem, kafkaWriteQueueSlots),
		closeCh: make(chan struct{}),
	}
	go exp.writeLoop()
	return exp
}

func newKafkaWriter(cfg ConfigDTO) (*kafkago.Writer, error) {
	if !cfg.KafkaEnabled {
		return nil, nil
	}
	brokers := splitKafkaBrokers(cfg.KafkaBrokers)
	if len(brokers) == 0 || cfg.KafkaTopic == "" {
		return nil, fmt.Errorf("kafka enabled but brokers/topic not configured")
	}

	w := &kafkago.Writer{
		Addr:     kafkago.TCP(brokers...),
		Topic:    cfg.KafkaTopic,
		Balancer: &kafkago.LeastBytes{},

		Async:        false,
		BatchTimeout: 1 * time.Second,
	}

	if cfg.KafkaSASLUsername != "" || cfg.KafkaTLS {
		transport := &kafkago.Transport{}
		if cfg.KafkaSASLUsername != "" {
			transport.SASL = plain.Mechanism{Username: cfg.KafkaSASLUsername, Password: cfg.KafkaSASLPassword}
		}
		if cfg.KafkaTLS {
			transport.TLS = &tls.Config{}
		}
		w.Transport = transport
	}

	return w, nil
}

func testKafkaConnection(cfg ConfigDTO) (int, error) {
	brokers := splitKafkaBrokers(cfg.KafkaBrokers)
	if len(brokers) == 0 {
		return 0, fmt.Errorf("brokers is required")
	}
	if cfg.KafkaTopic == "" {
		return 0, fmt.Errorf("topic is required")
	}

	dialer := &kafkago.Dialer{Timeout: 5 * time.Second}
	if cfg.KafkaSASLUsername != "" {
		dialer.SASLMechanism = plain.Mechanism{Username: cfg.KafkaSASLUsername, Password: cfg.KafkaSASLPassword}
	}
	if cfg.KafkaTLS {
		dialer.TLS = &tls.Config{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	const retries = 3
	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		for _, broker := range brokers {
			partitions, err := dialer.LookupPartitions(ctx, "tcp", broker, cfg.KafkaTopic)
			if err == nil {
				return len(partitions), nil
			}
			lastErr = err
		}
		if !errors.Is(lastErr, kafkago.LeaderNotAvailable) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	return 0, lastErr
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
	enabled := exp.writer != nil
	exp.mu.RUnlock()
	if !enabled {
		return
	}

	estBytes := int64(len(snap.kafkaFlows)) * estimatedBytesPerFlow
	if atomic.AddInt64(&exp.queuedBytes, estBytes) > kafkaQueueMemoryBudget {
		atomic.AddInt64(&exp.queuedBytes, -estBytes)
		log.Printf("kafka: write queue over memory budget, dropping tick %s", snap.start.Format(time.RFC3339))
		return
	}

	select {
	case exp.queueCh <- kafkaQueueItem{snap: snap, estBytes: estBytes}:
	default:
		atomic.AddInt64(&exp.queuedBytes, -estBytes)
		log.Printf("kafka: write queue full, dropping tick %s", snap.start.Format(time.RFC3339))
	}
}

func (exp *kafkaExporter) writeLoop() {
	for {
		select {
		case item := <-exp.queueCh:
			exp.writeTick(item.snap)
			atomic.AddInt64(&exp.queuedBytes, -item.estBytes)
		case <-exp.closeCh:
			return
		}
	}
}

func (exp *kafkaExporter) writeTick(snap tickSnapshot) {
	exp.mu.RLock()
	writer := exp.writer
	exp.mu.RUnlock()
	if writer == nil {
		return
	}

	payload := kafkaExportPayload{
		Timestamp: snap.start,
		Flows:     flowSamplesToStats(snap.kafkaFlows),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("kafka: encode payload: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := writer.WriteMessages(ctx, kafkago.Message{Value: body}); err != nil {
		log.Printf("kafka: publish failed: %v", err)
	}
}

func (exp *kafkaExporter) Close() {
	close(exp.closeCh)
	exp.mu.Lock()
	defer exp.mu.Unlock()
	if exp.writer != nil {
		exp.writer.Close()
		exp.writer = nil
	}
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
