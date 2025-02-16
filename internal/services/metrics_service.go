package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/process"
)

// MetricsService manages device telemetry
type MetricsService struct {
	PubTopic          string
	MetricsConfigFile string
	Interval          time.Duration
	DeviceInfo        identity.DeviceInfoInterface
	QOS               int
	MqttClient        mqtt.MQTTClient
	FileClient        file.FileOperations
	Logger            zerolog.Logger
	stopChan          chan struct{}
	running           bool
}

// Start begins periodic metrics collection and publishing
func (m *MetricsService) Start() error {
	if m.running {
		m.Logger.Warn().Msg("MetricsService is already running")
		return errors.New("metrics service is already running")
	}

	config, err := m.LoadMetricsConfig()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Error loading metrics config")
		return err
	}

	m.stopChan = make(chan struct{})
	m.running = true

	go func() {
		ticker := time.NewTicker(m.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				metrics := m.CollectMetrics(config)
				metrics.DeviceID = m.DeviceInfo.GetDeviceID()
				if err := m.PublishMetrics(metrics); err != nil {
					m.Logger.Error().Err(err).Msg("Error publishing metrics")
				}
			case <-m.stopChan:
				m.Logger.Info().Msg("Stopping MetricsService")
				m.running = false
				return
			}
		}
	}()

	m.Logger.Info().Str("topic", m.PubTopic).Msg("MetricsService started")
	return nil
}

// Stop stops the metrics service gracefully
func (m *MetricsService) Stop() error {
	if !m.running {
		m.Logger.Warn().Msg("MetricsService is not running")
		return errors.New("metrics service is not running")
	}

	if m.stopChan == nil {
		m.Logger.Error().Msg("Failed to stop MetricsService: stop channel is nil")
		return errors.New("stop channel is nil")
	}

	close(m.stopChan)
	m.running = false
	m.Logger.Info().Msg("MetricsService stopped")
	return nil
}

// LoadMetricsConfig loads and parses the configuration for metrics
func (m *MetricsService) LoadMetricsConfig() (*models.MetricsConfig, error) {
	data, err := m.FileClient.ReadFile(m.MetricsConfigFile)
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to read config file")
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config models.MetricsConfig
	err = json.Unmarshal([]byte(data), &config)
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to unmarshal config")
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	m.Logger.Info().Msg("Metrics configuration loaded successfully")
	return &config, nil
}

// CollectMetrics gathers system and process metrics
func (m *MetricsService) CollectMetrics(config *models.MetricsConfig) *models.SystemMetrics {
	metrics := &models.SystemMetrics{
		Timestamp: time.Now().UTC(),
		Processes: make(map[string]*models.ProcessMetrics),
	}

	if config.MonitorCPU {
		metrics.CPUUsage = m.getCPUUsage()
	}
	if config.MonitorMemory {
		metrics.Memory = m.getMemoryStats()
	}
	if config.MonitorDisk {
		metrics.Disk = m.getDiskUsage()
	}
	if config.MonitorNetwork {
		metrics.Network = m.getNetworkStats()
	}
	if len(config.ProcessNames) > 0 {
		m.getProcessMetrics(config, metrics)
	}

	m.Logger.Info().Interface("metrics", metrics).Msg("Metrics collected successfully")
	return metrics
}

// PublishMetrics sends the collected metrics via MQTT
func (m *MetricsService) PublishMetrics(metrics *models.SystemMetrics) error {
	metricsData, err := json.Marshal(metrics)
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to marshal metrics")
		return fmt.Errorf("failed to marshal metrics: %w", err)
	}

	token := m.MqttClient.Publish(m.PubTopic, byte(m.QOS), false, metricsData)
	if token.Wait() && token.Error() != nil {
		m.Logger.Error().Err(token.Error()).Msg("Failed to publish metrics via MQTT")
		return fmt.Errorf("failed to publish metrics: %w", token.Error())
	}

	m.Logger.Info().Msg("Metrics published successfully")
	return nil
}

// Helper functions to gather individual metrics

func (m *MetricsService) getCPUUsage() *float64 {
	cpuPercentages, err := cpu.Percent(0, false)
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get CPU usage")
		return nil
	}
	return &cpuPercentages[0]
}

func (m *MetricsService) getMemoryStats() *float64 {
	memStats, err := mem.VirtualMemory()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get memory stats")
		return nil
	}
	return &memStats.UsedPercent
}

func (m *MetricsService) getDiskUsage() *float64 {
	diskStats, err := disk.Usage("/")
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get disk usage")
		return nil
	}
	return &diskStats.UsedPercent
}

func (m *MetricsService) getNetworkStats() *float64 {
	netStats, err := net.IOCounters(false)
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get network stats")
		return nil
	}
	bytesRecv := float64(netStats[0].BytesRecv)
	return &bytesRecv
}

// getProcessMetrics gathers metrics for all processes specified in the config
func (m *MetricsService) getProcessMetrics(config *models.MetricsConfig, metrics *models.SystemMetrics) {
	procs, err := process.Processes()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get process metrics")
		return
	}

	for _, proc := range procs {
		name, err := proc.Name()
		if err != nil {
			continue
		}

		for _, procName := range config.ProcessNames {
			if name == procName {
				procMetrics := &models.ProcessMetrics{}

				if config.MonitorProcCPU {
					procMetrics.CPUUsage = m.getProcessCPU(proc)
				}

				if config.MonitorProcMem {
					procMetrics.Memory = m.getProcessMemory(proc)
				}

				metrics.Processes[procName] = procMetrics
				m.Logger.Info().Str("process", procName).Msg("Process metrics collected")
			}
		}
	}
}

func (m *MetricsService) getProcessCPU(p *process.Process) *float64 {
	procCPU, err := p.CPUPercent()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get process CPU usage")
		return nil
	}
	return &procCPU
}

func (m *MetricsService) getProcessMemory(p *process.Process) *float64 {
	procMem, err := p.MemoryInfo()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get process memory usage")
		return nil
	}
	rss := float64(procMem.RSS)
	return &rss
}
