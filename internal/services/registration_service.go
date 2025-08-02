package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"

	mqtt_middleware "github.com/benmeehan/iot-agent/internal/middlewares/mqtt"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
)

// RegistrationService manages the process of registering a device with the IoT backend.
type RegistrationService struct {
	// Configuration fields
	pubTopic        string
	clientID        string
	qos             int
	maxRetries      int
	baseDelay       time.Duration
	maxDelay        time.Duration
	responseTimeout time.Duration

	// Dependencies for handling device information, MQTT, file operations, and encryption
	deviceInfo     identity.DeviceInfoInterface
	mqttMiddleware mqtt_middleware.MQTTMiddleware
	fileClient     file.FileOperations
	logger         zerolog.Logger

	// Internal state for managing service lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

// NewRegistrationService initializes and returns a new RegistrationService instance.
func NewRegistrationService(
	pubTopic string,
	clientID string,
	qos int,
	maxRetries int,
	baseDelay time.Duration,
	maxDelay time.Duration,
	responseTimeout time.Duration,
	deviceInfo identity.DeviceInfoInterface,
	mqttMiddleware mqtt_middleware.MQTTMiddleware,
	fileClient file.FileOperations,
	logger zerolog.Logger,
) *RegistrationService {
	return &RegistrationService{
		pubTopic:        pubTopic,
		clientID:        clientID,
		qos:             qos,
		maxRetries:      maxRetries,
		baseDelay:       time.Duration(baseDelay) * time.Second,
		maxDelay:        time.Duration(maxDelay) * time.Second,
		responseTimeout: time.Duration(responseTimeout) * time.Second,
		deviceInfo:      deviceInfo,
		mqttMiddleware:  mqttMiddleware,
		fileClient:      fileClient,
		logger:          logger,
	}
}

// Start begins the registration process if it's not already running.
func (rs *RegistrationService) Start() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.ctx != nil {
		rs.logger.Warn().Msg("Registration service is already running")
		return errors.New("registration service is already running")
	}

	rs.ctx, rs.cancel = context.WithCancel(context.Background())

	rs.logger.Info().Str("client_id", rs.clientID).Msg("Starting registration process")

	return rs.Run()
}

// run initiates the registration process, excluding ClientID if DeviceID exists.
func (rs *RegistrationService) Run() error {
	existingDeviceID := rs.deviceInfo.GetDeviceID()

	payload := models.RegistrationPayload{
		Name:     rs.deviceInfo.GetDeviceIdentity().Name,
		OrgID:    rs.deviceInfo.GetDeviceIdentity().OrgID,
		Metadata: rs.deviceInfo.GetDeviceIdentity().Metadata,
	}

	if existingDeviceID != "" {
		rs.logger.Info().Str("device_id", existingDeviceID).Msg("Device ID found, registering with existing device ID")
		payload.DeviceID = existingDeviceID
	} else {
		rs.logger.Info().Msg("No existing device ID found, registering as new device with client ID")
		payload.ClientID = rs.clientID
	}

	return rs.Register(payload)
}

// Register sends a registration request over MQTT and waits for a response, subscribing with DeviceID if present.
func (rs *RegistrationService) Register(payload models.RegistrationPayload) error {
	existingDeviceID := rs.deviceInfo.GetDeviceID()
	respTopic := fmt.Sprintf("%s/response/%s", rs.pubTopic, rs.clientID)
	if existingDeviceID != "" {
		respTopic = fmt.Sprintf("%s/response/%s", rs.pubTopic, existingDeviceID)
	}

	rs.logger.Info().Str("topic", respTopic).Msg("Subscribing to response topic")

	responseChannel := make(chan string, 1)
	defer close(responseChannel)

	err := rs.mqttMiddleware.Subscribe(respTopic, byte(rs.qos), func(client MQTT.Client, msg MQTT.Message) {
		var response models.RegistrationResponse
		if err := json.Unmarshal(msg.Payload(), &response); err != nil {
			rs.logger.Error().Err(err).Msg("Error parsing registration response")
			return
		}
		if response.DeviceID == "" {
			rs.logger.Error().Msg("Invalid registration response")
			return
		}
		responseChannel <- response.DeviceID
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to response topic: %w", err)
	}

	defer func() {
		if err := rs.mqttMiddleware.Unsubscribe(respTopic); err != nil {
			rs.logger.Warn().Err(err).Msgf("failed to unsubscribe from %s:", respTopic)
		}
	}()

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to serialize registration payload: %w", err)
	}

	if rs.ctx == nil {
		rs.ctx = context.Background()
	}

	for attempt := 0; attempt <= rs.maxRetries; attempt++ {
		delay := rs.baseDelay * time.Duration(1<<uint(attempt))
		if delay > rs.maxDelay {
			delay = rs.maxDelay
		}
		jitter := time.Duration(float64(delay) * (0.5 + rand.Float64()*0.5))
		delay = time.Duration(float64(delay)*0.75) + jitter

		attemptTimeout := rs.responseTimeout + delay
		attemptCtx, cancel := context.WithTimeout(rs.ctx, attemptTimeout)
		defer cancel()

		err = rs.mqttMiddleware.Publish(rs.pubTopic, byte(rs.qos), false, payloadBytes)
		if err != nil {
			rs.logger.Error().Err(err).Int("attempt", attempt+1).Msg("Failed to publish registration message")
			if attempt == rs.maxRetries {
				return fmt.Errorf("failed to publish after %d attempts: %w", rs.maxRetries+1, err)
			}
		} else {
			select {
			case deviceID := <-responseChannel:
				rs.logger.Info().Str("device_id", deviceID).Int("attempt", attempt+1).Msg("Device registered successfully")
				if deviceID != rs.deviceInfo.GetDeviceID() {
					return rs.deviceInfo.SaveDeviceID(deviceID)
				}
				return nil
			case <-attemptCtx.Done():
				rs.logger.Warn().Int("attempt", attempt+1).Msg("Registration timeout or cancelled")
				if attempt == rs.maxRetries {
					return errors.New("registration timeout after maximum retries")
				}
				if errors.Is(attemptCtx.Err(), context.Canceled) {
					return errors.New("registration service stopped")
				}
			}
		}

		select {
		case <-time.After(delay):
			continue
		case <-rs.ctx.Done():
			rs.logger.Warn().Msg("Registration Service stopping during retry delay")
			return errors.New("registration service stopped")
		}
	}

	return errors.New("unexpected error: retry loop completed without resolution")
}

// Stop gracefully shuts down the registration service and unsubscribes from MQTT topics.
func (rs *RegistrationService) Stop() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.ctx == nil {
		return errors.New("registration service is not running")
	}

	rs.cancel()

	// Unsubscribe from the response topic, using DeviceID if present, otherwise ClientID
	existingDeviceID := rs.deviceInfo.GetDeviceID()
	topic := fmt.Sprintf("%s/response/%s", rs.pubTopic, rs.clientID)
	if existingDeviceID != "" {
		topic = fmt.Sprintf("%s/response/%s", rs.pubTopic, existingDeviceID)
	}
	err := rs.mqttMiddleware.Unsubscribe(topic)
	if err != nil {
		rs.logger.Error().Err(err).Str("topic", topic).Msg("Failed to unsubscribe from MQTT topic")
		return err
	}

	rs.ctx = nil
	rs.cancel = nil

	rs.logger.Info().Msg("Registration service stopped successfully")
	return nil
}
