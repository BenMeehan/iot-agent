package services_test

import (
	"testing"
	"time"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/tests/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHeartbeatService_Start(t *testing.T) {
	mockMQTTClient := new(mocks.MockMQTTClient)
	mockToken := new(mocks.MockToken)
	mockDeviceInfo := new(mocks.MockDeviceInfo)

	mockDeviceInfo.On("GetDeviceID").Return("")
	mockDeviceInfo.On("SaveDeviceID", "device123").Return(nil)

	mockMQTTClient.On("Publish", "iot-heartbeat", byte(1), false, mock.Anything).Return(mockToken)
	mockToken.On("Error").Return(nil)
	mockToken.On("Wait").Return(true)

	service := &services.HeartbeatService{
		PubTopic:   "iot-heartbeat",
		Interval:   1 * time.Second,
		DeviceInfo: mockDeviceInfo,
		QOS:        1,
		MqttClient: mockMQTTClient,
	}

	// Start the service in a separate goroutine
	go func() {
		err := service.Start()
		assert.NoError(t, err)
	}()

	// Wait and assert
	time.Sleep(2 * time.Second)
	mockMQTTClient.AssertCalled(t, "Publish", "iot-heartbeat", byte(1), false, mock.Anything)
}
