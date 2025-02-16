package identity

import (
	"encoding/json"
	"os"

	"github.com/benmeehan/iot-agent/pkg/file"
)

// Identity holds the device's unique identifier and other metadata.
type Identity struct {
	ID       string          `json:"device_id,omitempty"`
	Name     string          `json:"device_name,omitempty"`
	OrgID    string          `json:"org_id,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// DeviceInfoInterface defines methods for managing device identity.
type DeviceInfoInterface interface {
	LoadDeviceInfo() error
	SaveDeviceID(deviceID string) error
	GetDeviceID() string
	GetDeviceIdentity() *Identity
}

// DeviceInfo manages the device identity and its associated file operations.
type DeviceInfo struct {
	DeviceInfoFile string
	Identity       Identity
	fileOps        file.FileOperations
}

// NewDeviceInfo initializes a new DeviceInfo instance.
func NewDeviceInfo(filePath string, fileOps file.FileOperations) DeviceInfoInterface {
	return &DeviceInfo{
		DeviceInfoFile: filePath,
		fileOps:        fileOps,
		Identity:       Identity{},
	}
}

// LoadDeviceInfo reads the device information from the file and populates the Identity field.
func (d *DeviceInfo) LoadDeviceInfo() error {
	err := d.fileOps.ReadJsonFile(d.DeviceInfoFile, &d.Identity)
	if err != nil {
		if os.IsNotExist(err) {
			// File does not exist, initialize with default empty values
			d.Identity = Identity{}
			return nil
		}
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
func (d *DeviceInfo) SaveDeviceID(deviceID string) error {
	d.Identity.ID = deviceID
	return d.fileOps.WriteJsonFile(d.DeviceInfoFile, d.Identity)
}
