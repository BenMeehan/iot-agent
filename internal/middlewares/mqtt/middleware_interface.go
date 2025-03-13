package mqtt_middleware

import mqttLib "github.com/eclipse/paho.mqtt.golang"

// MQTTMiddleware defines a generic middleware contract.
type MQTTMiddleware interface {
	Init(authenticationCertificatePath string) error
	Publish(topic string, qos byte, retained bool, payload interface{}) error
	Subscribe(topic string, qos byte, callback mqttLib.MessageHandler) error
	Unsubscribe(topics ...string) error
}
