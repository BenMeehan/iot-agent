package utils

import (
	"os"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// Config represents the structure of the configuration file.
type Config struct {
	MQTT struct {
		Broker        string `yaml:"broker"`         // MQTT broker address
		ClientID      string `yaml:"client_id"`      // MQTT client ID
		CACertificate string `yaml:"ca_certificate"` // Path to the CA certificate
	} `yaml:"mqtt"`

	Identity struct {
		DeviceFile string `yaml:"device_file"` // Path to the device identity file
	} `yaml:"identity"`

	Services struct {
		Registration struct {
			Topic             string `yaml:"topic"`               // MQTT topic for registration service
			MaxBackoffSeconds int    `yaml:"max_backoff_seconds"` // Maximum backoff time for registration retries
			Enabled           bool   `yaml:"enabled"`             // Enable/disable registration service
			QOS               int    `yaml:"qos"`                 // MQTT QoS level for registration messages
		} `yaml:"registration"`

		Heartbeat struct {
			Topic    string `yaml:"topic"`    // MQTT topic for heartbeat service
			Enabled  bool   `yaml:"enabled"`  // Enable/disable heartbeat service
			Interval int    `yaml:"interval"` // Interval between heartbeats (in seconds)
			QOS      int    `yaml:"qos"`      // MQTT QoS level for heartbeat messages
		} `yaml:"heartbeat"`

		Metrics struct {
			Topic             string `yaml:"topic"`               // MQTT topic for metrics service
			Enabled           bool   `yaml:"enabled"`             // Enable/disable metrics service
			MetricsConfigFile string `yaml:"metrics_config_file"` // Path to the metrics configuration file
			Interval          int    `yaml:"interval"`            // Interval for sending metrics (in seconds)
			QOS               int    `yaml:"qos"`                 // MQTT QoS level for metrics messages
		} `yaml:"metrics"`

		Command struct {
			Topic            string `yaml:"topic"`              // MQTT topic for command service
			Enabled          bool   `yaml:"enabled"`            // Enable/disable command service
			OutputSizeLimit  int    `yaml:"output_size_limit"`  // Maximum size of command output in bytes
			MaxExecutionTime int    `yaml:"max_execution_time"` // Maximum execution time for commands (in seconds)
			QOS              int    `yaml:"qos"`                // MQTT QoS level for command service messages
		} `yaml:"command_service"`

		SSH struct {
			Topic          string `yaml:"topic"`            // MQTT topic for SSH service
			Enabled        bool   `yaml:"enabled"`          // Enable/disable SSH service
			BackendHost    string `yaml:"backend_host"`     // Host address for the SSH backend service
			BackendPort    int    `yaml:"backend_port"`     // Host port for the SSH backend service
			SSHUser        string `yaml:"ssh_user"`         // SSH username for connecting to the backend
			PrivateKeyPath string `yaml:"private_key_path"` // Path to the device's private key
			QOS            int    `yaml:"qos"`              // MQTT QoS level for ssh service messages
		} `yaml:"ssh"`

		Location struct {
			Topic             string `yaml:"topic"`           // MQTT topic for location service
			Enabled           bool   `yaml:"enabled"`         // Enable/disable location service
			Interval          int    `yaml:"interval"`        // Interval between geo-location messages (in seconds)
			QOS               int    `yaml:"qos"`             // MQTT QoS level for location messages
			SensorBased       bool   `yaml:"sensor_based"`    // Use sensor or geo-location api
			MapsAPIKey        string `yaml:"maps_api_key"`    // Google maps API Key
			GPSDeviceBaudRate int    `yaml:"gps_baud_rate"`   // The Baud rate for GPS sensor
			GPSDevicePort     string `yaml:"gps_device_port"` // UNIX Port where the GPS sensor is mounted
		} `yaml:"location_service"`

		Update struct {
			Topic          string `yaml:"topic"`            // MQTT topic for update service
			QOS            int    `yaml:"qos"`              // MQTT QoS level for update messages
			UpdateFilePath string `yaml:"update_file_path"` // Path to save downloaded update files
			StateFile      string `yaml:"state_file"`       // Path to store the update state
			Enabled        bool   `yaml:"enabled"`          // Enable/disable update service
		} `yaml:"update_service"`
	} `yaml:"services"`

	Security struct {
		JWTFile    string `yaml:"jwt_file"`     // Path to the JWT token file
		AESKeyFile string `yaml:"aes_key_file"` // Path to the AES key file
	}
}

// LoadConfig loads the YAML configuration from the specified file.
// It returns a pointer to the Config struct and an error if loading fails.
func LoadConfig(filename string, logger *logrus.Logger) (*Config, error) {
	logger.Infof("Loading configuration from file: %s", filename)

	file, err := os.Open(filename)
	if err != nil {
		logger.Errorf("Failed to open config file: %v", err)
		return nil, err
	}
	defer file.Close()

	// Decode the YAML file into the Config struct
	var config Config
	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		logger.Errorf("Failed to decode config file: %v", err)
		return nil, err
	}

	logger.Infof("Configuration loaded successfully from %s", filename)
	return &config, nil
}
