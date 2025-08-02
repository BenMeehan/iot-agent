package utils

import (
	"time"

	"github.com/benmeehan/iot-agent/pkg/file"
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
			Topic           string        `yaml:"topic"`            // MQTT topic for registration service
			Enabled         bool          `yaml:"enabled"`          // Enable/disable registration service
			QOS             int           `yaml:"qos"`              // MQTT QoS level for registration messages
			MaxRetries      int           `yaml:"max_retries"`      // Maximum number of retry attempts
			BaseDelay       time.Duration `yaml:"base_delay"`       // Initial delay between retries
			MaxBackoff      time.Duration `yaml:"max_backoff"`      // Maximum backoff time for registration retries
			ResponseTimeout time.Duration `yaml:"response_timeout"` // Timeout for response per attempt
		} `yaml:"registration"`

		Heartbeat struct {
			Topic    string        `yaml:"topic"`    // MQTT topic for heartbeat service
			Enabled  bool          `yaml:"enabled"`  // Enable/disable heartbeat service
			Interval time.Duration `yaml:"interval"` // Interval between heartbeats (in seconds)
			QOS      int           `yaml:"qos"`      // MQTT QoS level for heartbeat messages
		} `yaml:"heartbeat"`

		Metrics struct {
			Topic             string        `yaml:"topic"`               // MQTT topic for metrics service
			Enabled           bool          `yaml:"enabled"`             // Enable/disable metrics service
			MetricsConfigFile string        `yaml:"metrics_config_file"` // Path to the metrics configuration file
			Interval          time.Duration `yaml:"interval"`            // Interval for sending metrics (in seconds)
			Timeout           time.Duration `yaml:"timeout"`             // Timeout for collecting metrics (in seconds)
			QOS               int           `yaml:"qos"`                 // MQTT QoS level for metrics messages
		} `yaml:"metrics"`

		Command struct {
			Topic            string `yaml:"topic"`              // MQTT topic for command service
			Enabled          bool   `yaml:"enabled"`            // Enable/disable command service
			OutputSizeLimit  int    `yaml:"output_size_limit"`  // Maximum size of command output in bytes
			MaxExecutionTime int    `yaml:"max_execution_time"` // Maximum execution time for commands (in seconds)
			QOS              int    `yaml:"qos"`                // MQTT QoS level for command service messages
		} `yaml:"command"`

		SSH struct {
			Topic               string        `yaml:"topic"`                  // MQTT topic for SSH service
			Enabled             bool          `yaml:"enabled"`                // Enable/disable SSH service
			SSHUser             string        `yaml:"ssh_user"`               // SSH username for connecting to the backend
			PrivateKeyPath      string        `yaml:"private_key_path"`       // Path to the device's private key
			ServerPublicKeyPath string        `yaml:"server_public_key_path"` // Path to the server's public key
			QOS                 int           `yaml:"qos"`                    // MQTT QoS level for SSH service messages
			MaxSSHConnections   int           `yaml:"max_ssh_connections"`    // Maximum number of concurrent SSH connections
			ConnectionTimeout   time.Duration `yaml:"connection_timeout"`     // Timeout duration for establishing an SSH connection
		} `yaml:"ssh"`

		Location struct {
			Topic             string        `yaml:"topic"`           // MQTT topic for location service
			Enabled           bool          `yaml:"enabled"`         // Enable/disable location service
			Interval          time.Duration `yaml:"interval"`        // Interval between geo-location messages (in seconds)
			QOS               int           `yaml:"qos"`             // MQTT QoS level for location messages
			SensorBased       bool          `yaml:"sensor_based"`    // Use sensor or geo-location api
			MapsAPIKey        string        `yaml:"maps_api_key"`    // Google maps API Key
			GPSDeviceBaudRate int           `yaml:"gps_baud_rate"`   // The Baud rate for GPS sensor
			GPSDevicePort     string        `yaml:"gps_device_port"` // UNIX Port where the GPS sensor is mounted
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
		JWTFile       string `yaml:"jwt_file"`        // Path to the JWT token file
		JWTSecretFile string `yaml:"jwt_secret_file"` // Path to the JWT secret file
		AESKeyFile    string `yaml:"aes_key_file"`    // Path to the AES key file
	}

	Middlewares struct {
		Authentication struct {
			Enabled            bool          `yaml:"enabled"`              // Enable/disable authentication middleware
			Topic              string        `yaml:"topic"`                // MQTT topic for authentication middleware
			QOS                int           `yaml:"qos"`                  // MQTT QoS level for authentication messages
			RetryDelay         time.Duration `yaml:"retry_delay"`          // Delay between retries (in seconds)
			RequestWaitingTime time.Duration `yaml:"request_waiting_time"` // Max Duration to wait for MQTT response (in seconds)
			AuthenticationCert string        `yaml:"authentication_cert"`  // Path to the authentication certificate
		} `yaml:"authentication"`
	} `yaml:"middlewares"`
}

// LoadConfig loads the YAML configuration from the specified file.
// It returns a pointer to the Config struct and an error if loading fails.
func LoadConfig(filename string, fileClient file.FileOperations) (*Config, error) {
	// Use the ReadYamlFile method from fileClient
	var config Config
	err := fileClient.ReadYamlFile(filename, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
