package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"
	mqtt_middleware "github.com/benmeehan/iot-agent/internal/middlewares/mqtt"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/rs/zerolog"
)

// HeartbeatService manages periodic heartbeat messages.
type HeartbeatService struct {
	// Configuration fields
	pubTopic string
	interval time.Duration
	qos      int

	// Dependencies
	deviceInfo     identity.DeviceInfoInterface
	mqttMiddleware mqtt_middleware.MQTTMiddleware
	logger         zerolog.Logger

	// Internal state management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// newHeartbeatService initializes a new HeartbeatService.
func NewHeartbeatService(pubTopic string, interval time.Duration, qos int,
	deviceInfo identity.DeviceInfoInterface, mqttMiddleware mqtt_middleware.MQTTMiddleware, logger zerolog.Logger) *HeartbeatService {

	return &HeartbeatService{
		pubTopic:       pubTopic,
		interval:       interval,
		qos:            qos,
		deviceInfo:     deviceInfo,
		mqttMiddleware: mqttMiddleware,
		logger:         logger,
	}
}

// Start launches the heartbeat loop in a separate goroutine.
func (h *HeartbeatService) Start() error {
	if h.ctx != nil {
		h.logger.Warn().Msg("Heartbeat service is already running")
		return errors.New("heartbeat service is already running")
	}

	h.ctx, h.cancel = context.WithCancel(context.Background())

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.runHeartbeatLoop()
	}()

	h.logger.Info().Str("topic", h.pubTopic).Msg("Heartbeat service started successfully")
	return nil
}

// Stop gracefully stops the heartbeat service.
func (h *HeartbeatService) Stop() error {
	if h.ctx == nil {
		h.logger.Warn().Msg("Heartbeat service is not running")
		return errors.New("heartbeat service is not running")
	}

	h.cancel()
	h.wg.Wait()

	h.ctx = nil
	h.cancel = nil

	h.logger.Info().Msg("Heartbeat service stopped successfully")
	return nil
}

// runHeartbeatLoop continuously sends heartbeat messages at the specified interval.
func (h *HeartbeatService) runHeartbeatLoop() {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			heartbeatMessage := models.Heartbeat{
				DeviceID:  h.deviceInfo.GetDeviceID(),
				Timestamp: time.Now(),
				Status:    constants.StatusAlive,
			}

			payload, err := json.Marshal(heartbeatMessage)
			if err != nil {
				h.logger.Error().Err(err).Msg("Failed to serialize heartbeat message")
				continue
			}

			err = h.mqttMiddleware.Publish(h.pubTopic, byte(h.qos), false, payload)
			if err != nil {
				h.logger.Error().Err(err).Msg("Failed to publish heartbeat message")
			} else {
				h.logger.Debug().Msg("Heartbeat published successfully")
			}

		case <-h.ctx.Done():
			h.logger.Info().Msg("Stopping heartbeats...")
			return
		}
	}
}
