package services

import (
	"encoding/json"
	"errors"
	"fmt"
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
type RegistrationService struct {
	PubTopic          string
	ClientID          string
	QOS               int
	DeviceInfo        identity.DeviceInfoInterface
	MqttClient        mqtt.MQTTClient
	FileClient        file.FileOperations
	JWTManager        jwt.JWTManagerInterface
	EncryptionManager encryption.EncryptionManagerInterface
	Logger            *logrus.Logger
}

// Start initiates the device registration process
func (rs *RegistrationService) Start() error {
	// Check if device ID is already present
	existingDeviceID := rs.DeviceInfo.GetDeviceID()
	if existingDeviceID != "" {
		expired, err := rs.JWTManager.IsJWTExpired()
		if err != nil {
			rs.Logger.WithError(err).Error("Failed to check JWT token expiry")
		}

		if expired {
			payload := models.RegistrationPayload{
				DeviceID: existingDeviceID,
			}

			if err := rs.Register(payload); err != nil {
				rs.Logger.WithError(err).Error("failed to refresh JWT token")
				return err
			}

			rs.Logger.Warnf("JWT token expired for device: %s. Re-registering...", rs.ClientID)
		} else {
			rs.Logger.Infof("Device %s already registered with ID: %s", rs.ClientID, existingDeviceID)
			return nil
		}
	}

	payload := models.RegistrationPayload{
		ClientID: rs.ClientID,
		Name:     rs.DeviceInfo.GetDeviceIdentity().Name,
		OrgID:    rs.DeviceInfo.GetDeviceIdentity().OrgID,
		Metadata: rs.DeviceInfo.GetDeviceIdentity().Metadata,
	}

	if err := rs.Register(payload); err != nil {
		rs.Logger.WithError(err).Error("Failed to register device")
		return err
	}

	return nil
}

func (rs *RegistrationService) Register(payload models.RegistrationPayload) error {
	// Serialize the registration payload
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to serialize registration payload")
	}

	// Encrypt the registration payload
	encryptedPayload, err := rs.EncryptionManager.Encrypt(payloadBytes)
	if err != nil {
		rs.Logger.WithError(err).Error("Failed to encrypt registration payload")
	}

	// Publish registration message to the broker
	token := rs.MqttClient.Publish(rs.PubTopic, byte(rs.QOS), false, encryptedPayload)
	token.Wait()
	if err := token.Error(); err != nil {
		rs.Logger.WithError(err).Error("failed to publish registration message")
		return err
	}

	responseChannel := make(chan string, 1)
	go rs.waitForRegistrationResponse(responseChannel)

	// Wait for the response or timeout
	select {
	case deviceID := <-responseChannel:
		rs.Logger.Infof("Device %s registered successfully with ID: %s", rs.ClientID, deviceID)
		if err := rs.DeviceInfo.SaveDeviceID(deviceID); err != nil {
			rs.Logger.WithError(err).Error("Failed to save device ID to file")
			return err
		}
	case <-time.After(10 * time.Second):
		rs.Logger.Errorf("Registration timeout for client: %s, no response received", rs.ClientID)
		return fmt.Errorf("registration timeout for client: %s, no response received", rs.ClientID)
	}

	return nil
}

// waitForRegistrationResponse listens for the device registration response
func (rs *RegistrationService) waitForRegistrationResponse(responseChannel chan<- string) {
	respTopic := rs.PubTopic + "/response/" + rs.ClientID
	rs.Logger.Infof("Listening for registration response on topic: %s", respTopic)

	// Subscribe to the unique response topic
	token := rs.MqttClient.Subscribe(respTopic, byte(rs.QOS), func(client MQTT.Client, msg MQTT.Message) {
		var response models.RegistrationResponse
		err := json.Unmarshal(msg.Payload(), &response)
		if err != nil {
			rs.Logger.WithError(err).Error("Error parsing registration response")
			return
		}

		// Extract device ID from the response
		deviceID := response.DeviceID
		if deviceID == "" {
			rs.Logger.Error("Device ID not found in the registration response")
			return
		}

		// Extract JWT token from the response
		jwtToken := response.JWTToken
		if jwtToken == "" {
			rs.Logger.Error("JWT token not found in the registration response")
			return
		}

		// Save the JWT token securely
		if err := rs.JWTManager.SaveJWT(jwtToken); err != nil {
			rs.Logger.WithError(err).Error("Failed to save JWT token")
			return
		}

		// Send the device ID to the response channel
		responseChannel <- deviceID
	})

	token.Wait()
	if err := token.Error(); err != nil {
		rs.Logger.WithError(err).Error("Failed to subscribe to registration response topic")
		return
	}
}
