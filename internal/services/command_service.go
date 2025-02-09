package services

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	MQTT "github.com/eclipse/paho.mqtt.golang"

	"github.com/sirupsen/logrus"
)

// CommandService defines the structure and functionality of the command service
type CommandService struct {
	SubTopic         string
	DeviceInfo       identity.DeviceInfoInterface
	QOS              int
	MqttClient       mqtt.MQTTClient
	Logger           *logrus.Logger
	OutputSizeLimit  int
	MaxExecutionTime int
}

// NewCommandService initializes a new CommandService
func NewCommandService(subTopic string, deviceInfo identity.DeviceInfoInterface, qos int, mqttClient mqtt.MQTTClient, logger *logrus.Logger) *CommandService {
	return &CommandService{
		SubTopic:   subTopic,
		DeviceInfo: deviceInfo,
		QOS:        qos,
		MqttClient: mqttClient,
		Logger:     logger,
	}
}

// Start initializes the command subscription and starts listening for incoming commands
func (cs *CommandService) Start() error {
	topic := cs.SubTopic + "/" + cs.DeviceInfo.GetDeviceID()
	cs.Logger.Infof("Starting CommandService. Subscribing to topic: %s", topic)

	token := cs.MqttClient.Subscribe(topic, byte(cs.QOS), cs.handleCommand)
	token.Wait()
	if err := token.Error(); err != nil {
		cs.Logger.Errorf("Failed to subscribe to topic %s: %v", cs.SubTopic, err)
		return err
	}

	cs.Logger.Infof("Subscribed to topic: %s", topic)
	return nil
}

// handleCommand processes incoming commands, executes them, and publishes the output
func (cs *CommandService) handleCommand(client MQTT.Client, msg MQTT.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	cs.Logger.Infof("Received command on topic %s: %s", topic, string(payload))

	output, err := cs.executeCommand(string(payload))
	if err != nil {
		cs.Logger.Errorf("Failed to execute command: %v", err)
		output = fmt.Sprintf("Error executing command: %v", err)
	}

	// Limit output size
	if len(output) > cs.OutputSizeLimit {
		output = output[:cs.OutputSizeLimit] + "... (truncated)"
		cs.Logger.Warnf("Output truncated due to size limit")
	}

	err = cs.publishOutput(output)
	if err != nil {
		cs.Logger.Errorf("Failed to publish command output: %v", err)
	}
}

// executeCommand runs the command on the system and returns its output or error
func (cs *CommandService) executeCommand(cmd string) (string, error) {
	cs.Logger.Infof("Executing command: %s", cmd)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cs.MaxExecutionTime)*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			cs.Logger.Errorf("Command execution timed out")
			return "", ctx.Err()
		}
		cs.Logger.Errorf("Command execution failed: %v", err)
		return stderr.String(), err
	}

	return stdout.String(), nil
}

// publishOutput sends the command output to the MQTT topic
func (cs *CommandService) publishOutput(output string) error {
	topic := cs.SubTopic + "/response/" + cs.DeviceInfo.GetDeviceID()
	cs.Logger.Infof("Publishing output to topic: %s", topic)

	token := cs.MqttClient.Publish(topic, byte(cs.QOS), false, []byte(output))

	token.Wait()
	if err := token.Error(); err != nil {
		cs.Logger.Errorf("Failed to publish output: %v", err)
		return err
	}

	cs.Logger.Infof("Output successfully published to topic: %s", topic)
	return nil
}
