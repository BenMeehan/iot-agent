package services

import (
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

	stopChan chan struct{}
	running  bool
	mu       sync.Mutex
}

// NewRegistrationService initializes and returns a new RegistrationService.
func NewRegistrationService(pubTopic string, clientID string, qos int, deviceInfo identity.DeviceInfoInterface,
	mqttClient mqtt.MQTTClient, fileClient file.FileOperations, jwtManager jwt.JWTManagerInterface,
	encryptionManager encryption.EncryptionManagerInterface, maxBackoffSeconds int, logger zerolog.Logger) *RegistrationService {

	return &RegistrationService{
		PubTopic:          pubTopic,
		ClientID:          clientID,
		QOS:               qos,
		DeviceInfo:        deviceInfo,
		MqttClient:        mqttClient,
		FileClient:        fileClient,
		JWTManager:        jwtManager,
		EncryptionManager: encryptionManager,
		MaxBackoffSeconds: maxBackoffSeconds,
		Logger:            logger,
		stopChan:          make(chan struct{}),
	}
}

// Start initiates the device registration process.
func (rs *RegistrationService) Start() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.running {
		rs.Logger.Warn().Msg("RegistrationService is already running")
		return errors.New("registration service is already running")
	}

	rs.running = true
	rs.Logger.Info().Str("client_id", rs.ClientID).Msg("Starting registration process")

	existingDeviceID := rs.DeviceInfo.GetDeviceID()
	if existingDeviceID != "" {
		rs.Logger.Info().Str("device_id", existingDeviceID).Msg("Found existing device ID")

		valid, err := rs.JWTManager.IsJWTValid()
		if err != nil {
			rs.Logger.Error().Err(err).Msg("Failed to check JWT token validity")
			return err
		}

		if !valid {
			rs.Logger.Warn().Msg("JWT token is invalid, attempting re-registration")
			payload := models.RegistrationPayload{DeviceID: existingDeviceID}
			return rs.retryRegistration(payload)
		}

		rs.Logger.Info().Msg("Device is already registered and JWT is valid")
		return nil
	}

	payload := models.RegistrationPayload{
		ClientID: rs.ClientID,
		Name:     rs.DeviceInfo.GetDeviceIdentity().Name,
		OrgID:    rs.DeviceInfo.GetDeviceIdentity().OrgID,
		Metadata: rs.DeviceInfo.GetDeviceIdentity().Metadata,
	}

	return rs.retryRegistration(payload)
}

// retryRegistration implements retries with controlled exponential backoff.
func (rs *RegistrationService) retryRegistration(payload models.RegistrationPayload) error {
	backoff := 1 * time.Second
	maxBackoff := time.Duration(rs.MaxBackoffSeconds) * time.Second
	retryCount := 0

	// Precompute the payload once to avoid redundant processing
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to serialize registration payload")
	}

	encryptedPayload, err := rs.EncryptionManager.Encrypt(payloadBytes)
	if err != nil {
		rs.Logger.Error().Err(err).Msg("Failed to encrypt registration payload")
		return err
	}

	for {
		retryCount++
		rs.Logger.Info().Int("attempt", retryCount).Msg("Registering device")

		err := rs.Register(encryptedPayload, payload.DeviceID)
		if err == nil {
			rs.Logger.Info().Int("attempts", retryCount).Msg("Device registration succeeded")
			return nil
		}

		rs.Logger.Warn().Err(err).Int("attempt", retryCount).Dur("backoff", backoff).Msg("Registration failed, retrying")

		select {
		case <-time.After(backoff):
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff))) // Cap max backoff
			backoff += time.Duration(rand.Intn(500)) * time.Millisecond                // Jitter
		case <-rs.stopChan:
			rs.Logger.Warn().Msg("RegistrationService stopped during retry")
			return errors.New("registration service stopped")
		}
	}
}

// Register handles device registration and response processing.
func (rs *RegistrationService) Register(encryptedPayload []byte, deviceID string) error {
	respTopic := fmt.Sprintf("%s/response/%s", rs.PubTopic, rs.ClientID)
	if deviceID != "" {
		respTopic = fmt.Sprintf("%s/response/%s", rs.PubTopic, deviceID)
	}

	rs.Logger.Info().Str("topic", respTopic).Msg("Subscribing to response topic")

	responseChannel := make(chan string, 1)
	defer close(responseChannel)

	// Subscribe asynchronously to avoid blocking
	rs.MqttClient.Subscribe(respTopic, byte(rs.QOS), func(client MQTT.Client, msg MQTT.Message) {
		var response models.RegistrationResponse
		if err := json.Unmarshal(msg.Payload(), &response); err != nil {
			rs.Logger.Error().Err(err).Msg("Error parsing registration response")
			return
		}

		if response.DeviceID == "" || response.JWTToken == "" {
			rs.Logger.Error().Msg("Invalid registration response")
			return
		}

		if err := rs.JWTManager.SaveJWT(response.JWTToken); err != nil {
			rs.Logger.Error().Err(err).Msg("Failed to save JWT token")
			return
		}

		responseChannel <- response.DeviceID
	})

	rs.Logger.Info().Str("topic", rs.PubTopic).Msg("Publishing registration request")
	publishToken := rs.MqttClient.Publish(rs.PubTopic, byte(rs.QOS), false, encryptedPayload)
	publishToken.Wait()
	if err := publishToken.Error(); err != nil {
		rs.Logger.Error().Err(err).Msg("Failed to publish registration message")
		return err
	}

	select {
	case deviceID := <-responseChannel:
		rs.Logger.Info().Str("device_id", deviceID).Msg("Device registered successfully")
		return rs.DeviceInfo.SaveDeviceID(deviceID)
	case <-time.After(10 * time.Second):
		rs.Logger.Error().Str("client_id", rs.ClientID).Msg("Registration timeout")
		return errors.New("registration timeout")
	}
}

// Stop gracefully stops the registration service.
func (rs *RegistrationService) Stop() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if !rs.running {
		return errors.New("registration service is not running")
	}

	close(rs.stopChan)
	rs.running = false
	rs.Logger.Info().Msg("RegistrationService stopped")
	return nil
}
