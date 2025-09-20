package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"
	mqtt_middleware "github.com/benmeehan/iot-agent/internal/middlewares/mqtt"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/internal/state_managers"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
)

// CommandService manages execution of shell commands received via MQTT
// and sends status updates to the cloud.
type CommandService struct {
	// Configuration Fields
	subTopic         string
	qos              int
	outputSizeLimit  int
	maxExecutionTime time.Duration
	statusEndpoint   string // HTTP endpoint for status updates

	// Dependencies
	jwtManager     jwt.JWTManagerInterface
	mqttMiddleware mqtt_middleware.MQTTMiddleware
	deviceInfo     identity.DeviceInfoInterface
	logger         zerolog.Logger
	stateManager   *state_managers.CommandStateManager
	httpClient     *http.Client

	// Internal state management
	stopChan       chan struct{}
	wg             sync.WaitGroup
	mu             sync.Mutex
	activeCommands map[string]struct{} // Tracks running commands to prevent duplicates

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// NewCommandService initializes a new CommandService with given parameters.
func NewCommandService(
	jwtManager jwt.JWTManagerInterface,
	subTopic string,
	qos, outputSizeLimit, maxExecutionTime int,
	statusEndpoint string,
	mqttMiddleware mqtt_middleware.MQTTMiddleware,
	deviceInfo identity.DeviceInfoInterface,
	commandsStateFile string,
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
		jwtManager:       jwtManager,
		subTopic:         subTopic,
		qos:              qos,
		outputSizeLimit:  outputSizeLimit,
		maxExecutionTime: time.Duration(maxExecutionTime) * time.Second,
		statusEndpoint:   statusEndpoint, // e.g., "http://cloud:8080/status"
		mqttMiddleware:   mqttMiddleware,
		deviceInfo:       deviceInfo,
		logger:           logger,
		stateManager:     state_managers.NewCommandStateManager(commandsStateFile, logger),
		httpClient:       &http.Client{Timeout: 10 * time.Second},
		stopChan:         make(chan struct{}),
		activeCommands:   make(map[string]struct{}),
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start subscribes to the MQTT topic and processes pending commands from state.
func (cs *CommandService) Start() error {
	// Load and process pending commands
	if err := cs.processPendingCommands(); err != nil {
		cs.logger.Error().Err(err).Msg("Failed to process pending commands")
		return err
	}

	topic := cs.subTopic + "/" + cs.deviceInfo.GetDeviceID()
	cs.logger.Info().Str("topic", topic).Msg("Starting Command service and subscribing to MQTT topic")

	err := cs.mqttMiddleware.Subscribe(topic, byte(cs.qos), cs.HandleCommand)
	if err != nil {
		cs.logger.Error().Err(err).Str("topic", topic).Msg("Failed to subscribe to MQTT topic")
		return err
	}

	cs.logger.Info().Str("topic", topic).Msg("Successfully subscribed to MQTT topic")
	return nil
}

// Stop gracefully shuts down the service.
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

	cs.logger.Info().Msg("Command service stopped successfully")
	return nil
}

// processPendingCommands loads and resumes pending or in_progress commands.
func (cs *CommandService) processPendingCommands() error {
	states, err := cs.stateManager.LoadState()
	if err != nil {
		return err
	}

	for _, state := range states {
		if state.Status == constants.CommandStatusPending || state.Status == constants.CommandStatusInProgress {
			cs.mu.Lock()
			if _, exists := cs.activeCommands[state.ExecutionID]; exists {
				cs.mu.Unlock()
				continue
			}
			cs.activeCommands[state.ExecutionID] = struct{}{}
			cs.mu.Unlock()

			cs.wg.Add(1)
			go cs.executeCommand(state)
		}
	}
	return nil
}

// HandleCommand processes incoming MQTT commands.
func (cs *CommandService) HandleCommand(client MQTT.Client, msg MQTT.Message) {
	cs.mu.Lock()
	select {
	case <-cs.stopChan:
		cs.mu.Unlock()
		cs.logger.Warn().Msg("Received command but service is stopping, ignoring command")
		return
	default:
	}
	cs.mu.Unlock()

	request, err := cs.parseCommandRequest(msg.Payload())
	if err != nil {
		cs.logger.Error().Err(err).Msg("Error parsing command request")
		return
	}

	// Check for duplicate command
	cs.mu.Lock()
	if _, exists := cs.activeCommands[request.ExecutionID]; exists {
		cs.mu.Unlock()
		cs.logger.Warn().Str("execution_id", request.ExecutionID).Msg("Duplicate command ignored")
		return
	}
	cs.activeCommands[request.ExecutionID] = struct{}{}
	cs.mu.Unlock()

	// Save initial state
	state := models.CommandState{
		ExecutionID: request.ExecutionID,
		Command:     request.Command,
		Status:      constants.CommandStatusPending,
		CreatedAt:   time.Now(),
	}
	if err := cs.stateManager.UpdateCommandState(state); err != nil {
		cs.logger.Error().Err(err).Str("execution_id", request.ExecutionID).Msg("Failed to save command state")
		cs.mu.Lock()
		delete(cs.activeCommands, request.ExecutionID)
		cs.mu.Unlock()
		return
	}

	// Execute command in a goroutine
	cs.wg.Add(1)
	go cs.executeCommand(state)
}

// executeCommand executes the command and updates its state and status.
func (cs *CommandService) executeCommand(state models.CommandState) {
	defer cs.wg.Done()
	defer func() {
		cs.mu.Lock()
		delete(cs.activeCommands, state.ExecutionID)
		cs.mu.Unlock()
	}()

	// Send in_progress status
	inProgressUpdate := models.StatusUpdate{
		DeviceID:    cs.deviceInfo.GetDeviceID(),
		ExecutionID: state.ExecutionID,
		Status:      constants.CommandStatusInProgress,
		StartedAt:   time.Now().Unix(),
	}
	if err := cs.sendStatusUpdate(&inProgressUpdate); err != nil {
		cs.logger.Error().Err(err).Str("execution_id", state.ExecutionID).Msg("Failed to send in_progress status")
		return
	}

	// Update state to in_progress
	state.Status = constants.CommandStatusInProgress
	state.StartedAt = time.Now()
	if err := cs.stateManager.UpdateCommandState(state); err != nil {
		cs.logger.Error().Err(err).Str("execution_id", state.ExecutionID).Msg("Failed to update command state to in_progress")
		return
	}

	// Execute command
	output, err := cs.ExecuteCommand(cs.ctx, state.Command)
	finishedAt := time.Now()
	if err != nil {
		cs.logger.Error().Err(err).Str("execution_id", state.ExecutionID).Msg("Command execution failed")
		state.Status = constants.CommandStatusFailed
		if err := cs.stateManager.UpdateCommandState(state); err != nil {
			cs.logger.Error().Err(err).Str("execution_id", state.ExecutionID).Msg("Failed to update final command state")
			return
		}

		// Send failed status
		finalUpdate := models.StatusUpdate{
			DeviceID:    cs.deviceInfo.GetDeviceID(),
			ExecutionID: state.ExecutionID,
			Status:      constants.CommandStatusFailed,
			Output:      cs.truncateOutput(fmt.Sprintf("Error: %v", err)),
			FinishedAt:  finishedAt.Unix(),
		}
		if err := cs.sendStatusUpdate(&finalUpdate); err != nil {
			cs.logger.Error().Err(err).Str("execution_id", state.ExecutionID).Msg("Failed to send final status")
		}
		return
	}

	// Update state to success
	state.Status = constants.CommandStatusSuccess
	if err := cs.stateManager.UpdateCommandState(state); err != nil {
		cs.logger.Error().Err(err).Str("execution_id", state.ExecutionID).Msg("Failed to update final command state")
		return
	}

	// Send success status
	finalUpdate := models.StatusUpdate{
		DeviceID:    cs.deviceInfo.GetDeviceID(),
		ExecutionID: state.ExecutionID,
		Status:      constants.CommandStatusSuccess,
		Output:      cs.truncateOutput(output),
		FinishedAt:  finishedAt.Unix(),
	}
	if err := cs.sendStatusUpdate(&finalUpdate); err != nil {
		cs.logger.Error().Err(err).Str("execution_id", state.ExecutionID).Msg("Failed to send final status")
	}
}

// parseCommandRequest parses the incoming MQTT message into a CmdRequest.
func (cs *CommandService) parseCommandRequest(payload []byte) (*models.CmdRequest, error) {
	var request models.CmdRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, err
	}
	if request.ExecutionID == "" || request.Command == "" {
		return nil, fmt.Errorf("invalid command request: missing execution_id or command")
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

// sendStatusUpdate sends a status update to the cloud via HTTP.
func (cs *CommandService) sendStatusUpdate(status *models.StatusUpdate) error {
	url := fmt.Sprintf("%s/%s/%s", cs.statusEndpoint, status.DeviceID, status.ExecutionID)
	data, err := json.Marshal(status)
	if err != nil {
		cs.logger.Error().Err(err).Msg("Failed to marshal status update")
		return err
	}

	req, err := http.NewRequestWithContext(cs.ctx, "POST", url, bytes.NewBuffer(data))
	if err != nil {
		cs.logger.Error().Err(err).Msg("Failed to create status update request")
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cs.jwtManager.GetJWT()))
	req.Header.Set("Content-Type", "application/json")

	resp, err := cs.httpClient.Do(req)
	if err != nil {
		cs.logger.Error().Err(err).Msg("Failed to send status update")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	cs.logger.Info().Str("execution_id", status.ExecutionID).Str("status", status.Status).Msg("Status update sent successfully")
	return nil
}
