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

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(msg.Value, &raw); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := raw["protoPackets"]; ok {
		t.Errorf("payload still contains protoPackets, expected it to be dropped")
	}
	if _, ok := raw["protoBytes"]; ok {
		t.Errorf("payload still contains protoBytes, expected it to be dropped")
	}
	if _, ok := raw["timestamp"]; !ok {
		t.Errorf("payload missing timestamp field")
	}
	if _, ok := raw["flows"]; !ok {
		t.Errorf("payload missing flows field")
	}

	var payload kafkaExportPayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		t.Fatalf("unmarshal into kafkaExportPayload: %v", err)
	}

	t.Logf("consumed real tick from %s: timestamp=%s, %d flow(s)", topic, payload.Timestamp, len(payload.Flows))
	for i, f := range payload.Flows {
		if net.ParseIP(f.SrcIP) == nil {
			t.Errorf("flow[%d]: invalid srcIP %q", i, f.SrcIP)
		}
		if net.ParseIP(f.DstIP) == nil {
			t.Errorf("flow[%d]: invalid dstIP %q", i, f.DstIP)
		}
		if f.Proto == "" {
			t.Errorf("flow[%d]: empty proto", i)
		}
		if f.Packets == 0 && f.Bytes == 0 {
			t.Errorf("flow[%d]: both packets and bytes are zero", i)
		}
	}
}
