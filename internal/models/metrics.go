package models

import "time"

// SystemMetrics represents the system metrics collected at a specific time.
type SystemMetrics struct {
	Timestamp time.Time                  `json:"timestamp"`           // The timestamp when the metrics were collected
	DeviceID  string                     `json:"device_id"`           // Unique identifier for the device
	Metrics   map[string]Metric          `json:"metrics"`             // Map of metric names to their values
	Processes map[string]*ProcessMetrics `json:"processes,omitempty"` // Map of process names to their metrics (optional)
	JWTToken  string                     `json:"jwt_token"`           // JWTToken is an authentication token used to verify the identity of the device.
}

// Metric represents a single metric with its value and metadata.
type Metric struct {
	Value       interface{} `json:"value"`                 // The value of the metric (can be any type)
	Unit        string      `json:"unit,omitempty"`        // The unit of the metric (e.g., "percentage", "bytes")
	Description string      `json:"description,omitempty"` // A brief description of the metric
}

// ProcessMetrics contains metrics for an individual process running on the system.
type ProcessMetrics struct {
	CPUUsage float64 `json:"cpu_usage,omitempty"` // CPU usage by the process as a percentage (optional)
	Memory   float64 `json:"memory,omitempty"`    // Memory usage by the process in bytes (optional)
	ReadOps  float64 `json:"read_ops,omitempty"`  // Number of read operations performed by the process (optional)
	WriteOps float64 `json:"write_ops,omitempty"` // Number of write operations performed by the process (optional)
}

// MetricsConfig defines the structure of the configuration file for the metrics to be monitored.
type MetricsConfig struct {
	MonitorCPU      bool     `json:"monitor_cpu"`       // Flag to indicate whether CPU usage should be monitored
	MonitorMemory   bool     `json:"monitor_memory"`    // Flag to indicate whether memory usage should be monitored
	MonitorDisk     bool     `json:"monitor_disk"`      // Flag to indicate whether disk usage should be monitored
	MonitorNetwork  bool     `json:"monitor_network"`   // Flag to indicate whether network usage should be monitored
	ProcessNames    []string `json:"process_names"`     // List of specific process names to monitor
	MonitorProcCPU  bool     `json:"monitor_proc_cpu"`  // Flag to indicate whether to monitor CPU usage per process
	MonitorProcMem  bool     `json:"monitor_proc_mem"`  // Flag to indicate whether to monitor memory usage per process
	MonitorProcIOps bool     `json:"monitor_proc_iops"` // Flag to indicate whether to monitor io operations per process
}
