package services

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/rs/zerolog"
)

// HeartbeatService manages periodic heartbeat messages.
type HeartbeatService struct {
	PubTopic   string
	Interval   time.Duration
	DeviceInfo identity.DeviceInfoInterface
	QOS        int
	MqttClient mqtt.MQTTClient
	JWTManager jwt.JWTManagerInterface
	Logger     zerolog.Logger

	stopChan chan struct{}
	running  bool
	mu       sync.Mutex
}

// NewHeartbeatService initializes a new HeartbeatService.
func NewHeartbeatService(pubTopic string, interval time.Duration, deviceInfo identity.DeviceInfoInterface,
	qos int, mqttClient mqtt.MQTTClient, jwtManager jwt.JWTManagerInterface, logger zerolog.Logger) *HeartbeatService {

	return &HeartbeatService{
		PubTopic:   pubTopic,
		Interval:   interval,
		DeviceInfo: deviceInfo,
		QOS:        qos,
		MqttClient: mqttClient,
		JWTManager: jwtManager,
		Logger:     logger,
		stopChan:   make(chan struct{}), // Initialize only once
	}
}

// Start launches the heartbeat loop in a separate goroutine.
func (h *HeartbeatService) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.running {
		h.Logger.Warn().Msg("HeartbeatService is already running")
		return errors.New("heartbeat service is already running")
	}

	h.running = true

	go h.runHeartbeatLoop()
	h.Logger.Info().Str("topic", h.PubTopic).Msg("HeartbeatService started successfully")
	return nil
}

// runHeartbeatLoop continuously sends heartbeat messages at the specified interval.
func (h *HeartbeatService) runHeartbeatLoop() {
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

			// Publish heartbeat message
			token := h.MqttClient.Publish(h.PubTopic, byte(h.QOS), false, payload)
			token.Wait()

			if err := token.Error(); err != nil {
				h.Logger.Error().Err(err).Msg("Failed to publish heartbeat message")
			} else {
				h.Logger.Debug().Msg("Heartbeat published successfully")
			}

		case <-h.stopChan:
			h.mu.Lock()
			h.running = false
			h.mu.Unlock()
			h.Logger.Info().Msg("HeartbeatService stopped")
			return
		}
	}
}

// Stop gracefully stops the heartbeat service.
func (h *HeartbeatService) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		h.Logger.Warn().Msg("HeartbeatService is not running")
		return errors.New("heartbeat service is not running")
	}

	close(h.stopChan)
	h.stopChan = make(chan struct{}) // Reinitialize for future restarts
	h.running = false

	h.Logger.Info().Msg("HeartbeatService stopped successfully")
	return nil
}
