package services_test

import (
	"os"
	"testing"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/tests/mocks"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// initializeTestDependencies sets up common dependencies required for RegistrationService tests.
func initializeTestDependencies() (*mocks.DeviceInfoInterface, *mocks.FileOperations, *mocks.MQTTMiddleware, zerolog.Logger) {
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockFileClient := new(mocks.FileOperations)
	mockMqttMiddleware := new(mocks.MQTTMiddleware)
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	return mockDeviceInfo, mockFileClient, mockMqttMiddleware, logger
}

// newTestRegistrationService creates a new instance of RegistrationService using provided dependencies.
func newTestRegistrationService(
	mockDeviceInfo *mocks.DeviceInfoInterface,
	mockFileClient *mocks.FileOperations,
	mockMqttMiddleware *mocks.MQTTMiddleware,
	logger zerolog.Logger,
) *services.RegistrationService {
	return services.NewRegistrationService(
		"test/topic",
		"test-client",
		1,
		mockDeviceInfo,
		mockMqttMiddleware,
		mockFileClient,
		logger,
	)
}

// TestRegistrationService_Start_Success verifies successful start when device is already registered.
func TestRegistrationService_Start_Success(t *testing.T) {
	mockDeviceInfo, mockFileClient, mockMqttMiddleware, logger := initializeTestDependencies()

	mockDeviceInfo.On("GetDeviceID").Return("existing-device-id")

	rs := newTestRegistrationService(mockDeviceInfo, mockFileClient, mockMqttMiddleware, logger)

	err := rs.Start()
	assert.NoError(t, err)
	mockDeviceInfo.AssertExpectations(t)
}

// TestRegistrationService_Register_Success verifies a successful registration process.
func TestRegistrationService_Register_Success(t *testing.T) {
	mockDeviceInfo, mockFileClient, mockMqttMiddleware, logger := initializeTestDependencies()

	payload := models.RegistrationPayload{
		ClientID: "test-client",
		Name:     "test-device",
		OrgID:    "test-org",
	}

	mockMqttMiddleware.On("PublishWithJWT", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockMqttMiddleware.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		callback := args.Get(2).(MQTT.MessageHandler)
		mockMsg := mocks.NewMockMessage("test/topic/response/test-client", []byte(`{"device_id":"test-device-id"}`))
		callback(nil, mockMsg)
	}).Return(nil)

	mockDeviceInfo.On("SaveDeviceID", "test-device-id").Return(nil)

	rs := newTestRegistrationService(mockDeviceInfo, mockFileClient, mockMqttMiddleware, logger)

	err := rs.Register(payload)
	assert.NoError(t, err)
	mockMqttMiddleware.AssertExpectations(t)
	mockDeviceInfo.AssertExpectations(t)
}
