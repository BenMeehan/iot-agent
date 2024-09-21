package identity

import (
	"encoding/json"
	"os"

	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/sirupsen/logrus"
)

// Identity holds the device's unique identifier.
type Identity struct {
	ID string `json:"device_id"`
}

// DeviceInfoInterface defines the methods that DeviceInfo implements
type DeviceInfoInterface interface {
	LoadDeviceInfo() error
	GetDeviceID() string
	SaveDeviceID(deviceID string) error
}

// DeviceInfo manages the device identity and its associated file operations.
type DeviceInfo struct {
	DeviceInfoFile string
	Config         Identity
	fileOps        file.FileOperations
}

// NewDeviceInfo initializes a new DeviceInfo instance with the specified file path and file operations.
// Returns a pointer to a DeviceInfo instance with the file path and file operations set.
func NewDeviceInfo(filePath string, fileOps file.FileOperations) DeviceInfoInterface {
	return &DeviceInfo{
		DeviceInfoFile: filePath,
		fileOps:        fileOps,
	}
}

// LoadDeviceInfo reads the device information from the file and populates the Config field.
// If the file does not exist or is empty, it initializes the Config to default (empty) values.
func (d *DeviceInfo) LoadDeviceInfo() error {
	// Read the device info file
	data, err := d.fileOps.ReadFile(d.DeviceInfoFile)
	if err != nil {
		if os.IsNotExist(err) {
			// File does not exist, initialize with default empty values
			logrus.Warnf("Device info file does not exist: %s, initializing with default values", d.DeviceInfoFile)
			d.Config = Identity{}
			return nil
		}
		logrus.WithError(err).Error("Failed to read device info file")
		return err
	}

	// Check if the file content is empty
	if len(data) == 0 {
		logrus.Warnf("Device info file is empty: %s, initializing with default values", d.DeviceInfoFile)
		d.Config = Identity{}
		return nil
	}

	// Attempt to unmarshal the JSON content into the Config field
	if err := json.Unmarshal([]byte(data), &d.Config); err != nil {
		logrus.WithError(err).Error("Failed to parse device info from file")
		return err
	}

	return nil
}

// GetDeviceID returns the current device ID from the Config field.
func (d *DeviceInfo) GetDeviceID() string {
	return d.Config.ID
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

	if err := d.fileOps.WriteFile(d.DeviceInfoFile, string(data)); err != nil {
		logrus.WithError(err).Error("failed to write device info to file")
		return err
	}

	return nil
}
