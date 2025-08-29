package models

import "time"

// CmdRequest represents a command received by the device from the server.
type CmdRequest struct {
	Command     string `json:"command"`      // The encrypted command that the device must execute.
	ExecutionID string `json:"execution_id"` // A unique ID for this command execution.
}

// CmdResponse represents the response sent by the device after executing a command.
type CmdResponse struct {
	UserID   string `json:"user_id"`   // The ID of the user who triggered the command.
	DeviceID string `json:"device_id"` // The current device ID sending the response.
	Response string `json:"output"`    // The result/output of the executed command.
}

// CommandState represents the state of a command in the state file
type CommandState struct {
	ExecutionID string    `json:"execution_id"`         // Unique ID for the command execution
	Command     string    `json:"command"`              // The command that was executed
	Status      string    `json:"status"`               // Current status of the command (e.g., in_progress, success, failed)
	CreatedAt   time.Time `json:"created_at"`           // Timestamp when the command state was created
	StartedAt   time.Time `json:"started_at,omitempty"` // Timestamp when the command execution started
}

// StatusUpdate represents the status update payload sent by a device
type StatusUpdate struct {
	DeviceID    string `json:"device_id"`             // Added device ID
	ExecutionID string `json:"execution_id"`          // Added execution ID
	Status      string `json:"status"`                // Status of the command: in_progress, success, failed
	Output      string `json:"output,omitempty"`      // Optional, only for success/failed
	StartedAt   int64  `json:"started_at,omitempty"`  // Optional, only for in_progress
	FinishedAt  int64  `json:"finished_at,omitempty"` // Optional, only for success/failed
}
