package services

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/mqtt"

	"github.com/sirupsen/logrus"
)

// HeartbeatService defines the heartbeat service
type HeartbeatService struct {
	PubTopic string
	Interval time.Duration
	DeviceID string
	QOS      int
}

const STATUS_ALIVE = "1"

// Start runs the heartbeat service and publishes to the MQTT broker
func (h *HeartbeatService) Start() error {
	client := mqtt.Client()
	if client == nil {
		logrus.Error("MQTT client not initialized")
		return fmt.Errorf("MQTT client not initialized")
	}

	go func() {
		for {
			message := models.Heartbeat{
				DeviceID:  h.DeviceID,
				Timestamp: time.Now(),
				Status:    STATUS_ALIVE,
			}

			payload, err := json.Marshal(message)
			if err != nil {
				logrus.WithError(err).Error("Failed to serialize heartbeat message")
				continue
			}

			token := client.Publish(h.PubTopic, byte(h.QOS), false, payload)
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
