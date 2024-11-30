package models

// SSHRequest represents the structure of the MQTT message that contains the SSH port request
type SSHRequest struct {
	LocalPort   int    `json:"lport"`
	RemotePort  int    `json:"rport"`
	BackendHost string `json:"backend_host"`
	ServerID    string `json:"server_id"`
}

// DeviceReply represents a reply message from a device that it had established an SSH connection
type DeviceReply struct {
	DeviceID string `json:"device_id"`
	Lport    int    `json:"lport"`
	Rport    int    `json:"rport"`
}

// DisconnectRequest represents the structure of a request sent by an IoT device to disconnect a session.
type DisconnectRequest struct {
	DeviceID string `json:"deviceId"`
	Rport    int    `json:"rport"`
	Lport    int    `json:"lport"`
}
