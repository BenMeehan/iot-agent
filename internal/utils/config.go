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
			Topic            string `yaml:"topic"`              // MQTT topic for registration service
			Enabled          bool   `yaml:"enabled"`            // Enable/disable registration service
			DeviceSecretFile string `yaml:"device_secret_file"` // Path to the device secret file
			QOS              int    `yaml:"qos"`                // MQTT QoS level for registration messages
			DeviceIDFile     string `yaml:"deviceid_file"`      // Path to the device ID file (if any)
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
	} `yaml:"services"`
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
