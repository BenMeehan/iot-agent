package models

// UpdateInstruction represents an instruction for a file operation during an update process.
//
// Fields:
//   TargetPath: The path of the target file to be modified or replaced.
//               If "-", it indicates a new file operation.
//   NewFile: The name of the new file to be added or replaced at the TargetPath.
//            If "-", it indicates a delete operation.
type UpdateInstruction struct {
	TargetPath string `json:"target_path"` // Path of the file to be modified, replaced, or deleted
	NewFile    string `json:"new_file"`    // Name of the new file or "-" for delete operations
}

// UpdateCommandPayload defines the structure of a command payload for initiating an update.
//
// Fields:
//   UpdateURL: The URL from where the update files should be downloaded.
//   Version: The version of the update, used to check if the update is newer than the current version.
type UpdateCommandPayload struct {
	UpdateURL string `json:"update_url"` // URL to fetch the update files
	Version   string `json:"version"`    // Version of the update package
}
