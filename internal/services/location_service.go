package services

import (
	"encoding/json"
	"errors"
	"time"

	mqtt_middleware "github.com/benmeehan/iot-agent/internal/middlewares/mqtt"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/location"
	"github.com/rs/zerolog"
)

// LocationService manages the retrieval and publishing of device location data.
type LocationService struct {
	PubTopic         string
	Interval         time.Duration
	DeviceInfo       identity.DeviceInfoInterface
	QOS              int
	mqttMiddleware   mqtt_middleware.MQTTMiddleware
	Logger           zerolog.Logger
	LocationProvider location.Provider
	stopChan         chan struct{}
	running          bool
}

// NewLocationService creates and returns a new instance of LocationService.
func NewLocationService(pubTopic string, interval time.Duration, qos int, deviceInfo identity.DeviceInfoInterface,
	mqttMiddleware mqtt_middleware.MQTTMiddleware, logger zerolog.Logger, locationProvider location.Provider) *LocationService {

	return &LocationService{
		PubTopic:         pubTopic,
		Interval:         interval,
		DeviceInfo:       deviceInfo,
		QOS:              qos,
		mqttMiddleware:   mqttMiddleware,
		Logger:           logger,
		LocationProvider: locationProvider,
		stopChan:         make(chan struct{}),
		running:          false,
	}
}

// Start initiates the location service and continuously publishes location messages to the MQTT broker.
func (l *LocationService) Start() error {
	if l.running {
		l.Logger.Warn().Msg("Location service is already running")
		return errors.New("location service is already running")
	}

	l.stopChan = make(chan struct{})
	l.running = true

	go func() {
		ticker := time.NewTicker(l.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := l.publishCurrentLocation(); err != nil {
					l.Logger.Error().Err(err).Msg("Failed to publish current location")
				}
			case <-l.stopChan:
				l.Logger.Info().Msg("Stopping Location service")
				l.running = false
				return
			}
		}
	}()

	l.Logger.Info().Str("topic", l.PubTopic).Msg("Location service started")
	return nil
}

// Stop gracefully stops the location service.
func (l *LocationService) Stop() error {
	if !l.running {
		l.Logger.Warn().Msg("Location service is not running")
		return errors.New("location service is not running")
	}

	if l.stopChan == nil {
		l.Logger.Error().Msg("Failed to stop Location service: stop channel is nil")
		return errors.New("stop channel is nil")
	}

	close(l.stopChan)
	l.running = false
	l.Logger.Info().Msg("Location service stopped")
	return nil
}

// publishCurrentLocation fetches the location and publishes it to the MQTT broker.
func (l *LocationService) publishCurrentLocation() error {
	location, err := l.LocationProvider.GetLocation()
	if err != nil {
		l.Logger.Error().Err(err).Msg("Failed to get location from provider")
		return err
	}

	locationMessage := models.Location{
		DeviceID:  l.DeviceInfo.GetDeviceID(),
		Timestamp: time.Now(),
		Latitude:  location.Latitude,
		Longitude: location.Longitude,
		Accuracy:  location.Accuracy,
	}

	payload, err := json.Marshal(locationMessage)
	if err != nil {
		l.Logger.Error().Err(err).Msg("Failed to serialize location message")
		return err
	}

	// Publish the location message to the MQTT topic
	err = l.mqttMiddleware.Publish(l.PubTopic, byte(l.QOS), false, payload)
	if err != nil {
		l.Logger.Error().Err(err).Msg("Failed to publish location message to MQTT")
		return err
	}

	l.Logger.Info().Interface("message", locationMessage).Msg("Location published successfully")
	return nil
}
