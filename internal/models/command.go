package models

// CmdRequest represents a command received by the device from the server.
type CmdRequest struct {
	Command  string `json:"command"`   // The encrypted command that the device must execute.
	DeviceID string `json:"device_id"` // ID of the current device receiving the command.
	UserID   string `json:"user_id"`   // The ID of the user who initiated the command.
}

// CmdResponse represents the response sent by the device after executing a command.
type CmdResponse struct {
	UserID   string `json:"user_id"`   // The ID of the user who triggered the command.
	DeviceID string `json:"device_id"` // The current device ID sending the response.
	Response string `json:"output"`    // The result/output of the executed command.
}
