package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"
	mqtt_middleware "github.com/benmeehan/iot-agent/internal/middlewares/mqtt"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/identity"
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
	maxExecutionTime time.Duration

	// Dependencies
	mqttMiddleware mqtt_middleware.MQTTAuthMiddleware
	deviceInfo     identity.DeviceInfoInterface
	logger         zerolog.Logger

	// Internal state management
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// NewCommandService initializes a new CommandService with given parameters.
func NewCommandService(
	subTopic string,
	qos, outputSizeLimit, maxExecutionTime int,
	mqttMiddleware mqtt_middleware.MQTTAuthMiddleware,
	deviceInfo identity.DeviceInfoInterface,
	logger zerolog.Logger,
) *CommandService {
	if outputSizeLimit == 0 {
		outputSizeLimit = constants.DefaultOutputSizeLimit
	}
	if maxExecutionTime == 0 {
		maxExecutionTime = constants.DefaultMaxExecutionTime
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &CommandService{
		subTopic:         subTopic,
		qos:              qos,
		outputSizeLimit:  outputSizeLimit,
		maxExecutionTime: time.Duration(maxExecutionTime) * time.Second,
		mqttMiddleware:   mqttMiddleware,
		deviceInfo:       deviceInfo,
		logger:           logger,
		stopChan:         make(chan struct{}),
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start subscribes to the MQTT topic and listens for incoming commands.
func (cs *CommandService) Start() error {
	topic := cs.subTopic + "/" + cs.deviceInfo.GetDeviceID()
	cs.logger.Info().Str("topic", topic).Msg("Starting CommandService and subscribing to MQTT topic")

	err := cs.mqttMiddleware.Subscribe(topic, byte(cs.qos), cs.HandleCommand)
	if err != nil {
		cs.logger.Error().Err(err).Str("topic", topic).Msg("Failed to subscribe to MQTT topic")
		return err
	}

	cs.logger.Info().Str("topic", topic).Msg("Successfully subscribed to MQTT topic")
	return nil
}

// Stop gracefully shuts down the service, unsubscribing from MQTT and waiting for ongoing commands to finish.
func (cs *CommandService) Stop() error {
	cs.cancel()
	close(cs.stopChan)
	cs.wg.Wait()

	topic := cs.subTopic + "/" + cs.deviceInfo.GetDeviceID()
	err := cs.mqttMiddleware.Unsubscribe(topic)
	if err != nil {
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

	cs.logger.Debug().Msgf("Raw MQTT message: %s", string(msg.Payload()))

	request, err := cs.parseCommandRequest(msg.Payload())
	if err != nil {
		cs.logger.Error().Err(err).Msg("Error parsing command request")
		return
	}

	cs.logger.Debug().
		Str("topic", msg.Topic()).
		Str("command", request.Command).
		Msg("Received command from MQTT topic")

	output, err := cs.ExecuteCommand(cs.ctx, request.Command)
	if err != nil {
		cs.logger.Error().Err(err).Msg("Command execution failed")
		output = fmt.Sprintf("Error executing command: %v", err)
	}

	output = cs.truncateOutput(output)

	cmdResponse := &models.CmdResponse{
		UserID:   request.UserID,
		DeviceID: cs.deviceInfo.GetDeviceID(),
		Response: output,
	}

	if err := cs.PublishOutput(cmdResponse); err != nil {
		cs.logger.Error().Err(err).Msg("Failed to publish command output")
	}
}

// parseCommandRequest parses the incoming MQTT message into a CmdRequest.
func (cs *CommandService) parseCommandRequest(payload []byte) (*models.CmdRequest, error) {
	var request models.CmdRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, err
	}
	return &request, nil
}

// truncateOutput ensures the output does not exceed the size limit.
func (cs *CommandService) truncateOutput(output string) string {
	if len(output) > cs.outputSizeLimit {
		output = output[:cs.outputSizeLimit] + "... (truncated)"
		cs.logger.Warn().Int("limit", cs.outputSizeLimit).Msg("Command output truncated due to size limit")
	}
	return output
}

// ExecuteCommand runs the given shell command and returns its output or an error.
func (cs *CommandService) ExecuteCommand(ctx context.Context, cmd string) (string, error) {
	cs.logger.Debug().Str("command", cmd).Msg("Executing shell command")

	ctx, cancel := context.WithTimeout(ctx, cs.maxExecutionTime)
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

// publishOutput sends the command execution result to an MQTT response topic.
func (cs *CommandService) PublishOutput(cmdResponse *models.CmdResponse) error {
	topic := fmt.Sprintf("%s/response", cs.subTopic)
	cs.logger.Info().Str("topic", topic).Msg("Publishing command output to MQTT topic")

	cmdOutputJSON, err := json.Marshal(cmdResponse)
	if err != nil {
		cs.logger.Error().Err(err).Msg("Failed to marshal command")
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- cs.mqttMiddleware.Publish(topic, byte(cs.qos), false, []byte(cmdOutputJSON))
		close(done)
	}()

	select {
	case err := <-done:
		if err != nil {
			cs.logger.Error().Err(err).Str("topic", topic).Msg("Failed to publish command output")
			return err
		}
	case <-cs.ctx.Done():
		cs.logger.Warn().Str("topic", topic).Msg("Publish operation cancelled")
		return cs.ctx.Err()
	}

	cs.logger.Info().Str("topic", topic).Msg("Command output published successfully")
	return nil
}
