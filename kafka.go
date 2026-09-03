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

	"github.com/oschwald/geoip2-golang"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
)

const kafkaWriteQueueSlots = 1024

const kafkaQueueMemoryBudget = 64 * 1024 * 1024

const estimatedBytesPerFlow = 130

const kafkaMaxMessageBytes = 900 * 1024

type kafkaQueueItem struct {
	snap     tickSnapshot
	estBytes int64
}

type kafkaExporter struct {
	mu                sync.RWMutex
	writer            *kafkago.Writer
	queueCh           chan kafkaQueueItem
	closeCh           chan struct{}
	queuedBytes       int64
	droppedTicksTotal int64
	writeErrorsTotal  int64

	geoDB  *geoip2.Reader
	ipTags *ipTagCache
}

type kafkaFlowRecord struct {
	Timestamp time.Time `json:"timestamp"`
	FlowStat
}

func newKafkaExporter(geoDB *geoip2.Reader, ipTags *ipTagCache) *kafkaExporter {
	exp := &kafkaExporter{
		queueCh: make(chan kafkaQueueItem, kafkaWriteQueueSlots),
		closeCh: make(chan struct{}),
		geoDB:   geoDB,
		ipTags:  ipTags,
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
		atomic.AddInt64(&exp.droppedTicksTotal, 1)
		log.Printf("kafka: write queue over memory budget, dropping tick %s", snap.start.Format(time.RFC3339))
		return
	}

	select {
	case exp.queueCh <- kafkaQueueItem{snap: snap, estBytes: estBytes}:
	default:
		atomic.AddInt64(&exp.queuedBytes, -estBytes)
		atomic.AddInt64(&exp.droppedTicksTotal, 1)
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

	flows := flowSamplesToStats(snap.kafkaFlows)
	annotateCountriesFlows(exp.geoDB, flows)
	annotateIPTagsFlows(exp.ipTags, flows)
	records := make([]kafkaFlowRecord, len(flows))
	for i, f := range flows {
		records[i] = kafkaFlowRecord{Timestamp: snap.start, FlowStat: f}
	}
	chunkSize := kafkaMaxMessageBytes / estimatedBytesPerFlow

	var msgs []kafkago.Message
	if len(records) == 0 {
		body, err := json.Marshal(records)
		if err != nil {
			log.Printf("kafka: encode payload: %v", err)
			return
		}
		msgs = append(msgs, kafkago.Message{Value: body})
	} else {
		for i := 0; i < len(records); i += chunkSize {
			end := i + chunkSize
			if end > len(records) {
				end = len(records)
			}
			body, err := json.Marshal(records[i:end])
			if err != nil {
				log.Printf("kafka: encode payload: %v", err)
				continue
			}
			msgs = append(msgs, kafkago.Message{Value: body})
		}
	}
	if len(msgs) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := writer.WriteMessages(ctx, msgs...); err != nil {
		atomic.AddInt64(&exp.writeErrorsTotal, 1)
		log.Printf("kafka: publish failed: %v", err)
	}
}

func (exp *kafkaExporter) stats() (queuedBytes, droppedTicks, writeErrors int64) {
	return atomic.LoadInt64(&exp.queuedBytes), atomic.LoadInt64(&exp.droppedTicksTotal), atomic.LoadInt64(&exp.writeErrorsTotal)
}

func (exp *kafkaExporter) enabled() bool {
	exp.mu.RLock()
	defer exp.mu.RUnlock()
	return exp.writer != nil
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
		srcPort, dstPort, svcPort := ntohs(s.key.Sport), ntohs(s.key.Dport), ntohs(s.svcPort)
		service, dpi, svcOnSrc := resolveServiceForFlow(srcPort, dstPort, svcPort, s.dpiService)
		out = append(out, FlowStat{
			SrcIP: ipString(s.key.Saddr), SrcPort: srcPort,
			DstIP: ipString(s.key.Daddr), DstPort: dstPort,
			Proto: protoName(s.key.Proto), Service: service, DPI: dpi, SvcOnSrc: svcOnSrc,
			Domain:  s.domain,
			Packets: s.packets, Bytes: s.bytes,
		})
	}
	return out
}
