package services

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"

	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/sirupsen/logrus"
)

// RegistrationService handles the device registration process
type RegistrationService struct {
	PubTopic         string
	DeviceSecretFile string
	ClientID         string
	QOS              int
	DeviceInfo       identity.DeviceInfo
}

// Start begins the device registration process
func (rs *RegistrationService) Start() error {
	// Check if device ID is already present
	existingDeviceID, err := rs.DeviceInfo.GetDeviceID()
	if err == nil && existingDeviceID != "" {
		logrus.Infof("Device %s already registered with ID: %s", rs.ClientID, existingDeviceID)
		return nil
	}

	// Read the device secret from the file
	deviceSecret, err := rs.readDeviceSecret()
	if err != nil {
		return err
	}

	client := mqtt.Client()
	logrus.Infof("Starting device registration for client: %s", rs.ClientID)

	// Create registration payload
	payload := map[string]string{
		"client_id":     rs.ClientID,
		"device_secret": deviceSecret,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to serialize registration payload")
	}

	// Publish registration message to the broker
	if err := client.Publish(rs.PubTopic, byte(rs.QOS), false, payloadBytes); err != nil {
		return errors.New("failed to publish registration message")
	}

	// Use a channel to wait for the response
	responseChannel := make(chan string, 1)
	go rs.waitForRegistrationResponse(responseChannel)

	// Wait for the response or timeout
	select {
	case deviceID := <-responseChannel:
		logrus.Infof("Device %s registered successfully with ID: %s", rs.ClientID, deviceID)
		if err := rs.DeviceInfo.SaveDeviceID(deviceID); err != nil {
			logrus.WithError(err).Error("Failed to save device ID to file")
		}
	case <-time.After(10 * time.Second):
		logrus.Errorf("Registration timeout for client: %s, no response received", rs.ClientID)
	}

	return nil
}

// waitForRegistrationResponse listens for a device-specific response
func (rs *RegistrationService) waitForRegistrationResponse(responseChannel chan<- string) {
	client := mqtt.Client()
	respTopic := rs.PubTopic + "/response/" + rs.ClientID
	logrus.Infof("Listening for registration response on topic: %s", respTopic)

	// Subscribe to the unique response topic
	client.Subscribe(respTopic, byte(rs.QOS), func(client MQTT.Client, msg MQTT.Message) {
		var response map[string]string
		err := json.Unmarshal(msg.Payload(), &response)
		if err != nil {
			logrus.WithError(err).Error("Error parsing registration response")
			return
		}

		deviceID, exists := response["device_id"]
		if !exists {
			logrus.Error("Device ID not found in the registration response")
			return
		}

		// Send the device ID to the response channel
		responseChannel <- deviceID
	})
}

// readDeviceSecret reads the device secret from the DeviceSecretFile
func (rs *RegistrationService) readDeviceSecret() (string, error) {
	data, err := os.ReadFile(rs.DeviceSecretFile)
	if err != nil {
		logrus.WithError(err).Error("Failed to read device secret file")
		return "", errors.New("failed to read device secret file")
	}

	// Trim any extraneous whitespace characters (like newlines)
	secret := strings.TrimSpace(string(data))
	return secret, nil
}
