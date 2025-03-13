package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	pubTopic string
	clientID string
	qos      int

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
	deviceInfo identity.DeviceInfoInterface,
	mqttMiddleware mqtt_middleware.MQTTMiddleware,
	fileClient file.FileOperations,
	logger zerolog.Logger,
) *RegistrationService {
	return &RegistrationService{
		pubTopic:       pubTopic,
		clientID:       clientID,
		qos:            qos,
		deviceInfo:     deviceInfo,
		mqttMiddleware: mqttMiddleware,
		fileClient:     fileClient,
		logger:         logger,
	}
}

// Start begins the registration process if it's not already running.
func (rs *RegistrationService) Start() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.ctx != nil {
		rs.logger.Warn().Msg("RegistrationService is already running")
		return errors.New("registration service is already running")
	}

	rs.ctx, rs.cancel = context.WithCancel(context.Background())

	rs.logger.Info().Str("client_id", rs.clientID).Msg("Starting registration process")

	return rs.run()
}

// run initiates the registration process, excluding ClientID if DeviceID exists.
func (rs *RegistrationService) run() error {
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
	// Determine the response topic based on whether DeviceID is present
	existingDeviceID := rs.deviceInfo.GetDeviceID()
	respTopic := fmt.Sprintf("%s/response/%s", rs.pubTopic, rs.clientID)
	if existingDeviceID != "" {
		respTopic = fmt.Sprintf("%s/response/%s", rs.pubTopic, existingDeviceID)
	}

	rs.logger.Info().Str("topic", respTopic).Msg("Subscribing to response topic")

	responseChannel := make(chan string, 1)
	defer close(responseChannel)

	// Subscribe to the response topic to receive registration confirmation.
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

	// Serialize the payload (with either ClientID or DeviceID) before sending it over MQTT.
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		defer rs.mqttMiddleware.Unsubscribe(respTopic)
		return fmt.Errorf("failed to serialize registration payload: %w", err)
	}

	// Publish the payload to the MQTT topic.
	err = rs.mqttMiddleware.Publish(rs.pubTopic, byte(rs.qos), false, payloadBytes)
	if err != nil {
		rs.logger.Error().Err(err).Msg("Failed to publish registration message")
		defer rs.mqttMiddleware.Unsubscribe(respTopic)
		return err
	}

	// Wait for a response or timeout.
	select {
	case deviceID := <-responseChannel:
		rs.logger.Info().Str("device_id", deviceID).Msg("Device registered successfully")
		defer rs.mqttMiddleware.Unsubscribe(respTopic)
		// Save the device ID if it's different or not yet saved
		if deviceID != rs.deviceInfo.GetDeviceID() {
			return rs.deviceInfo.SaveDeviceID(deviceID)
		}
		return nil
	case <-time.After(10 * time.Second):
		rs.logger.Error().Str("client_id", rs.clientID).Msg("Registration timeout")
		defer rs.mqttMiddleware.Unsubscribe(respTopic)
		return errors.New("registration timeout")
	case <-rs.ctx.Done():
		rs.logger.Warn().Msg("RegistrationService stopping during registration")
		defer rs.mqttMiddleware.Unsubscribe(respTopic)
		return errors.New("registration service stopped")
	}
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

	rs.logger.Info().Msg("RegistrationService stopped successfully")
	return nil
}
