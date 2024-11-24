package models

// SSHRequest represents the structure of the MQTT message that contains the SSH port request
type SSHRequest struct {
	LocalPort   int    `json:"lport"`
	RemotePort  int    `json:"rport"`
	BackendHost string `json:"backend_host"`
}
