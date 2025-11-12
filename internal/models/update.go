package models

import (
	"time"
)

type UpdateCommandPayload struct {
	UpdateId       string `json:"update_id,omitempty"`
	UpdateVersion  string `json:"artifact_version,omitempty"`
	FileName       string `json:"update_file_name,omitempty"`
	FileUrl        string `json:"update_file_url,omitempty"`
	FileSize       string `json:"update_file_size,omitempty"`
	UpdateStatus   string `json:"update_status,omitempty"`
	SHA256Checksum string `json:"checksum,omitempty"`
	ManifestData   string `json:"manifest,omitempty"`
}

type Partition struct {
	Device     string `json:"device,omitempty"`
	MountPoint string `json:"mountPoint,omitempty"`
	PARTUUID   string `json:"partUUID,omitempty"`
}

type PartitionMetadata struct {
	TimeStamp         time.Time       `json:"time_stamp,omitempty"`
	ActivePartition   Partition       `json:"active_partition,omitempty"`
	InActivePartition Partition       `json:"inactive_partition,omitempty"`
	Update            UpdatesMetaData `json:"update,omitempty"`
}

type UpdatesMetaData struct {
	TimeStamp      time.Time `json:"time_stamp,omitempty"`
	UpdateId       string    `json:"update_id"`
	FileName       string    `json:"file_name"`
	FileUrl        string    `json:"update_file_url,omitempty"`
	FileSize       string    `json:"file_size,omitempty"`
	Version        string    `json:"artifact_version"`
	SHA256Checksum string    `json:"checksum"`
	Status         string    `json:"status,omitempty"`
	ErrorLog       string    `json:"error_log,omitempty"`
	ManifestData   string    `json:"manifest"`
}

type Ack struct {
	DeviceId        string `json:"device_id"`
	ActivePartition string `json:"active_partition"`
	UpdatePartition string `json:"update_partition"`
	Status          string `json:"status"`
	Error           string `json:"error"`
}

type AckResponse struct {
	Status  string `json:"status"`
	Message Ack    `json:"message"`
}

type Manifest struct {
	OSUpdate string `json:"os_update"`
	Updates  Update `json:"updates"`
}

type Update struct {
	FileUpdates   []FileUpdate   `json:"file_update"`
	FolderUpdates []FolderUpdate `json:"folder_update"`
}

type FileUpdate struct {
	Path   string `json:"path"`
	Update string `json:"update"`
}

type FolderUpdate struct {
	Path      string `json:"path"`
	Update    string `json:"update"`
	Overwrite bool   `json:"overwrite"`
}
