package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTClient defines the interface for an MQTT client.
type MQTTClient interface {
	Connect() mqtt.Token
	Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token
	Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token
	Unsubscribe(topics ...string) mqtt.Token
	Disconnect(quiesce uint)
}

// MqttService provides methods for MQTT operations.
type MqttService struct {
	client MQTTClient
}

// NewMqttService creates a new MqttService instance.
func NewMqttService() *MqttService {
	return &MqttService{}
}

// Initialize sets up the MQTT client with SSL/TLS and starts the connection.
func (s *MqttService) Initialize(context context.Context, broker, clientID, caCertPath string) error {
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		return err
	}

	// Create a CA certificate pool and append the CA certificate to it
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// Create a TLS configuration with the CA certificate
	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		InsecureSkipVerify: true, // Disable certificate validation for testing (not recommended for production)
	}

	// Set up the MQTT client options
	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID(clientID)
	opts.SetTLSConfig(tlsConfig)
	opts.SetAutoReconnect(true)

	// Create and assign the MQTT client to the service
	client := mqtt.NewClient(opts)
	s.client = client

	// Connect to the MQTT broker using the Connect method
	token := s.Connect()
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	return nil
}

// Connect connects to the MQTT broker.
func (s *MqttService) Connect() mqtt.Token {
	return s.client.Connect()
}

// Publish sends a message to the specified topic.
func (s *MqttService) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	return s.client.Publish(topic, qos, retained, payload)
}

// Subscribe subscribes to the specified topic with a message handler.
func (s *MqttService) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	return s.client.Subscribe(topic, qos, callback)
}

// Unsubscribe unsubscribes from the specified topics.
func (s *MqttService) Unsubscribe(topics ...string) mqtt.Token {
	return s.client.Unsubscribe(topics...)
}

// Disconnect gracefully disconnects the MQTT client.
func (s *MqttService) Disconnect(quiesce uint) {
	s.client.Disconnect(quiesce)
}
