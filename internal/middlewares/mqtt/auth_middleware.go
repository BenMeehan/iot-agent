package mqtt

import (
	"context"
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

// MQTTAuthenticationMiddleware handles JWT-based authentication for MQTT operations.
type MQTTAuthenticationMiddleware struct {
	next                      MQTTMiddleware
	authTopic                 string
	qos                       int
	authenticationCertificate []byte
	clientID                  string
	deviceInfo                identity.DeviceInfoInterface
	mqttClient                mqtt.MQTTClient
	jwtManager                jwt.JWTManagerInterface
	fileClient                file.FileOperations
	logger                    zerolog.Logger
	retryDelay                time.Duration
	requestWaitingTime        time.Duration
	jwtMutex                  sync.Mutex
	refreshingJWT             bool
	jwtRefreshDone            chan struct{}
}

// NewMQTTAuthenticationMiddleware creates a new authentication middleware instance.
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
		deviceInfo:         deviceInfo,
		mqttClient:         mqttClient,
		jwtManager:         jwtManager,
		fileClient:         fileClient,
		logger:             logger,
		retryDelay:         retryDelay * time.Second,
		requestWaitingTime: requestWaitingTime * time.Second,
		jwtRefreshDone:     make(chan struct{}, 1),
	}
}

// SetNext sets the next middleware in the chain.
func (m *MQTTAuthenticationMiddleware) SetNext(next MQTTMiddleware) {
	m.next = next
}

// Init initializes the middleware by loading tokens and ensuring a valid JWT.
func (m *MQTTAuthenticationMiddleware) Init(params interface{}) error {
	authenticationCertPath, ok := params.(string)
	if !ok {
		return fmt.Errorf("Init: expected string, got %T", params)
	}

	if err := m.jwtManager.LoadTokens(); err != nil {
		m.logger.Error().Err(err).Msg("Failed to load existing JWT tokens")
	}

	certificate, err := m.fileClient.ReadFileRaw(authenticationCertPath)
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
		ctx, cancel := context.WithTimeout(context.Background(), m.requestWaitingTime)
		defer cancel()
		if err := m.requestJWTWithCertificate(ctx); err != nil {
			return fmt.Errorf("initial JWT request failed: %w", err)
		}
	}
	return nil
}

// onAuthMessage handles authentication responses from the MQTT broker.
func (m *MQTTAuthenticationMiddleware) onAuthMessage(msg mqttLib.Message) error {
	m.logger.Info().Str("topic", msg.Topic()).Msg("Received authentication response")

	var authResponse models.AuthResponse
	if err := json.Unmarshal(msg.Payload(), &authResponse); err != nil {
		m.logger.Error().Err(err).Msg("Failed to parse authentication response")
		return err
	}

	if err := m.jwtManager.SaveTokens(authResponse.AccessToken, authResponse.RefreshToken, authResponse.ExpiresIn); err != nil {
		m.logger.Error().Err(err).Msg("Failed to save tokens")
		return err
	}
	m.logger.Info().Msg("JWT and refresh tokens successfully stored")
	return nil
}

// waitForJWTResponse waits for a JWT response with a timeout and unsubscribes afterward.
func (m *MQTTAuthenticationMiddleware) waitForJWTResponse(ctx context.Context, jwtChan chan error, responseTopic string) error {
	defer func() {
		if err := m.Unsubscribe(responseTopic); err != nil {
			m.logger.Warn().Err(err).Msgf("Failed to unsubscribe from topic: %s", responseTopic)
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(m.requestWaitingTime):
		return errors.New("authentication response timeout")
	case err := <-jwtChan:
		if err != nil {
			return fmt.Errorf("authentication response error: %w", err)
		}
		return nil
	}
}

// requestJWTWithCertificate requests a JWT using the authentication certificate.
func (m *MQTTAuthenticationMiddleware) requestJWTWithCertificate(ctx context.Context) error {
	var attempt int
	for {
		m.logger.Info().Int("attempt", attempt).Msg("Attempting to request JWT with certificate")

		err := func() error {
			payload := models.AuthRequest{}
			var identifier string
			if id := m.deviceInfo.GetDeviceID(); id != "" {
				payload.DeviceID = id
				identifier = id
			} else {
				payload.ClientID = m.clientID
				identifier = m.clientID
			}
			payload.Key = m.authenticationCertificate

			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("failed to marshal authentication request: %w", err)
			}

			jwtChan := make(chan error, 1)
			handler := func(client mqttLib.Client, msg mqttLib.Message) {
				jwtChan <- m.onAuthMessage(msg)
			}
			authResponseTopic := fmt.Sprintf("%s/response/%s", m.authTopic, identifier)
			if err := m.Subscribe(authResponseTopic, byte(m.qos), handler); err != nil {
				return fmt.Errorf("failed to subscribe for auth response: %w", err)
			}

			m.logger.Info().Str("topic", m.authTopic).Msg("Publishing authentication request with certificate")
			token := m.mqttClient.Publish(m.authTopic, byte(m.qos), false, payloadBytes)
			token.Wait()
			if token.Error() != nil {
				return fmt.Errorf("failed to publish authentication request: %w", token.Error())
			}

			return m.waitForJWTResponse(ctx, jwtChan, authResponseTopic)
		}()

		if err == nil {
			m.logger.Info().Int("attempt", attempt).Msg("Successfully obtained JWT with certificate")
			return nil
		}

		jitter := time.Duration(rand.Int63n(int64(m.retryDelay) / 10))
		totalDelay := m.retryDelay*time.Duration(attempt) + jitter
		m.logger.Warn().
			Int("attempt", attempt).
			Dur("retry_delay_ms", totalDelay).
			Err(err).
			Msg("Failed to obtain JWT, retrying after delay")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(totalDelay):
		}

		attempt++
	}
}

// refreshJWT requests a new JWT using a refresh token or certificate.
func (m *MQTTAuthenticationMiddleware) refreshJWT(ctx context.Context) error {
	m.jwtMutex.Lock()
	if m.refreshingJWT {
		m.jwtMutex.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.jwtRefreshDone:
			return nil
		}
	}
	m.refreshingJWT = true
	m.jwtMutex.Unlock()

	defer func() {
		m.jwtMutex.Lock()
		m.refreshingJWT = false
		select {
		case m.jwtRefreshDone <- struct{}{}:
		default:
		}
		m.jwtMutex.Unlock()
	}()

	refreshToken, expiresIn := m.jwtManager.GetRefreshToken()
	isValid := time.Now().Before(time.Now().Add(time.Duration(expiresIn) * time.Second))

	if isValid {
		var attempt int
		for {
			m.logger.Info().Int("attempt", attempt).Msg("Attempting to refresh JWT with refresh token")

			err := func() error {
				authRequestPayload := models.AuthRequest{
					DeviceID:     m.deviceInfo.GetDeviceID(),
					RefreshToken: refreshToken,
				}
				payloadBytes, err := json.Marshal(authRequestPayload)
				if err != nil {
					return fmt.Errorf("failed to serialize refresh request: %w", err)
				}

				jwtChan := make(chan error, 1)
				handler := func(client mqttLib.Client, msg mqttLib.Message) {
					jwtChan <- m.onAuthMessage(msg)
				}
				authResponseTopic := fmt.Sprintf("%s/response/%s", m.authTopic, m.deviceInfo.GetDeviceID())
				if err := m.Subscribe(authResponseTopic, byte(m.qos), handler); err != nil {
					return fmt.Errorf("failed to subscribe for refresh response: %w", err)
				}

				m.logger.Info().Str("topic", m.authTopic).Msg("Publishing token refresh request")
				token := m.mqttClient.Publish(m.authTopic, byte(m.qos), false, payloadBytes)
				token.Wait()
				if token.Error() != nil {
					return fmt.Errorf("failed to publish refresh request: %w", token.Error())
				}

				return m.waitForJWTResponse(ctx, jwtChan, authResponseTopic)
			}()

			if err == nil {
				m.logger.Info().Int("attempt", attempt).Msg("JWT successfully refreshed with refresh token")
				return nil
			}

			jitter := time.Duration(rand.Int63n(int64(m.retryDelay) / 10))
			totalDelay := m.retryDelay*time.Duration(attempt) + jitter
			m.logger.Warn().
				Int("attempt", attempt).
				Dur("retry_delay_ms", totalDelay).
				Err(err).
				Msg("Failed to refresh JWT with refresh token, retrying after delay")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(totalDelay):
			}

			attempt++
		}
	}

	m.logger.Info().Msg("No valid refresh token or refresh failed, falling back to certificate")
	return m.requestJWTWithCertificate(ctx)
}

// validateJWT ensures a valid JWT is available.
func (m *MQTTAuthenticationMiddleware) validateJWT(ctx context.Context) (string, error) {
	token := m.jwtManager.GetJWT()

	isJWTValid, err := m.jwtManager.CheckExpiration(token, constants.AccessToken)
	if err != nil {
		return "", fmt.Errorf("failed to validate JWT: %w", err)
	}

	if !isJWTValid {
		m.logger.Info().Msg("JWT is expired, attempting refresh")
		if err := m.refreshJWT(ctx); err != nil {
			return "", fmt.Errorf("failed to refresh JWT: %w", err)
		}
		token = m.jwtManager.GetJWT()
	}

	m.logger.Debug().Str("token", token).Msg("JWT validated successfully")
	return token, nil
}

// Publish wraps the payload with a JWT and sends it via MQTT.
func (m *MQTTAuthenticationMiddleware) Publish(topic string, qos byte, retained bool, payload interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), m.requestWaitingTime)
	defer cancel()

	jwtToken, err := m.validateJWT(ctx)
	if err != nil {
		return fmt.Errorf("failed JWT validation: %w", err)
	}

	wrappedPayload := models.WrappedPayload{
		JWT:     jwtToken,
		Payload: payload,
	}
	payloadBytes, err := json.Marshal(wrappedPayload)
	if err != nil {
		m.logger.Error().Err(err).Msg("Failed to serialize wrapped payload")
		return fmt.Errorf("failed to serialize wrapped payload: %w", err)
	}

	m.logger.Debug().Str("topic", topic).Msg("Publishing message with JWT")
	if m.next != nil {
		return m.next.Publish(topic, qos, retained, payloadBytes)
	}

	token := m.mqttClient.Publish(topic, qos, retained, payloadBytes)
	token.Wait()
	if token.Error() != nil {
		m.logger.Error().Err(token.Error()).Msg("Failed to publish MQTT message")
		return token.Error()
	}

	m.logger.Debug().Str("topic", topic).Msg("Message published successfully")
	return nil
}

// Subscribe subscribes to a topic with the provided callback.
func (m *MQTTAuthenticationMiddleware) Subscribe(topic string, qos byte, callback mqttLib.MessageHandler) error {
	m.logger.Debug().Str("topic", topic).Msg("Subscribing to topic")
	if m.next != nil {
		return m.next.Subscribe(topic, qos, callback)
	}

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
	m.logger.Debug().Strs("topics", topics).Msg("Unsubscribing from topics")
	if m.next != nil {
		return m.next.Unsubscribe(topics...)
	}

	token := m.mqttClient.Unsubscribe(topics...)
	token.Wait()
	if token.Error() != nil {
		m.logger.Error().Err(token.Error()).Msg("Failed to unsubscribe from MQTT topics")
		return token.Error()
	}

	m.logger.Debug().Strs("topics", topics).Msg("Unsubscribed from topics successfully")
	return nil
}
