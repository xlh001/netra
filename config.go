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
	Language string `json:"language"`

	RefreshIntervalMs int  `json:"refreshIntervalMs"`
	PersistScanAlerts bool `json:"persistScanAlerts"`

	DBFlowTopK    int `json:"dbFlowTopK"`
	TopKPerBucket int `json:"topKPerBucket"`

	AnomalyEnabled             bool    `json:"anomalyEnabled"`
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
	KafkaFlowTopK     int    `json:"kafkaFlowTopK"`

	SQLAuditEnabled    bool `json:"sqlAuditEnabled"`
	SQLAuditMaxPerTick int  `json:"sqlAuditMaxPerTick"`

	WeakAuthEnabled bool `json:"weakAuthEnabled"`
}

const defaultSQLAuditMaxPerTick = 500

const (
	LangZH = "zh"
	LangEN = "en"
)

func bi(lang, zh, en string) string {
	if lang == LangEN {
		return en
	}
	return zh
}

func defaultConfig() *Config {
	return &Config{v: ConfigDTO{
		Language:                   LangZH,
		RefreshIntervalMs:          5000,
		PersistScanAlerts:          true,
		DBFlowTopK:                 defaultDBFlowTopK,
		TopKPerBucket:              defaultTopKPerBucket,
		AnomalyEnabled:             false,
		AnomalyWindowSec:           int(defaultAnomalyWindow / time.Second),
		AnomalyPeerThreshold:       defaultAnomalyPeerThreshold,
		AnomalyAvgPacketsThreshold: defaultAnomalyAvgPacketsThreshold,
		VolumeThresholdBytes:       defaultVolumeThresholdBytes,
		AIEnabled:                  false,
		KafkaEnabled:               false,
		KafkaFlowTopK:              defaultKafkaFlowTopK,
		SQLAuditEnabled:            false,
		SQLAuditMaxPerTick:         defaultSQLAuditMaxPerTick,
		WeakAuthEnabled:            false,
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
	agg.UpdateAnomalyConfig(v.AnomalyEnabled, time.Duration(v.AnomalyWindowSec)*time.Second, v.AnomalyPeerThreshold, v.AnomalyAvgPacketsThreshold, v.VolumeThresholdBytes)
}

func applyCapacityConfig(agg *aggregator, cfg *Config) {
	v := cfg.Snapshot()
	agg.UpdateCapacityConfig(v.DBFlowTopK, v.TopKPerBucket, v.KafkaFlowTopK)
}
