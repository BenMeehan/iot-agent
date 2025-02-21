package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/internal/metrics_collectors"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/process"
)

// MetricsService handles system telemetry collection and publishing over MQTT.
type MetricsService struct {
	pubTopic          string
	metricsConfigFile string
	metricsConfig     *models.MetricsConfig
	interval          time.Duration
	deviceInfo        identity.DeviceInfoInterface
	qos               int
	mqttClient        mqtt.MQTTClient
	fileClient        file.FileOperations
	logger            zerolog.Logger
	timeout           time.Duration
	registry          *metrics_collectors.MetricsRegistry
	workerPool        *utils.WorkerPool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewMetricsService initializes and returns a new instance of MetricsService.
func NewMetricsService(
	pubTopic, metricsConfigFile string,
	interval, timeout time.Duration,
	deviceInfo identity.DeviceInfoInterface,
	qos int,
	mqttClient mqtt.MQTTClient,
	fileClient file.FileOperations,
	logger zerolog.Logger,
) *MetricsService {
	service := &MetricsService{
		pubTopic:          pubTopic,
		metricsConfigFile: metricsConfigFile,
		interval:          interval,
		timeout:           timeout,
		deviceInfo:        deviceInfo,
		qos:               qos,
		mqttClient:        mqttClient,
		fileClient:        fileClient,
		logger:            logger,
		registry:          metrics_collectors.NewMetricsRegistry(),
		workerPool:        utils.NewWorkerPool(10),
	}

	// Register default metric collectors
	service.registerDefaultCollectors()

	return service
}

// registerDefaultCollectors registers the default metric collectors.
func (m *MetricsService) registerDefaultCollectors() {
	m.registry.Register(&metrics_collectors.CPUMetricCollector{Logger: m.logger})
	m.registry.Register(&metrics_collectors.MemoryMetricCollector{Logger: m.logger})
	m.registry.Register(&metrics_collectors.DiskMetricCollector{Logger: m.logger})
	m.registry.Register(&metrics_collectors.NetworkMetricCollector{Logger: m.logger})
}

// Start initiates periodic metrics collection and publishing.
func (m *MetricsService) Start() error {
	if m.ctx != nil {
		m.logger.Warn().Msg("MetricsService is already running")
		return errors.New("metrics service is already running")
	}

	m.logger.Info().Msg("Starting MetricsService...")

	// Load and validate metrics configuration
	config, err := m.loadAndValidateMetricsConfig()
	if err != nil {
		m.logger.Error().Err(err).Msg("Failed to load or validate metrics configuration")
		return err
	}
	m.metricsConfig = config

	// Register process metrics collector if required
	if len(config.ProcessNames) > 0 {
		if err := m.registerProcessMetricsCollector(config); err != nil {
			m.logger.Error().Err(err).Msg("Failed to register process metrics collector")
			return err
		}
	}

	// Start the metrics collection loop
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.wg.Add(1)
	go m.runMetricsCollectionLoop()

	m.logger.Info().Str("topic", m.pubTopic).Msg("MetricsService started successfully")
	return nil
}

// loadAndValidateMetricsConfig loads and validates the metrics configuration.
func (m *MetricsService) loadAndValidateMetricsConfig() (*models.MetricsConfig, error) {
	config, err := m.loadMetricsConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load metrics config: %w", err)
	}

	if err := m.validateMetricsConfig(config); err != nil {
		return nil, fmt.Errorf("invalid metrics config: %w", err)
	}

	return config, nil
}

// loadMetricsConfig reads and parses the metrics configuration file.
func (m *MetricsService) loadMetricsConfig() (*models.MetricsConfig, error) {
	data, err := m.fileClient.ReadFileRaw(m.metricsConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config models.MetricsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	m.logger.Info().Msg("Metrics configuration loaded successfully")
	return &config, nil
}

// validateMetricsConfig checks if the provided configuration is valid.
func (m *MetricsService) validateMetricsConfig(config *models.MetricsConfig) error {
	if !config.MonitorCPU && !config.MonitorMemory && !config.MonitorDisk &&
		!config.MonitorNetwork && len(config.ProcessNames) == 0 {
		return errors.New("no metrics enabled in configuration")
	}

	if len(config.ProcessNames) > 0 && !config.MonitorProcCPU && !config.MonitorProcMem && !config.MonitorProcIOps {
		return errors.New("process names specified but no process metrics enabled")
	}

	return nil
}

// registerProcessMetricsCollector filters and registers processes specified in the configuration
// for metrics collection. It identifies running processes that match the configured names and
// sets up a ProcessMetricCollector to monitor CPU, memory, and I/O metrics for these processes.
func (m *MetricsService) registerProcessMetricsCollector(config *models.MetricsConfig) error {
	procs, err := process.Processes()
	if err != nil {
		return fmt.Errorf("failed to retrieve process list: %w", err)
	}

	requiredProcs := utils.SliceToSet(config.ProcessNames)
	filteredProcs := make([]*process.Process, 0)

	for _, proc := range procs {
		name, err := proc.Name()
		if err != nil {
			continue
		}
		name = strings.ToLower(name)
		if _, exists := requiredProcs[name]; exists {
			filteredProcs = append(filteredProcs, proc)
		}
	}

	m.registry.Register(&metrics_collectors.ProcessMetricCollector{
		Logger:         m.logger,
		ProcessNames:   filteredProcs,
		MonitorProcCPU: config.MonitorProcCPU,
		MonitorProcMem: config.MonitorProcMem,
		MonitorProcIO:  config.MonitorProcIOps,
		WorkerPool:     m.workerPool,
	})

	return nil
}

// runMetricsCollectionLoop runs the main metrics collection and publishing loop.
func (m *MetricsService) runMetricsCollectionLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			metrics := m.collectMetrics()
			metrics.DeviceID = m.deviceInfo.GetDeviceID()

			if err := m.publishMetrics(metrics); err != nil {
				m.logger.Error().Err(err).Msg("Failed to publish metrics")
			}
		case <-m.ctx.Done():
			m.logger.Info().Msg("Stopping metrics collection")
			return
		}
	}
}

// collectMetrics gathers system and process metrics concurrently.
func (m *MetricsService) collectMetrics() *models.SystemMetrics {
	metrics := &models.SystemMetrics{
		Timestamp: time.Now().UTC(),
		Metrics:   make(map[string]models.Metric),
		Processes: make(map[string]*models.ProcessMetrics),
	}

	ctx, cancel := context.WithTimeout(m.ctx, m.timeout)
	defer cancel()

	var wg sync.WaitGroup
	metricsMutex := &sync.Mutex{}

	for name, collector := range m.registry.GetCollectors() {
		if collector.IsEnabled(m.metricsConfig) {
			wg.Add(1)
			m.workerPool.Submit(func() {
				defer wg.Done()
				collectedValue := collector.Collect(ctx)

				metricsMutex.Lock()
				defer metricsMutex.Unlock()

				if name == "process" {
					if processMetrics, ok := collectedValue.(map[string]*models.ProcessMetrics); ok {
						for procName, procMetric := range processMetrics {
							metrics.Processes[procName] = procMetric
						}
					}
				} else {
					metrics.Metrics[name] = models.Metric{
						Value: collectedValue,
						Unit:  collector.Unit(),
					}
				}
			})
		}
	}

	wg.Wait()
	m.logger.Info().Interface("metrics", metrics).Msg("Metrics collected successfully")
	return metrics
}

// publishMetrics sends the collected metrics via MQTT.
func (m *MetricsService) publishMetrics(metrics *models.SystemMetrics) error {
	metricsData, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("failed to serialize metrics: %w", err)
	}

	retries := 3
	for i := 0; i < retries; i++ {
		token := m.mqttClient.Publish(m.pubTopic, byte(m.qos), false, metricsData)
		if token.Wait() && token.Error() == nil {
			m.logger.Debug().Msg("Metrics published successfully")
			return nil
		}
		m.logger.Warn().Err(token.Error()).Int("retry", i+1).Msg("Retrying to publish metrics...")
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	return fmt.Errorf("failed to publish metrics after %d retries", retries)
}

// Stop gracefully stops the metrics service.
func (m *MetricsService) Stop() error {
	if m.ctx == nil {
		m.logger.Warn().Msg("MetricsService is not running")
		return errors.New("metrics service is not running")
	}

	m.logger.Info().Msg("Stopping MetricsService...")
	m.cancel()
	m.wg.Wait()
	m.workerPool.Shutdown()
	m.logger.Info().Msg("MetricsService stopped successfully")
	return nil
}
