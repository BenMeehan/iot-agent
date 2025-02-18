package services

import (
	"context"
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
	ctx               context.Context
	cancel            context.CancelFunc
	Timeout           time.Duration
}

// NewMetricsService creates and returns a new instance of MetricsService.
func NewMetricsService(pubTopic, metricsConfigFile string, interval, timeout time.Duration,
	deviceInfo identity.DeviceInfoInterface, qos int, mqttClient mqtt.MQTTClient,
	fileClient file.FileOperations, logger zerolog.Logger) *MetricsService {

	return &MetricsService{
		PubTopic:          pubTopic,
		MetricsConfigFile: metricsConfigFile,
		Interval:          interval,
		Timeout:           timeout,
		DeviceInfo:        deviceInfo,
		QOS:               qos,
		MqttClient:        mqttClient,
		FileClient:        fileClient,
		Logger:            logger,
	}
}

// Start begins periodic metrics collection and publishing
func (m *MetricsService) Start() error {
	if m.ctx != nil {
		m.Logger.Warn().Msg("MetricsService is already running")
		return errors.New("metrics service is already running")
	}

	// Load metrics configuration
	config, err := m.LoadMetricsConfig()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Error loading metrics config")
		return err
	}

	// Create a context with cancel for graceful stop
	m.ctx, m.cancel = context.WithCancel(context.Background())

	go m.collectAndPublishMetrics(config)

	m.Logger.Info().Str("topic", m.PubTopic).Msg("MetricsService started")
	return nil
}

// Stop stops the metrics service gracefully
func (m *MetricsService) Stop() error {
	if m.ctx == nil {
		m.Logger.Warn().Msg("MetricsService is not running")
		return errors.New("metrics service is not running")
	}

	// Cancel the context to stop the service
	m.cancel()
	m.Logger.Info().Msg("MetricsService stopped")
	return nil
}

// collectAndPublishMetrics runs the collection and publishing loop
func (m *MetricsService) collectAndPublishMetrics(config *models.MetricsConfig) {
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
		case <-m.ctx.Done():
			m.Logger.Info().Msg("Stopping MetricsService gracefully")
			return
		}
	}
}

// LoadMetricsConfig loads and parses the configuration for metrics
func (m *MetricsService) LoadMetricsConfig() (*models.MetricsConfig, error) {
	data, err := m.FileClient.ReadFileRaw(m.MetricsConfigFile)
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to read config file")
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config models.MetricsConfig
	if err := json.Unmarshal(data, &config); err != nil {
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

	// Use context with timeout for each individual metric collection
	metricsCollectionCtx, cancel := context.WithTimeout(m.ctx, m.Timeout)
	defer cancel()

	if config.MonitorCPU {
		metrics.CPUUsage = m.getCPUUsage(metricsCollectionCtx)
	}
	if config.MonitorMemory {
		metrics.Memory = m.getMemoryStats(metricsCollectionCtx)
	}
	if config.MonitorDisk {
		metrics.Disk = m.getDiskUsage(metricsCollectionCtx)
	}
	if config.MonitorNetwork {
		metrics.NetworkIn, metrics.NetworkOut = m.getNetworkStats(metricsCollectionCtx)
	}

	// Collect process metrics if needed
	if len(config.ProcessNames) > 0 {
		m.getProcessMetrics(config, metrics, metricsCollectionCtx)
	}

	m.Logger.Debug().Interface("metrics", metrics).Msg("Metrics collected successfully")
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

	m.Logger.Debug().Msg("Metrics published successfully")
	return nil
}

// Helper functions to gather individual metrics

// getCPUUsage collects CPU usage information
func (m *MetricsService) getCPUUsage(ctx context.Context) *float64 {
	cpuPercentagesCh := make(chan []float64, 1)
	go func() {
		cpuPercentages, err := cpu.Percent(0, false)
		if err != nil {
			m.Logger.Error().Err(err).Msg("Failed to get CPU usage")
			cpuPercentagesCh <- nil
			return
		}
		cpuPercentagesCh <- cpuPercentages
	}()
	select {
	case cpuPercentages := <-cpuPercentagesCh:
		if len(cpuPercentages) > 0 {
			return &cpuPercentages[0]
		}
		return nil
	case <-ctx.Done():
		m.Logger.Warn().Msg("Timeout reached while collecting CPU usage")
		return nil
	}
}

// getMemoryStats collects memory usage statistics
func (m *MetricsService) getMemoryStats(ctx context.Context) *float64 {
	memStatsCh := make(chan *mem.VirtualMemoryStat, 1)
	go func() {
		memStats, err := mem.VirtualMemory()
		if err != nil {
			m.Logger.Error().Err(err).Msg("Failed to get memory stats")
			memStatsCh <- nil
			return
		}
		memStatsCh <- memStats
	}()
	select {
	case memStats := <-memStatsCh:
		if memStats != nil {
			return &memStats.UsedPercent
		}
		return nil
	case <-ctx.Done():
		m.Logger.Warn().Msg("Timeout reached while collecting memory stats")
		return nil
	}
}

// getDiskUsage collects disk usage information
func (m *MetricsService) getDiskUsage(ctx context.Context) *float64 {
	diskStatsCh := make(chan *disk.UsageStat, 1)
	go func() {
		diskStats, err := disk.Usage("/")
		if err != nil {
			m.Logger.Error().Err(err).Msg("Failed to get disk usage")
			diskStatsCh <- nil
			return
		}
		diskStatsCh <- diskStats
	}()
	select {
	case diskStats := <-diskStatsCh:
		if diskStats != nil {
			return &diskStats.UsedPercent
		}
		return nil
	case <-ctx.Done():
		m.Logger.Warn().Msg("Timeout reached while collecting disk usage")
		return nil
	}
}

// getNetworkStats collects network I/O statistics
func (m *MetricsService) getNetworkStats(ctx context.Context) (*float64, *float64) {
	netStatsCh := make(chan []net.IOCountersStat, 1)
	go func() {
		netStats, err := net.IOCounters(false)
		if err != nil {
			m.Logger.Error().Err(err).Msg("Failed to get network stats")
			netStatsCh <- nil
			return
		}
		netStatsCh <- netStats
	}()
	select {
	case netStats := <-netStatsCh:
		if len(netStats) > 0 {
			bytesRecv := float64(netStats[0].BytesRecv)
			bytesSent := float64(netStats[0].BytesSent)
			return &bytesRecv, &bytesSent
		}
		return nil, nil
	case <-ctx.Done():
		m.Logger.Warn().Msg("Timeout reached while collecting network stats")
		return nil, nil
	}
}

// getProcessMetrics collects metrics for specific processes
func (m *MetricsService) getProcessMetrics(config *models.MetricsConfig, metrics *models.SystemMetrics, ctx context.Context) {
	procsCh := make(chan []*process.Process, 1)
	go func() {
		procs, err := process.Processes()
		if err != nil {
			m.Logger.Error().Err(err).Msg("Failed to get process metrics")
			procsCh <- nil
			return
		}
		procsCh <- procs
	}()
	select {
	case procs := <-procsCh:
		for _, proc := range procs {
			name, err := proc.Name()
			if err != nil {
				continue
			}
			for _, procName := range config.ProcessNames {
				if name == procName {
					procMetrics := &models.ProcessMetrics{}

					// Collect process-specific metrics based on configuration
					if config.MonitorProcCPU {
						procMetrics.CPUUsage = m.getProcessCPU(proc)
					}

					if config.MonitorProcMem {
						procMetrics.Memory = m.getProcessMemory(proc)
					}

					readOps, writeOps := m.getProcessIOPS(proc)
					procMetrics.ReadOps = readOps
					procMetrics.WriteOps = writeOps

					// Store the process metrics
					metrics.Processes[procName] = procMetrics
					m.Logger.Debug().Str("process", procName).Msg("Process metrics collected")
				}
			}
		}
	case <-ctx.Done():
		m.Logger.Warn().Msg("Timeout reached while collecting process metrics")
	}
}

// getProcessCPU collects CPU usage for a specific process
func (m *MetricsService) getProcessCPU(p *process.Process) *float64 {
	procCPU, err := p.CPUPercent()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get process CPU usage")
		return nil
	}
	return &procCPU
}

// getProcessMemory collects memory usage for a specific process
func (m *MetricsService) getProcessMemory(p *process.Process) *float64 {
	procMem, err := p.MemoryInfo()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get process memory usage")
		return nil
	}
	rss := float64(procMem.RSS)
	return &rss
}

// getProcessIOPS collects I/O operations for a specific process
func (m *MetricsService) getProcessIOPS(p *process.Process) (*float64, *float64) {
	ioCounters, err := p.IOCounters()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to get process I/O counters")
		return nil, nil
	}

	readOps := float64(ioCounters.ReadCount)
	writeOps := float64(ioCounters.WriteCount)

	return &readOps, &writeOps
}
