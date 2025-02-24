package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/encryption"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
)

// RegistrationService manages the device registration process, ensuring secure communication
// and proper state management during registration attempts.
type RegistrationService struct {
	// Configuration fields
	pubTopic          string
	clientID          string
	qos               int
	maxBackoffSeconds int

	// Dependencies
	deviceInfo        identity.DeviceInfoInterface
	mqttClient        mqtt.MQTTClient
	fileClient        file.FileOperations
	jwtManager        jwt.JWTManagerInterface
	encryptionManager encryption.EncryptionManagerInterface
	logger            zerolog.Logger

	// Internal state management
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

// NewRegistrationService initializes and returns a new RegistrationService instance.
func NewRegistrationService(
	pubTopic string,
	clientID string,
	qos int,
	maxBackoffSeconds int,
	deviceInfo identity.DeviceInfoInterface,
	mqttClient mqtt.MQTTClient,
	fileClient file.FileOperations,
	jwtManager jwt.JWTManagerInterface,
	encryptionManager encryption.EncryptionManagerInterface,
	logger zerolog.Logger,
) *RegistrationService {
	return &RegistrationService{
		pubTopic:          pubTopic,
		clientID:          clientID,
		qos:               qos,
		maxBackoffSeconds: maxBackoffSeconds,
		deviceInfo:        deviceInfo,
		mqttClient:        mqttClient,
		fileClient:        fileClient,
		jwtManager:        jwtManager,
		encryptionManager: encryptionManager,
		logger:            logger,
	}
}

// Start initiates the device registration process by launching the registration routine
// in a separate goroutine and handling graceful shutdown through context management.
func (rs *RegistrationService) Start() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.ctx != nil {
		rs.logger.Warn().Msg("RegistrationService is already running")
		return errors.New("registration service is already running")
	}

	rs.ctx, rs.cancel = context.WithCancel(context.Background())

	rs.logger.Info().Str("client_id", rs.clientID).Msg("Starting registration process")

	err := rs.run()
	if err != nil {
		return err
	}

	return nil
}

// run manages the core registration logic, including JWT validation and conditional re-registration.
func (rs *RegistrationService) run() error {
	existingDeviceID := rs.deviceInfo.GetDeviceID()
	if existingDeviceID != "" {
		rs.logger.Info().Str("device_id", existingDeviceID).Msg("Found existing device ID")

		valid, err := rs.jwtManager.IsJWTValid()
		if err != nil {
			rs.logger.Error().Err(err).Msg("Failed to check JWT token validity")
			return err
		}

		if !valid {
			rs.logger.Warn().Msg("JWT token is invalid, attempting re-registration")
			payload := models.RegistrationPayload{DeviceID: existingDeviceID}
			err = rs.RetryRegistration(payload)
			if err != nil {
				rs.logger.Error().Err(err).Msg("Failed to re-register device")
				return err
			}
			return nil
		}

		rs.logger.Info().Msg("Device is already registered and JWT is valid")
		return nil
	}

	payload := models.RegistrationPayload{
		ClientID: rs.clientID,
		Name:     rs.deviceInfo.GetDeviceIdentity().Name,
		OrgID:    rs.deviceInfo.GetDeviceIdentity().OrgID,
		Metadata: rs.deviceInfo.GetDeviceIdentity().Metadata,
	}

	_ = rs.RetryRegistration(payload)
	return nil
}

// RetryRegistration attempts device registration with exponential backoff and jitter.
func (rs *RegistrationService) RetryRegistration(payload models.RegistrationPayload) error {
	backoff := 1 * time.Second
	maxBackoff := time.Duration(rs.maxBackoffSeconds) * time.Second
	retryCount := 0

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to serialize registration payload")
	}

	encryptedPayload, err := rs.encryptionManager.Encrypt(payloadBytes)
	if err != nil {
		rs.logger.Error().Err(err).Msg("Failed to encrypt registration payload")
		return err
	}

	for {
		retryCount++
		rs.logger.Info().Int("attempt", retryCount).Msg("Registering device")

		if err := rs.Register(encryptedPayload, payload.DeviceID); err == nil {
			rs.logger.Info().Int("attempts", retryCount).Msg("Device registration succeeded")
			return nil
		}

		rs.logger.Warn().Int("attempt", retryCount).Dur("backoff", backoff).Msg("Retrying registration")

		select {
		case <-time.After(backoff):
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			backoff += time.Duration(rand.Intn(500)) * time.Millisecond
		case <-rs.ctx.Done():
			rs.logger.Warn().Msg("RegistrationService stopping during retry")
			return errors.New("registration service stopped")
		}
	}
}

// Register handles publishing the registration request and processing the server's response.
func (rs *RegistrationService) Register(encryptedPayload []byte, deviceID string) error {
	respTopic := fmt.Sprintf("%s/response/%s", rs.pubTopic, rs.clientID)
	if deviceID != "" {
		respTopic = fmt.Sprintf("%s/response/%s", rs.pubTopic, deviceID)
	}

	rs.logger.Info().Str("topic", respTopic).Msg("Subscribing to response topic")

	responseChannel := make(chan string, 1)
	defer close(responseChannel)

	rs.mqttClient.Subscribe(respTopic, byte(rs.qos), func(client MQTT.Client, msg MQTT.Message) {
		var response models.RegistrationResponse
		if err := json.Unmarshal(msg.Payload(), &response); err != nil {
			rs.logger.Error().Err(err).Msg("Error parsing registration response")
			return
		}

		if response.DeviceID == "" || response.JWTToken == "" {
			rs.logger.Error().Msg("Invalid registration response")
			return
		}

		if err := rs.jwtManager.SaveJWT(response.JWTToken); err != nil {
			rs.logger.Error().Err(err).Msg("Failed to save JWT token")
			return
		}

		responseChannel <- response.DeviceID
	})

	rs.logger.Info().Str("topic", rs.pubTopic).Msg("Publishing registration request")
	publishToken := rs.mqttClient.Publish(rs.pubTopic, byte(rs.qos), false, encryptedPayload)
	publishToken.Wait()
	if err := publishToken.Error(); err != nil {
		rs.logger.Error().Err(err).Msg("Failed to publish registration message")
		return err
	}

	select {
	case deviceID := <-responseChannel:
		rs.logger.Info().Str("device_id", deviceID).Msg("Device registered successfully")
		return rs.deviceInfo.SaveDeviceID(deviceID)
	case <-time.After(10 * time.Second):
		rs.logger.Error().Str("client_id", rs.clientID).Msg("Registration timeout")
		return errors.New("registration timeout")
	case <-rs.ctx.Done():
		rs.logger.Warn().Msg("RegistrationService stopping during registration")
		return errors.New("registration service stopped")
	}
}

// Stop gracefully stops the registration service by cancelling its context
// and waiting for all ongoing routines to complete.
func (rs *RegistrationService) Stop() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.ctx == nil {
		return errors.New("registration service is not running")
	}

	rs.cancel()

	rs.ctx = nil
	rs.cancel = nil

	rs.logger.Info().Msg("RegistrationService stopped successfully")
	return nil
}
