package identity

import (
	"encoding/json"
	"os"

	"github.com/benmeehan/iot-agent/pkg/file"
)

// Identity holds the device's unique identifier and other metadata.
type Identity struct {
	ID       string          `json:"device_id,omitempty"`   // Unique identifier for the device
	Name     string          `json:"device_name,omitempty"` // Name of the device
	OrgID    string          `json:"org_id,omitempty"`      // Organization ID associated with the device
	Metadata json.RawMessage `json:"metadata,omitempty"`    // Additional metadata for the device
}

// DeviceInfoInterface defines the methods that DeviceInfo implements.
type DeviceInfoInterface interface {
	LoadDeviceInfo() error              // Loads device information from the file
	SaveDeviceID(deviceID string) error // Saves the device ID to the file
	GetDeviceID() string                // Retrieves the current device ID
	GetDeviceIdentity() *Identity       // Retrieves the current device identity
}

// DeviceInfo manages the device identity and its associated file operations.
type DeviceInfo struct {
	DeviceInfoFile string              // Path to the device info file
	Identity       Identity            // Current device identity
	fileOps        file.FileOperations // Interface for file operations
}

// NewDeviceInfo initializes a new DeviceInfo instance with the specified file path and file operations.
// Returns a pointer to a DeviceInfo instance with the file path and file operations set.
func NewDeviceInfo(filePath string, fileOps file.FileOperations) DeviceInfoInterface {
	return &DeviceInfo{
		DeviceInfoFile: filePath,
		fileOps:        fileOps,
		Identity:       Identity{},
	}
}

// LoadDeviceInfo reads the device information from the file and populates the Identity field.
// If the file does not exist or is empty, it initializes the Identity to default (empty) values.
func (d *DeviceInfo) LoadDeviceInfo() error {
	// Read the device info file
	data, err := d.fileOps.ReadFile(d.DeviceInfoFile)
	if err != nil {
		if os.IsNotExist(err) {
			// File does not exist, initialize with default empty values
			d.Identity = Identity{}
			return nil
		}
		return err
	}

	// Check if the file content is empty
	if len(data) == 0 {
		d.Identity = Identity{}
		return nil
	}

	// Attempt to unmarshal the JSON content into the Identity field
	if err := json.Unmarshal([]byte(data), &d.Identity); err != nil {
		return err
	}

	return nil
}

// GetDeviceIdentity returns the current device Identity.
func (d *DeviceInfo) GetDeviceIdentity() *Identity {
	return &d.Identity
}

// GetDeviceID returns the current device ID.
func (d *DeviceInfo) GetDeviceID() string {
	return d.Identity.ID
}

// SaveDeviceID updates the device ID in the Identity field and writes it back to the file.
// Returns an error if marshalling the Identity or writing the file fails.
func (d *DeviceInfo) SaveDeviceID(deviceID string) error {
	// Update the device ID in the Identity field.
	d.Identity.ID = deviceID

	// Marshal the Identity to JSON format
	data, err := json.Marshal(d.Identity)
	if err != nil {
		return err
	}

	// Write the serialized JSON to the file
	if err := d.fileOps.WriteFile(d.DeviceInfoFile, string(data)); err != nil {
		return err
	}

	return nil
}
