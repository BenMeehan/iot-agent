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
	ID             string `json:"id,omitempty"`
	UpdateVersion  string `json:"update_version,omitempty"`
	FileName       string `json:"update_file_name,omitempty"`
	FileUrl        string `json:"update_file_url,omitempty"`
	UpdateStatus   string `json:"update_status,omitempty"`
	SHA256Checksum string `json:"checksum,omitempty"`
}

type Partition struct {
	Device     string `json:"device,omitempty"`
	MountPoint string `json:"mountPoint,omitempty"`
	PARTUUID   string `json:"partUUID,omitempty"`
}

type PartitionMetadata struct {
	TimeStamp         time.Time         `json:"time_stamp,omitempty"`
	ActivePartition   Partition         `json:"active_partition,omitempty"`
	InActivePartition Partition         `json:"inactive_partition,omitempty"`
	Updates           []UpdatesMetaData `json:"updates,omitempty"`
}

type UpdatesMetaData struct {
	TimeStamp time.Time `json:"time_stamp,omitempty"`
	Status    string    `json:"status,omitempty"`
	ErrorLog  string    `json:"error_log,omitempty"`
}

type StatusUpdatePayload struct {
	UpdateId string `json:"update_id"`
	DeviceId string `json:"device_id"`
	Status   string `json:"status"`
}

type Ack struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}
