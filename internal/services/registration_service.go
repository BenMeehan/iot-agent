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

// RegistrationService manages device registration, refresh, and metadata updates with secure validation.
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

// NewRegistrationService initializes a new RegistrationService instance.
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

// Start initiates the registration process.
func (rs *RegistrationService) Start() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.ctx != nil {
		rs.logger.Warn().Msg("RegistrationService is already running")
		return errors.New("registration service is already running")
	}

	rs.ctx, rs.cancel = context.WithCancel(context.Background())

	rs.logger.Info().Str("client_id", rs.clientID).Msg("Starting registration process")

	err := rs.Run()
	if err != nil {
		rs.logger.Error().Err(err).Msg("Failed to start registration service")
		return err
	}

	return nil
}

// run manages registration and refresh logic.
func (rs *RegistrationService) Run() error {
	existingDeviceID := rs.deviceInfo.GetDeviceID()
	if existingDeviceID != "" {
		rs.logger.Info().Str("device_id", existingDeviceID).Msg("Found existing device ID")

		valid, err := rs.jwtManager.IsJWTValid()
		if err != nil {
			rs.logger.Error().Err(err).Msg("Failed to check JWT validity")
			return err
		}

		if !valid {
			rs.logger.Warn().Msg("JWT invalid or expired, attempting refresh")
			if err := rs.RefreshToken(); err != nil {
				rs.logger.Warn().Err(err).Msg("Refresh failed, attempting re-registration")
				payload := models.RegistrationPayload{DeviceID: existingDeviceID}
				return rs.RetryRegistration(payload)
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

	return rs.RetryRegistration(payload)
}

// RetryRegistration attempts registration with exponential backoff.
func (rs *RegistrationService) RetryRegistration(payload models.RegistrationPayload) error {
	backoff := 1 * time.Second
	maxBackoff := time.Duration(rs.maxBackoffSeconds) * time.Second
	retryCount := 0

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to serialize registration payload")
	}

	signedPayload, err := rs.encryptionManager.SignPayload(payloadBytes)
	if err != nil {
		return err
	}
	encryptedPayload, err := rs.encryptionManager.Encrypt(signedPayload)
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

// Register handles initial registration with signature validation.
func (rs *RegistrationService) Register(encryptedPayload []byte, deviceID string) error {
	respTopic := fmt.Sprintf("%s/response/%s", rs.pubTopic, rs.clientID)
	if deviceID != "" {
		respTopic = fmt.Sprintf("%s/response/%s", rs.pubTopic, deviceID)
	}

	rs.logger.Info().Str("topic", respTopic).Msg("Subscribing to response topic")

	responseChannel := make(chan models.RegistrationResponse, 1)
	defer close(responseChannel)

	rs.mqttClient.Subscribe(respTopic, byte(rs.qos), func(client MQTT.Client, msg MQTT.Message) {
		decrypted, err := rs.encryptionManager.Decrypt(msg.Payload())
		if err != nil {
			rs.logger.Error().Err(err).Msg("Failed to decrypt registration response")
			return
		}

		if !rs.encryptionManager.VerifyPayloadSignature(decrypted) {
			rs.logger.Error().Msg("Invalid registration response signature")
			return
		}

		payload := decrypted[:len(decrypted)-32]
		var response models.RegistrationResponse
		if err := json.Unmarshal(payload, &response); err != nil {
			rs.logger.Error().Err(err).Msg("Error parsing registration response")
			return
		}

		if response.JWTToken == "" {
			rs.logger.Error().Msg("Invalid registration response")
			return
		}

		// Validate JWT signature
		if _, err := rs.jwtManager.ValidateJWT(response.JWTToken); err != nil {
			rs.logger.Error().Err(err).Msg("Invalid JWT signature")
			return
		}

		if err := rs.jwtManager.SaveJWT(response.JWTToken); err != nil {
			rs.logger.Error().Err(err).Msg("Failed to save JWT token")
			return
		}
		if response.RefreshToken != "" {
			if err := rs.jwtManager.SaveRefreshToken(response.RefreshToken); err != nil {
				rs.logger.Error().Err(err).Msg("Failed to save refresh token")
				return
			}
		}

		responseChannel <- response
	})

	rs.logger.Info().Str("topic", rs.pubTopic).Msg("Publishing registration request")
	publishToken := rs.mqttClient.Publish(rs.pubTopic, byte(rs.qos), false, encryptedPayload)
	publishToken.Wait()
	if err := publishToken.Error(); err != nil {
		rs.logger.Error().Err(err).Msg("Failed to publish registration message")
		return err
	}

	select {
	case response := <-responseChannel:
		rs.logger.Info().Str("device_id", response.DeviceID).Msg("Device registered successfully")
		return rs.deviceInfo.SaveDeviceID(response.DeviceID)
	case <-time.After(10 * time.Second):
		rs.logger.Error().Str("client_id", rs.clientID).Msg("Registration timeout")
		return errors.New("registration timeout")
	case <-rs.ctx.Done():
		rs.logger.Warn().Msg("RegistrationService stopping during registration")
		return errors.New("registration service stopped")
	}
}

// RefreshToken refreshes the JWT using the stored refresh token.
func (rs *RegistrationService) RefreshToken() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	deviceID := rs.deviceInfo.GetDeviceID()
	if deviceID == "" {
		return errors.New("no device ID available for refresh")
	}

	refreshToken, err := rs.jwtManager.GetRefreshToken()
	if err != nil || refreshToken == "" {
		return fmt.Errorf("failed to get refresh token: %v", err)
	}

	payload := map[string]string{
		"device_id":     deviceID,
		"refresh_token": refreshToken,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to serialize refresh payload: %v", err)
	}

	signedPayload, err := rs.encryptionManager.SignPayload(payloadBytes)
	if err != nil {
		return err
	}
	encryptedPayload, err := rs.encryptionManager.Encrypt(signedPayload)
	if err != nil {
		return fmt.Errorf("failed to encrypt refresh payload: %v", err)
	}

	respTopic := fmt.Sprintf("%s/refresh/response/%s", rs.pubTopic, deviceID)
	responseChannel := make(chan models.RegistrationResponse, 1)
	defer close(responseChannel)

	rs.mqttClient.Subscribe(respTopic, byte(rs.qos), func(client MQTT.Client, msg MQTT.Message) {
		decrypted, err := rs.encryptionManager.Decrypt(msg.Payload())
		if err != nil {
			rs.logger.Error().Err(err).Msg("Failed to decrypt refresh response")
			return
		}

		if !rs.encryptionManager.VerifyPayloadSignature(decrypted) {
			rs.logger.Error().Msg("Invalid refresh response signature")
			return
		}

		payload := decrypted[:len(decrypted)-32]
		var response models.RegistrationResponse
		if err := json.Unmarshal(payload, &response); err != nil {
			rs.logger.Error().Err(err).Msg("Error parsing refresh response")
			return
		}

		if response.JWTToken == "" {
			rs.logger.Error().Msg("Invalid refresh response")
			return
		}

		if _, err := rs.jwtManager.ValidateJWT(response.JWTToken); err != nil {
			rs.logger.Error().Err(err).Msg("Invalid JWT signature")
			return
		}

		if err := rs.jwtManager.SaveJWT(response.JWTToken); err != nil {
			rs.logger.Error().Err(err).Msg("Failed to save new JWT token")
			return
		}
		if response.RefreshToken != "" { // Support rotation
			if err := rs.jwtManager.SaveRefreshToken(response.RefreshToken); err != nil {
				rs.logger.Error().Err(err).Msg("Failed to save new refresh token")
				return
			}
		}

		responseChannel <- response
	})

	refreshTopic := fmt.Sprintf("%s/refresh", rs.pubTopic)
	rs.logger.Info().Str("topic", refreshTopic).Msg("Publishing refresh request")
	publishToken := rs.mqttClient.Publish(refreshTopic, byte(rs.qos), false, encryptedPayload)
	publishToken.Wait()
	if err := publishToken.Error(); err != nil {
		rs.logger.Error().Err(err).Msg("Failed to publish refresh message")
		return err
	}

	select {
	case <-responseChannel:
		rs.logger.Info().Str("device_id", deviceID).Msg("JWT refreshed successfully")
		return nil
	case <-time.After(10 * time.Second):
		rs.logger.Error().Str("device_id", deviceID).Msg("Refresh timeout")
		return errors.New("refresh timeout")
	case <-rs.ctx.Done():
		rs.logger.Warn().Msg("RegistrationService stopping during refresh")
		return errors.New("refresh service stopped")
	}
}

// Stop gracefully stops the service.
func (rs *RegistrationService) Stop() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.ctx == nil {
		return errors.New("registration service is not running")
	}

	rs.cancel()

	rs.ctx = nil
	rs.cancel = nil

	topics := []string{
		fmt.Sprintf("%s/response/%s", rs.pubTopic, rs.deviceInfo.GetDeviceID()),
		fmt.Sprintf("%s/refresh/response/%s", rs.pubTopic, rs.deviceInfo.GetDeviceID()),
	}
	for _, topic := range topics {
		token := rs.mqttClient.Unsubscribe(topic)
		token.Wait()
		if err := token.Error(); err != nil {
			rs.logger.Error().Err(err).Str("topic", topic).Msg("Failed to unsubscribe from MQTT topic")
			return err
		}
	}

	rs.logger.Info().Msg("RegistrationService stopped successfully")
	return nil
}
