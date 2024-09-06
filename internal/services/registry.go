package services

import (
	"github.com/sirupsen/logrus"
)

// ServiceRegistry holds registered services
type ServiceRegistry struct {
	services map[string]Service
}

// NewServiceRegistry creates a new ServiceRegistry
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		services: make(map[string]Service),
	}
}

// RegisterService registers a service with a given name
func (sr *ServiceRegistry) RegisterService(name string, svc Service) {
	sr.services[name] = svc
}

// StartServices starts all registered services
func (sr *ServiceRegistry) StartServices() {
	for name, svc := range sr.services {
		go func(name string, svc Service) {
			logrus.Infof("Starting service: %s", name)
			if err := svc.Start(); err != nil {
				logrus.WithError(err).Errorf("Error starting service: %s", name)
			}
		}(name, svc)
	}
}
