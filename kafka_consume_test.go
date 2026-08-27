package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

func TestKafkaExporterConsumeReal(t *testing.T) {
	brokers := os.Getenv("NETRA_TEST_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("NETRA_TEST_KAFKA_BROKERS not set, skipping (needs a real Kafka broker with Netra actually publishing to it)")
	}
	topic := os.Getenv("NETRA_TEST_KAFKA_TOPIC")
	if topic == "" {
		topic = "netra-flows"
	}

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     splitKafkaBrokers(brokers),
		Topic:       topic,
		Partition:   0,
		StartOffset: kafkago.FirstOffset,
	})
	defer reader.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	msg, err := reader.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("read message: %v", err)
	}

	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(msg.Value, &raw); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(raw) == 0 {
		t.Fatalf("payload is an empty array")
	}
	if _, ok := raw[0]["protoPackets"]; ok {
		t.Errorf("payload still contains protoPackets, expected it to be dropped")
	}
	if _, ok := raw[0]["protoBytes"]; ok {
		t.Errorf("payload still contains protoBytes, expected it to be dropped")
	}
	if _, ok := raw[0]["timestamp"]; !ok {
		t.Errorf("record missing its own timestamp field")
	}

	var records []kafkaFlowRecord
	if err := json.Unmarshal(msg.Value, &records); err != nil {
		t.Fatalf("unmarshal into []kafkaFlowRecord: %v", err)
	}

	t.Logf("consumed real tick from %s: %d record(s)", topic, len(records))
	for i, r := range records {
		if r.Timestamp.IsZero() {
			t.Errorf("record[%d]: zero timestamp", i)
		}
		if net.ParseIP(r.SrcIP) == nil {
			t.Errorf("record[%d]: invalid srcIP %q", i, r.SrcIP)
		}
		if net.ParseIP(r.DstIP) == nil {
			t.Errorf("record[%d]: invalid dstIP %q", i, r.DstIP)
		}
		if r.Proto == "" {
			t.Errorf("record[%d]: empty proto", i)
		}
		if r.Packets == 0 && r.Bytes == 0 {
			t.Errorf("record[%d]: both packets and bytes are zero", i)
		}
	}
}
