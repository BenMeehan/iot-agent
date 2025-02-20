package services

import (
	"context"
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

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
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
	}
}

// Start launches the heartbeat loop in a separate goroutine.
func (h *HeartbeatService) Start() error {
	if h.ctx != nil {
		h.Logger.Warn().Msg("HeartbeatService is already running")
		return errors.New("heartbeat service is already running")
	}

	h.ctx, h.cancel = context.WithCancel(context.Background())

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.runHeartbeatLoop()
	}()

	h.Logger.Info().Str("topic", h.PubTopic).Msg("HeartbeatService started successfully")
	return nil
}

// Stop gracefully stops the heartbeat service.
func (h *HeartbeatService) Stop() error {
	if h.ctx == nil {
		h.Logger.Warn().Msg("HeartbeatService is not running")
		return errors.New("heartbeat service is not running")
	}

	h.cancel()
	h.wg.Wait()

	h.ctx = nil
	h.cancel = nil

	h.Logger.Info().Msg("HeartbeatService stopped successfully")
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

			token := h.MqttClient.Publish(h.PubTopic, byte(h.QOS), false, payload)
			token.Wait()

			if err := token.Error(); err != nil {
				h.Logger.Error().Err(err).Msg("Failed to publish heartbeat message")
			} else {
				h.Logger.Debug().Msg("Heartbeat published successfully")
			}

		case <-h.ctx.Done():
			h.Logger.Info().Msg("HeartbeatService stopping gracefully")
			return
		}
	}
}
