package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/internal/metrics_collectors"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/rs/zerolog"
)

// MetricsService collects and publishes system telemetry data via MQTT.
type MetricsService struct {
	// Configuration and state management
	pubTopic          string
	metricsConfigFile string
	metricsConfig     *models.MetricsConfig
	interval          time.Duration
	timeout           time.Duration
	qos               int

	// Dependencies
	mqttClient mqtt.Wrapper
	fileClient file.FileOperations
	deviceInfo identity.DeviceInfoInterface
	logger     zerolog.Logger

	// Workers and registry
	registry   *metrics_collectors.MetricsRegistry
	workerPool *utils.WorkerPool

	// Context management for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewMetricsService initializes and returns a new MetricsService instance.
func NewMetricsService(
	pubTopic, metricsConfigFile string,
	interval, timeout time.Duration,
	deviceInfo identity.DeviceInfoInterface,
	qos int,
	mqttClient mqtt.Wrapper,
	fileClient file.FileOperations,
	logger zerolog.Logger,
) *MetricsService {
	service := &MetricsService{
		// Assign configurations
		pubTopic:          pubTopic,
		metricsConfigFile: metricsConfigFile,
		interval:          interval,
		timeout:           timeout,
		qos:               qos,

		// Inject dependencies
		mqttClient: mqttClient,
		fileClient: fileClient,
		deviceInfo: deviceInfo,
		logger:     logger,

		// Initialize supporting components
		registry:   metrics_collectors.NewMetricsRegistry(),
		workerPool: utils.NewWorkerPool(10),
	}

	// Register default metric collectors
	service.registerDefaultCollectors()
	return service
}

// registerDefaultCollectors registers core system metric collectors.
func (m *MetricsService) registerDefaultCollectors() {
	m.registry.Register(&metrics_collectors.CPUMetricCollector{Logger: m.logger})
	m.registry.Register(&metrics_collectors.MemoryMetricCollector{Logger: m.logger})
	m.registry.Register(&metrics_collectors.DiskMetricCollector{Logger: m.logger})
	m.registry.Register(&metrics_collectors.NetworkMetricCollector{Logger: m.logger})
}

// Start begins periodic metrics collection and publishing.
func (m *MetricsService) Start() error {
	if m.ctx != nil {
		m.logger.Warn().Msg("MetricsService is already running")
		return errors.New("metrics service is already running")
	}

	m.logger.Info().Msg("Starting MetricsService...")

	// Load and validate metrics configuration
	config, err := m.LoadAndValidateMetricsConfig()
	if err != nil {
		m.logger.Error().Err(err).Msg("Failed to load or validate metrics configuration")
		return err
	}
	m.metricsConfig = config

	// Register process metrics collector if needed
	if len(config.ProcessNames) > 0 {
		if err := m.registerProcessMetricsCollector(config); err != nil {
			m.logger.Error().Err(err).Msg("Failed to register process metrics collector")
			return err
		}
	}

	// Initialize context for running the collection loop
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.wg.Add(1)
	go m.RunMetricsCollectionLoop()

	m.logger.Info().Str("topic", m.pubTopic).Msg("MetricsService started successfully")
	return nil
}

// Stop gracefully shuts down the MetricsService.
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

// loadAndValidateMetricsConfig loads and validates the metrics configuration.
func (m *MetricsService) LoadAndValidateMetricsConfig() (*models.MetricsConfig, error) {
	config, err := m.LoadMetricsConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load metrics config: %w", err)
	}

	if err := m.ValidateMetricsConfig(config); err != nil {
		return nil, fmt.Errorf("invalid metrics config: %w", err)
	}

	return config, nil
}

// loadMetricsConfig reads and parses the metrics configuration file.
func (m *MetricsService) LoadMetricsConfig() (*models.MetricsConfig, error) {
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
func (m *MetricsService) ValidateMetricsConfig(config *models.MetricsConfig) error {
	if !config.MonitorCPU && !config.MonitorMemory && !config.MonitorDisk &&
		!config.MonitorNetwork && len(config.ProcessNames) == 0 {
		return errors.New("no metrics enabled in configuration")
	}

	if len(config.ProcessNames) > 0 && !config.MonitorProcCPU && !config.MonitorProcMem && !config.MonitorProcIOps {
		return errors.New("process names specified but no process metrics enabled")
	}

	return nil
}

// runMetricsCollectionLoop collects and publishes metrics at configured intervals.
func (m *MetricsService) RunMetricsCollectionLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			metrics := m.CollectMetrics()
			metrics.DeviceID = m.deviceInfo.GetDeviceID()

			if err := m.PublishMetrics(metrics); err != nil {
				m.logger.Error().Err(err).Msg("Failed to publish metrics")
			}
		case <-m.ctx.Done():
			m.logger.Info().Msg("Stopping metrics collection")
			return
		}
	}
}

// collectMetrics gathers system and process metrics concurrently.
func (m *MetricsService) CollectMetrics() *models.SystemMetrics {
	metrics := &models.SystemMetrics{
		Timestamp: time.Now().UTC(),
		Metrics:   make(map[string]models.Metric),
		Processes: make([]*models.ProcessMetrics, 0),
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
					if processMetrics, ok := collectedValue.([]*models.ProcessMetrics); ok {
						metrics.Processes = append(metrics.Processes, processMetrics...)

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

// publishMetrics add JWT to the message and sends collected metrics via MQTT.
func (m *MetricsService) PublishMetrics(metrics *models.SystemMetrics) error {
	metricsData, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("failed to serialize metrics: %w", err)
	}

	retries := 3
	for i := 0; i < retries; i++ {
		token, err := m.mqttClient.MQTTPublish(m.pubTopic, byte(m.qos), false, metricsData)
		if err != nil {
			m.logger.Warn().Err(token.Error()).Int("retry", i+1).Msg("Retrying to publish metrics...")
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}

		if token.Wait() && token.Error() == nil {
			m.logger.Debug().Msg("Metrics published successfully")
			return nil
		}
		m.logger.Warn().Err(token.Error()).Int("retry", i+1).Msg("Retrying to publish metrics...")
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	return fmt.Errorf("failed to publish metrics after %d retries", retries)
}

// registerProcessMetricsCollector filters and registers processes specified in the configuration.
func (m *MetricsService) registerProcessMetricsCollector(config *models.MetricsConfig) error {
	targetNames := make(map[string]struct{})
	for _, name := range config.ProcessNames {
		targetNames[name] = struct{}{}
	}

	m.registry.Register(&metrics_collectors.ProcessMetricCollector{
		Logger:             m.logger,
		TargetProcessNames: targetNames,
		MonitorProcCPU:     config.MonitorProcCPU,
		MonitorProcMem:     config.MonitorProcMem,
		MonitorProcIO:      config.MonitorProcIOps,
		WorkerPool:         m.workerPool,
	})

	return nil
}
