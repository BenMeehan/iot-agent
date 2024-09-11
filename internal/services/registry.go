package services

import (
	"os"
	"time"

	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
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
	services   *orderedmap.OrderedMap[string, Service]
	mqttClient mqtt.MQTTClient
	fileClient file.FileOperations
}

// NewServiceRegistry initializes and returns a new ServiceRegistry instance
func NewServiceRegistry(mqttClient mqtt.MQTTClient, fileClient file.FileOperations) *ServiceRegistry {
	return &ServiceRegistry{
		services:   orderedmap.NewOrderedMap[string, Service](),
		mqttClient: mqttClient,
		fileClient: fileClient,
	}
}

// RegisterService adds a service to the registry and maintains the order of registration
func (sr *ServiceRegistry) RegisterService(name string, svc Service) {
	if _, exists := sr.services.Get(name); exists {
		logrus.Warnf("Service %s is already registered", name)
		return
	}
	sr.services.Set(name, svc)
	logrus.Infof("Registered service: %s", name)
}

// StartServices starts all registered services in the order they were added
func (sr *ServiceRegistry) StartServices() {
	for el := sr.services.Front(); el != nil; el = el.Next() {
		name := el.Key
		svc := el.Value

		logrus.Infof("Starting service: %s", name)
		if err := svc.Start(); err != nil {
			logrus.WithError(err).Errorf("Failed to start service: %s", name)
			os.Exit(1)
		}
	}
}

// RegisterServices registers services based on the provided configuration and device information.
func (sr *ServiceRegistry) RegisterServices(config *utils.Config, deviceInfo identity.DeviceInfoInterface) {
	serviceConfigs := map[string]func() Service{
		"registration": func() Service {
			return &RegistrationService{
				PubTopic:         config.Services.Registration.Topic,
				DeviceSecretFile: config.Services.Registration.DeviceSecretFile,
				ClientID:         config.MQTT.ClientID,
				QOS:              config.Services.Registration.QOS,
				DeviceInfo:       deviceInfo,
				MqttClient:       sr.mqttClient,
				FileClient:       sr.fileClient,
			}
		},
		"heartbeat": func() Service {
			return &HeartbeatService{
				PubTopic:   config.Services.Heartbeat.Topic,
				Interval:   time.Duration(config.Services.Heartbeat.Interval) * time.Second,
				DeviceID:   deviceInfo.GetDeviceID(),
				QOS:        config.Services.Heartbeat.QOS,
				MqttClient: sr.mqttClient,
			}
		},
	}

	for name, createService := range serviceConfigs {
		switch name {
		case "registration":
			if config.Services.Registration.Enabled {
				sr.RegisterService(name, createService())
				logrus.Infof("%s service registered", name)
			}
		case "heartbeat":
			if config.Services.Heartbeat.Enabled {
				sr.RegisterService(name, createService())
				logrus.Infof("%s service registered", name)
			}
		}
	}
}
