package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/encryption"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/sirupsen/logrus"
)

// RegistrationService manages the device registration process
// and handles the publishing of registration requests and processing of responses.
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
	Logger            *logrus.Logger
}

// Start initiates the device registration process.
// If the device is already registered and its JWT is valid, it skips re-registration.
// Otherwise, it handles registration or re-registration if required.
func (rs *RegistrationService) Start() error {
	rs.Logger.Infof("Starting registration process for client: %s", rs.ClientID)

	// Check if the device is already registered
	existingDeviceID := rs.DeviceInfo.GetDeviceID()
	if existingDeviceID != "" {
		rs.Logger.Infof("Found existing device ID: %s", existingDeviceID)

		// Check if the JWT token is still valid
		valid, err := rs.JWTManager.IsJWTValid()
		if err != nil {
			rs.Logger.WithError(err).Error("Failed to check JWT token validity")
			return err
		}

		if !valid {
			rs.Logger.Warn("JWT token is invalid, attempting re-registration")
			payload := models.RegistrationPayload{
				DeviceID: existingDeviceID,
			}
			return rs.retryRegistration(payload)
		}

		rs.Logger.Infof("Device %s is already registered and JWT is valid", rs.ClientID)
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

// retryRegistration implements retries with exponential backoff for the registration process.
func (rs *RegistrationService) retryRegistration(payload models.RegistrationPayload) error {
	backoff := 1 * time.Second // Initial backoff duration
	maxBackoff := time.Duration(rs.MaxBackoffSeconds) * time.Second
	retryCount := 0

	for {
		retryCount++
		rs.Logger.Infof("Attempt %d: Registering device...", retryCount)

		err := rs.Register(payload)
		if err == nil {
			rs.Logger.Infof("Device registration succeeded after %d attempts", retryCount)
			return nil
		}

		rs.Logger.WithError(err).Warnf("Registration attempt %d failed, retrying after %s", retryCount, backoff)

		// Sleep for the backoff duration
		time.Sleep(backoff)

		// Exponential backoff with jitter
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
		backoff = backoff + time.Duration(rand.Intn(1000))*time.Millisecond // Add random jitter
	}
}

// Register handles the device registration by publishing a request to the MQTT broker
// and waits for a response from the broker to confirm the registration.
func (rs *RegistrationService) Register(payload models.RegistrationPayload) error {
	// Serialize the registration payload
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to serialize registration payload")
	}

	// Encrypt the serialized payload
	encryptedPayload, err := rs.EncryptionManager.Encrypt(payloadBytes)
	if err != nil {
		rs.Logger.WithError(err).Error("Failed to encrypt registration payload")
		return err
	}

	// Create a channel to receive the response
	responseChannel := make(chan string, 1)

	// Subscribe to the response topic before publishing the request
	var respTopic string
	if payload.DeviceID != "" {
		respTopic = rs.PubTopic + "/response/" + payload.DeviceID
	} else {
		respTopic = rs.PubTopic + "/response/" + rs.ClientID
	}
	rs.Logger.Infof("Subscribing to response topic: %s", respTopic)

	token := rs.MqttClient.Subscribe(respTopic, byte(rs.QOS), func(client MQTT.Client, msg MQTT.Message) {
		var response models.RegistrationResponse
		if err := json.Unmarshal(msg.Payload(), &response); err != nil {
			rs.Logger.WithError(err).Error("Error parsing registration response")
			return
		}

		// Validate the response and extract the device ID
		if response.DeviceID == "" {
			rs.Logger.Error("Device ID not found in the registration response")
			return
		}
		if response.JWTToken == "" {
			rs.Logger.Error("JWT token not found in the registration response")
			return
		}

		// Save the JWT token securely
		if err := rs.JWTManager.SaveJWT(response.JWTToken); err != nil {
			rs.Logger.WithError(err).Error("Failed to save JWT token")
			return
		}

		// Send the device ID to the response channel
		responseChannel <- response.DeviceID
	})

	token.Wait()
	if err := token.Error(); err != nil {
		rs.Logger.WithError(err).Error("Failed to subscribe to registration response topic")
		return err
	}

	// Publish the registration message to the MQTT broker
	rs.Logger.Infof("Publishing registration request to topic: %s", rs.PubTopic)
	publishToken := rs.MqttClient.Publish(rs.PubTopic, byte(rs.QOS), false, encryptedPayload)
	publishToken.Wait()
	if err := publishToken.Error(); err != nil {
		rs.Logger.WithError(err).Error("Failed to publish registration message")
		return err
	}

	// Wait for a response or timeout after 10 seconds
	select {
	case deviceID := <-responseChannel:
		rs.Logger.Infof("Device registered successfully with ID: %s", deviceID)
		if err := rs.DeviceInfo.SaveDeviceID(deviceID); err != nil {
			rs.Logger.WithError(err).Error("Failed to save device ID to file")
			return err
		}
	case <-time.After(10 * time.Second):
		rs.Logger.Errorf("Registration timeout for client: %s, no response received", rs.ClientID)
		return fmt.Errorf("registration timeout for client: %s", rs.ClientID)
	}

	return nil
}
