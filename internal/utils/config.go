package utils

import (
	"io/ioutil"
	"os"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// Config represents the YAML configuration file structure
type Config struct {
	MQTT     MQTTConfig               `yaml:"mqtt"`
	Services map[string]ServiceConfig `yaml:"services"`
}

type MQTTConfig struct {
	Broker   string `yaml:"broker"`
	ClientID string `yaml:"client_id"`
}

type ServiceConfig struct {
	Topic   string `yaml:"topic"`
	Enabled bool   `yaml:"enabled"`
}

// LoadConfig loads the YAML configuration file and unmarshals it into a Config struct
func LoadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		logrus.WithError(err).Error("Unable to open config file")
		return nil, err
	}
	defer file.Close()

	bytes, err := ioutil.ReadAll(file)
	if err != nil {
		logrus.WithError(err).Error("Error reading config file")
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(bytes, &config); err != nil {
		logrus.WithError(err).Error("Error parsing config YAML")
		return nil, err
	}

	logrus.Info("Configuration loaded successfully")
	return &config, nil
}
