package mqtt_middleware

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	mqttLib "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
)

// AuthResponse represents the expected structure of the authentication response.
type AuthResponse struct {
	JWT          string `json:"jwt"`
	RefreshToken string `json:"refresh_token"`
}

// WrappedPayload represents the final structure sent over MQTT.
type WrappedPayload struct {
	JWT     string      `json:"jwt"`
	Payload interface{} `json:"payload"`
}

// MQTTAuthenticationMiddleware handles authentication for MQTT operations.
type MQTTAuthenticationMiddleware struct {
	// Configuration
	authTopic                 string
	qos                       int
	authenticationCertificate []byte

	// Dependencies
	mqttClient mqtt.MQTTClient
	jwtManager jwt.JWTManagerInterface
	fileClient file.FileOperations
	logger     zerolog.Logger

	// Retry settings
	retryDelay time.Duration

	// Synchronization
	jwtMutex      sync.Mutex // Protects JWT refresh
	refreshingJWT bool       // Tracks if a refresh is in progress
}

// NewMQTTAuthenticationMiddleware initializes and returns a new instance.
func NewMQTTAuthenticationMiddleware(
	authTopic string,
	qos int,
	mqttClient mqtt.MQTTClient,
	jwtManager jwt.JWTManagerInterface,
	fileClient file.FileOperations,
	logger zerolog.Logger,
	retryDelay int,
) *MQTTAuthenticationMiddleware {
	return &MQTTAuthenticationMiddleware{
		authTopic:     authTopic,
		qos:           qos,
		retryDelay:    time.Duration(retryDelay) * time.Second,
		mqttClient:    mqttClient,
		jwtManager:    jwtManager,
		fileClient:    fileClient,
		logger:        logger,
		refreshingJWT: false,
	}
}

// Init sets up the middleware by loading tokens and ensuring a valid JWT.
func (m *MQTTAuthenticationMiddleware) Init(authenticationCertificatePath string) error {
	if err := m.jwtManager.LoadTokens(); err != nil {
		m.logger.Error().Err(err).Msg("Failed to load existing JWT tokens")
	}
	certificate, err := m.fileClient.ReadFileRaw(authenticationCertificatePath)
	if err != nil {
		return fmt.Errorf("failed to load authentication certificate: %w", err)
	}
	m.authenticationCertificate = certificate

	isValid, err := m.jwtManager.IsJWTValid()

	if err != nil {
		return fmt.Errorf("failed to validate JWT: %w", err)
	}

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
	var authResponse AuthResponse
	if err := json.Unmarshal(msg.Payload(), &authResponse); err != nil {
		m.logger.Error().Err(err).Msg("Failed to parse authentication response")
		return err
	}
	if err := m.jwtManager.SaveTokens(authResponse.JWT, authResponse.RefreshToken); err != nil {
		m.logger.Error().Err(err).Msg("Failed to save tokens")
		return err
	}
	m.logger.Info().Msg("JWT and refresh tokens successfully stored")
	return nil
}

// waitForJWTResponse waits for a JWT response with a timeout.
func (m *MQTTAuthenticationMiddleware) waitForJWTResponse(jwtChan chan error) error {
	select {
	case <-time.After(10 * time.Second):
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
			payloadBytes := m.authenticationCertificate
			jwtChan := make(chan error, 1)
			handler := func(client mqttLib.Client, msg mqttLib.Message) {
				jwtChan <- m.onAuthMessage(msg)
			}
			authResponseTopic := fmt.Sprintf("%s/response", m.authTopic)
			if err := m.Subscribe(authResponseTopic, byte(m.qos), handler); err != nil {
				return fmt.Errorf("failed to subscribe for auth response: %w", err)
			}
			defer m.Unsubscribe(authResponseTopic)
			m.logger.Info().Str("topic", m.authTopic).Msg("Publishing authentication request with certificate")
			token := m.mqttClient.Publish(m.authTopic, byte(m.qos), false, payloadBytes)
			token.Wait()
			if token.Error() != nil {
				return fmt.Errorf("failed to publish authentication request: %w", token.Error())
			}
			return m.waitForJWTResponse(jwtChan)
		}()

		if err == nil {
			m.logger.Info().Int("attempt", attempt).Msg("Successfully obtained JWT with certificate")
			return nil
		}

		jitter := time.Duration(rand.Int63n(int64(m.retryDelay) / 10))
		totalDelay := m.retryDelay + jitter

		m.logger.Warn().
			Int("attempt", attempt).
			Dur("retry_delay_ms", m.retryDelay).
			Err(err).
			Msg("Failed to obtain JWT, retrying after delay")
		time.Sleep(totalDelay)
	}
}

// refreshJWT requests a new JWT token using either refresh token or certificate with infinite retries.
func (m *MQTTAuthenticationMiddleware) refreshJWT() error {
	m.jwtMutex.Lock()
	if m.refreshingJWT {
		m.jwtMutex.Unlock()
		return errors.New("JWT refresh already in progress, try again later")
	}
	m.refreshingJWT = true
	m.jwtMutex.Unlock()

	// Ensure we reset the flag when done
	defer func() {
		m.jwtMutex.Lock()
		m.refreshingJWT = false
		m.jwtMutex.Unlock()
	}()

	refreshToken := m.jwtManager.GetRefreshToken()

	isValid, err := m.jwtManager.CheckExpiration(refreshToken, constants.RefreshToken)
	if err != nil {
		return fmt.Errorf("failed to validate refresh token: %w", err)
	}

	if isValid {
		attempt := 0
		for {
			attempt++
			m.logger.Info().Int("attempt", attempt).Msg("Attempting to refresh JWT with refresh token")
			err := func() error {
				authRequestPayload := map[string]string{
					"request":      "refresh_token",
					"refreshToken": refreshToken,
				}
				payloadBytes, err := json.Marshal(authRequestPayload)
				if err != nil {
					return fmt.Errorf("failed to serialize refresh request: %w", err)
				}
				jwtChan := make(chan error, 1)
				handler := func(client mqttLib.Client, msg mqttLib.Message) {
					jwtChan <- m.onAuthMessage(msg)
				}
				authResponseTopic := fmt.Sprintf("%s/response", m.authTopic)
				if err := m.Subscribe(authResponseTopic, byte(m.qos), handler); err != nil {
					return fmt.Errorf("failed to subscribe for refresh response: %w", err)
				}
				defer m.Unsubscribe(authResponseTopic)
				m.logger.Info().Str("topic", m.authTopic).Msg("Publishing token refresh request")
				token := m.mqttClient.Publish(m.authTopic, byte(m.qos), false, payloadBytes)
				token.Wait()
				if token.Error() != nil {
					return fmt.Errorf("failed to publish refresh request: %w", token.Error())
				}
				return m.waitForJWTResponse(jwtChan)
			}()

			if err == nil {
				m.logger.Info().Int("attempt", attempt).Msg("JWT successfully refreshed with refresh token")
				return nil
			}
			m.logger.Warn().
				Int("attempt", attempt).
				Dur("retry_delay_ms", m.retryDelay/time.Second).
				Err(err).
				Msg("Failed to refresh JWT, retrying after delay")
			time.Sleep(m.retryDelay)
		}
	}
	m.logger.Info().Msg("No valid refresh token or refresh failed, using certificate")
	return m.requestJWTWithCertificate()
}

// validateJWT ensures a valid JWT is available before performing actions.
func (m *MQTTAuthenticationMiddleware) validateJWT() (string, error) {
	token := m.jwtManager.GetJWT()

	isJWTValid, err := m.jwtManager.CheckExpiration(token, constants.AccessToken)
	if err != nil {
		return "", fmt.Errorf("failed to validate JWT: %w", err)
	}

	if !isJWTValid {
		m.logger.Info().Err(err).Msg("JWT is expired, attempting refresh")
		if err := m.refreshJWT(); err != nil {
			return "", fmt.Errorf("failed to refresh JWT: %w", err)
		}
		token = m.jwtManager.GetJWT()
	}
	m.logger.Debug().Str("token", token).Msg("JWT validated successfully")
	return token, nil
}

// PublishWithJWT wraps any payload with a JWT and sends it via MQTT.
func (m *MQTTAuthenticationMiddleware) Publish(topic string, qos byte, retained bool, payload interface{}) error {
	jwtToken, err := m.validateJWT()
	if err != nil {
		return fmt.Errorf("failed JWT validation: %w", err)
	}
	wrappedPayload := WrappedPayload{
		JWT:     jwtToken,
		Payload: payload,
	}
	payloadBytes, err := json.Marshal(wrappedPayload)
	if err != nil {
		m.logger.Error().Err(err).Msg("Failed to serialize wrapped payload")
		return fmt.Errorf("failed to serialize wrapped payload: %w", err)
	}
	m.logger.Info().Str("topic", topic).Msg("Publishing message with JWT")
	token := m.mqttClient.Publish(topic, qos, retained, payloadBytes)
	token.Wait()
	if token.Error() != nil {
		m.logger.Error().Err(token.Error()).Msg("MQTT Publish failed")
		return token.Error()
	}
	m.logger.Info().Str("topic", topic).Msg("Message published successfully")
	return nil
}

// Subscribe subscribes to a topic with the provided callback function.
func (m *MQTTAuthenticationMiddleware) Subscribe(topic string, qos byte, callback mqttLib.MessageHandler) error {
	token := m.mqttClient.Subscribe(topic, qos, callback)
	token.Wait()
	if token.Error() != nil {
		m.logger.Error().Err(token.Error()).Msg("MQTT Subscribe failed")
		return token.Error()
	}
	m.logger.Info().Str("topic", topic).Msg("Subscribed successfully")
	return nil
}

// Unsubscribe unsubscribes from a given topic.
func (m *MQTTAuthenticationMiddleware) Unsubscribe(topics ...string) error {
	token := m.mqttClient.Unsubscribe(topics...)
	token.Wait()
	if token.Error() != nil {
		m.logger.Error().Err(token.Error()).Msg("MQTT Unsubscribe failed")
		return token.Error()
	}
	m.logger.Info().Strs("topics", topics).Msg("Unsubscribed successfully")
	return nil
}
