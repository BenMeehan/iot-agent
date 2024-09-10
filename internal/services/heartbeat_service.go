package services

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/mqtt"

	"github.com/sirupsen/logrus"
)

// HeartbeatService defines the structure and functionality of the heartbeat service
type HeartbeatService struct {
	PubTopic string
	Interval time.Duration
	DeviceID string
	QOS      int
}

const StatusAlive = "1"

// Start initiates the heartbeat service and continuously publishes heartbeat messages to the MQTT broker
func (h *HeartbeatService) Start() error {
	client := mqtt.Client()
	if client == nil {
		logrus.Error("MQTT client not initialized")
		return fmt.Errorf("MQTT client not initialized")
	}

	go func() {
		for {
			heartbeatMessage := models.Heartbeat{
				DeviceID:  h.DeviceID,
				Timestamp: time.Now(),
				Status:    StatusAlive,
			}

			payload, err := json.Marshal(heartbeatMessage)
			if err != nil {
				logrus.WithError(err).Error("Failed to serialize heartbeat message")
				continue
			}

			// Publish the heartbeat message to the MQTT topic
			token := client.Publish(h.PubTopic, byte(h.QOS), false, payload)
			token.Wait()
			if token.Error() != nil {
				logrus.WithError(token.Error()).Error("Failed to publish heartbeat")
			} else {
				logrus.WithField("message", heartbeatMessage).Info("Heartbeat published successfully")
			}

			// Sleep for the specified interval before sending the next heartbeat
			time.Sleep(h.Interval)
		}
	}()

	return nil
}
