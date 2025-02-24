package services_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/tests/mocks"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestMetricsService_PublishMetrics(t *testing.T) {
	// Setup mocks
	mockMqttClient := new(mocks.MQTTClient)
	mockJWTManager := new(mocks.JWTManagerInterface)
	mockMqttToken := new(mocks.MockToken)
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	mockJWTManager.On("GetJWT").Return("test-jwt-token")
	mockMqttToken.On("Wait").Return(true)
	mockMqttToken.On("Error").Return(nil)
	mockMqttClient.On("Publish", "metrics/topic", byte(1), false, mock.Anything).Return(mockMqttToken)

	// Create service instance
	service := services.NewMetricsService(
		"metrics/topic",
		"metrics_config.json",
		1*time.Second,
		2*time.Second,
		nil, // DeviceInfo not needed for this test
		1,
		mockMqttClient,
		nil, // FileOperations not needed
		mockJWTManager,
		logger,
	)

	// Create sample metrics data
	metrics := &models.SystemMetrics{
		Timestamp: time.Now().UTC(),
		DeviceID:  "device123",
		Metrics:   map[string]models.Metric{"cpu": {Value: 50.0, Unit: "%"}},
	}

	// Serialize metrics
	metrics.JWTToken = mockJWTManager.GetJWT()
	metricsData, err := json.Marshal(metrics)
	assert.NoError(t, err)

	// Publish metrics
	err = service.PublishMetrics(metrics)
	assert.NoError(t, err)

	// Verify mock expectations
	mockMqttClient.AssertCalled(t, "Publish", "metrics/topic", byte(1), false, metricsData)
	mockJWTManager.AssertExpectations(t)
	mockMqttClient.AssertExpectations(t)
}

func TestMetricsService_LoadConfig(t *testing.T) {
	mockFileClient := new(mocks.FileOperations)
	mockLogger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	mockFileClient.On("ReadFileRaw", "metrics_config.json").Return([]byte(`{"MonitorCPU": true, "MonitorMemory": true}`), nil)

	service := services.NewMetricsService(
		"metrics/topic",
		"metrics_config.json",
		1*time.Second,
		2*time.Second,
		nil,
		1,
		nil,
		mockFileClient,
		nil,
		mockLogger,
	)

	config, err := service.LoadMetricsConfig()
	assert.NoError(t, err)
	assert.NotNil(t, config.MonitorCPU)
	assert.NotNil(t, config.MonitorMemory)
}
