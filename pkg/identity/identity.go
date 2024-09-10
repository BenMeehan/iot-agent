package identity

import (
	"encoding/json"
	"os"

	"github.com/sirupsen/logrus"
)

// Identity holds the device's unique identifier.
type Identity struct {
	ID string `json:"device_id"`
}

// DeviceInfo manages the device identity and its associated file operations.
type DeviceInfo struct {
	DeviceInfoFile string
	Config         Identity
}

// Init initializes a new DeviceInfo instance with the specified file path.
// Returns a pointer to a DeviceInfo instance with the file path set.
func Init(filePath string) *DeviceInfo {
	return &DeviceInfo{
		DeviceInfoFile: filePath,
	}
}

// LoadDeviceInfo reads the device information from the file and populates the Config field.
// Returns an error if reading the file or unmarshalling the JSON fails.
func (d *DeviceInfo) LoadDeviceInfo() error {
	data, err := os.ReadFile(d.DeviceInfoFile)
	if err != nil {
		logrus.WithError(err).Error("failed to read device info file")
		return err
	}

	if err := json.Unmarshal(data, &d.Config); err != nil {
		logrus.WithError(err).Error("failed to parse device info from file")
		return err
	}

	return nil
}

// GetDeviceID returns the current device ID from the Config field.
func (d *DeviceInfo) GetDeviceID() (string, error) {
	return d.Config.ID, nil
}

// SaveDeviceID updates the device ID in the Config field and writes it back to the file.
// Returns an error if marshalling the Config or writing the file fails.
func (d *DeviceInfo) SaveDeviceID(deviceID string) error {
	// Update the device ID in the Config field.
	d.Config.ID = deviceID

	data, err := json.Marshal(d.Config)
	if err != nil {
		logrus.WithError(err).Error("failed to serialize device info")
		return err
	}

	if err := os.WriteFile(d.DeviceInfoFile, data, 0644); err != nil {
		logrus.WithError(err).Error("failed to write device info to file")
		return err
	}

	return nil
}
