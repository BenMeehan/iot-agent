package models

import "time"

// Heartbeat represents the structure for a device heartbeat event.
// It captures essential information about the device's health and connectivity status.
type Heartbeat struct {
	// DeviceID is a unique identifier for the device sending the heartbeat.
	DeviceID string `json:"device_id" gorm:"column:device_id"`

	// Timestamp records the exact time when the heartbeat event was generated.
	Timestamp time.Time `json:"timestamp" gorm:"column:timestamp"`

	// Status indicates the current state of the device (e.g., "online", "offline", "error").
	Status string `json:"status" gorm:"column:status"`

	// JWTToken is an authentication token used to verify the identity of the device.
	// This helps in ensuring secure communication between the device and the server.
	JWTToken string `json:"jwt_token" gorm:"column:jwt_token"`
}
