package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/file"
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
	mqttClient := mqtt.NewMqttService()
	err = mqttClient.Initialize(config.MQTT.Broker, config.MQTT.ClientID, config.MQTT.CACertificate)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to initialize MQTT connection")
	}

	// Initialize file operations handler
	fileClient := file.NewFileService()

	// Initialize DeviceInfo
	deviceInfo := identity.NewDeviceInfo(config.Identity.DeviceFile, fileClient)
	err = deviceInfo.LoadDeviceInfo()
	if err != nil {
		logrus.WithError(err).Fatal("Failed to load device information")
	}

	// Create a new service registry to manage services
	serviceRegistry := services.NewServiceRegistry(mqttClient, fileClient)

	// Register all services based on the configuration
	serviceRegistry.RegisterServices(config, deviceInfo)

	// Start all registered services in the registry
	serviceRegistry.StartServices()
	logrus.Info("All services started successfully")

	// Handle graceful shutdown
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	<-stopCh

	logrus.Info("Shutting down gracefully...")
	mqttClient.Disconnect(250)
}
