package services_test

import (
	"testing"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/tests/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestRegistrationService_Start(t *testing.T) {
	mockMQTTClient := new(mocks.MockMQTTClient)
	mockFileOps := new(mocks.MockFileOperations)
	mockDeviceInfo := new(mocks.MockDeviceInfo)
	mockToken := new(mocks.MockToken)

	// Setup expectations for file operations
	mockFileOps.On("ReadFile", "secrets/.device.secret.PSK.txt").Return("secret", nil)
	mockDeviceInfo.On("GetDeviceID").Return("")
	mockDeviceInfo.On("SaveDeviceID", "device123").Return(nil)

	// Setup expectations for MQTT operations
	mockMQTTClient.On("Publish", "iot-registration", byte(2), false, mock.Anything).Return(mockToken)
	mockMQTTClient.On("Subscribe", "iot-registration/response/test-client", byte(2), mock.Anything).Return(mockToken)
	mockToken.On("Wait").Return(true)
	mockToken.On("Error").Return(nil)

	service := &services.RegistrationService{
		PubTopic:         "iot-registration",
		DeviceSecretFile: "secrets/.device.secret.PSK.txt",
		ClientID:         "test-client",
		QOS:              2,
		DeviceInfo:       mockDeviceInfo,
		MqttClient:       mockMQTTClient,
		FileClient:       mockFileOps,
	}

	err := service.Start()
	assert.NoError(t, err)

	// Verify expectations
	mockMQTTClient.AssertCalled(t, "Publish", "iot-registration", byte(2), false, mock.Anything)
	mockMQTTClient.AssertCalled(t, "Subscribe", "iot-registration/response/test-client", byte(2), mock.Anything)
	mockDeviceInfo.AssertCalled(t, "SaveDeviceID", "device123")
}
