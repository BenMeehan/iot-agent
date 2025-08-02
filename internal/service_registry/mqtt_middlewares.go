package service_registry

import (
	"fmt"

	"github.com/benmeehan/iot-agent/internal/constants"
	mqtt_middleware "github.com/benmeehan/iot-agent/internal/middlewares/mqtt"
	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/identity"
)

// InitializeMiddlewares sets up the middleware chain based on configuration.
func (sr *ServiceRegistry) InitializeMiddlewares(config *utils.Config, deviceInfo identity.DeviceInfoInterface) (mqtt_middleware.MQTTMiddleware, error) {
	var middlewares []mqtt_middleware.MQTTMiddleware

	// Ordered middleware definitions
	middlewaresInOrder := []struct {
		name        string
		enabled     bool
		constructor func() (mqtt_middleware.MQTTMiddleware, error)
	}{
		{
			name:    constants.AUTHENTICATION_MIDDLEWARE,
			enabled: config.Middlewares.Authentication.Enabled,
			constructor: func() (mqtt_middleware.MQTTMiddleware, error) {
				authMiddleware := mqtt_middleware.NewMQTTAuthenticationMiddleware(
					config.Middlewares.Authentication.Topic,
					config.Middlewares.Authentication.QOS,
					config.MQTT.ClientID,
					deviceInfo,
					sr.mqttClient,
					sr.jwtManager,
					sr.fileClient,
					sr.logger,
					config.Middlewares.Authentication.RetryDelay,
					config.Middlewares.Authentication.RequestWaitingTime,
				)
				if err := authMiddleware.Init(config.Middlewares.Authentication.AuthenticationCert); err != nil {
					return nil, fmt.Errorf("failed to initialize authentication middleware: %w", err)
				}
				return authMiddleware, nil
			},
		},
	}

	// Initialize middlewares in order
	for _, mw := range middlewaresInOrder {
		if mw.enabled {
			middlewareInstance, err := mw.constructor()
			if err != nil {
				sr.logger.Error().Err(err).Msgf("Failed to initialize %s middleware", mw.name)
				return nil, fmt.Errorf("failed to initialize %s middleware: %w", mw.name, err)
			}
			middlewares = append(middlewares, middlewareInstance)
			sr.logger.Info().Str("middleware", mw.name).Msg("Middleware initialized")
		} else {
			sr.logger.Debug().Str("middleware", mw.name).Msg("Middleware is disabled, skipping")
		}
	}

	// Create and return chained MQTT client
	chainedClient := mqtt_middleware.NewChainedMQTTClient(sr.mqttClient, middlewares)
	sr.logger.Info().Int("middleware_count", len(middlewares)).Msg("Middleware chain initialized")
	return chainedClient, nil
}
