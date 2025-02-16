package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
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

// RegistrationService manages the device registration process.
type RegistrationService struct {
	PubTopic          string
	ClientID          string
	QOS               int
	DeviceInfo        identity.DeviceInfoInterface
	MqttClient        mqtt.MQTTClient
	FileClient        file.FileOperations
	JWTManager        jwt.JWTManagerInterface
	EncryptionManager encryption.EncryptionManagerInterface
	MaxBackoffSeconds int
	Logger            zerolog.Logger
	stopChan          chan struct{}
	running           bool
}

// Start initiates the device registration process.
func (rs *RegistrationService) Start() error {
	if rs.running {
		rs.Logger.Warn().Msg("RegistrationService is already running")
		return errors.New("registration service is already running")
	}

	rs.stopChan = make(chan struct{})
	rs.running = true

	rs.Logger.Info().Str("client_id", rs.ClientID).Msg("Starting registration process")

	existingDeviceID := rs.DeviceInfo.GetDeviceID()
	if existingDeviceID != "" {
		rs.Logger.Info().Str("device_id", existingDeviceID).Msg("Found existing device ID")

		// Check if the JWT token is valid
		valid, err := rs.JWTManager.IsJWTValid()
		if err != nil {
			rs.Logger.Error().Err(err).Msg("Failed to check JWT token validity")
			return err
		}

		if !valid {
			rs.Logger.Warn().Msg("JWT token is invalid, attempting re-registration")
			payload := models.RegistrationPayload{
				DeviceID: existingDeviceID,
			}
			return rs.retryRegistration(payload)
		}

		rs.Logger.Info().Str("client_id", rs.ClientID).Msg("Device is already registered and JWT is valid")
		return nil
	}

	// Prepare payload for initial registration
	payload := models.RegistrationPayload{
		ClientID: rs.ClientID,
		Name:     rs.DeviceInfo.GetDeviceIdentity().Name,
		OrgID:    rs.DeviceInfo.GetDeviceIdentity().OrgID,
		Metadata: rs.DeviceInfo.GetDeviceIdentity().Metadata,
	}

	// Register the device
	return rs.retryRegistration(payload)
}

// retryRegistration implements retries with exponential backoff for registration.
func (rs *RegistrationService) retryRegistration(payload models.RegistrationPayload) error {
	backoff := 1 * time.Second
	maxBackoff := time.Duration(rs.MaxBackoffSeconds) * time.Second
	retryCount := 0

	for {
		retryCount++
		rs.Logger.Info().Int("attempt", retryCount).Msg("Registering device")

		err := rs.Register(payload)
		if err == nil {
			rs.Logger.Info().Int("attempts", retryCount).Msg("Device registration succeeded")
			return nil
		}

		rs.Logger.Warn().Err(err).Int("attempt", retryCount).Dur("backoff", backoff).Msg("Registration failed, retrying")

		select {
		case <-time.After(backoff):
			// Exponential backoff with jitter
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			backoff = backoff + time.Duration(rand.Intn(1000))*time.Millisecond
		case <-rs.stopChan:
			rs.Logger.Warn().Msg("RegistrationService stopped during retry")
			return errors.New("registration service stopped")
		}
	}
}

// Register handles device registration by publishing a request and processing the response.
func (rs *RegistrationService) Register(payload models.RegistrationPayload) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to serialize registration payload")
	}

	// Encrypt payload
	encryptedPayload, err := rs.EncryptionManager.Encrypt(payloadBytes)
	if err != nil {
		rs.Logger.Error().Err(err).Msg("Failed to encrypt registration payload")
		return err
	}

	// Subscribe to response topic
	var respTopic string
	if payload.DeviceID != "" {
		respTopic = fmt.Sprintf("%s/response/%s", rs.PubTopic, payload.DeviceID)
	} else {
		respTopic = fmt.Sprintf("%s/response/%s", rs.PubTopic, rs.ClientID)
	}

	rs.Logger.Info().Str("topic", respTopic).Msg("Subscribing to response topic")

	responseChannel := make(chan string, 1)
	token := rs.MqttClient.Subscribe(respTopic, byte(rs.QOS), func(client MQTT.Client, msg MQTT.Message) {
		var response models.RegistrationResponse
		if err := json.Unmarshal(msg.Payload(), &response); err != nil {
			rs.Logger.Error().Err(err).Msg("Error parsing registration response")
			return
		}

		if response.DeviceID == "" {
			rs.Logger.Error().Msg("Device ID not found in the registration response")
			return
		}
		if response.JWTToken == "" {
			rs.Logger.Error().Msg("JWT token not found in the registration response")
			return
		}

		if err := rs.JWTManager.SaveJWT(response.JWTToken); err != nil {
			rs.Logger.Error().Err(err).Msg("Failed to save JWT token")
			return
		}

		responseChannel <- response.DeviceID
	})

	token.Wait()
	if err := token.Error(); err != nil {
		rs.Logger.Error().Err(err).Msg("Failed to subscribe to registration response topic")
		return err
	}

	// Publish the registration message
	rs.Logger.Info().Str("topic", rs.PubTopic).Msg("Publishing registration request")
	publishToken := rs.MqttClient.Publish(rs.PubTopic, byte(rs.QOS), false, encryptedPayload)
	publishToken.Wait()
	if err := publishToken.Error(); err != nil {
		rs.Logger.Error().Err(err).Msg("Failed to publish registration message")
		return err
	}

	// Wait for response with timeout
	select {
	case deviceID := <-responseChannel:
		rs.Logger.Info().Str("device_id", deviceID).Msg("Device registered successfully")
		if err := rs.DeviceInfo.SaveDeviceID(deviceID); err != nil {
			rs.Logger.Error().Err(err).Msg("Failed to save device ID")
			return err
		}
	case <-time.After(10 * time.Second):
		rs.Logger.Error().Str("client_id", rs.ClientID).Msg("Registration timeout, no response received")
		return fmt.Errorf("registration timeout for client: %s", rs.ClientID)
	}

	return nil
}

// Stop gracefully stops the registration process.
func (rs *RegistrationService) Stop() error {
	if !rs.running {
		rs.Logger.Warn().Msg("RegistrationService is not running")
		return errors.New("registration service is not running")
	}

	if rs.stopChan == nil {
		rs.Logger.Error().Msg("Failed to stop RegistrationService: stop channel is nil")
		return errors.New("stop channel is nil")
	}

	close(rs.stopChan)
	rs.running = false
	rs.Logger.Info().Msg("RegistrationService stopped")
	return nil
}
