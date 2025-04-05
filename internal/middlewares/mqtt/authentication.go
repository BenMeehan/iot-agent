package mqtt_middleware

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	mqttLib "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
)

// MQTTAuthMiddleware defines a generic MQTT middleware contract.
type MQTTAuthMiddleware interface {
	Init(authenticationCertificatePath string) error
	Publish(topic string, qos byte, retained bool, payload interface{}) error
	Subscribe(topic string, qos byte, callback mqttLib.MessageHandler) error
	Unsubscribe(topics ...string) error
}

// MQTTAuthenticationMiddleware handles authentication for MQTT operations.
type MQTTAuthenticationMiddleware struct {
	// Configuration
	authTopic                 string // MQTT topic for authentication requests
	qos                       int    // Quality of Service level for MQTT messages
	authenticationCertificate []byte // Certificate used for initial authentication
	clientID                  string // Unique identifier for the MQTT client

	// Dependencies
	deviceInfo identity.DeviceInfoInterface // Interface for retrieving device information
	mqttClient mqtt.MQTTClient              // MQTT client instance
	jwtManager jwt.JWTManagerInterface      // Manages JWT tokens
	fileClient file.FileOperations          // Handles file operations
	logger     zerolog.Logger               // Logger for tracking events and errors

	// Retry settings
	retryDelay         time.Duration // Base delay between retry attempts
	requestWaitingTime time.Duration // Timeout duration for waiting on JWT responses

	// Synchronization
	jwtMutex      sync.Mutex // Protects JWT refresh operations from concurrent access
	refreshingJWT bool       // Tracks if a JWT refresh is currently in progress
}

// NewMQTTAuthenticationMiddleware initializes and returns a new instance of MQTTAuthenticationMiddleware.
func NewMQTTAuthenticationMiddleware(
	authTopic string,
	qos int,
	clientID string,
	deviceInfo identity.DeviceInfoInterface,
	mqttClient mqtt.MQTTClient,
	jwtManager jwt.JWTManagerInterface,
	fileClient file.FileOperations,
	logger zerolog.Logger,
	retryDelay time.Duration,
	requestWaitingTime time.Duration,
) *MQTTAuthenticationMiddleware {
	return &MQTTAuthenticationMiddleware{
		authTopic:          authTopic,
		qos:                qos,
		clientID:           clientID,
		retryDelay:         time.Duration(retryDelay) * time.Second,
		requestWaitingTime: time.Duration(requestWaitingTime) * time.Second,
		deviceInfo:         deviceInfo,
		mqttClient:         mqttClient,
		jwtManager:         jwtManager,
		fileClient:         fileClient,
		logger:             logger,
		refreshingJWT:      false,
	}
}

// Init sets up the middleware by loading tokens and ensuring a valid JWT.
func (m *MQTTAuthenticationMiddleware) Init(authenticationCertificatePath string) error {
	// Attempt to load existing JWT tokens
	if err := m.jwtManager.LoadTokens(); err != nil {
		m.logger.Error().Err(err).Msg("Failed to load existing JWT tokens")
	}

	// Read the authentication certificate from the specified path
	certificate, err := m.fileClient.ReadFileRaw(authenticationCertificatePath)
	if err != nil {
		return fmt.Errorf("failed to load authentication certificate: %w", err)
	}
	m.authenticationCertificate = certificate

	// Validate the current JWT
	isValid, err := m.jwtManager.IsJWTValid()
	if err != nil {
		return fmt.Errorf("failed to validate JWT: %w", err)
	}

	// If no valid JWT exists, request one using the certificate
	if !isValid {
		m.logger.Info().Msg("No valid JWT found or JWT invalid, requesting initial authentication")
		if err := m.requestJWTWithCertificate(); err != nil {
			return fmt.Errorf("initial JWT request failed: %w", err)
		}
	}
	return nil
}

// onAuthMessage handles authentication responses from the MQTT broker.
func (m *MQTTAuthenticationMiddleware) onAuthMessage(msg mqttLib.Message) error {
	m.logger.Info().Str("topic", msg.Topic()).Msg("Received authentication response")

	// Parse the authentication response
	var authResponse models.AuthResponse
	if err := json.Unmarshal(msg.Payload(), &authResponse); err != nil {
		m.logger.Error().Err(err).Msg("Failed to parse authentication response")
		return err
	}

	// Save the received tokens
	if err := m.jwtManager.SaveTokens(authResponse.AccessToken, authResponse.RefreshToken, authResponse.ExpiresIn); err != nil {
		m.logger.Error().Err(err).Msg("Failed to save tokens")
		return err
	}
	m.logger.Info().Msg("JWT and refresh tokens successfully stored")
	return nil
}

// waitForJWTResponse waits for a JWT response with a timeout and unsubscribes afterward.
func (m *MQTTAuthenticationMiddleware) waitForJWTResponse(jwtChan chan error, responseTopic string) error {
	defer func() {
		// Clean up by unsubscribing from the response topic
		if err := m.Unsubscribe(responseTopic); err != nil {
			m.logger.Warn().Err(err).Msgf("Failed to unsubscribe from topic: %s", responseTopic)
		}
	}()

	// Wait for response or timeout
	select {
	case <-time.After(60 * time.Second):
		return errors.New("authentication response timeout")
	case err := <-jwtChan:
		if err != nil {
			return fmt.Errorf("authentication response error: %w", err)
		}
		return nil
	}
}

// requestJWTWithCertificate requests a JWT using the authentication certificate with infinite retries.
func (m *MQTTAuthenticationMiddleware) requestJWTWithCertificate() error {
	attempt := 0
	for {
		attempt++
		m.logger.Info().Int("attempt", attempt).Msg("Attempting to request JWT with certificate")

		err := func() error {
			// Prepare the authentication request payload
			payload := models.AuthRequest{}
			var identifier string
			if identifier = m.deviceInfo.GetDeviceID(); identifier != "" {
				payload.DeviceID = identifier
			} else {
				payload.ClientID = m.clientID
				identifier = m.clientID
			}
			payload.Key = m.authenticationCertificate

			// Serialize the payload
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return errors.New("failed to marshal authentication request")
			}

			// Set up response handling
			jwtChan := make(chan error, 1)
			handler := func(client mqttLib.Client, msg mqttLib.Message) {
				jwtChan <- m.onAuthMessage(msg)
			}
			authResponseTopic := fmt.Sprintf("%s/response/%s", m.authTopic, identifier)
			if err := m.Subscribe(authResponseTopic, byte(m.qos), handler); err != nil {
				return fmt.Errorf("failed to subscribe for auth response: %w", err)
			}

			// Publish the authentication request
			m.logger.Info().Str("topic", m.authTopic).Msg("Publishing authentication request with certificate")
			token := m.mqttClient.Publish(m.authTopic, byte(m.qos), false, payloadBytes)
			token.Wait()
			if token.Error() != nil {
				return fmt.Errorf("failed to publish authentication request: %w", token.Error())
			}

			// Wait for the response
			return m.waitForJWTResponse(jwtChan, authResponseTopic)
		}()

		if err == nil {
			m.logger.Info().Int("attempt", attempt).Msg("Successfully obtained JWT with certificate")
			return nil
		}

		// Apply jitter to retry delay to prevent thundering herd problem
		jitter := time.Duration(rand.Int63n(int64(m.retryDelay) / 10))
		totalDelay := m.retryDelay + jitter

		m.logger.Warn().
			Int("attempt", attempt).
			Dur("retry_delay_ms", totalDelay).
			Err(err).
			Msg("Failed to obtain JWT, retrying after delay")
		time.Sleep(totalDelay)
	}
}

// refreshJWT requests a new JWT token using either refresh token or certificate with infinite retries.
func (m *MQTTAuthenticationMiddleware) refreshJWT() error {
	// Prevent concurrent JWT refreshes
	m.jwtMutex.Lock()
	if m.refreshingJWT {
		m.jwtMutex.Unlock()
		return errors.New("JWT refresh already in progress, try again later")
	}
	m.refreshingJWT = true
	m.jwtMutex.Unlock()

	// Reset the refresh flag when done
	defer func() {
		m.jwtMutex.Lock()
		m.refreshingJWT = false
		m.jwtMutex.Unlock()
	}()

	// Check if refresh token is still valid
	refreshToken, expiresIn := m.jwtManager.GetRefreshToken()
	isValid := time.Now().Before(time.Now().Add(time.Duration(expiresIn) * time.Second))

	if isValid {
		attempt := 0
		for {
			attempt++
			m.logger.Info().Int("attempt", attempt).Msg("Attempting to refresh JWT with refresh token")

			err := func() error {
				// Prepare refresh request payload
				authRequestPayload := models.AuthRequest{
					DeviceID:     m.deviceInfo.GetDeviceID(),
					RefreshToken: refreshToken,
				}
				payloadBytes, err := json.Marshal(authRequestPayload)
				if err != nil {
					return fmt.Errorf("failed to serialize refresh request: %w", err)
				}

				// Set up response handling
				jwtChan := make(chan error, 1)
				handler := func(client mqttLib.Client, msg mqttLib.Message) {
					jwtChan <- m.onAuthMessage(msg)
				}
				authResponseTopic := fmt.Sprintf("%s/response/%s", m.authTopic, m.deviceInfo.GetDeviceID())
				if err := m.Subscribe(authResponseTopic, byte(m.qos), handler); err != nil {
					return fmt.Errorf("failed to subscribe for refresh response: %w", err)
				}

				// Publish the refresh request
				m.logger.Info().Str("topic", m.authTopic).Msg("Publishing token refresh request")
				token := m.mqttClient.Publish(m.authTopic, byte(m.qos), false, payloadBytes)
				token.Wait()
				if token.Error() != nil {
					return fmt.Errorf("failed to publish refresh request: %w", token.Error())
				}

				// Wait for the response
				return m.waitForJWTResponse(jwtChan, authResponseTopic)
			}()

			if err == nil {
				m.logger.Info().Int("attempt", attempt).Msg("JWT successfully refreshed with refresh token")
				return nil
			}

			m.logger.Warn().
				Int("attempt", attempt).
				Dur("retry_delay_ms", m.retryDelay).
				Err(err).
				Msg("Failed to refresh JWT with refresh token, retrying after delay")
			time.Sleep(m.retryDelay)
		}
	}

	// Fallback to certificate if refresh token is invalid
	m.logger.Info().Msg("No valid refresh token or refresh failed, falling back to certificate")
	return m.requestJWTWithCertificate()
}

// validateJWT ensures a valid JWT is available before performing actions.
func (m *MQTTAuthenticationMiddleware) validateJWT() (string, error) {
	token := m.jwtManager.GetJWT()

	// Check if the current JWT is still valid
	isJWTValid, err := m.jwtManager.CheckExpiration(token, constants.AccessToken)
	if err != nil {
		return "", fmt.Errorf("failed to validate JWT: %w", err)
	}

	// Refresh JWT if it’s expired
	if !isJWTValid {
		m.logger.Info().Msg("JWT is expired, attempting refresh")
		if err := m.refreshJWT(); err != nil {
			return "", fmt.Errorf("failed to refresh JWT: %w", err)
		}
		token = m.jwtManager.GetJWT()
	}

	m.logger.Debug().Str("token", token).Msg("JWT validated successfully")
	return token, nil
}

// Publish wraps any payload with a JWT and sends it via MQTT.
func (m *MQTTAuthenticationMiddleware) Publish(topic string, qos byte, retained bool, payload interface{}) error {
	// Validate and retrieve a valid JWT
	jwtToken, err := m.validateJWT()
	if err != nil {
		return fmt.Errorf("failed JWT validation: %w", err)
	}

	// Wrap the payload with the JWT
	wrappedPayload := models.WrappedPayload{
		JWT:     jwtToken,
		Payload: payload,
	}
	payloadBytes, err := json.Marshal(wrappedPayload)
	if err != nil {
		m.logger.Error().Err(err).Msg("Failed to serialize wrapped payload")
		return fmt.Errorf("failed to serialize wrapped payload: %w", err)
	}

	// Publish the message
	m.logger.Debug().Str("topic", topic).Msg("Publishing message with JWT")
	token := m.mqttClient.Publish(topic, qos, retained, payloadBytes)
	token.Wait()
	if token.Error() != nil {
		m.logger.Error().Err(token.Error()).Msg("Failed to publish MQTT message")
		return token.Error()
	}

	m.logger.Debug().Str("topic", topic).Msg("Message published successfully")
	return nil
}

// Subscribe subscribes to a topic with the provided callback function.
func (m *MQTTAuthenticationMiddleware) Subscribe(topic string, qos byte, callback mqttLib.MessageHandler) error {
	// Subscribe to the specified topic
	token := m.mqttClient.Subscribe(topic, qos, callback)
	token.Wait()
	if token.Error() != nil {
		m.logger.Error().Err(token.Error()).Msg("Failed to subscribe to MQTT topic")
		return token.Error()
	}

	m.logger.Debug().Str("topic", topic).Msg("Subscribed to topic successfully")
	return nil
}

// Unsubscribe unsubscribes from the specified topics.
func (m *MQTTAuthenticationMiddleware) Unsubscribe(topics ...string) error {
	// Unsubscribe from the specified topics
	token := m.mqttClient.Unsubscribe(topics...)
	token.Wait()
	if token.Error() != nil {
		m.logger.Error().Err(token.Error()).Msg("Failed to unsubscribe from MQTT topics")
		return token.Error()
	}

	m.logger.Debug().Strs("topics", topics).Msg("Unsubscribed from topics successfully")
	return nil
}
