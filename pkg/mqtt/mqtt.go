package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"os"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

var client mqtt.Client

// Initialize sets up the MQTT client with SSL/TLS
func Initialize(broker, clientID, caFile string) error {
	// Load CA certificate
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		logrus.WithError(err).Error("Error reading CA certificate")
		return err
	}

	// Create a CA certificate pool and add the CA certificate
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// Create a TLS configuration with the CA certificate
	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		InsecureSkipVerify: true, // Verify the server certificate
	}

	// Set up the MQTT client options
	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)

	clientID = clientID + uuid.New().String()
	logrus.Info("Using MQTT client ID ", clientID)

	opts.SetClientID(clientID)
	opts.SetTLSConfig(tlsConfig)
	opts.SetAutoReconnect(true)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		logrus.Info("MQTT client connected")
	})
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		logrus.WithError(err).Error("MQTT connection lost")
	})

	// Create and start the MQTT client
	client = mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		logrus.WithError(token.Error()).Error("Error connecting to MQTT broker")
		return token.Error()
	}

	logrus.Info("MQTT client initialized")
	return nil
}

// Client returns the MQTT client instance
func Client() mqtt.Client {
	return client
}
