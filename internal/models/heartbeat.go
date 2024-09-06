package models

import "time"

type Heartbeat struct {
	DeviceID  string    `json:"device_id" gorm:"column:device_id"`
	Timestamp time.Time `json:"timestamp" gorm:"column:timestamp"`
	Status    string    `json:"status" gorm:"column:status"`
}
