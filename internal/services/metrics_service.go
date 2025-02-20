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

// MetricsService handles system telemetry collection and publishing over MQTT.
type MetricsService struct {
	PubTopic          string
	MetricsConfigFile string
	MetricsConfig     *models.MetricsConfig
	Interval          time.Duration
	DeviceInfo        identity.DeviceInfoInterface
	QOS               int
	MqttClient        mqtt.MQTTClient
	FileClient        file.FileOperations
	Logger            zerolog.Logger
	Timeout           time.Duration
	registry          *metrics_collectors.MetricsRegistry

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewMetricsService initializes and returns a new instance of MetricsService.
func NewMetricsService(pubTopic, metricsConfigFile string, interval, timeout time.Duration,
	deviceInfo identity.DeviceInfoInterface, qos int, mqttClient mqtt.MQTTClient,
	fileClient file.FileOperations, logger zerolog.Logger) *MetricsService {

	service := &MetricsService{
		PubTopic:          pubTopic,
		MetricsConfigFile: metricsConfigFile,
		Interval:          interval,
		Timeout:           timeout,
		DeviceInfo:        deviceInfo,
		QOS:               qos,
		MqttClient:        mqttClient,
		FileClient:        fileClient,
		Logger:            logger,
		registry:          metrics_collectors.NewMetricsRegistry(),
	}

	// Register default metric collectors
	service.registry.Register(&metrics_collectors.CPUMetricCollector{Logger: logger})
	service.registry.Register(&metrics_collectors.MemoryMetricCollector{Logger: logger})
	service.registry.Register(&metrics_collectors.DiskMetricCollector{Logger: logger})
	service.registry.Register(&metrics_collectors.NetworkMetricCollector{Logger: logger})

	return service
}

// Start initiates periodic metrics collection and publishing.
func (m *MetricsService) Start() error {
	if m.ctx != nil {
		m.Logger.Warn().Msg("MetricsService is already running")
		return errors.New("metrics service is already running")
	}

	m.Logger.Info().Msg("Starting MetricsService...")

	// Load metrics configuration
	config, err := m.LoadMetricsConfig()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to load metrics configuration")
		return err
	}

	// Validate configuration
	if err := m.validateMetricsConfig(config); err != nil {
		m.Logger.Error().Err(err).Msg("Invalid metrics configuration")
		return fmt.Errorf("invalid metrics configuration: %w", err)
	}

	m.MetricsConfig = config

	// Register process metrics collector if required
	if len(m.MetricsConfig.ProcessNames) > 0 {
		m.registry.Register(&metrics_collectors.ProcessMetricCollector{
			Logger:         m.Logger,
			ProcessNames:   utils.SliceToSet(m.MetricsConfig.ProcessNames),
			MonitorProcCPU: m.MetricsConfig.MonitorProcCPU,
			MonitorProcMem: m.MetricsConfig.MonitorProcMem,
			MonitorProcIO:  m.MetricsConfig.MonitorProcIOps,
		})
	}

	m.ctx, m.cancel = context.WithCancel(context.Background())

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.collectAndPublishMetrics()
	}()

	m.Logger.Info().Str("topic", m.PubTopic).Msg("MetricsService started successfully")
	return nil
}

// Stop gracefully stops the metrics service.
func (m *MetricsService) Stop() error {
	if m.ctx == nil {
		m.Logger.Warn().Msg("MetricsService is not running")
		return errors.New("metrics service is not running")
	}

	m.Logger.Info().Msg("Stopping MetricsService...")
	m.cancel()
	m.wg.Wait()
	m.Logger.Info().Msg("MetricsService stopped successfully")
	return nil
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

// collectAndPublishMetrics continuously gathers and publishes metrics at a set interval.
func (m *MetricsService) collectAndPublishMetrics() {
	ticker := time.NewTicker(m.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			metrics := m.CollectMetrics()
			metrics.DeviceID = m.DeviceInfo.GetDeviceID()

			if err := m.PublishMetrics(metrics); err != nil {
				m.Logger.Error().Err(err).Msg("Failed to publish metrics")
			}
		case <-m.ctx.Done():
			m.Logger.Info().Msg("Stopping metrics collection")
			return
		}
	}
}

// LoadMetricsConfig reads and parses the metrics configuration file.
func (m *MetricsService) LoadMetricsConfig() (*models.MetricsConfig, error) {
	m.Logger.Debug().Msg("Loading metrics configuration...")

	data, err := m.FileClient.ReadFileRaw(m.MetricsConfigFile)
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to read config file")
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config models.MetricsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		m.Logger.Error().Err(err).Msg("Failed to parse config file")
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	m.Logger.Info().Msg("Metrics configuration loaded successfully")
	return &config, nil
}

// CollectMetrics gathers system and process metrics concurrently.
func (m *MetricsService) CollectMetrics() *models.SystemMetrics {
	m.Logger.Debug().Msg("Collecting system metrics...")

	metrics := &models.SystemMetrics{
		Timestamp: time.Now().UTC(),
		Metrics:   make(map[string]models.Metric),
		Processes: make(map[string]*models.ProcessMetrics),
	}

	metricsCollectionCtx, cancel := context.WithTimeout(m.ctx, m.Timeout)
	defer cancel()

	var wg sync.WaitGroup
	metricsMutex := &sync.Mutex{}

	for name, collector := range m.registry.GetCollectors() {
		if collector.IsEnabled(m.MetricsConfig) {
			wg.Add(1)
			go func(name string, collector metrics_collectors.MetricCollector) {
				defer wg.Done()

				collectedValue := collector.Collect(metricsCollectionCtx)

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
			}(name, collector)
		}
	}

	wg.Wait()
	m.Logger.Info().Interface("metrics", metrics).Msg("Metrics collected successfully")
	return metrics
}

// PublishMetrics sends the collected metrics via MQTT.
func (m *MetricsService) PublishMetrics(metrics *models.SystemMetrics) error {
	m.Logger.Debug().Msg("Publishing metrics...")

	metricsData, err := json.Marshal(metrics)
	if err != nil {
		m.Logger.Error().Err(err).Msg("Failed to serialize metrics")
		return fmt.Errorf("failed to serialize metrics: %w", err)
	}

	retries := 3
	for i := 0; i < retries; i++ {
		token := m.MqttClient.Publish(m.PubTopic, byte(m.QOS), false, metricsData)
		if token.Wait() && token.Error() == nil {
			m.Logger.Debug().Msg("Metrics published successfully")
			return nil
		}
		m.Logger.Warn().Err(token.Error()).Int("retry", i+1).Msg("Retrying to publish metrics...")
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	m.Logger.Error().Msg("Failed to publish metrics after retries")
	return fmt.Errorf("failed to publish metrics after %d retries", retries)
}
