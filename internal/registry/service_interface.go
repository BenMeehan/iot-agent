package registry

// Service is the interface for all plug-in services
type Service interface {
	Start() error
	Stop() error
}
