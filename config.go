package main

import (
	"sync"
	"time"
)

type Config struct {
	mu sync.RWMutex
	v  ConfigDTO
}

type ConfigDTO struct {
	RefreshIntervalMs int  `json:"refreshIntervalMs"`
	PersistScanAlerts bool `json:"persistScanAlerts"`

	DBFlowTopK    int `json:"dbFlowTopK"`
	TopKPerBucket int `json:"topKPerBucket"`

	AnomalyWindowSec           int     `json:"anomalyWindowSec"`
	AnomalyPeerThreshold       int     `json:"anomalyPeerThreshold"`
	AnomalyAvgPacketsThreshold float64 `json:"anomalyAvgPacketsThreshold"`

	VolumeThresholdBytes uint64 `json:"volumeThresholdBytes"`

	AIEnabled  bool   `json:"aiEnabled"`
	AIProvider string `json:"aiProvider"`
	AIBaseURL  string `json:"aiBaseURL"`
	AIAPIKey   string `json:"aiApiKey,omitempty"`
	AIModel    string `json:"aiModel"`

	KafkaEnabled      bool   `json:"kafkaEnabled"`
	KafkaBrokers      string `json:"kafkaBrokers"`
	KafkaTopic        string `json:"kafkaTopic"`
	KafkaSASLUsername string `json:"kafkaSaslUsername,omitempty"`
	KafkaSASLPassword string `json:"kafkaSaslPassword,omitempty"`
	KafkaTLS          bool   `json:"kafkaTls"`
}

func defaultConfig() *Config {
	return &Config{v: ConfigDTO{
		RefreshIntervalMs:          5000,
		PersistScanAlerts:          true,
		DBFlowTopK:                 defaultDBFlowTopK,
		TopKPerBucket:              defaultTopKPerBucket,
		AnomalyWindowSec:           int(defaultAnomalyWindow / time.Second),
		AnomalyPeerThreshold:       defaultAnomalyPeerThreshold,
		AnomalyAvgPacketsThreshold: defaultAnomalyAvgPacketsThreshold,
		VolumeThresholdBytes:       defaultVolumeThresholdBytes,
		AIEnabled:                  false,
		KafkaEnabled:               false,
	}}
}

func (c *Config) Snapshot() ConfigDTO {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.v
}

func (c *Config) Apply(dto ConfigDTO) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.v = dto
}

func applyAnomalyConfig(agg *aggregator, cfg *Config) {
	v := cfg.Snapshot()
	agg.UpdateAnomalyConfig(time.Duration(v.AnomalyWindowSec)*time.Second, v.AnomalyPeerThreshold, v.AnomalyAvgPacketsThreshold, v.VolumeThresholdBytes)
}

func applyCapacityConfig(agg *aggregator, cfg *Config) {
	v := cfg.Snapshot()
	agg.UpdateCapacityConfig(v.DBFlowTopK, v.TopKPerBucket)
}
