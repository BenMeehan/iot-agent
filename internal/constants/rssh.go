package constants

import "time"

const (
	// MaxListeners defines the maximum number of active SSH listeners.
	MaxListeners = 10

	// ConnectionTimeout specifies the timeout duration for establishing an SSH connection.
	ConnectionTimeout = 30 * time.Second

	// AutoDisconnect sets the duration after which inactive SSH connections will be closed.
	AutoDisconnect = 1 * time.Hour

	// MaxSSHConnections limits the number of concurrent SSH connections that can be established.
	MaxSSHConnections = 5
)
