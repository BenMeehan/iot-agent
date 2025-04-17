package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	mqtt_middleware "github.com/benmeehan/iot-agent/internal/middlewares/mqtt"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/location"
	"github.com/rs/zerolog"
)

// LocationService manages the retrieval and publishing of device location data to an MQTT broker.
type LocationService struct {
	// Configuration fields
	topic    string
	interval time.Duration
	qos      int

	// Dependencies
	deviceInfo       identity.DeviceInfoInterface
	mqttMiddleware   mqtt_middleware.MQTTAuthMiddleware
	logger           zerolog.Logger
	locationProvider location.Provider

	// Internal state management
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
}

// NewLocationService creates a new LocationService instance with the provided configuration.
func NewLocationService(topic string, interval time.Duration, qos int, deviceInfo identity.DeviceInfoInterface,
	mqttMiddleware mqtt_middleware.MQTTAuthMiddleware, logger zerolog.Logger, locationProvider location.Provider) *LocationService {
	return &LocationService{
		topic:            topic,
		interval:         interval,
		qos:              qos,
		deviceInfo:       deviceInfo,
		mqttMiddleware:   mqttMiddleware,
		logger:           logger,
		locationProvider: locationProvider,
		running:          false,
	}
}

// Start initiates the LocationService, periodically publishing location data to the MQTT broker.
func (l *LocationService) Start() error {
	if l.running {
		l.logger.Warn().Msg("LocationService is already running")
		return errors.New("location service is already running")
	}

	// Initialize context and cancel function
	l.ctx, l.cancel = context.WithCancel(context.Background())
	l.running = true

	// Start the location publishing goroutine
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()

		ticker := time.NewTicker(l.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := l.publishCurrentLocation(); err != nil {
					l.logger.Error().
						Err(err).
						Msg("Failed to publish current location")
				}
			case <-l.ctx.Done():
				l.logger.Info().Msg("LocationService is stopping")
				l.running = false
				return
			}
		}
	}()

	l.logger.Info().
		Str("topic", l.topic).
		Dur("interval_ms", l.interval).
		Int("qos", l.qos).
		Msg("LocationService started")
	return nil
}

// Stop gracefully stops the LocationService, ensuring all goroutines are terminated.
func (l *LocationService) Stop() error {
	if !l.running {
		l.logger.Warn().Msg("LocationService is not running")
		return errors.New("location service is not running")
	}

	// Signal cancellation and wait for the goroutine to exit
	l.cancel()
	l.wg.Wait()

	// Close the location provider
	if err := l.locationProvider.Close(); err != nil {
		l.logger.Error().Err(err).Msg("Failed to close location provider")
		return err
	}

	l.running = false
	l.logger.Info().Msg("LocationService stopped")
	return nil
}

// publishCurrentLocation fetches the current location and publishes it to the MQTT broker.
func (l *LocationService) publishCurrentLocation() error {
	// Fetch location from the provider
	location, err := l.locationProvider.GetLocation()
	if err != nil {
		l.logger.Error().
			Err(err).
			Msg("Failed to get location from provider")
		return err
	}

	// Construct location message
	locationMessage := models.Location{
		DeviceID:  l.deviceInfo.GetDeviceID(),
		Timestamp: time.Now(),
		Latitude:  location.Latitude,
		Longitude: location.Longitude,
		Accuracy:  location.Accuracy,
	}

	// Serialize the location message to JSON
	payload, err := json.Marshal(locationMessage)
	if err != nil {
		l.logger.Error().Err(err).Msg("Failed to serialize location message")
		return err
	}

	// Publish the location message to the MQTT topic
	err = l.mqttMiddleware.Publish(l.topic, byte(l.qos), false, payload)
	if err != nil {
		l.logger.Error().
			Err(err).
			Str("topic", l.topic).
			Msg("Failed to publish location message to MQTT")
		return err
	}

	l.logger.Info().
		Interface("message", locationMessage).
		Str("topic", l.topic).
		Msg("Location published successfully")
	return nil
}
