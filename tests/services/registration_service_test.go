package services

import (
	"testing"
	"time"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/tests/mocks"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestRegistrationService_Start_Success tests the successful start of the service.
func TestRegistrationService_Start_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTAuthMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Mock the device Identity to get name, orgid and device id
	mockDeviceInfo.On("GetDeviceID").Return("existing-device-id")
	mockDeviceInfo.On("GetDeviceIdentity").Return(&identity.Identity{
		Name:  "test-device",
		OrgID: "test-org",
	})
	mockDeviceInfo.On("SaveDeviceID", mock.Anything).Return(nil)

	// Mock the MQTT middleware to simulate a response
	mockMQTTMiddleware.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		handler := args.Get(2).(mqtt.MessageHandler)
		go func() {
			time.Sleep(100 * time.Millisecond)
			handler(nil, mocks.NewMockMessage(mock.Anything, []byte(`{"device_id": "new-device-id"}`)))
		}()
	}).Return(nil)

	mockMQTTMiddleware.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockMQTTMiddleware.On("Unsubscribe", mock.Anything).Return(nil)

	rs := services.NewRegistrationService(
		"test-topic",
		"test-client-id",
		2,
		6,
		2,
		60,
		10,
		mockDeviceInfo,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Execute
	err := rs.Start()

	// Assert
	assert.NoError(t, err)

	// Try to start again (should fail)
	err = rs.Start()
	assert.Error(t, err)
	assert.Equal(t, "registration service is already running", err.Error())

	// Ensure all expectations were met
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
}

// TestRegistrationService_Run_WithExistingDeviceID tests the Run method with an existing device ID.
func TestRegistrationService_Run_WithExistingDeviceID(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTAuthMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Mock the device Identity to get name, orgid and device id
	mockDeviceInfo.On("GetDeviceID").Return("existing-device-id")
	mockDeviceInfo.On("GetDeviceIdentity").Return(&identity.Identity{
		Name:  "test-device",
		OrgID: "test-org",
	})

	// Mock the MQTT middleware to simulate a response
	mockMQTTMiddleware.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		handler := args.Get(2).(mqtt.MessageHandler)
		go func() {
			time.Sleep(100 * time.Millisecond)
			handler(nil, mocks.NewMockMessage(mock.Anything, []byte(`{"device_id": "existing-device-id"}`)))
		}()
	}).Return(nil)

	mockMQTTMiddleware.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockMQTTMiddleware.On("Unsubscribe", mock.Anything).Return(nil)

	rs := services.NewRegistrationService(
		"test-topic",
		"test-client-id",
		1,
		6,
		2,
		60,
		10,
		mockDeviceInfo,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Execute
	err := rs.Start()

	// Assert
	assert.NoError(t, err)
	mockDeviceInfo.AssertExpectations(t)
}

// TestRegistrationService_Run_WithoutDeviceID tests the Run method without an existing device ID.
func TestRegistrationService_Run_WithoutDeviceID(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTAuthMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Mock the device Identity to get name, orgid and device id
	mockDeviceInfo.On("GetDeviceID").Return("")
	mockDeviceInfo.On("GetDeviceIdentity").Return(&identity.Identity{
		Name:  "test-device",
		OrgID: "test-org",
	})
	mockDeviceInfo.On("SaveDeviceID", "new-device-id").Return(nil)

	// Mock the MQTT middleware to simulate a response
	mockMQTTMiddleware.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		handler := args.Get(2).(mqtt.MessageHandler)
		go func() {
			time.Sleep(100 * time.Millisecond)
			handler(nil, mocks.NewMockMessage(mock.Anything, []byte(`{"device_id": "new-device-id"}`)))
		}()
	}).Return(nil)

	mockMQTTMiddleware.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockMQTTMiddleware.On("Unsubscribe", mock.Anything).Return(nil)

	rs := services.NewRegistrationService(
		"test-topic",
		"test-client-id",
		1,
		6,
		2,
		60,
		10,
		mockDeviceInfo,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Execute
	err := rs.Start()

	// Assert
	assert.NoError(t, err)
	mockDeviceInfo.AssertExpectations(t)
}

// TestRegistrationService_Register_Timeout tests the Register method with a timeout.
func TestRegistrationService_Register_Timeout(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTAuthMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Mock the device Identity to get name, orgid, and device id
	mockDeviceInfo.On("GetDeviceID").Return("")
	mockDeviceInfo.On("GetDeviceIdentity").Return(&identity.Identity{
		Name:  "test-device",
		OrgID: "test-org",
	})

	// Mock the MQTT middleware to simulate no response
	mockMQTTMiddleware.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockMQTTMiddleware.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockMQTTMiddleware.On("Unsubscribe", mock.Anything).Return(nil)

	// Create the RegistrationService with test parameters
	rs := services.NewRegistrationService(
		"test-topic",
		"test-client-id",
		1, // QoS
		2, // maxRetries (3 attempts total)
		1, // baseDelay (1s)
		5, // maxDelay (5s)
		1, // responseTimeout (1s per attempt)
		mockDeviceInfo,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Execute
	startTime := time.Now()
	err := rs.Start()
	duration := time.Since(startTime)

	// Assert
	assert.Error(t, err, "Expected an error due to timeout")
	assert.Contains(t, err.Error(), "registration timeout", "Error message should indicate timeout")

	// Ensure the timeout occurred within a reasonable timeframe
	// With 3 attempts, baseDelay=1s, maxDelay=5s, and jitter, the total duration should be:
	// - At least: responseTimeout * number of attempts = 1s * 3 = 3s
	// - At most: (maxDelay + responseTimeout) * number of attempts + jitter = (5s + 1s) * 3 + jitter = ~20s
	assert.GreaterOrEqual(t, duration, 3*time.Second, "Should wait at least 3s for 3 attempts")
	assert.Less(t, duration, 20*time.Second, "Should complete within a reasonable timeframe")

	// Ensure all expectations were met
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
}

// TestRegistrationService_Stop_Success tests the successful stop of the service.
func TestRegistrationService_Stop_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTAuthMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Mock the device Identity to get name, orgid and device id
	mockDeviceInfo.On("GetDeviceID").Return("existing-device-id")
	mockDeviceInfo.On("GetDeviceIdentity").Return(&identity.Identity{
		Name:  "test-device",
		OrgID: "test-org",
	})

	// Mock the MQTT middleware to simulate a response
	mockMQTTMiddleware.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		handler := args.Get(2).(mqtt.MessageHandler)
		go func() {
			time.Sleep(100 * time.Millisecond)
			handler(nil, mocks.NewMockMessage(mock.Anything, []byte(`{"device_id": "existing-device-id"}`)))
		}()
	}).Return(nil)

	mockMQTTMiddleware.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockMQTTMiddleware.On("Unsubscribe", mock.Anything).Return(nil)

	rs := services.NewRegistrationService(
		"test-topic",
		"test-client-id",
		1,
		6,
		2,
		60,
		10,
		mockDeviceInfo,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Start the service to initialize ctx and cancel
	err := rs.Start()
	assert.NoError(t, err)

	// Execute
	err = rs.Stop()

	// Assert
	assert.NoError(t, err)
	mockMQTTMiddleware.AssertExpectations(t)
}

// TestRegistrationService_Stop_NotRunning tests the Stop method when the service is not running.
func TestRegistrationService_Stop_NotRunning(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTAuthMiddleware)
	mockFileClient := new(mocks.FileOperations)
	logger := zerolog.Nop()

	rs := services.NewRegistrationService(
		"test-topic",
		"test-client-id",
		1,
		6,
		2,
		60,
		10,
		mockDeviceInfo,
		mockMQTTMiddleware,
		mockFileClient,
		logger,
	)

	// Execute
	err := rs.Stop()

	// Assert
	assert.Error(t, err)
	assert.Equal(t, "registration service is not running", err.Error())
}
