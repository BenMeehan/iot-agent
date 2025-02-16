package models

import "time"

// SystemMetrics represents the system metrics collected at a specific time
type SystemMetrics struct {
	Timestamp time.Time                  `json:"timestamp"`
	DeviceID  string                     `json:"device_id"`
	CPUUsage  *float64                   `json:"cpu_usage,omitempty"`
	Memory    *float64                   `json:"memory,omitempty"`
	Disk      *float64                   `json:"disk,omitempty"`
	Network   *float64                   `json:"network,omitempty"`
	Processes map[string]*ProcessMetrics `json:"processes,omitempty"`
}

// ProcessMetrics contains metrics for an individual process
type ProcessMetrics struct {
	CPUUsage *float64 `json:"cpu_usage,omitempty"`
	Memory   *float64 `json:"memory,omitempty"`
}

// MetricsConfig defines the structure of the config file for the metrics to be monitored
type MetricsConfig struct {
	MonitorCPU     bool     `json:"monitor_cpu"`
	MonitorMemory  bool     `json:"monitor_memory"`
	MonitorDisk    bool     `json:"monitor_disk"`
	MonitorNetwork bool     `json:"monitor_network"`
	ProcessNames   []string `json:"process_names"`
	MonitorProcCPU bool     `json:"monitor_proc_cpu"`
	MonitorProcMem bool     `json:"monitor_proc_mem"`
}
