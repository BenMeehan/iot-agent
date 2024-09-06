package services

import (
	"fmt"
	"time"

	"github.com/benmeehan/iot-agent/pkg/mqtt"

	"github.com/sirupsen/logrus"
)

// HeartbeatService defines the heartbeat service
type HeartbeatService struct {
	PubTopic string
	Interval time.Duration
}

// Start runs the heartbeat service and publishes to the MQTT broker
func (h *HeartbeatService) Start() error {
	client := mqtt.Client()
	if client == nil {
		logrus.Error("MQTT client not initialized")
		return fmt.Errorf("MQTT client not initialized")
	}

	go func() {
		for {
			message := time.Now().Format(time.RFC3339)
			token := client.Publish(h.PubTopic, 0, false, message)
			token.Wait()
			if token.Error() != nil {
				logrus.WithError(token.Error()).Error("Failed to publish heartbeat")
			} else {
				logrus.WithField("message", message).Info("Published heartbeat")
			}
			time.Sleep(10 * time.Second)
		}
	}()

	return nil
}
