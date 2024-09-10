package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"os"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

var mqttClient mqtt.Client

// Initialize sets up the MQTT client with SSL/TLS and starts the connection.
// It takes the MQTT broker address, client ID, and path to the CA certificate as arguments.
func Initialize(broker, clientID, caCertPath string) error {
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		logrus.WithError(err).Error("Failed to read CA certificate")
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
		logrus.Info("MQTT client connected successfully")
	})

	// Handler for MQTT connection loss
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		logrus.WithError(err).Error("MQTT connection lost")
	})

	// Create and connect the MQTT client
	mqttClient = mqtt.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		logrus.WithError(token.Error()).Error("Failed to connect to MQTT broker")
		return token.Error()
	}

	logrus.Info("MQTT client initialized and connected")
	return nil
}

// Client returns the initialized MQTT client instance.
func Client() mqtt.Client {
	return mqttClient
}
