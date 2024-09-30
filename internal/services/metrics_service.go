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
	metrics := &models.SystemMetrics{
		Processes: make(map[string]*models.ProcessMetrics),
	}

	// Get the current timestamp
	timestamp := time.Now().UTC()

	// Collect CPU usage
	if config.MonitorCPU {
		metrics.CPUUsage = &models.TimestampedMetric{
			Value:     m.getCPUUsage(),
			Timestamp: timestamp,
		}
	}

	// Collect memory usage
	if config.MonitorMemory {
		metrics.Memory = &models.TimestampedMetric{
			Value:     m.getMemoryStats(),
			Timestamp: timestamp,
		}
	}

	// Collect disk usage
	if config.MonitorDisk {
		metrics.Disk = &models.TimestampedMetric{
			Value:     m.getDiskUsage(),
			Timestamp: timestamp,
		}
	}

	// Collect network usage
	if config.MonitorNetwork {
		metrics.Network = &models.TimestampedMetric{
			Value:     m.getNetworkStats(),
			Timestamp: timestamp,
		}
	}

	// Collect process metrics
	if len(config.ProcessNames) > 0 {
		m.getProcessMetrics(config, metrics, timestamp)
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

func (m *MetricsService) getCPUUsage() float64 {
	cpuPercentages, err := cpu.Percent(0, false)
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get CPU usage")
		return 0
	}
	return cpuPercentages[0]
}

func (m *MetricsService) getMemoryStats() map[string]interface{} {
	memStats, err := mem.VirtualMemory()
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get memory stats")
		return nil
	}
	return map[string]interface{}{
		"total":        memStats.Total,
		"used":         memStats.Used,
		"used_percent": memStats.UsedPercent,
	}
}

func (m *MetricsService) getDiskUsage() map[string]interface{} {
	diskStats, err := disk.Usage("/")
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get disk usage")
		return nil
	}
	return map[string]interface{}{
		"total":        diskStats.Total,
		"used":         diskStats.Used,
		"used_percent": diskStats.UsedPercent,
	}
}

func (m *MetricsService) getNetworkStats() map[string]interface{} {
	netStats, err := net.IOCounters(false)
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get network stats")
		return nil
	}
	return map[string]interface{}{
		"bytes_sent": netStats[0].BytesSent,
		"bytes_recv": netStats[0].BytesRecv,
	}
}

// getProcessMetrics gathers metrics for all processes specified in the config
func (m *MetricsService) getProcessMetrics(config *MetricsConfig, metrics *models.SystemMetrics, timestamp time.Time) {
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
					procMetrics.CPUUsage = &models.TimestampedMetric{
						Value:     m.getProcessCPU(proc),
						Timestamp: timestamp,
					}
				}

				// Collect process memory usage
				if config.MonitorProcMem {
					procMetrics.Memory = &models.TimestampedMetric{
						Value:     m.getProcessMemory(proc),
						Timestamp: timestamp,
					}
				}

				metrics.Processes[procName] = procMetrics
				m.Logger.WithField("process", procName).Info("Process metrics collected")
			}
		}
	}
}

func (m *MetricsService) getProcessCPU(p *process.Process) float64 {
	procCPU, err := p.CPUPercent()
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get process CPU usage")
		return 0
	}
	return procCPU
}

func (m *MetricsService) getProcessMemory(p *process.Process) map[string]interface{} {
	procMem, err := p.MemoryInfo()
	if err != nil {
		m.Logger.WithError(err).Error("Failed to get process memory usage")
		return nil
	}
	return map[string]interface{}{
		"rss": procMem.RSS,
		"vms": procMem.VMS,
	}
}
