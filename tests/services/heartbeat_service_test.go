package services

import (
	"errors"
	"testing"
	"time"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/tests/mocks"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestHeartbeatService_Start_Success tests the successful start of the HeartbeatService.
func TestHeartbeatService_Start_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTAuthMiddleware)
	logger := zerolog.Nop()

	mockDeviceInfo.On("GetDeviceID").Return("test-device-id")

	h := services.NewHeartbeatService(
		"test-topic",
		1*time.Second,
		1,
		mockDeviceInfo,
		mockMQTTMiddleware,
		logger,
	)

	// Execute
	err := h.Start()

	// Assert
	assert.NoError(t, err)

	// Try to start again (should fail)
	err = h.Start()
	assert.Error(t, err)
	assert.Equal(t, "heartbeat service is already running", err.Error())

	// Cleanup
	err = h.Stop()
	assert.NoError(t, err)
}

// TestHeartbeatService_Stop_Success tests the successful stop of the HeartbeatService.
func TestHeartbeatService_Stop_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTAuthMiddleware)
	logger := zerolog.Nop()

	mockDeviceInfo.On("GetDeviceID").Return("test-device-id")

	h := services.NewHeartbeatService(
		"test-topic",
		1*time.Second,
		1,
		mockDeviceInfo,
		mockMQTTMiddleware,
		logger,
	)

	// Start the service
	err := h.Start()
	assert.NoError(t, err)

	// Execute
	err = h.Stop()

	// Assert
	assert.NoError(t, err)

	// Try to stop again (should fail)
	err = h.Stop()
	assert.Error(t, err)
	assert.Equal(t, "heartbeat service is not running", err.Error())
}

// TestHeartbeatService_runHeartbeatLoop_Success tests the heartbeat loop with successful publishing.
func TestHeartbeatService_runHeartbeatLoop_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTAuthMiddleware)
	logger := zerolog.Nop()

	mockDeviceInfo.On("GetDeviceID").Return("test-device-id")

	// Mock the MQTT middleware to simulate successful publishing
	mockMQTTMiddleware.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	h := services.NewHeartbeatService(
		"test-topic",
		100*time.Millisecond, // Short interval for testing
		1,
		mockDeviceInfo,
		mockMQTTMiddleware,
		logger,
	)

	// Start the service
	err := h.Start()
	assert.NoError(t, err)

	// Wait for at least one heartbeat to be published
	time.Sleep(150 * time.Millisecond)

	// Stop the service
	err = h.Stop()
	assert.NoError(t, err)

	// Assert
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
}

// TestHeartbeatService_runHeartbeatLoop_PublishError tests the heartbeat loop with a publishing error.
func TestHeartbeatService_runHeartbeatLoop_PublishError(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTAuthMiddleware)
	logger := zerolog.Nop()

	mockDeviceInfo.On("GetDeviceID").Return("test-device-id")

	// Mock the MQTT middleware to simulate a publishing error
	mockMQTTMiddleware.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("publish failed"))

	h := services.NewHeartbeatService(
		"test-topic",
		100*time.Millisecond, // Short interval for testing
		1,
		mockDeviceInfo,
		mockMQTTMiddleware,
		logger,
	)

	// Start the service
	err := h.Start()
	assert.NoError(t, err)

	// Wait for at least one heartbeat to be attempted
	time.Sleep(150 * time.Millisecond)

	// Stop the service
	err = h.Stop()
	assert.NoError(t, err)

	// Assert
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
}
