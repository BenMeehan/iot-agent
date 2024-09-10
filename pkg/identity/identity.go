package identity

import (
	"encoding/json"
	"fmt"
	"os"
)

// Identity represents the structure of the device information.
type Identity struct {
	ID string `json:"device_id"` // DeviceID is the unique identifier for the device.
}

// DeviceInfo manages the device information and the associated file operations.
type DeviceInfo struct {
	DeviceInfoFile string   // Path to the file where device information is stored.
	Config         Identity // Device information configuration.
}

// Init initializes a new DeviceInfo instance with the specified file path.
// This function returns a pointer to a DeviceInfo instance with the DeviceInfoFile set.
func Init(file string) *DeviceInfo {
	return &DeviceInfo{
		DeviceInfoFile: file,
	}
}

// LoadDeviceInfo reads the device information from the file and populates the Config field.
// It returns an error if reading or unmarshalling the file fails.
func (i *DeviceInfo) LoadDeviceInfo() error {
	// Read the content of the device info file.
	data, err := os.ReadFile(i.DeviceInfoFile)
	if err != nil {
		return fmt.Errorf("failed to read device ID file: %w", err)
	}

	// Unmarshal the JSON data into the Config field.
	if err := json.Unmarshal(data, &i.Config); err != nil {
		return fmt.Errorf("failed to parse device info from file: %w", err)
	}

	return nil
}

// GetDeviceID returns the current device ID from the Config field.
// It returns the device ID as a string and any error encountered.
func (i *DeviceInfo) GetDeviceID() (string, error) {
	return i.Config.ID, nil
}

// SaveDeviceID updates the device ID in the Config field and writes it to the file.
// It returns an error if marshalling the Config or writing the file fails.
func (i *DeviceInfo) SaveDeviceID(deviceID string) error {
	// Update the device ID in the Config field.
	i.Config.ID = deviceID

	// Marshal the updated Config into JSON format.
	data, err := json.Marshal(i.Config)
	if err != nil {
		return fmt.Errorf("failed to serialize device info: %w", err)
	}

	// Write the JSON data to the file.
	if err := os.WriteFile(i.DeviceInfoFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write device ID to file: %w", err)
	}

	return nil
}
