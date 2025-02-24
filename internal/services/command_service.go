package services

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
)

// CommandService manages execution of shell commands received via MQTT
// and publishes the results back to a response topic.
type CommandService struct {
	// Configuration Fields
	subTopic         string
	qos              int
	outputSizeLimit  int
	maxExecutionTime int

	// Dependencies
	mqttClient mqtt.MQTTClient
	deviceInfo identity.DeviceInfoInterface
	logger     zerolog.Logger

	// Internal state management
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// NewCommandService initializes a new CommandService with given parameters.
func NewCommandService(subTopic string, qos, outputSizeLimit, maxExecutionTime int, mqttClient mqtt.MQTTClient, deviceInfo identity.DeviceInfoInterface, logger zerolog.Logger) *CommandService {
	if outputSizeLimit == 0 {
		outputSizeLimit = constants.DefaultOutputSizeLimit
	}
	if maxExecutionTime == 0 {
		maxExecutionTime = constants.DefaultMaxExecutionTime
	}

	// Initialize context and cancel function
	ctx, cancel := context.WithCancel(context.Background())

	return &CommandService{
		subTopic:         subTopic,
		deviceInfo:       deviceInfo,
		qos:              qos,
		mqttClient:       mqttClient,
		logger:           logger,
		outputSizeLimit:  outputSizeLimit,
		maxExecutionTime: maxExecutionTime,
		stopChan:         make(chan struct{}),
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start subscribes to the MQTT topic and listens for incoming commands.
func (cs *CommandService) Start() error {
	topic := cs.subTopic + "/" + cs.deviceInfo.GetDeviceID()
	cs.logger.Info().Str("topic", topic).Msg("Starting CommandService and subscribing to MQTT topic")
	token := cs.mqttClient.Subscribe(topic, byte(cs.qos), cs.HandleCommand)
	token.Wait()
	if err := token.Error(); err != nil {
		cs.logger.Error().Err(err).Str("topic", topic).Msg("Failed to subscribe to MQTT topic")
		return err
	}

	cs.logger.Info().Str("topic", topic).Msg("Successfully subscribed to MQTT topic")
	return nil
}

// Stop gracefully shuts down the service, unsubscribing from MQTT and waiting for ongoing commands to finish.
func (cs *CommandService) Stop() error {
	cs.cancel() // Cancel the context to signal all goroutines to stop
	close(cs.stopChan)
	cs.wg.Wait() // Wait for all running commands to complete before proceeding

	topic := cs.subTopic + "/" + cs.deviceInfo.GetDeviceID()
	token := cs.mqttClient.Unsubscribe(topic)
	token.Wait()
	if err := token.Error(); err != nil {
		cs.logger.Error().Err(err).Str("topic", topic).Msg("Failed to unsubscribe from MQTT topic")
		return err
	}

	cs.logger.Info().Msg("CommandService stopped successfully")
	return nil
}

// HandleCommand processes incoming commands, executes them, and publishes the output.
func (cs *CommandService) HandleCommand(client MQTT.Client, msg MQTT.Message) {
	cs.mu.Lock()

	select {
	case <-cs.stopChan:
		cs.mu.Unlock()
		cs.logger.Warn().Msg("Received command but service is stopping, ignoring command")
		return
	default:
		cs.wg.Add(1)
		cs.mu.Unlock()
	}

	defer cs.wg.Done()

	topic := msg.Topic()
	payload := string(msg.Payload())

	cs.logger.Info().Str("topic", topic).Str("command", payload).Msg("Received command from MQTT topic")

	output, err := cs.ExecuteCommand(cs.ctx, payload)
	if err != nil {
		cs.logger.Error().Err(err).Msg("Command execution failed")
		output = fmt.Sprintf("Error executing command: %v", err)
	}

	// Truncate output if it exceeds the defined limit
	if len(output) > cs.outputSizeLimit {
		output = output[:cs.outputSizeLimit] + "... (truncated)"
		cs.logger.Warn().Int("limit", cs.outputSizeLimit).Msg("Command output truncated due to size limit")
	}

	if err := cs.PublishOutput(cs.ctx, output); err != nil {
		cs.logger.Error().Err(err).Msg("Failed to publish command output")
	}
}

// ExecuteCommand runs the given shell command and returns its output or an error.
func (cs *CommandService) ExecuteCommand(ctx context.Context, cmd string) (string, error) {
	cs.logger.Debug().Str("command", cmd).Msg("Executing shell command")

	ctx, cancel := context.WithTimeout(ctx, time.Duration(cs.maxExecutionTime)*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	stdout.Grow(cs.outputSizeLimit)
	stderr.Grow(cs.outputSizeLimit)

	command := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			cs.logger.Error().Msg("Command execution timed out")
			return "", ctx.Err()
		}
		cs.logger.Error().Err(err).Msg("Command execution failed")
		return stderr.String(), err
	}

	return stdout.String(), nil
}

// PublishOutput sends the command execution result to an MQTT response topic.
func (cs *CommandService) PublishOutput(ctx context.Context, output string) error {
	topic := fmt.Sprintf("%s/%s/response", cs.subTopic, cs.deviceInfo.GetDeviceID())
	cs.logger.Info().Str("topic", topic).Msg("Publishing command output to MQTT topic")

	token := cs.mqttClient.Publish(topic, byte(cs.qos), false, []byte(output))
	select {
	case <-token.Done():
		if err := token.Error(); err != nil {
			cs.logger.Error().Err(err).Str("topic", topic).Msg("Failed to publish command output")
			return err
		}
	case <-ctx.Done():
		cs.logger.Warn().Str("topic", topic).Msg("Publish operation cancelled")
		return ctx.Err()
	}

	cs.logger.Info().Str("topic", topic).Msg("Command output published successfully")
	return nil
}
