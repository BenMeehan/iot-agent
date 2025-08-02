package services

import (
	"testing"
	"time"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/tests/mocks"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestMetricsService_Start_Success verifies that the service starts successfully and prevents multiple starts.
func TestMetricsService_Start_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Mock file client to return a valid metrics config
	mockFileClient.On("ReadFileRaw", mock.Anything).Return([]byte(`{
		"monitor_cpu": true,
		"monitor_memory": true,
		"monitor_disk": true,
		"monitor_network": true,
		"process_names": []
	}`), nil)

	m := services.NewMetricsService(
		"test-topic",
		"test-config.json",
		1*time.Second,
		5*time.Second,
		mockDeviceInfo,
		1,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Execute
	err := m.Start()

	// Assert
	assert.NoError(t, err)

	// Try to start again (should fail)
	err = m.Start()
	assert.Error(t, err)
	assert.Equal(t, "metrics service is already running", err.Error())

	// Cleanup
	err = m.Stop()
	assert.NoError(t, err)
}

// TestMetricsService_Stop_Success verifies that the service stops successfully and prevents multiple stops.
func TestMetricsService_Stop_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Mock file client to return a valid metrics config
	mockFileClient.On("ReadFileRaw", mock.Anything).Return([]byte(`{
		"monitor_cpu": true,
		"monitor_memory": true,
		"monitor_disk": true,
		"monitor_network": true,
		"process_names": []
	}`), nil)

	m := services.NewMetricsService(
		"test-topic",
		"test-config.json",
		1*time.Second,
		5*time.Second,
		mockDeviceInfo,
		1,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Start the service
	err := m.Start()
	assert.NoError(t, err)

	// Execute
	err = m.Stop()

	// Assert
	assert.NoError(t, err)

	// Try to stop again (should fail)
	err = m.Stop()
	assert.Error(t, err)
	assert.Equal(t, "metrics service is not running", err.Error())
}

// TestMetricsService_LoadAndValidateMetricsConfig_Success verifies that a valid metrics config loads successfully.
func TestMetricsService_LoadAndValidateMetricsConfig_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Mock file client to return a valid metrics config
	mockFileClient.On("ReadFileRaw", mock.Anything).Return([]byte(`{
		"monitor_cpu": true,
		"monitor_memory": true,
		"monitor_disk": true,
		"monitor_network": true,
		"process_names": []
	}`), nil)

	m := services.NewMetricsService(
		"test-topic",
		"test-config.json",
		1*time.Second,
		5*time.Second,
		mockDeviceInfo,
		1,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Execute
	config, err := m.LoadAndValidateMetricsConfig()

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.True(t, config.MonitorCPU)
	assert.True(t, config.MonitorMemory)
	assert.True(t, config.MonitorDisk)
	assert.True(t, config.MonitorNetwork)
	assert.Empty(t, config.ProcessNames)
}

// TestMetricsService_LoadAndValidateMetricsConfig_InvalidConfig verifies that invalid configs return an error.
func TestMetricsService_LoadAndValidateMetricsConfig_InvalidConfig(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Mock file client to return an invalid metrics config (invalid key names)
	mockFileClient.On("ReadFileRaw", mock.Anything).Return([]byte(`{
		"monitorCPU": true,
		"monitorMemory": true,
		"monitorDisk": true,
		"monitorNetwork": true,
		"processNames": []
	}`), nil)

	m := services.NewMetricsService(
		"test-topic",
		"test-config.json",
		1*time.Second,
		5*time.Second,
		mockDeviceInfo,
		1,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Execute
	config, err := m.LoadAndValidateMetricsConfig()

	// Assert
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Equal(t, "invalid metrics config: no metrics enabled in configuration", err.Error())
}

// TestMetricsService_CollectMetrics_Success verifies that metrics are collected successfully.
func TestMetricsService_CollectMetrics_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Mock file client to return a valid metrics config
	mockFileClient.On("ReadFileRaw", mock.Anything).Return([]byte(`{
		"monitor_cpu": true,
		"monitor_memory": true,
		"monitor_disk": true,
		"monitor_network": true,
		"process_names": []
	}`), nil)

	m := services.NewMetricsService(
		"test-topic",
		"test-config.json",
		1*time.Second,
		5*time.Second,
		mockDeviceInfo,
		1,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Start the service
	err := m.Start()
	assert.NoError(t, err)

	// Execute
	metrics := m.CollectMetrics()

	// Assert
	assert.NotNil(t, metrics)
	assert.NotEmpty(t, metrics.Metrics)

	// Cleanup
	err = m.Stop()
	assert.NoError(t, err)
}

// TestMetricsService_PublishMetrics_Success verifies that metrics are published successfully.
func TestMetricsService_PublishMetrics_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Mock file client to return a valid metrics config
	mockFileClient.On("ReadFileRaw", mock.Anything).Return([]byte(`{
		"monitor_cpu": true,
		"monitor_memory": true,
		"monitor_disk": true,
		"monitor_network": true,
		"process_names": []
	}`), nil)

	// Mock MQTT middleware to simulate successful publishing
	mockMQTTMiddleware.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	m := services.NewMetricsService(
		"test-topic",
		"test-config.json",
		1*time.Second,
		5*time.Second,
		mockDeviceInfo,
		1,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Start the service
	err := m.Start()
	assert.NoError(t, err)

	// Execute
	metrics := &models.SystemMetrics{
		DeviceID:  "test-device-id",
		Timestamp: time.Now().UTC(),
		Metrics:   map[string]models.Metric{"cpu": {Value: 50.0, Unit: "%"}},
	}
	err = m.PublishMetrics(metrics)

	// Assert
	assert.NoError(t, err)

	// Cleanup
	err = m.Stop()
	assert.NoError(t, err)
}
