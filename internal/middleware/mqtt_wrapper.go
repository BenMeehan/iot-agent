package middleware

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
)

// MQTTPayload defines the structure for the MQTT message.
type MQTTPayload struct {
	JWT     string      `json:"jwt"`
	Payload interface{} `json:"payload"`
}

// MQTTMiddleware acts as a bridge between the application and MQTT.
type MQTTMiddleware struct {
	mqttClient          mqtt.MQTTClient
	jwtManager          jwt.JWTManagerInterface
	registrationService *services.RegistrationService
	deviceInfo          identity.DeviceInfoInterface
	mu                  sync.Mutex
	logger              zerolog.Logger
}

var _ mqtt.Wrapper = (mqtt.Wrapper)(nil)

// NewMQTTMiddleware initializes the middleware.
func NewMQTTMiddleware(mqttClient mqtt.MQTTClient, jwtManager jwt.JWTManagerInterface, registrationService *services.RegistrationService, deviceInfo identity.DeviceInfoInterface, logger zerolog.Logger) *MQTTMiddleware {
	return &MQTTMiddleware{
		mqttClient:          mqttClient,
		jwtManager:          jwtManager,
		registrationService: registrationService,
		deviceInfo:          deviceInfo,
		logger:              logger,
	}
}

// MQTTPublish publishes a message to an MQTT topic, ensuring a valid JWT.
func (m *MQTTMiddleware) MQTTPublish(topic string, qos byte, retained bool, payload interface{}) (MQTT.Token, error) {
	// Ensure JWT is valid
	jwtToken, err := m.validateOrRefreshJWT()
	if err != nil {
		return nil, fmt.Errorf("failed to validate or refresh JWT: %v", err)
	}

	// Wrap payload with JWT
	wrappedPayload := MQTTPayload{
		JWT:     jwtToken,
		Payload: payload,
	}

	// Serialize to JSON
	payloadBytes, err := json.Marshal(wrappedPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize payload: %v", err)
	}

	// Publish to MQTT
	return m.mqttClient.Publish(topic, qos, retained, payloadBytes), nil
}

// validateOrRefreshJWT checks if JWT is valid, else refreshes/registers a new one.
func (m *MQTTMiddleware) validateOrRefreshJWT() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get current JWT
	jwtToken := m.jwtManager.GetJWT()

	// Check if JWT is valid
	if len(jwtToken) > 0 {
		return jwtToken, nil
	}

	m.logger.Info().Msg("JWT is invalid or expired. Refreshing...")

	// Try refreshing the token
	err := m.registrationService.RefreshToken()
	if err != nil {
		log.Println("Failed to refresh JWT, attempting registration...")

		// If refresh fails, attempt new registration
		payload := models.RegistrationPayload{DeviceID: m.deviceInfo.GetDeviceID()}
		err = m.registrationService.RetryRegistration(payload)
		if err != nil {
			return "", fmt.Errorf("failed to refresh or register JWT: %v", err)
		}
	}

	// Get new JWT
	newToken := m.jwtManager.GetJWT()
	if len(newToken) == 0 {
		return "", fmt.Errorf("failed to get new JWT")
	}

	return newToken, nil
}

// MQTTSubscribe subscribes to an MQTT topic with a given QoS level and a callback function.
func (m *MQTTMiddleware) MQTTSubscribe(topic string, qos byte, callback MQTT.MessageHandler) MQTT.Token {
	return m.mqttClient.Subscribe(topic, qos, callback)
}

// MQTTUnsubscribe unsubscribes from an MQTT topic.
func (m *MQTTMiddleware) MQTTUnsubscribe(topics ...string) MQTT.Token {
	return m.mqttClient.Unsubscribe(topics...)
}
