package services

import (
	"os"

	"github.com/sirupsen/logrus"
)

// ServiceRegistry holds registered services in a specific order
type ServiceRegistry struct {
	services map[string]Service
	order    []string
}

// NewServiceRegistry creates a new ServiceRegistry
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		services: make(map[string]Service),
		order:    []string{},
	}
}

// RegisterService registers a service with a given name and maintains the order
func (sr *ServiceRegistry) RegisterService(name string, svc Service) {
	sr.services[name] = svc
	sr.order = append(sr.order, name)
}

// StartServices starts all registered services in the order they were registered
func (sr *ServiceRegistry) StartServices() {
	for _, name := range sr.order {
		svc := sr.services[name]
		logrus.Infof("Starting service: %s", name)
		if err := svc.Start(); err != nil {
			logrus.WithError(err).Errorf("Error starting service: %s", name)
			os.Exit(1)
		}
	}
}
