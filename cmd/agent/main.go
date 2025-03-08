package main

import (
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/benmeehan/iot-agent/internal/service_registry"
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

// startProfiler starts an HTTP server for pprof profiling
func startProfiler() {
	go func() {
		log.Info().Msg("Starting profiler on :6060")
		if err := http.ListenAndServe("0.0.0.0:6060", nil); err != nil {
			log.Error().Err(err).Msg("Failed to start pprof profiler")
		}
	}()
}

// getLogLevel sets the log level from environment variable
func getLogLevel() zerolog.Level {
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		return zerolog.DebugLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

func main() {
	// Set up logging
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger().Level(getLogLevel())

	// Start the profiler
	startProfiler()

	// Initialize file operations handler
	fileClient := file.NewFileService()
	log.Info().Msg("File client initialized successfully")

	// Load and validate configuration
	config, err := utils.LoadConfig("configs/config.yaml", fileClient)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}
	log.Info().Msg("Configuration loaded successfully")

	if config.MQTT.Broker == "" || config.Security.AESKeyFile == "" || config.Identity.DeviceFile == "" {
		log.Fatal().Msg("Invalid configuration: missing required fields")
	}

	// Generate a unique MQTT Client ID
	config.MQTT.ClientID = config.MQTT.ClientID + "-" + uuid.NewString()
	log.Info().Str("clientID", config.MQTT.ClientID).Msg("Using MQTT Client ID")

	// Initialize MQTT client
	mqttClient := mqtt.NewMqttService(fileClient)
	if err := mqttClient.Initialize(config.MQTT.Broker, config.MQTT.ClientID, config.MQTT.CACertificate); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize MQTT connection")
	}
	defer mqttClient.Disconnect(250) // Ensure MQTT client disconnects on shutdown
	log.Info().Msg("MQTT client initialized successfully")

	// Load device info
	deviceInfo := identity.NewDeviceInfo(config.Identity.DeviceFile, fileClient)
	if err := deviceInfo.LoadDeviceInfo(); err != nil {
		log.Fatal().Err(err).Msg("Failed to load device information")
	}
	log.Info().Msg("Device information loaded successfully")

	// Initialize encryption manager
	encryptionManager := encryption.NewEncryptionManager(fileClient)
	if err := encryptionManager.Initialize(config.Security.AESKeyFile); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize encryption manager")
	}
	log.Info().Msg("Encryption manager initialized successfully")

	// Initialize JWT manager
	jwtManager := jwt.NewJWTManager(config.Security.JWTFile, fileClient, encryptionManager)
	if err := jwtManager.LoadJWT(); err != nil {
		log.Fatal().Err(err).Msg("Failed to load JWT")
	}
	log.Info().Msg("JWT manager initialized successfully")

	// Create and register services
	serviceRegistry := service_registry.NewServiceRegistry(mqttClient, fileClient, encryptionManager, jwtManager, log.Logger)
	if err := serviceRegistry.RegisterServices(config, deviceInfo); err != nil {
		log.Fatal().Err(err).Msg("Failed to register services")
	}
	log.Info().Msg("Services registered successfully")

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
		if multiErr, ok := err.(interface{ Unwrap() []error }); ok {
			for _, e := range multiErr.Unwrap() {
				log.Error().Err(e).Msg("Service shutdown error")
			}
		} else {
			log.Fatal().Err(err).Msg("Failed to stop services gracefully")
		}
	}
}
