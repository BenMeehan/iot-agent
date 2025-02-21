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

func TestRegistrationService_Start_Success(t *testing.T) {
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockFileClient := new(mocks.FileOperations)
	mockJWTManager := new(mocks.JWTManagerInterface)
	mockEncryption := new(mocks.EncryptionManagerInterface)
	mockMqttClient := new(mocks.MQTTClient)
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	mockDeviceInfo.On("GetDeviceID").Return("existing-device-id")
	mockJWTManager.On("IsJWTValid").Return(true, nil)

	rs := &services.RegistrationService{
		PubTopic:          "test/topic",
		ClientID:          "test-client",
		QOS:               1,
		DeviceInfo:        mockDeviceInfo,
		MqttClient:        mockMqttClient,
		FileClient:        mockFileClient,
		JWTManager:        mockJWTManager,
		EncryptionManager: mockEncryption,
		MaxBackoffSeconds: 10,
		Logger:            logger,
	}

	err := rs.Start()
	assert.NoError(t, err)
	mockDeviceInfo.AssertExpectations(t)
	mockJWTManager.AssertExpectations(t)
}

func TestRegistrationService_Start_InvalidJWT(t *testing.T) {
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockFileClient := new(mocks.FileOperations)
	mockJWTManager := new(mocks.JWTManagerInterface)
	mockEncryption := new(mocks.EncryptionManagerInterface)
	mockMqttClient := new(mocks.MQTTClient)
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	mockDeviceInfo.On("GetDeviceID").Return("existing-device-id")
	mockJWTManager.On("IsJWTValid").Return(false, errors.New("invalid JWT"))

	rs := &services.RegistrationService{
		PubTopic:          "test/topic",
		ClientID:          "test-client",
		QOS:               1,
		DeviceInfo:        mockDeviceInfo,
		MqttClient:        mockMqttClient,
		FileClient:        mockFileClient,
		JWTManager:        mockJWTManager,
		EncryptionManager: mockEncryption,
		MaxBackoffSeconds: 10,
		Logger:            logger,
	}

	err := rs.Start()
	assert.Error(t, err)
	mockDeviceInfo.AssertExpectations(t)
	mockJWTManager.AssertExpectations(t)
}

func TestRegistrationService_Register_Success(t *testing.T) {
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockFileClient := new(mocks.FileOperations)
	mockJWTManager := new(mocks.JWTManagerInterface)
	mockEncryption := new(mocks.EncryptionManagerInterface)
	mockMqttClient := new(mocks.MQTTClient)
	mockMqttToken := new(mocks.MockToken)
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	payload := models.RegistrationPayload{
		ClientID: "test-client",
		Name:     "test-device",
		OrgID:    "test-org",
	}

	// Mock encryption
	mockEncryption.On("Encrypt", mock.Anything).Return([]byte("encrypted-payload"), nil)

	// Mock MQTT publish and subscribe
	mockMqttToken.On("Wait").Return(true)
	mockMqttToken.On("Error").Return(nil)
	mockMqttClient.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockMqttToken)

	mockMqttClient.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			// Correctly typecast the callback
			callback := args.Get(2).(MQTT.MessageHandler)

			// Simulate an MQTT response message
			mockMsg := mocks.NewMockMessage("test/topic/response/test-client", []byte(`{"device_id":"test-device-id", "jwt_token":"test-jwt-token"}`))
			callback(nil, mockMsg) // Invoke callback
		}).
		Return(mockMqttToken)

	// Mock saving JWT and Device ID
	mockJWTManager.On("SaveJWT", "test-jwt-token").Return(nil).Once()
	mockDeviceInfo.On("SaveDeviceID", "test-device-id").Return(nil).Once()

	rs := &services.RegistrationService{
		PubTopic:          "test/topic",
		ClientID:          "test-client",
		QOS:               1,
		DeviceInfo:        mockDeviceInfo,
		MqttClient:        mockMqttClient,
		FileClient:        mockFileClient,
		JWTManager:        mockJWTManager,
		EncryptionManager: mockEncryption,
		MaxBackoffSeconds: 10,
		Logger:            logger,
	}

	// Run the registration
	err := rs.RetryRegistration(payload)

	// Assertions
	assert.NoError(t, err)
	mockEncryption.AssertExpectations(t)
	mockMqttClient.AssertExpectations(t)
	mockJWTManager.AssertExpectations(t)
	mockDeviceInfo.AssertExpectations(t)
}

func TestRegistrationService_Register_FailEncrypt(t *testing.T) {
	mockEncryption := new(mocks.EncryptionManagerInterface)
	mockMqttClient := new(mocks.MQTTClient)
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	payload := models.RegistrationPayload{ClientID: "test-client"}
	mockEncryption.On("Encrypt", mock.Anything).Return(nil, errors.New("encryption error"))

	rs := &services.RegistrationService{
		PubTopic:          "test/topic",
		ClientID:          "test-client",
		QOS:               1,
		MqttClient:        mockMqttClient,
		EncryptionManager: mockEncryption,
		Logger:            logger,
	}

	err := rs.RetryRegistration(payload)
	assert.Error(t, err)
	mockEncryption.AssertExpectations(t)
}
