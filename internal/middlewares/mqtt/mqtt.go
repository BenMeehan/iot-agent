package mqtt

import mqttLib "github.com/eclipse/paho.mqtt.golang"

// MQTTMiddleware defines the contract for MQTT middleware.
type MQTTMiddleware interface {
	Init(interface{}) error
	Publish(topic string, qos byte, retained bool, payload interface{}) error
	Subscribe(topic string, qos byte, callback mqttLib.MessageHandler) error
	Unsubscribe(topics ...string) error
	SetNext(next MQTTMiddleware)
}
