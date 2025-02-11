package models

import "time"

// Heartbeat represents the structure for a device heartbeat event.
type Heartbeat struct {
	DeviceID  string    `json:"device_id" gorm:"column:device_id"`
	Timestamp time.Time `json:"timestamp" gorm:"column:timestamp"`
	Status    string    `json:"status" gorm:"column:status"`
	JWTToken  string    `json:"jwt_token" gorm:"column:jwt_token"`
}
