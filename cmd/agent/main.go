package main

import (
	"os"
	"time"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/internal/utils"
	"github.com/benmeehan/iot-agent/pkg/mqtt"

	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.InfoLevel)

	config, err := utils.LoadConfig("configs/config.yaml")
	if err != nil {
		logrus.WithError(err).Fatal("Error loading config")
	}

	// Initialize shared MQTT connection
	if err := mqtt.Initialize(config.MQTT.Broker, config.MQTT.ClientID, config.MQTT.CAFile); err != nil {
		logrus.WithError(err).Fatal("Error initializing MQTT")
	}

	// Create a new service registry
	registry := services.NewServiceRegistry()

	// Register services based on configuration
	for serviceName, serviceConfig := range config.Services {
		if serviceConfig.Enabled {
			switch serviceName {
			case "heartbeat":
				heartbeatService := &services.HeartbeatService{
					PubTopic: serviceConfig.Topic,
					Interval: time.Duration(serviceConfig.Interval) * time.Second,
				}
				registry.RegisterService(serviceName, heartbeatService)
			}
		}
	}

	// Start all registered services
	registry.StartServices()

	// Block the main thread to keep services running
	logrus.Info("Agent is running...")
	select {}
}
