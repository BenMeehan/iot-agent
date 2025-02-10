package services

import (
	"encoding/json"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/sirupsen/logrus"
)

type HeartbeatService struct {
	PubTopic   string
	Interval   time.Duration
	DeviceInfo identity.DeviceInfoInterface
	QOS        int
	MqttClient mqtt.MQTTClient
	JWTManager jwt.JWTManagerInterface
	Logger     *logrus.Logger
	stopChan   chan struct{}
}

// Start initiates the heartbeat service and continuously publishes heartbeat messages to the MQTT broker
func (h *HeartbeatService) Start() error {
	h.stopChan = make(chan struct{}) // Channel to signal the goroutine to stop

	go func() {
		ticker := time.NewTicker(h.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				heartbeatMessage := models.Heartbeat{
					DeviceID:  h.DeviceInfo.GetDeviceID(),
					Timestamp: time.Now(),
					Status:    constants.StatusAlive,
					JWTToken:  h.JWTManager.GetJWT(),
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

			case <-h.stopChan:
				// Stop the goroutine
				return
			}
		}
	}()

	return nil
}

// Stop gracefully stops the heartbeat service
func (h *HeartbeatService) Stop() {
	if h.stopChan != nil {
		close(h.stopChan)
	}
}
