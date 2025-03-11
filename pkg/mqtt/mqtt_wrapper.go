package mqtt

import MQTT "github.com/eclipse/paho.mqtt.golang"

// Wrapper defines the interface for an MQTT client middlewares
// that wraps common MQTT operations such as publishing, subscribing, and unsubscribing.
type Wrapper interface {
	// MQTTPublish publishes a message to the given MQTT topic with the specified
	// Quality of Service (QoS), retention flag, and payload.
	// It returns the MQTT Token associated with the publish operation
	// and any potential error encountered during the process.
	MQTTPublish(topic string, qos byte, retained bool, payload interface{}) (MQTT.Token, error)

	// MQTTSubscribe subscribes to the given MQTT topic with the specified
	// Quality of Service (QoS) level and a callback function that will be
	// triggered when messages are received on that topic.
	// It returns the MQTT Token associated with the subscription.
	MQTTSubscribe(topic string, qos byte, callback MQTT.MessageHandler) MQTT.Token

	// MQTTUnsubscribe unsubscribes from one or more specified MQTT topics.
	// It returns the MQTT Token associated with the unsubscribe operation.
	MQTTUnsubscribe(topics ...string) MQTT.Token
}
