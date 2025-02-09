package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"os"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// MQTTClient defines the interface for an MQTT client.
type MQTTClient interface {
	Connect() mqtt.Token
	Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token
	Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token
	Disconnect(quiesce uint)
}

// MqttService provides methods for MQTT operations.
type MqttService struct {
	client MQTTClient
	Logger *logrus.Logger
}

// NewMqttService creates a new MqttService instance with the provided client.
func NewMqttService(logger *logrus.Logger) *MqttService {
	return &MqttService{
		Logger: logger,
	}
}

// Initialize sets up the MQTT client with SSL/TLS and starts the connection.
// This method now acts on an existing MqttService instance.
func (s *MqttService) Initialize(broker, clientID, caCertPath string) error {
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		s.Logger.WithError(err).Error("Failed to read CA certificate")
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

	// Handler for successful MQTT connection
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		s.Logger.Info("MQTT client connected successfully")
	})

	// Handler for MQTT connection loss
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		s.Logger.WithError(err).Error("MQTT connection lost")
	})

	// Create and assign the MQTT client to the service
	client := mqtt.NewClient(opts)
	s.client = client

	// Connect to the MQTT broker using the Connect method
	token := s.Connect()
	if token.Wait() && token.Error() != nil {
		s.Logger.WithError(token.Error()).Error("Failed to connect to MQTT broker")
		return token.Error()
	}

	s.Logger.Info("MQTT client initialized and connected")
	return nil
}

// Connect connects to the MQTT broker.
func (s *MqttService) Connect() mqtt.Token {
	return s.client.Connect()
}

// Publish sends a message to the specified topic.
func (s *MqttService) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	token := s.client.Publish(topic, qos, retained, payload)
	return token
}

// Subscribe subscribes to the specified topic with a message handler.
func (s *MqttService) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	token := s.client.Subscribe(topic, qos, callback)
	return token
}

// Disconnect gracefully disconnects the MQTT client.
func (s *MqttService) Disconnect(quiesce uint) {
	s.client.Disconnect(quiesce)
}
