package services

import (
	"encoding/json"
	"time"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"

	"github.com/sirupsen/logrus"
)

// HeartbeatService defines the structure and functionality of the heartbeat service
type HeartbeatService struct {
	PubTopic   string
	Interval   time.Duration
	DeviceInfo identity.DeviceInfoInterface
	QOS        int
	MqttClient mqtt.MQTTClient
	Logger     *logrus.Logger
}

const StatusAlive = "1"

// Start initiates the heartbeat service and continuously publishes heartbeat messages to the MQTT broker
func (h *HeartbeatService) Start() error {
	go func() {
		for range time.Tick(h.Interval) {
			heartbeatMessage := models.Heartbeat{
				DeviceID:  h.DeviceInfo.GetDeviceID(),
				Timestamp: time.Now(),
				Status:    StatusAlive,
			}

			payload, err := json.Marshal(heartbeatMessage)
			if err != nil {
				h.Logger.WithError(err).Error("failed to serialize heartbeat message")
				continue
			}

			// Publish the heartbeat message to the MQTT topic
			token := h.MqttClient.Publish(h.PubTopic, byte(h.QOS), false, payload)
			token.Wait()

			if err := token.Error(); err != nil {
				h.Logger.WithError(err).Error("failed to publish heartbeat message")
				continue
			} else {
				h.Logger.WithField("message", heartbeatMessage).Info("Heartbeat published successfully")
			}
		}
	}()

	return nil
}
