package services

import (
	"encoding/json"
	"time"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/location"
	"github.com/benmeehan/iot-agent/pkg/mqtt"

	"github.com/sirupsen/logrus"
)

// LocationService manages the retrieval and publishing of device location data.
type LocationService struct {
	PubTopic         string
	Interval         time.Duration
	DeviceInfo       identity.DeviceInfoInterface
	QOS              int
	MqttClient       mqtt.MQTTClient
	Logger           *logrus.Logger
	LocationProvider location.Provider
}

// Start initiates the location service and continuously publishes location messages to the MQTT broker.
func (l *LocationService) Start() error {
	go l.publishLocation()
	return nil
}

// publishLocation retrieves the location periodically and publishes it.
func (l *LocationService) publishLocation() {
	go func() {
		for range time.Tick(l.Interval) {
			if err := l.publishCurrentLocation(); err != nil {
				l.Logger.WithError(err).Error("failed to publish current location")
			}
		}
	}()
}

// publishCurrentLocation fetches the location and publishes it to the MQTT broker.
func (l *LocationService) publishCurrentLocation() error {
	location, err := l.LocationProvider.GetLocation()
	if err != nil {
		l.Logger.WithError(err).Error("failed to get location from provider")
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
		l.Logger.WithError(err).Error("failed to serialize location message")
		return err
	}

	// Publish the location message to the MQTT topic
	token := l.MqttClient.Publish(l.PubTopic, byte(l.QOS), false, payload)
	token.Wait()

	if err := token.Error(); err != nil {
		l.Logger.WithError(err).Error("failed to publish location message to MQTT")
		return err
	}

	l.Logger.WithField("message", locationMessage).Info("Location published successfully")
	return nil
}
