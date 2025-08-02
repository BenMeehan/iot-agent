package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/tests/mocks"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestCommandService_Start_Success tests the successful start of the CommandService.
func TestCommandService_Start_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	logger := zerolog.Nop()

	mockDeviceInfo.On("GetDeviceID").Return("test-device-id")
	mockMQTTMiddleware.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	cs := services.NewCommandService(
		"test-topic",
		1,
		1024,
		10,
		mockMQTTMiddleware,
		mockDeviceInfo,
		logger,
	)

	// Execute
	err := cs.Start()

	// Assert
	assert.NoError(t, err)
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
}

// TestCommandService_Start_Failure tests the failure to start the CommandService.
func TestCommandService_Start_Failure(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	logger := zerolog.Nop()

	mockDeviceInfo.On("GetDeviceID").Return("test-device-id")
	mockMQTTMiddleware.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("subscribe failed"))

	cs := services.NewCommandService(
		"test-topic",
		1,
		1024,
		10,
		mockMQTTMiddleware,
		mockDeviceInfo,
		logger,
	)

	// Execute
	err := cs.Start()

	// Assert
	assert.Error(t, err)
	assert.Equal(t, "subscribe failed", err.Error())
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
}

// TestCommandService_Stop_Success tests the successful stop of the CommandService.
func TestCommandService_Stop_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	logger := zerolog.Nop()

	mockDeviceInfo.On("GetDeviceID").Return("test-device-id")
	mockMQTTMiddleware.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockMQTTMiddleware.On("Unsubscribe", mock.Anything).Return(nil)

	cs := services.NewCommandService(
		"test-topic",
		1,
		1024,
		10,
		mockMQTTMiddleware,
		mockDeviceInfo,
		logger,
	)

	// Start the service
	err := cs.Start()
	assert.NoError(t, err)

	// Execute
	err = cs.Stop()

	// Assert
	assert.NoError(t, err)
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
}

// TestCommandService_Stop_Failure tests the failure to stop the CommandService.
func TestCommandService_Stop_Failure(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	logger := zerolog.Nop()

	mockDeviceInfo.On("GetDeviceID").Return("test-device-id")
	mockMQTTMiddleware.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockMQTTMiddleware.On("Unsubscribe", mock.Anything).Return(errors.New("unsubscribe failed"))

	cs := services.NewCommandService(
		"test-topic",
		1,
		1024,
		10,
		mockMQTTMiddleware,
		mockDeviceInfo,
		logger,
	)

	// Start the service
	err := cs.Start()
	assert.NoError(t, err)

	// Execute
	err = cs.Stop()

	// Assert
	assert.Error(t, err)
	assert.Equal(t, "unsubscribe failed", err.Error())
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
}

// TestCommandService_HandleCommand_Success tests the successful handling of a command.
func TestCommandService_HandleCommand_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	logger := zerolog.Nop()

	mockDeviceInfo.On("GetDeviceID").Return("test-device-id")
	mockMQTTMiddleware.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	cs := services.NewCommandService(
		"test-topic",
		1,
		1024,
		10,
		mockMQTTMiddleware,
		mockDeviceInfo,
		logger,
	)

	// Simulate an incoming MQTT message
	cmdRequest := &models.CmdRequest{
		UserID:  "test-user",
		Command: "echo hello",
	}
	payload, _ := json.Marshal(cmdRequest)
	msg := new(mocks.Message)
	msg.On("Topic").Return("test-topic/test-device-id")
	msg.On("Payload").Return(payload)

	// Execute
	cs.HandleCommand(nil, msg)

	// Assert
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
	msg.AssertExpectations(t)
}

// TestCommandService_HandleCommand_InvalidPayload tests handling of an invalid command payload.
func TestCommandService_HandleCommand_InvalidPayload(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	logger := zerolog.Nop()

	cs := services.NewCommandService(
		"test-topic",
		1,
		1024,
		10,
		mockMQTTMiddleware,
		mockDeviceInfo,
		logger,
	)

	// Simulate an invalid MQTT message
	msg := new(mocks.Message)
	msg.On("Payload").Return([]byte("invalid-json"))

	// Execute
	cs.HandleCommand(nil, msg)

	// Assert
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
	msg.AssertExpectations(t)
}

// TestCommandService_ExecuteCommand_Success tests the successful execution of a command.
func TestCommandService_ExecuteCommand_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	logger := zerolog.Nop()

	cs := services.NewCommandService(
		"test-topic",
		1,
		1024,
		10,
		mockMQTTMiddleware,
		mockDeviceInfo,
		logger,
	)

	// Execute
	output, err := cs.ExecuteCommand(context.Background(), "echo hello")

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, "hello\n", output)
}

// TestCommandService_ExecuteCommand_Timeout tests the timeout of a command execution.
func TestCommandService_ExecuteCommand_Timeout(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	logger := zerolog.Nop()

	cs := services.NewCommandService(
		"test-topic",
		1,
		1024,
		1, // 1-second timeout
		mockMQTTMiddleware,
		mockDeviceInfo,
		logger,
	)

	// Execute
	output, err := cs.ExecuteCommand(context.Background(), "sleep 2")

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
	assert.Empty(t, output)
}

// TestCommandService_publishOutput_Success tests the successful publishing of command output.
func TestCommandService_publishOutput_Success(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	logger := zerolog.Nop()

	mockMQTTMiddleware.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	cs := services.NewCommandService(
		"test-topic",
		1,
		1024,
		10,
		mockMQTTMiddleware,
		mockDeviceInfo,
		logger,
	)

	// Execute
	cmdResponse := &models.CmdResponse{
		UserID:   "test-user",
		DeviceID: "test-device-id",
		Response: "hello",
	}
	err := cs.PublishOutput(cmdResponse)

	// Assert
	assert.NoError(t, err)
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
}

// TestCommandService_publishOutput_Failure tests the failure to publish command output.
func TestCommandService_publishOutput_Failure(t *testing.T) {
	// Setup
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTTMiddleware := new(mocks.MQTTMiddleware)
	logger := zerolog.Nop()

	mockMQTTMiddleware.On("Publish", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("publish failed"))

	cs := services.NewCommandService(
		"test-topic",
		1,
		1024,
		10,
		mockMQTTMiddleware,
		mockDeviceInfo,
		logger,
	)

	// Execute
	cmdResponse := &models.CmdResponse{
		UserID:   "test-user",
		DeviceID: "test-device-id",
		Response: "hello",
	}
	err := cs.PublishOutput(cmdResponse)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, "publish failed", err.Error())
	mockDeviceInfo.AssertExpectations(t)
	mockMQTTMiddleware.AssertExpectations(t)
}
