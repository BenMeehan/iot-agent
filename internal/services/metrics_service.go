package services

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/process"
	"github.com/sirupsen/logrus"
)

// MetricsService manages the device telemetry
type MetricsService struct {
	PubTopic          string
	MetricsConfigFile string
	Interval          time.Duration
	DeviceInfo        identity.DeviceInfoInterface
	QOS               int
	MqttClient        mqtt.MQTTClient
	FileClient        file.FileOperations
	Logger            *logrus.Logger
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

// LoadMetricsConfig loads and parses the configuration for metrics
func (m *MetricsService) LoadMetricsConfig() (*MetricsConfig, error) {
	data, err := m.FileClient.ReadFile(m.MetricsConfigFile)
	if err != nil {
		m.Logger.WithError(err).Error("Failed to read config file")
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config MetricsConfig
	err = json.Unmarshal([]byte(data), &config)
	if err != nil {
		m.Logger.WithError(err).Error("Failed to unmarshal config")
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	m.Logger.Info("Metrics configuration loaded successfully")
	return &config, nil
}

// CollectMetrics gathers system and process metrics based on the config, including timestamps
func (m *MetricsService) CollectMetrics(config *MetricsConfig) *models.SystemMetrics {
	// Create a new SystemMetrics struct and assign the current timestamp
	metrics := &models.SystemMetrics{
		Timestamp: time.Now().UTC(),
		Processes: make(map[string]*models.ProcessMetrics),
	}

	// Collect CPU usage
	if config.MonitorCPU {
		metrics.CPUUsage = m.getCPUUsage()
	}

	// Collect memory usage
	if config.MonitorMemory {
		metrics.Memory = m.getMemoryStats()
	}

	// Collect disk usage
	if config.MonitorDisk {
		metrics.Disk = m.getDiskUsage()
	}

	// Collect network usage
	if config.MonitorNetwork {
		metrics.Network = m.getNetworkStats()
	}

	// Collect process metrics
	if len(config.ProcessNames) > 0 {
		m.getProcessMetrics(config, metrics)
	}

	m.Logger.WithFields(logrus.Fields{
		"metrics": metrics,
	}).Info("Metrics collected successfully")

	return metrics
}

// PublishMetrics sends the collected metrics via MQTT
func (m *MetricsService) PublishMetrics(metrics *models.SystemMetrics) error {
	// Marshal the SystemMetrics struct directly into JSON
	metricsData, err := json.Marshal(metrics)
	if err != nil {
		m.Logger.WithError(err).Error("Failed to marshal metrics")
		return fmt.Errorf("failed to marshal metrics: %w", err)
	}

	// Publish the JSON data over MQTT
	token := m.MqttClient.Publish(m.PubTopic, byte(m.QOS), false, metricsData)
	if token.Wait() && token.Error() != nil {
		m.Logger.WithError(token.Error()).Error("Failed to publish metrics via MQTT")
		return fmt.Errorf("failed to publish metrics: %w", token.Error())
	}

	m.Logger.Info("Metrics published successfully")
	return nil
}

// Start begins the periodic metrics collection and publishing process
func (m *MetricsService) Start() error {
	config, err := m.LoadMetricsConfig()
	if err != nil {
		m.Logger.WithError(err).Error("Error loading metrics config")
		return err
	}

	for range time.Tick(m.Interval) {
		metrics := m.CollectMetrics(config)
		metrics.DeviceID = m.DeviceInfo.GetDeviceID()
		if err := m.PublishMetrics(metrics); err != nil {
			m.Logger.WithError(err).Error("Error publishing metrics")
			return err
		}
	}
	return nil
}

// Helper functions to gather individual metrics

func (m *MetricsService) getCPUUsage() *float64 {
	cpuPercentages, err := cpu.Percent(0, false)
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get CPU usage")
		return nil
	}
	return &cpuPercentages[0]
}

func (m *MetricsService) getMemoryStats() *float64 {
	memStats, err := mem.VirtualMemory()
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get memory stats")
		return nil
	}
	return &memStats.UsedPercent
}

func (m *MetricsService) getDiskUsage() *float64 {
	diskStats, err := disk.Usage("/")
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get disk usage")
		return nil
	}
	return &diskStats.UsedPercent
}

func (m *MetricsService) getNetworkStats() *float64 {
	netStats, err := net.IOCounters(false)
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get network stats")
		return nil
	}
	// Convert uint64 to float64 for compatibility
	bytesRecv := float64(netStats[0].BytesRecv)
	return &bytesRecv
}

// getProcessMetrics gathers metrics for all processes specified in the config
func (m *MetricsService) getProcessMetrics(config *MetricsConfig, metrics *models.SystemMetrics) {
	procs, err := process.Processes()
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get process metrics")
		return
	}

	for _, proc := range procs {
		name, err := proc.Name()
		if err != nil {
			continue // Skip processes we can't fetch the name for
		}

		// Check if the process is in the configured list
		for _, procName := range config.ProcessNames {
			if name == procName {
				procMetrics := &models.ProcessMetrics{}

				// Collect process CPU usage
				if config.MonitorProcCPU {
					procMetrics.CPUUsage = m.getProcessCPU(proc)
				}

				// Collect process memory usage
				if config.MonitorProcMem {
					procMetrics.Memory = m.getProcessMemory(proc)
				}

				metrics.Processes[procName] = procMetrics
				m.Logger.WithField("process", procName).Info("Process metrics collected")
			}
		}
	}
}

func (m *MetricsService) getProcessCPU(p *process.Process) *float64 {
	procCPU, err := p.CPUPercent()
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get process CPU usage")
		return nil
	}
	return &procCPU
}

func (m *MetricsService) getProcessMemory(p *process.Process) *float64 {
	procMem, err := p.MemoryInfo()
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get process memory usage")
		return nil
	}
	// Returning RSS memory as an example
	rss := float64(procMem.RSS)
	return &rss
}
