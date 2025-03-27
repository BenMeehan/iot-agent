package models

import "encoding/json"

// UpdateInstruction represents an instruction for a file operation during an update process.
//
// Fields:
//   TargetPath: The path of the target file to be modified or replaced.
//               If "-", it indicates a new file operation.
//   NewFile: The name of the new file to be added or replaced at the TargetPath.
//            If "-", it indicates a delete operation.
type UpdateInstruction struct {
	IsDir  bool            `json:"isDir"`
	Create []string        `json:"create,omitempty"`
	Delete []string        `json:"delete,omitempty"`
	Modify json.RawMessage `json:"delete,omitempty"`
}

// UpdateCommandPayload defines the structure of a command payload for initiating an update.
//
// Fields:
//   UpdateURL: The URL from where the update files should be downloaded.
//   Version: The version of the update, used to check if the update is newer than the current version.
type UpdateCommandPayload struct {
	ID             string `json:"id,omitempty"`
	UpdateVersion  string `json:"update_version,omitempty"`
	FileUrl        string `json:"update_file_url,omitempty"`
	UpdateStatus   string `json:"update_status,omitempty"`
	SHA256Checksum string `json:"checksum,omitempty"`
}
