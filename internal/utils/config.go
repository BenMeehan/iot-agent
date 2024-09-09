package utils

import (
	"os"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type Config struct {
	MQTT struct {
		Broker        string `yaml:"broker"`
		ClientID      string `yaml:"client_id"`
		CACertificate string `yaml:"ca_certificate"`
	} `yaml:"mqtt"`

	Identity struct {
		DeviceFile string `yaml:"device_file"`
	} `yaml:"identity"`

	Services struct {
		Registration struct {
			Topic            string `yaml:"topic"`
			Enabled          bool   `yaml:"enabled"`
			DeviceSecretFile string `yaml:"device_secret_file"`
			QOS              int    `yaml:"qos"`
			DeviceIDFile     string `yaml:"deviceid_file"`
		} `yaml:"registration"`

		Heartbeat struct {
			Topic    string `yaml:"topic"`
			Enabled  bool   `yaml:"enabled"`
			Interval int    `yaml:"interval"`
			QOS      int    `yaml:"qos"`
		} `yaml:"heartbeat"`
	} `yaml:"services"`
}

// LoadConfig loads the YAML configuration file
func LoadConfig(filename string) (*Config, error) {
	logrus.Infof("Loading configuration from file: %s", filename)

	file, err := os.Open(filename)
	if err != nil {
		logrus.Errorf("Error opening config file: %v", err)
		return nil, err
	}
	defer file.Close()

	var config Config
	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		logrus.Errorf("Error decoding config file: %v", err)
		return nil, err
	}

	logrus.Infof("Configuration loaded successfully")
	return &config, nil
}
