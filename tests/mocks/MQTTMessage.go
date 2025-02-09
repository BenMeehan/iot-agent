package mocks

// MockMessage implements MQTT.Message for testing
type MockMessage struct {
	payload []byte
	topic   string
}

// NewMockMessage creates a new mock MQTT message
func NewMockMessage(topic string, payload []byte) *MockMessage {
	return &MockMessage{
		payload: payload,
		topic:   topic,
	}
}

func (m *MockMessage) Payload() []byte   { return m.payload }
func (m *MockMessage) Topic() string     { return m.topic }
func (m *MockMessage) Duplicate() bool   { return false }
func (m *MockMessage) Qos() byte         { return 1 }
func (m *MockMessage) Retained() bool    { return false }
func (m *MockMessage) MessageID() uint16 { return 1 }
func (m *MockMessage) Ack()              {} // No-op for testing
