package mqtt

import (
	"fmt"

	"github.com/benmeehan/iot-agent/pkg/mqtt"
	mqttLib "github.com/eclipse/paho.mqtt.golang"
)

// ChainedMQTTClient wraps an MQTT client with a middleware chain.
type ChainedMQTTClient struct {
	middlewares []MQTTMiddleware
	mqttClient  mqtt.MQTTClient
}

// NewChainedMQTTClient creates a new chained MQTT client.
func NewChainedMQTTClient(mqttClient mqtt.MQTTClient, middlewares []MQTTMiddleware) *ChainedMQTTClient {
	// Chain middlewares
	for i := 0; i < len(middlewares)-1; i++ {
		middlewares[i].SetNext(middlewares[i+1])
	}
	if len(middlewares) > 0 {
		middlewares[len(middlewares)-1].SetNext(&directMQTTClient{mqttClient: mqttClient})
	}
	return &ChainedMQTTClient{
		middlewares: middlewares,
		mqttClient:  mqttClient,
	}
}

// Init initializes all middlewares in the chain.
func (c *ChainedMQTTClient) Init(params interface{}) error {
	for _, mw := range c.middlewares {
		if err := mw.Init(params); err != nil {
			return fmt.Errorf("failed to init middleware: %w", err)
		}
	}
	return nil
}

// Publish sends a message through the middleware chain.
func (c *ChainedMQTTClient) Publish(topic string, qos byte, retained bool, payload interface{}) error {
	if len(c.middlewares) == 0 {
		token := c.mqttClient.Publish(topic, qos, retained, payload)
		token.Wait()
		return token.Error()
	}
	return c.middlewares[0].Publish(topic, qos, retained, payload)
}

// Subscribe subscribes through the middleware chain.
func (c *ChainedMQTTClient) Subscribe(topic string, qos byte, callback mqttLib.MessageHandler) error {
	if len(c.middlewares) == 0 {
		token := c.mqttClient.Subscribe(topic, qos, callback)
		token.Wait()
		return token.Error()
	}
	return c.middlewares[0].Subscribe(topic, qos, callback)
}

// Unsubscribe unsubscribes through the middleware chain.
func (c *ChainedMQTTClient) Unsubscribe(topics ...string) error {
	if len(c.middlewares) == 0 {
		token := c.mqttClient.Unsubscribe(topics...)
		token.Wait()
		return token.Error()
	}
	return c.middlewares[0].Unsubscribe(topics...)
}

// SetNext implements the MQTTMiddleware interface (no-op for the chain entry point).
func (c *ChainedMQTTClient) SetNext(next MQTTMiddleware) {
	// No-op: ChainedMQTTClient is the entry point and does not need a next middleware.
}

// directMQTTClient is a fallback middleware that delegates to the MQTT client.
type directMQTTClient struct {
	mqttClient mqtt.MQTTClient
}

func (d *directMQTTClient) Init(_ interface{}) error {
	return nil
}

func (d *directMQTTClient) SetNext(_ MQTTMiddleware) {}

func (d *directMQTTClient) Publish(topic string, qos byte, retained bool, payload interface{}) error {
	token := d.mqttClient.Publish(topic, qos, retained, payload)
	token.Wait()
	return token.Error()
}

func (d *directMQTTClient) Subscribe(topic string, qos byte, callback mqttLib.MessageHandler) error {
	token := d.mqttClient.Subscribe(topic, qos, callback)
	token.Wait()
	return token.Error()
}

func (d *directMQTTClient) Unsubscribe(topics ...string) error {
	token := d.mqttClient.Unsubscribe(topics...)
	token.Wait()
	return token.Error()
}
