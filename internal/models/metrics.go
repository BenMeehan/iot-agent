package models

import "time"

// TimestampedMetric wraps a metric value with its associated timestamp
type TimestampedMetric struct {
	Value     interface{} `json:"value"`
	Timestamp time.Time   `json:"timestamp"`
}

// SystemMetrics represents all the system metrics collected at a specific time
type SystemMetrics struct {
	CPUUsage  *TimestampedMetric         `json:"cpu_usage,omitempty"`
	Memory    *TimestampedMetric         `json:"memory,omitempty"`
	Disk      *TimestampedMetric         `json:"disk,omitempty"`
	Network   *TimestampedMetric         `json:"network,omitempty"`
	Processes map[string]*ProcessMetrics `json:"processes,omitempty"`
	DeviceID  string                     `json:"deviceid"`
}

// ProcessMetrics contains metrics for an individual process
type ProcessMetrics struct {
	CPUUsage *TimestampedMetric `json:"cpu_usage,omitempty"`
	Memory   *TimestampedMetric `json:"memory,omitempty"`
}
