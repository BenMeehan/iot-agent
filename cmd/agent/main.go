package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/encryption"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

func main() {
	// Set up structured logging with JSON output
	var log = &logrus.Logger{
		Out:       os.Stdout,
		Formatter: new(logrus.JSONFormatter),
		Hooks:     make(logrus.LevelHooks),
		Level:     logrus.InfoLevel,
	}

	// Load configuration from file
	config, err := utils.LoadConfig("configs/config.yaml", log)
	if err != nil {
		log.WithError(err).Fatal("Failed to load configuration")
	}

	// Generate a unique MQTT Client ID by appending a UUID
	config.MQTT.ClientID = config.MQTT.ClientID + "-" + uuid.New().String()
	log.Infof("Using MQTT Client ID: %s", config.MQTT.ClientID)

	// Initialize the shared MQTT connection
	mqttClient := mqtt.NewMqttService(log)
	err = mqttClient.Initialize(config.MQTT.Broker, config.MQTT.ClientID, config.MQTT.CACertificate)
	if err != nil {
		log.WithError(err).Fatal("Failed to initialize MQTT connection")
	}

	// Initialize file operations handler
	fileClient := file.NewFileService(log)

	// Initialize DeviceInfo
	deviceInfo := identity.NewDeviceInfo(config.Identity.DeviceFile, fileClient, log)
	err = deviceInfo.LoadDeviceInfo()
	if err != nil {
		log.WithError(err).Fatal("Failed to load device information")
	}

	AESKey, err := fileClient.ReadFileRaw(config.Security.AESKeyFile)
	if err != nil {
		log.WithError(err).Fatal("failed to read AES key")
	}

	EncryptionManager, err := encryption.NewEncryptionManager(AESKey)
	if err != nil {
		log.WithError(err).Fatal("failed to create encryption manager")
	}

	JWTManager := jwt.NewJWTManager(config.Security.JWTFile, fileClient, EncryptionManager)
	JWTManager.LoadJWT()

	// Create a new service registry to manage services
	serviceRegistry := services.NewServiceRegistry(mqttClient, fileClient, EncryptionManager, JWTManager, log)

	// Register all services based on the configuration
	serviceRegistry.RegisterServices(config, deviceInfo)

	// Start all registered services in the registry
	serviceRegistry.StartServices()
	log.Info("All services started successfully")

	// Handle graceful shutdown
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	<-stopCh

	log.Info("Shutting down gracefully...")
	mqttClient.Disconnect(250)
}
