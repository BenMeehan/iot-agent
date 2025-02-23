package services_test

import (
	"errors"
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
func initializeTestDependencies() (*mocks.DeviceInfoInterface, *mocks.FileOperations, *mocks.JWTManagerInterface,
	*mocks.EncryptionManagerInterface, *mocks.MQTTClient, zerolog.Logger) {

	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockFileClient := new(mocks.FileOperations)
	mockJWTManager := new(mocks.JWTManagerInterface)
	mockEncryption := new(mocks.EncryptionManagerInterface)
	mockMqttClient := new(mocks.MQTTClient)
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	return mockDeviceInfo, mockFileClient, mockJWTManager, mockEncryption, mockMqttClient, logger
}

// newTestRegistrationService creates a new instance of RegistrationService using provided dependencies.
func newTestRegistrationService(
	mockDeviceInfo *mocks.DeviceInfoInterface,
	mockFileClient *mocks.FileOperations,
	mockJWTManager *mocks.JWTManagerInterface,
	mockEncryption *mocks.EncryptionManagerInterface,
	mockMqttClient *mocks.MQTTClient,
	logger zerolog.Logger,
) *services.RegistrationService {
	return services.NewRegistrationService(
		"test/topic",
		"test-client",
		1,
		10,
		mockDeviceInfo,
		mockMqttClient,
		mockFileClient,
		mockJWTManager,
		mockEncryption,
		logger,
	)
}

// TestRegistrationService_Start_Success tests the successful start of the registration service when JWT is valid.
func TestRegistrationService_Start_Success(t *testing.T) {
	mockDeviceInfo, mockFileClient, mockJWTManager, mockEncryption, mockMqttClient, logger := initializeTestDependencies()

	mockDeviceInfo.On("GetDeviceID").Return("existing-device-id")
	mockJWTManager.On("IsJWTValid").Return(true, nil)

	rs := newTestRegistrationService(mockDeviceInfo, mockFileClient, mockJWTManager, mockEncryption, mockMqttClient, logger)

	err := rs.Start()
	assert.NoError(t, err)
	mockDeviceInfo.AssertExpectations(t)
	mockJWTManager.AssertExpectations(t)
}

// TestRegistrationService_Start_InvalidJWT tests the registration service start behavior with an invalid JWT.
func TestRegistrationService_Start_InvalidJWT(t *testing.T) {
	mockDeviceInfo, mockFileClient, mockJWTManager, mockEncryption, mockMqttClient, logger := initializeTestDependencies()

	mockDeviceInfo.On("GetDeviceID").Return("existing-device-id")
	mockJWTManager.On("IsJWTValid").Return(false, errors.New("invalid JWT"))

	rs := newTestRegistrationService(mockDeviceInfo, mockFileClient, mockJWTManager, mockEncryption, mockMqttClient, logger)

	err := rs.Start()
	assert.Error(t, err)
	mockDeviceInfo.AssertExpectations(t)
	mockJWTManager.AssertExpectations(t)
}

// TestRegistrationService_Register_Success tests the successful registration process, including encryption and MQTT interactions.
func TestRegistrationService_Register_Success(t *testing.T) {
	mockDeviceInfo, mockFileClient, mockJWTManager, mockEncryption, mockMqttClient, logger := initializeTestDependencies()
	mockMqttToken := new(mocks.MockToken)

	payload := models.RegistrationPayload{
		ClientID: "test-client",
		Name:     "test-device",
		OrgID:    "test-org",
	}

	// Mock encryption and MQTT publish/subscribe
	mockEncryption.On("Encrypt", mock.Anything).Return([]byte("encrypted-payload"), nil)
	mockMqttToken.On("Wait").Return(true)
	mockMqttToken.On("Error").Return(nil)
	mockMqttClient.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockMqttToken)
	mockMqttClient.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			callback := args.Get(2).(MQTT.MessageHandler)
			mockMsg := mocks.NewMockMessage("test/topic/response/test-client", []byte(`{"device_id":"test-device-id", "jwt_token":"test-jwt-token"}`))
			callback(nil, mockMsg)
		}).
		Return(mockMqttToken)

	// Mock saving JWT and device ID
	mockJWTManager.On("SaveJWT", "test-jwt-token").Return(nil).Once()
	mockDeviceInfo.On("SaveDeviceID", "test-device-id").Return(nil).Once()

	rs := newTestRegistrationService(mockDeviceInfo, mockFileClient, mockJWTManager, mockEncryption, mockMqttClient, logger)

	err := rs.RetryRegistration(payload)
	assert.NoError(t, err)
	mockEncryption.AssertExpectations(t)
	mockMqttClient.AssertExpectations(t)
	mockJWTManager.AssertExpectations(t)
	mockDeviceInfo.AssertExpectations(t)
}

// TestRegistrationService_Register_FailEncrypt tests the behavior when encryption fails during registration.
func TestRegistrationService_Register_FailEncrypt(t *testing.T) {
	_, _, _, mockEncryption, mockMqttClient, logger := initializeTestDependencies()

	payload := models.RegistrationPayload{ClientID: "test-client"}
	mockEncryption.On("Encrypt", mock.Anything).Return(nil, errors.New("encryption error"))

	rs := newTestRegistrationService(nil, nil, nil, mockEncryption, mockMqttClient, logger)

	err := rs.RetryRegistration(payload)
	assert.Error(t, err)
	mockEncryption.AssertExpectations(t)
}
