package models

import (
	"net"
	"time"
)

// SSHRequest represents an SSH port forwarding request sent via MQTT.
// This request is typically sent by a backend service to initiate an SSH reverse tunnel
// from a device to the backend.
type SSHRequest struct {
	LocalPort   int    `json:"lport"`        // Local port on the IoT device
	RemotePort  int    `json:"rport"`        // Remote port on the backend server
	BackendHost string `json:"backend_host"` // Hostname or IP address of the backend server
	BackendPort int    `json:"backend_port"` // Port number of the ssh server (22, 2222, ...)
	ServerID    string `json:"server_id"`    // Unique identifier for the backend server
}

// DeviceReply represents a response from an IoT device indicating that an SSH tunnel
// has been successfully established and is available for use.
type DeviceReply struct {
	DeviceID string `json:"device_id"` // Unique identifier for the IoT device
	Lport    int    `json:"lport"`     // Local port on the IoT device that is forwarded
	Rport    int    `json:"rport"`     // Remote port on the backend server
}

// DisconnectRequest represents a request to terminate an active SSH session.
// This request is typically sent when a session is no longer needed, or when a device
// is shutting down to clean up resources.
type DisconnectRequest struct {
	DeviceID string `json:"deviceId"` // Unique identifier for the IoT device
	Rport    int    `json:"rport"`    // Remote port on the backend server
	Lport    int    `json:"lport"`    // Local port on the IoT device
}

// ListenerWrapper holds a listener and its creation time for expiration tracking.
// This is used to track the creation time of a listener so that it can be closed
// after expiry period to prevent resource leaks and misuse
type ListenerWrapper struct {
	Listener  net.Listener // Listener for the SSH tunnel
	StartTime time.Time    // Time when the listener was created
}
