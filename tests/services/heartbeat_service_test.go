package services_test

import (
	"testing"
	"time"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/tests/mocks"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHeartbeatService_Start(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockJWTManager := new(mocks.JWTManagerInterface)
	mockMqttClient := new(mocks.MQTTClient)
	mockMqttToken := new(mocks.MockToken)
	logger := logrus.New()

	// Mock expectations
	mockDeviceInfo.On("GetDeviceID").Return("device123")
	mockJWTManager.On("GetJWT").Return("test-jwt-token")

	// Mock expectations for MQTT client
	mockMqttToken.On("Wait").Return(true)
	mockMqttToken.On("Error").Return(nil)
	mockMqttClient.On("Publish", "heartbeat/topic", byte(1), false, mock.Anything).Return(mockMqttToken)

	// Create HeartbeatService instance
	service := &services.HeartbeatService{
		PubTopic:   "heartbeat/topic",
		Interval:   1 * time.Second,
		DeviceInfo: mockDeviceInfo,
		QOS:        1,
		MqttClient: mockMqttClient,
		JWTManager: mockJWTManager,
		Logger:     logger,
	}

	// Start the service
	err := service.Start()
	assert.NoError(t, err)

	// Wait for the first heartbeat to be published
	time.Sleep(2 * time.Second)

	// Stop the service
	service.Stop()

	// Assert that the mocks were called as expected
	mockDeviceInfo.AssertExpectations(t)
	mockJWTManager.AssertExpectations(t)
	mockMqttClient.AssertExpectations(t)
	mockMqttToken.AssertExpectations(t)
}
