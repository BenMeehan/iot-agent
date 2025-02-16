package services

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
)

// CommandService defines the structure and functionality of the command service
type CommandService struct {
	SubTopic         string
	DeviceInfo       identity.DeviceInfoInterface
	QOS              int
	MqttClient       mqtt.MQTTClient
	Logger           zerolog.Logger
	OutputSizeLimit  int
	MaxExecutionTime int
	stopChan         chan struct{}
	wg               sync.WaitGroup
}

// NewCommandService initializes a new CommandService
func NewCommandService(subTopic string, deviceInfo identity.DeviceInfoInterface, qos int, mqttClient mqtt.MQTTClient, logger zerolog.Logger, outputSizeLimit, maxExecutionTime int) *CommandService {
	return &CommandService{
		SubTopic:         subTopic,
		DeviceInfo:       deviceInfo,
		QOS:              qos,
		MqttClient:       mqttClient,
		Logger:           logger,
		OutputSizeLimit:  outputSizeLimit,
		MaxExecutionTime: maxExecutionTime,
		stopChan:         make(chan struct{}),
	}
}

// Start initializes the command subscription and starts listening for incoming commands
func (cs *CommandService) Start() error {
	topic := cs.SubTopic + "/" + cs.DeviceInfo.GetDeviceID()
	cs.Logger.Info().Str("topic", topic).Msg("Starting CommandService and subscribing")

	token := cs.MqttClient.Subscribe(topic, byte(cs.QOS), cs.handleCommand)
	token.Wait()
	if err := token.Error(); err != nil {
		cs.Logger.Error().Err(err).Str("topic", topic).Msg("Failed to subscribe")
		return err
	}

	cs.Logger.Info().Str("topic", topic).Msg("Subscribed successfully")
	return nil
}

// Stop unsubscribes from MQTT topic and stops the service gracefully
func (cs *CommandService) Stop() error {
	close(cs.stopChan)
	cs.wg.Wait() // Wait for any running commands to finish

	topic := cs.SubTopic + "/" + cs.DeviceInfo.GetDeviceID()
	cs.Logger.Info().Str("topic", topic).Msg("Unsubscribing from topic")

	token := cs.MqttClient.Unsubscribe(topic)
	token.Wait()
	if err := token.Error(); err != nil {
		cs.Logger.Error().Err(err).Str("topic", topic).Msg("Failed to unsubscribe")
		return err
	}

	cs.Logger.Info().Str("topic", topic).Msg("Successfully unsubscribed")
	return nil
}

// handleCommand processes incoming commands, executes them, and publishes the output
func (cs *CommandService) handleCommand(client MQTT.Client, msg MQTT.Message) {
	select {
	case <-cs.stopChan:
		cs.Logger.Warn().Msg("Received command but service is stopping")
		return
	default:
	}

	cs.wg.Add(1)
	defer cs.wg.Done()

	topic := msg.Topic()
	payload := msg.Payload()

	cs.Logger.Info().Str("topic", topic).Str("command", string(payload)).Msg("Received command")

	output, err := cs.executeCommand(string(payload))
	if err != nil {
		cs.Logger.Error().Err(err).Msg("Failed to execute command")
		output = fmt.Sprintf("Error executing command: %v", err)
	}

	// Limit output size
	if len(output) > cs.OutputSizeLimit {
		output = output[:cs.OutputSizeLimit] + "... (truncated)"
		cs.Logger.Warn().Int("limit", cs.OutputSizeLimit).Msg("Output truncated")
	}

	if err := cs.publishOutput(output); err != nil {
		cs.Logger.Error().Err(err).Msg("Failed to publish command output")
	}
}

// executeCommand runs the command on the system and returns its output or error
func (cs *CommandService) executeCommand(cmd string) (string, error) {
	cs.Logger.Info().Str("command", cmd).Msg("Executing command")

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cs.MaxExecutionTime)*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			cs.Logger.Error().Msg("Command execution timed out")
			return "", ctx.Err()
		}
		cs.Logger.Error().Err(err).Msg("Command execution failed")
		return stderr.String(), err
	}

	return stdout.String(), nil
}

// publishOutput sends the command output to the MQTT topic
func (cs *CommandService) publishOutput(output string) error {
	topic := cs.SubTopic + "/response/" + cs.DeviceInfo.GetDeviceID()
	cs.Logger.Info().Str("topic", topic).Msg("Publishing output")

	token := cs.MqttClient.Publish(topic, byte(cs.QOS), false, []byte(output))

	token.Wait()
	if err := token.Error(); err != nil {
		cs.Logger.Error().Err(err).Str("topic", topic).Msg("Failed to publish output")
		return err
	}

	cs.Logger.Info().Str("topic", topic).Msg("Output published successfully")
	return nil
}
