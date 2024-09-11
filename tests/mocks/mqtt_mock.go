package mocks

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/mock"
)

// MockMQTTClient is a mock implementation of the MQTTClient interface
type MockMQTTClient struct {
	mock.Mock
}

func (m *MockMQTTClient) Connect() mqtt.Token {
	args := m.Called()
	return args.Get(0).(mqtt.Token)
}

func (m *MockMQTTClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	args := m.Called(topic, qos, retained, payload)
	return args.Get(0).(mqtt.Token)
}

func (m *MockMQTTClient) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	args := m.Called(topic, qos, callback)
	return args.Get(0).(mqtt.Token)
}

func (m *MockMQTTClient) Disconnect(quiesce uint) {
	m.Called(quiesce)
}
