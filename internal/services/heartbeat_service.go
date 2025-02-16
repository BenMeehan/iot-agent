package services

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/rs/zerolog"
)

// HeartbeatService handles sending periodic heartbeat messages.
type HeartbeatService struct {
	PubTopic   string
	Interval   time.Duration
	DeviceInfo identity.DeviceInfoInterface
	QOS        int
	MqttClient mqtt.MQTTClient
	JWTManager jwt.JWTManagerInterface
	Logger     zerolog.Logger
	stopChan   chan struct{}
	running    bool
}

// Start begins publishing heartbeat messages at regular intervals.
func (h *HeartbeatService) Start() error {
	if h.running {
		h.Logger.Warn().Msg("HeartbeatService is already running")
		return errors.New("heartbeat service is already running")
	}

	h.stopChan = make(chan struct{}) // Channel to signal the goroutine to stop
	h.running = true

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
					h.Logger.Error().Err(err).Msg("Failed to serialize heartbeat message")
					continue
				}

				// Publish the heartbeat message to the MQTT topic
				token := h.MqttClient.Publish(h.PubTopic, byte(h.QOS), false, payload)
				token.Wait()

				if err := token.Error(); err != nil {
					h.Logger.Error().Err(err).Msg("Failed to publish heartbeat message")
				} else {
					h.Logger.Info().Msg("Heartbeat published successfully")
				}

			case <-h.stopChan:
				h.running = false
				return
			}
		}
	}()

	h.Logger.Info().Str("topic", h.PubTopic).Msg("HeartbeatService started")
	return nil
}

// Stop gracefully stops the heartbeat service.
func (h *HeartbeatService) Stop() error {
	if !h.running {
		h.Logger.Warn().Msg("HeartbeatService is not running")
		return errors.New("heartbeat service is not running")
	}

	if h.stopChan == nil {
		h.Logger.Error().Msg("Failed to stop HeartbeatService: stop channel is nil")
		return errors.New("stop channel is nil")
	}

	close(h.stopChan)
	h.running = false
	h.Logger.Info().Msg("HeartbeatService stopped")
	return nil
}
