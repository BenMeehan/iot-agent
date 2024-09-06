package mqtt

import (
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

var mqttClient MQTT.Client

// Initialize initializes the shared MQTT client
func Initialize(broker, clientID string) error {
	logrus.Info("Initializing MQTT connection...")

	opts := MQTT.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID(clientID)

	mqttClient = MQTT.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		logrus.WithError(token.Error()).Error("Failed to connect to MQTT broker")
		return token.Error()
	}

	logrus.WithField("broker", broker).Info("MQTT connection established")
	return nil
}

// Client returns the shared MQTT client
func Client() MQTT.Client {
	return mqttClient
}
