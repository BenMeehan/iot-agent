package models

// SSHRequest represents the request for SSH port forwarding.
type SSHRequest struct {
	LocalPort   int    `json:"lport"`        // Local port on the IoT device
	RemotePort  int    `json:"rport"`        // Remote port on the backend server
	BackendHost string `json:"backend_host"` // Hostname or IP address of the backend server
	BackendPort int    `json:"backend_port"` // Port number of the ssh server (22, 2222, ...)
}

// AgentPortInfo represents the port information for the agent.
type AgentPortInfo struct {
	RemotePort  int    `json:"rport"`        // Remote port on the backend server
	LocalPort   int    `json:"lport"`        // Local port on the IoT device
}