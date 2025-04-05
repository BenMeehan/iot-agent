package models

// PortForwardRequest represents a request to establish an HTTP2 tunnel from the IoT device
// to a backend server.
type PortForwardRequest struct {
	LocalPort  int `json:"lport"` // Local port on the IoT device
	RemotePort int `json:"rport"` // Remote port on the backend server
}
