package main

import (
	"os"
	"time"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

func main() {
	// Set up logging
	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.InfoLevel)

	// Load configuration
	config, err := utils.LoadConfig("configs/config.yaml")
	if err != nil {
		logrus.WithError(err).Fatal("Error loading config")
	}
	config.MQTT.ClientID = config.MQTT.ClientID + uuid.New().String()
	logrus.Info("Using MQTT client ID ", config.MQTT.ClientID)

	// Initialize shared MQTT connection
	if err := mqtt.Initialize(config.MQTT.Broker, config.MQTT.ClientID, config.MQTT.CACertificate); err != nil {
		logrus.WithError(err).Fatal("Error initializing MQTT")
	}

	// Load initial device config
	info := identity.Init(config.Identity.DeviceFile)
	if err := info.LoadDeviceInfo(); err != nil {
		logrus.WithError(err).Fatal("Error loading device info")
	}

	// Create a new service registry
	registry := services.NewServiceRegistry()

	// Register services
	if config.Services.Registration.Enabled {
		registration := &services.RegistrationService{
			PubTopic:         config.Services.Registration.Topic,
			DeviceSecretFile: config.Services.Registration.DeviceSecretFile,
			ClientID:         config.MQTT.ClientID,
			QOS:              config.Services.Registration.QOS,
			DeviceInfo:       *info,
		}
		registry.RegisterService("registration", registration)
	}

	if config.Services.Heartbeat.Enabled {
		heartbeat := &services.HeartbeatService{
			PubTopic: config.Services.Heartbeat.Topic,
			Interval: time.Duration(config.Services.Heartbeat.Interval) * time.Second,
			DeviceID: info.Config.ID,
			QOS:      config.Services.Heartbeat.QOS,
		}
		registry.RegisterService("heartbeat", heartbeat)
	}

	// Start all registered services
	registry.StartServices()

	// Block the main thread to keep services running
	logrus.Info("Agent is running...")
	select {}
}
