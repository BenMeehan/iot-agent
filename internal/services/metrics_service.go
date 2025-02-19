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
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/rs/zerolog"
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
	wg                sync.WaitGroup
	registry          *metrics_collectors.MetricsRegistry
}

// NewMetricsService creates and returns a new instance of MetricsService.
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

	service.registry.Register(&metrics_collectors.CPUMetricCollector{Logger: logger})
	service.registry.Register(&metrics_collectors.MemoryMetricCollector{Logger: logger})
	service.registry.Register(&metrics_collectors.DiskMetricCollector{Logger: logger})
	service.registry.Register(&metrics_collectors.NetworkMetricCollector{Logger: logger})
	service.registry.Register(&metrics_collectors.ProcessMetricCollector{Logger: logger})

	return service
}

// Start begins periodic metrics collection and publishing
func (m *MetricsService) Start() error {
	if m.ctx != nil {
		m.Logger.Warn().Msg("MetricsService is already running")
		return errors.New("metrics service is already running")
	}

	config, err := m.LoadMetricsConfig()
	if err != nil {
		m.Logger.Error().Err(err).Msg("Error loading metrics config")
		return err
	}

	if err := m.validateMetricsConfig(config); err != nil {
		m.Logger.Error().Err(err).Msg("Invalid metrics configuration")
		return fmt.Errorf("invalid metrics configuration: %w", err)
	}

	m.ctx, m.cancel = context.WithCancel(context.Background())

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.collectAndPublishMetrics(config)
	}()

	m.Logger.Info().Str("topic", m.PubTopic).Msg("MetricsService started")
	return nil
}

// Stop stops the metrics service gracefully
func (m *MetricsService) Stop() error {
	if m.ctx == nil {
		m.Logger.Warn().Msg("MetricsService is not running")
		return errors.New("metrics service is not running")
	}

	m.cancel()
	m.wg.Wait()
	m.Logger.Info().Msg("MetricsService stopped")
	return nil
}

func (m *MetricsService) validateMetricsConfig(config *models.MetricsConfig) error {
	if !config.MonitorCPU && !config.MonitorMemory && !config.MonitorDisk && !config.MonitorNetwork && len(config.ProcessNames) == 0 {
		return errors.New("no metrics enabled in configuration")
	}
	if len(config.ProcessNames) > 0 && !config.MonitorProcCPU && !config.MonitorProcMem && !config.MonitorIOps {
		return errors.New("process names specified but no process metrics enabled")
	}
	return nil
}

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
		Metrics:   make(map[string]models.Metric),
		Processes: make(map[string]*models.ProcessMetrics),
	}

	metricsCollectionCtx, cancel := context.WithTimeout(m.ctx, m.Timeout)
	defer cancel()

	for name, collector := range m.registry.GetCollectors() {
		if collector.IsEnabled(config) {
			collectedValue := collector.Collect(metricsCollectionCtx)
			metrics.Metrics[name] = models.Metric{
				Value: collectedValue,
				Unit:  collector.Unit(),
			}

			if name == "Process" {
				if processMetrics, ok := collectedValue.(map[string]*models.ProcessMetrics); ok {
					for procName, procMetric := range processMetrics {
						metrics.Processes[procName] = procMetric
					}
				}
			}
		}
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

	retries := 3
	for i := 0; i < retries; i++ {
		token := m.MqttClient.Publish(m.PubTopic, byte(m.QOS), false, metricsData)
		if token.Wait() && token.Error() == nil {
			m.Logger.Debug().Msg("Metrics published successfully")
			return nil
		}
		m.Logger.Warn().Err(token.Error()).Int("retry", i+1).Msg("Failed to publish metrics, retrying...")
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	m.Logger.Error().Msg("Failed to publish metrics after retries")
	return fmt.Errorf("failed to publish metrics after %d retries", retries)
}
