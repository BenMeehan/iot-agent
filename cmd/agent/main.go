package main

import (
	"os"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

func main() {
	// Set up structured logging with JSON output
	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.InfoLevel)

	// Load configuration from file
	config, err := utils.LoadConfig("configs/config.yaml")
	if err != nil {
		logrus.WithError(err).Fatal("Failed to load configuration")
	}

	// Generate a unique MQTT Client ID by appending a UUID
	config.MQTT.ClientID = config.MQTT.ClientID + "-" + uuid.New().String()
	logrus.Infof("Using MQTT Client ID: %s", config.MQTT.ClientID)

	// Initialize the shared MQTT connection
	err = mqtt.Initialize(config.MQTT.Broker, config.MQTT.ClientID, config.MQTT.CACertificate)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to initialize MQTT connection")
	}

	// Load initial device information from the identity service
	deviceInfo := identity.Init(config.Identity.DeviceFile)
	err = deviceInfo.LoadDeviceInfo()
	if err != nil {
		logrus.WithError(err).Fatal("Failed to load device information")
	}

	// Create a new service registry to manage services
	serviceRegistry := services.NewServiceRegistry()

	// Register all services based on the configuration
	serviceRegistry.RegisterServices(config, deviceInfo)

	// Start all registered services in the registry
	serviceRegistry.StartServices()
	logrus.Info("All services started successfully")

	// Keep the agent running by blocking the main thread
	logrus.Info("Agent is running and waiting for tasks...")
	select {}
}
