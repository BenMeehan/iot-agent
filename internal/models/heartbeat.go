package models

import "time"

// Heartbeat represents the structure for a device heartbeat event.
// It captures essential information about the device's health and connectivity status.
type Heartbeat struct {
	// DeviceID is a unique identifier for the device sending the heartbeat.
	DeviceID string `json:"device_id"`

	// Timestamp records the exact time when the heartbeat event was generated.
	Timestamp time.Time `json:"timestamp"`

	// Status indicates the current state of the device (e.g., "online", "offline", "error").
	Status string `json:"status"`
}
