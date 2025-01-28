package services

import (
	"os"
	"time"

	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/encryption"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	"github.com/benmeehan/iot-agent/pkg/location"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/elliotchance/orderedmap/v2"
	"github.com/sirupsen/logrus"
)

// ServiceRegistryInterface defines methods for registering and starting services.
type ServiceRegistryInterface interface {
	RegisterService(name string, svc Service)
	StartServices()
	RegisterServices(config *utils.Config, deviceInfo identity.DeviceInfoInterface)
}

// ServiceRegistry manages a collection of services and their startup order
type ServiceRegistry struct {
	services          *orderedmap.OrderedMap[string, Service]
	mqttClient        mqtt.MQTTClient
	fileClient        file.FileOperations
	encryptionManager encryption.EncryptionManagerInterface
	jwtManager        jwt.JWTManagerInterface
	Logger            *logrus.Logger
}

// NewServiceRegistry initializes and returns a new ServiceRegistry instance
func NewServiceRegistry(mqttClient mqtt.MQTTClient, fileClient file.FileOperations, encryptionManager encryption.EncryptionManagerInterface,
	jwtManager jwt.JWTManagerInterface, logger *logrus.Logger) *ServiceRegistry {

	return &ServiceRegistry{
		services:          orderedmap.NewOrderedMap[string, Service](),
		mqttClient:        mqttClient,
		fileClient:        fileClient,
		encryptionManager: encryptionManager,
		jwtManager:        jwtManager,
		Logger:            logger,
	}
}

// RegisterService adds a service to the registry and maintains the order of registration
func (sr *ServiceRegistry) RegisterService(name string, svc Service) {
	if _, exists := sr.services.Get(name); exists {
		sr.Logger.Warnf("Service %s is already registered", name)
		return
	}
	sr.services.Set(name, svc)
	sr.Logger.Infof("Registered service: %s", name)
}

// StartServices starts all registered services in the order they were added
func (sr *ServiceRegistry) StartServices() {
	for el := sr.services.Front(); el != nil; el = el.Next() {
		name := el.Key
		svc := el.Value

		sr.Logger.Infof("Starting service: %s", name)
		if err := svc.Start(); err != nil {
			sr.Logger.WithError(err).Errorf("Failed to start service: %s", name)
			os.Exit(1)
		}
	}
}

// RegisterServices registers services based on the provided configuration and device information.
func (sr *ServiceRegistry) RegisterServices(config *utils.Config, deviceInfo identity.DeviceInfoInterface) {
	serviceConfigs := map[string]func() Service{
		"registration": func() Service {
			return &RegistrationService{
				PubTopic:          config.Services.Registration.Topic,
				ClientID:          config.MQTT.ClientID,
				QOS:               config.Services.Registration.QOS,
				DeviceInfo:        deviceInfo,
				MqttClient:        sr.mqttClient,
				FileClient:        sr.fileClient,
				EncryptionManager: sr.encryptionManager,
				JWTManager:        sr.jwtManager,
				Logger:            sr.Logger,
			}
		},
		"heartbeat": func() Service {
			return &HeartbeatService{
				PubTopic:   config.Services.Heartbeat.Topic,
				Interval:   time.Duration(config.Services.Heartbeat.Interval) * time.Second,
				DeviceInfo: deviceInfo,
				QOS:        config.Services.Heartbeat.QOS,
				MqttClient: sr.mqttClient,
				Logger:     sr.Logger,
			}
		},
		"metrics": func() Service {
			return &MetricsService{
				PubTopic:          config.Services.Metrics.Topic,
				MetricsConfigFile: config.Services.Metrics.MetricsConfigFile,
				Interval:          time.Duration(config.Services.Metrics.Interval) * time.Second,
				DeviceInfo:        deviceInfo,
				QOS:               config.Services.Metrics.QOS,
				MqttClient:        sr.mqttClient,
				FileClient:        sr.fileClient,
				Logger:            sr.Logger,
			}
		},
		"command": func() Service {
			return &CommandService{
				SubTopic:         config.Services.Command.Topic,
				DeviceInfo:       deviceInfo,
				QOS:              config.Services.Command.QOS,
				MqttClient:       sr.mqttClient,
				Logger:           sr.Logger,
				OutputSizeLimit:  config.Services.Command.OutputSizeLimit,
				MaxExecutionTime: config.Services.Command.MaxExecutionTime,
			}
		},
		"ssh": func() Service {
			return &SSHService{
				SubTopic:       config.Services.SSH.Topic,
				DeviceInfo:     deviceInfo,
				MqttClient:     sr.mqttClient,
				Logger:         sr.Logger,
				SSHUser:        config.Services.SSH.SSHUser,
				BackendHost:    config.Services.SSH.BackendHost,
				BackendPort:    config.Services.SSH.BackendPort,
				FileClient:     sr.fileClient,
				PrivateKeyPath: config.Services.SSH.PrivateKeyPath,
				QOS:            config.Services.SSH.QOS,
			}
		},
		"location": func() Service {
			var provider location.Provider
			var err error

			if config.Services.Location.SensorBased {
				provider, err = location.NewGoogleGeolocationProvider(config.Services.Location.MapsAPIKey)
				if err != nil {
					sr.Logger.WithError(err).Error("failed to create Google Geolocation provider")
				}
			} else {
				provider = location.NewDeviceSensorProvider(config.Services.Location.GPSDevicePort, config.Services.Location.GPSDeviceBaudRate)
			}

			return &LocationService{
				Interval:         time.Duration(config.Services.Location.Interval),
				QOS:              config.Services.Location.Interval,
				PubTopic:         config.Services.Location.Topic,
				MqttClient:       sr.mqttClient,
				Logger:           sr.Logger,
				DeviceInfo:       deviceInfo,
				LocationProvider: provider,
			}
		},
		"update": func() Service {
			return &UpdateService{
				SubTopic:       config.Services.Update.Topic,
				DeviceInfo:     deviceInfo,
				QOS:            config.Services.Update.QOS,
				MqttClient:     sr.mqttClient,
				Logger:         sr.Logger,
				StateFile:      config.Services.Update.StateFile,
				UpdateFilePath: config.Services.Update.UpdateFilePath,
			}
		},
	}

	for name, createService := range serviceConfigs {
		switch name {
		case "registration":
			if config.Services.Registration.Enabled {
				sr.RegisterService(name, createService())
				sr.Logger.Infof("%s service registered", name)
			}
		case "heartbeat":
			if config.Services.Heartbeat.Enabled {
				sr.RegisterService(name, createService())
				sr.Logger.Infof("%s service registered", name)
			}
		case "metrics":
			if config.Services.Metrics.Enabled {
				sr.RegisterService(name, createService())
				sr.Logger.Infof("%s service registered", name)
			}
		case "command":
			if config.Services.Command.Enabled {
				sr.RegisterService(name, createService())
				sr.Logger.Infof("%s service registered", name)
			}
		case "ssh":
			if config.Services.SSH.Enabled {
				sr.RegisterService(name, createService())
				sr.Logger.Infof("%s service registered", name)
			}
		case "location":
			if config.Services.Location.Enabled {
				sr.RegisterService(name, createService())
				sr.Logger.Infof("%s service registered", name)
			}
		case "update":
			if config.Services.Update.Enabled {
				sr.RegisterService(name, createService())
				sr.Logger.Infof("%s service registered", name)
			}
		}
	}
}
