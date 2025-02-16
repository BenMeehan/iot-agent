package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"

	"github.com/benmeehan/iot-agent/pkg/file"
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
	client     MQTTClient
	fileClient file.FileOperations
}

// NewMqttService creates a new MqttService instance.
func NewMqttService(fileClient file.FileOperations) *MqttService {
	return &MqttService{
		fileClient: fileClient,
	}
}

// Initialize sets up the MQTT client with SSL/TLS and starts the connection.
func (s *MqttService) Initialize(broker, clientID, caCertPath string) error {
	caCert, err := s.fileClient.ReadFileRaw(caCertPath)
	if err != nil {
		return fmt.Errorf("failed to read CA certificate: %v", err)
	}

	// Create a CA certificate pool and append the CA certificate to it
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return fmt.Errorf("failed to append CA certificate")
	}
	// Create a TLS configuration with the CA certificate
	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		InsecureSkipVerify: true, // Disable certificate validation for testing (not recommended for production)
	}

	// TODO: Add username and password authentication

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
