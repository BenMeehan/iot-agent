package models

import (
	"encoding/json"
	"time"
)

// UpdateInstruction represents an instruction for a file operation during an update process.
//
// Fields:
//
//	TargetPath: The path of the target file to be modified or replaced.
//	            If "-", it indicates a new file operation.
//	NewFile: The name of the new file to be added or replaced at the TargetPath.
//	         If "-", it indicates a delete operation.
type UpdateInstruction struct {
	IsDir  bool            `json:"isDir"`
	Create []string        `json:"create,omitempty"`
	Delete []string        `json:"delete,omitempty"`
	Modify json.RawMessage `json:"delete,omitempty"`
}

// UpdateCommandPayload defines the structure of a command payload for initiating an update.
//
// Fields:
//
//	UpdateURL: The URL from where the update files should be downloaded.
//	Version: The version of the update, used to check if the update is newer than the current version.
type UpdateCommandPayload struct {
	UpdateId       string `json:"update_id,omitempty"`
	UpdateVersion  string `json:"update_version,omitempty"`
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
	Version        string    `json:"version"`
	SHA256Checksum string    `json:"checksum"`
	Status         string    `json:"status,omitempty"`
	ErrorLog       string    `json:"error_log,omitempty"`
	ManifestData   string    `json:"manifest"`
}

type StatusUpdatePayload struct {
	UpdateId string `json:"update_id"`
	DeviceId string `json:"device_id"`
	Status   string `json:"status"`
}

type Ack struct {
	UpdateId string `json:"update_id"`
	DeviceId string `json:"device_id"`
	Status   string `json:"status"`
	Error    string `json:"error"`
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
