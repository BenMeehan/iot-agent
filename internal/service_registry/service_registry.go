package service_registry

import (
	"errors"
	"fmt"
	"time"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/encryption"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/location"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/rs/zerolog"
)

// ServiceRegistry manages the lifecycle of various services in the system.
type ServiceRegistry struct {
	services          map[string]Service // Stores registered services
	serviceKeys       []string           // Maintains order of service registration
	mqttClient        mqtt.MQTTClient
	fileClient        file.FileOperations
	encryptionManager encryption.EncryptionManagerInterface
	jwtManager        jwt.JWTManagerInterface
	Logger            zerolog.Logger
}

// NewServiceRegistry initializes a new service registry with dependencies.
func NewServiceRegistry(mqttClient mqtt.MQTTClient, fileClient file.FileOperations, encryptionManager encryption.EncryptionManagerInterface,
	jwtManager jwt.JWTManagerInterface, logger zerolog.Logger) *ServiceRegistry {
	return &ServiceRegistry{
		services:          make(map[string]Service),
		mqttClient:        mqttClient,
		fileClient:        fileClient,
		encryptionManager: encryptionManager,
		jwtManager:        jwtManager,
		Logger:            logger,
	}
}

// RegisterService adds a new service to the registry.
func (sr *ServiceRegistry) RegisterService(name string, svc Service) {
	if _, exists := sr.services[name]; exists {
		sr.Logger.Warn().Msgf("Service %s is already registered", name)
		return
	}
	sr.services[name] = svc
	sr.serviceKeys = append(sr.serviceKeys, name)
	sr.Logger.Info().Msgf("Registered service: %s", name)
}

// StartServices initiates all registered services in order.
// If a service fails to start, it stops already started services.
func (sr *ServiceRegistry) StartServices() error {
	startedServices := []string{}

	for _, name := range sr.serviceKeys {
		svc := sr.services[name]
		sr.Logger.Info().Msgf("Starting service: %s", name)
		if err := svc.Start(); err != nil {
			sr.Logger.Error().Err(err).Msgf("Failed to start service: %s", name)

			// Stop already started services before returning
			sr.Logger.Warn().Msg("Stopping already started services due to startup failure...")
			for i := len(startedServices) - 1; i >= 0; i-- {
				_ = sr.services[startedServices[i]].Stop()
			}
			return err
		}
		startedServices = append(startedServices, name)
	}

	return nil
}

// StopServices stops all services in reverse order.
func (sr *ServiceRegistry) StopServices() error {
	var stopErrors []error
	for i := len(sr.serviceKeys) - 1; i >= 0; i-- {
		name := sr.serviceKeys[i]
		if err := sr.services[name].Stop(); err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("failed to stop %s: %w", name, err))
		}
	}
	if len(stopErrors) > 0 {
		for _, e := range stopErrors {
			sr.Logger.Error().Err(e).Msg("Service stop failure")
		}
		return errors.Join(stopErrors...)
	}
	return nil
}

// RegisterServices initializes and registers enabled services based on configuration.
func (sr *ServiceRegistry) RegisterServices(config *utils.Config, deviceInfo identity.DeviceInfoInterface) error {
	// Map of service constructors to be dynamically created.
	serviceConfigs := map[string]func() (Service, error){
		"registration": func() (Service, error) {
			return services.NewRegistrationService(
				config.Services.Registration.Topic,
				config.MQTT.ClientID,
				config.Services.Registration.QOS,
				deviceInfo,
				sr.mqttClient,
				sr.fileClient,
				sr.jwtManager,
				sr.encryptionManager,
				config.Services.Registration.MaxBackoffSeconds,
				sr.Logger,
			), nil
		},
		"heartbeat": func() (Service, error) {
			return services.NewHeartbeatService(
				config.Services.Heartbeat.Topic,
				time.Duration(config.Services.Heartbeat.Interval)*time.Second,
				deviceInfo,
				config.Services.Heartbeat.QOS,
				sr.mqttClient,
				sr.jwtManager,
				sr.Logger,
			), nil
		},
		"metrics": func() (Service, error) {
			return services.NewMetricsService(
				config.Services.Metrics.Topic,
				config.Services.Metrics.MetricsConfigFile,
				time.Duration(config.Services.Metrics.Interval)*time.Second,
				time.Duration(config.Services.Metrics.Timeout)*time.Second,
				deviceInfo,
				config.Services.Metrics.QOS,
				sr.mqttClient,
				sr.fileClient,
				sr.Logger,
			), nil
		},
		"command": func() (Service, error) {
			return services.NewCommandService(
				config.Services.Command.Topic,
				deviceInfo,
				config.Services.Command.QOS,
				sr.mqttClient,
				sr.Logger,
				config.Services.Command.OutputSizeLimit,
				config.Services.Command.MaxExecutionTime,
			), nil
		},
		"ssh": func() (Service, error) {
			return services.NewSSHService(
				config.Services.SSH.Topic,
				deviceInfo,
				sr.mqttClient,
				sr.Logger,
				config.Services.SSH.BackendHost,
				config.Services.SSH.BackendPort,
				config.Services.SSH.SSHUser,
				config.Services.SSH.PrivateKeyPath,
				sr.fileClient,
				config.Services.SSH.QOS,
			), nil
		},
		"location": func() (Service, error) {
			var provider location.Provider
			var err error

			if config.Services.Location.SensorBased {
				provider, err = location.NewGoogleGeolocationProvider(config.Services.Location.MapsAPIKey)
				if err != nil {
					sr.Logger.Error().Err(err).Msg("failed to create Google Geolocation provider")
					return nil, err
				}
			} else {
				provider = location.NewDeviceSensorProvider(config.Services.Location.GPSDevicePort, config.Services.Location.GPSDeviceBaudRate)
			}

			return services.NewLocationService(
				config.Services.Location.Topic,
				time.Duration(config.Services.Location.Interval),
				deviceInfo,
				config.Services.Location.Interval,
				sr.mqttClient,
				sr.Logger,
				provider,
			), nil
		},
		"update": func() (Service, error) {
			return services.NewUpdateService(
				config.Services.Update.Topic,
				deviceInfo,
				config.Services.Update.QOS,
				sr.mqttClient,
				sr.fileClient,
				sr.Logger,
				config.Services.Update.StateFile,
				config.Services.Update.UpdateFilePath,
			), nil
		},
	}

	registeredServices := []string{}
	for name, createService := range serviceConfigs {
		if isEnabled(name, config) {
			svc, err := createService()
			if err != nil {
				return err
			}
			sr.RegisterService(name, svc)
			registeredServices = append(registeredServices, name)
		}
	}
	sr.Logger.Info().Msgf("Registered services: %v", registeredServices)
	return nil
}

// isEnabled checks if a given service is enabled in the configuration.
func isEnabled(name string, config *utils.Config) bool {
	switch name {
	case "registration":
		return config.Services.Registration.Enabled
	case "heartbeat":
		return config.Services.Heartbeat.Enabled
	case "metrics":
		return config.Services.Metrics.Enabled
	case "command":
		return config.Services.Command.Enabled
	case "ssh":
		return config.Services.SSH.Enabled
	case "location":
		return config.Services.Location.Enabled
	case "update":
		return config.Services.Update.Enabled
	default:
		return false
	}
}
