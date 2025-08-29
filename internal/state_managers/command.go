package state_managers

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/benmeehan/iot-agent/internal/constants"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/rs/zerolog"
)

// CommandStateManager handles file-based command state persistence
type CommandStateManager struct {
	filePath string
	logger   zerolog.Logger
	mu       sync.Mutex
}

// NewCommandStateManager initializes a new CommandStateManager
func NewCommandStateManager(filePath string, logger zerolog.Logger) *CommandStateManager {
	return &CommandStateManager{
		filePath: filePath,
		logger:   logger,
	}
}

// LoadState reads the command state from the file
func (sm *CommandStateManager) LoadState() (map[string]models.CommandState, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	data, err := os.ReadFile(sm.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]models.CommandState), nil
		}
		sm.logger.Error().Err(err).Msg("Failed to read state file")
		return nil, err
	}

	var states map[string]models.CommandState
	if err := json.Unmarshal(data, &states); err != nil {
		sm.logger.Error().Err(err).Msg("Failed to unmarshal state file")
		return nil, err
	}
	return states, nil
}

// SaveState writes the command state to the file
func (sm *CommandStateManager) SaveState(states map[string]models.CommandState) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		sm.logger.Error().Err(err).Msg("Failed to marshal state")
		return err
	}

	if err := os.WriteFile(sm.filePath, data, 0644); err != nil {
		sm.logger.Error().Err(err).Msg("Failed to write state file")
		return err
	}
	return nil
}

// Updatemodels.CommandState updates or adds a command state, removing completed commands
func (sm *CommandStateManager) UpdateCommandState(state models.CommandState) error {
	states, err := sm.LoadState()
	if err != nil {
		return err
	}

	// Remove completed commands (success or failed)
	if state.Status == constants.CommandStatusSuccess || state.Status == constants.CommandStatusFailed {
		delete(states, state.ExecutionID)
	} else {
		states[state.ExecutionID] = state
	}

	return sm.SaveState(states)
}
