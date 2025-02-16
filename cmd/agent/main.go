package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "net/http/pprof"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/encryption"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Configure zerolog
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger().Level(zerolog.InfoLevel)

	go func() {
		err := http.ListenAndServe("localhost:6060", nil)
		if err != nil {
			log.Logger.Error().Err(err).Msg("Failed to start pprof server")
		}
	}()

	// Load and validate configuration
	config, err := utils.LoadConfig("configs/config.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}
	if config.MQTT.Broker == "" || config.Security.AESKeyFile == "" || config.Identity.DeviceFile == "" {
		log.Fatal().Msg("Invalid configuration: missing required fields")
	}

	// Generate a unique MQTT Client ID
	config.MQTT.ClientID = config.MQTT.ClientID + "-" + uuid.New().String()
	log.Info().Str("clientID", config.MQTT.ClientID).Msg("Using MQTT Client ID")

	// Initialize MQTT client with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mqttClient := mqtt.NewMqttService()
	if err := mqttClient.Initialize(ctx, config.MQTT.Broker, config.MQTT.ClientID, config.MQTT.CACertificate); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize MQTT connection")
	}
	defer mqttClient.Disconnect(250) // Ensure MQTT client disconnects on shutdown

	// Initialize file operations handler
	fileClient := file.NewFileService()

	// Load device info
	deviceInfo := identity.NewDeviceInfo(config.Identity.DeviceFile, fileClient)
	if err := deviceInfo.LoadDeviceInfo(); err != nil {
		log.Fatal().Err(err).Msg("Failed to load device information")
	}

	// Load AES key securely
	AESKey, err := fileClient.ReadFileRaw(config.Security.AESKeyFile)
	if err != nil || len(AESKey) == 0 {
		log.Fatal().Err(err).Msg("Failed to read or validate AES key")
	}

	// Initialize encryption manager
	encryptionManager, err := encryption.NewEncryptionManager(AESKey)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create encryption manager")
	}

	// Initialize JWT manager
	jwtManager := jwt.NewJWTManager(config.Security.JWTFile, fileClient, encryptionManager)
	if err := jwtManager.LoadJWT(); err != nil {
		log.Fatal().Err(err).Msg("Failed to load JWT")
	}

	// Create and register services
	serviceRegistry := services.NewServiceRegistry(mqttClient, fileClient, encryptionManager, jwtManager, log.Logger)
	if err := serviceRegistry.RegisterServices(config, deviceInfo); err != nil {
		log.Fatal().Err(err).Msg("Failed to register services")
	}

	// Start all services
	if err := serviceRegistry.StartServices(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start services")
	}
	log.Info().Msg("All services started successfully")

	// Graceful shutdown handling
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	<-stopCh // Block until a termination signal is received
	log.Info().Msg("Shutting down gracefully...")

	if err := serviceRegistry.StopServices(); err != nil {
		log.Error().Err(err).Msg("Failed to stop services")
	}
}
