package services

import (
	"errors"
	"testing"
	"time"

	"github.com/benmeehan/iot-agent/internal/services"
	"github.com/benmeehan/iot-agent/tests/mocks"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const privateKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAqm39GkEYU1QBAcjRQTg8Nw4xTDoGfRv2rC1luefLXHOSvv42
FGLkOlnRxZra/KsHX2CEWX1GEhDxq1jchWZrX36V1Dbo4Aq061BoJbCArh1M5Nu0
S8Atk6vNJYEhs1WMeYZ0Z0vb8r0w331rcB74gg1hrgQ7a8z+S0/hNxEIiuPr6tHg
bJ9zujulocoEobid/7byoTRh3/ZyDSfSAQNTsWaOCfq+Yxv+lK3aqDpZkjY/wFzp
zEY0vaHIqVBHWKs7FGb6SDm8YYaClBgj/Vmv7rsVjLIjPPvMQ2XKbQUGYhYcfLWr
a95t3vQtXfWWwyTvTyynAZg7oNRUvmpvZdslcwIDAQABAoIBAGQWpAW/JOIK+2xo
2ztKI1LR6vGxQg5HVd5X6t362ts4pH22HVxrl00NYryB7QlmB7ZjoFZN09DYUpUm
YpuVQomadbNja+/nWci4N/GqbmfSnU3qGUBDZIDM7HWSGJCRNSZJaCMh0dIEeadG
qMn35km6QhtIMP1mLhFcoA8O2c12h7geEnkzwf2Xn+LIBUfIRfsqP8QGL7bVlKov
7DXsw6D1su939ouU1He37b1KZj9/COc36+rX5uQYYWCpuuE3XT+cFlH7pVlEW0eF
IwkbW1gDTPOFfJsTi3+Dn7vQKNOBZ9iizedLBq+EsrRSC7grtf0jyR2dIPmP5VS9
OAbXWkECgYEA1YCDCC7Tjk51zxoQC9g4BS6PeDIjdOtjFNpvha54yKKSYOmWsUQP
YSqMkIjfiqixVkpd93gAqdAdYrwfVskpkTRUP1ttaNst1CSEahDpUXklksTuMxeR
p/B4YXqkY0zkeUZHaZesFf8+5AS9gvQR1j0EndswfXWT1qv6bXh5LUUCgYEAzFqg
FM0ABah57BCWNp68lMAEw9LngJVmATJ5/41+MBTi1jKtH6sh5i1wl2dm6rqodFqx
ZZERHjC6Xw4pXDAHxDjEhfB054a1Ie6XAiSkD5PezZp3XbRkgs6Ys9GnN7slN41p
501n8MlWPHbuwHcRFpB36uqpGIfVfeGtzrX+Z1cCgYA3DkC753dehxUSJuJka4lm
rK8Ki8Ng7yJJylpf2rIC6wlcPGBDrg1ZPSOqUeFzXDT+z4aTvjpNkAFD6MccFhvF
+fyPqf/4vix/PDt5Los8G0V5J5dVTYqeCADDAmFJyhZQv7LCo/4YXg3VtvM3xcCj
wnBiVJeYgq1w+kBF4n89EQKBgQDLbYveKRTUjRqR/REL3oksKtqTdegvAIpCttTr
qRbtFl2ZjWj6FYnxcVqb3bt9/8Kh0Ya27Op1e1yMM7TIqKeSllBMZUp7EIZP+Qsq
fv8y4qjxU8tv5JwJ+0/X8eTcfdhWrNe4Aj5uXH8UQfD6d4zzQW2e1WrvmIjWf0pe
dJ2EkQKBgDNBqBtnuwTApJKV+ndv6/7WsF0sKvGF/2Mms0RFTV9TevT6qmsFnBdg
DfQZ93dI1HN/jaJk2xvzBHzHSJmQKUsKqcl/5jRTPTDMU/odNdCLYJVDRi6g5OON
G+nENZZknShVN4LzrLVx9ALr3D1CGEAnSWEK9KjHQddhDMeCXM4N
-----END RSA PRIVATE KEY-----
`

const hostKey = `ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCqbf0aQRhTVAEByNFBODw3DjFMOgZ9G/asLWW558tcc5K+/jYUYuQ6WdHFmtr8qwdfYIRZfUYSEPGrWNyFZmtffpXUNujgCrTrUGglsICuHUzk27RLwC2Tq80lgSGzVYx5hnRnS9vyvTDffWtwHviCDWGuBDtrzP5LT+E3EQiK4+vq0eBsn3O6O6WhygShuJ3/tvKhNGHf9nINJ9IBA1OxZo4J+r5jG/6UrdqoOlmSNj/AXOnMRjS9ocipUEdYqzsUZvpIObxhhoKUGCP9Wa/uuxWMsiM8+8xDZcptBQZiFhx8tatr3m3e9C1d9ZbDJO9PLKcBmDug1FS+am9l2yVz`

// TestSSHService_Start_Success tests the successful start of the SSH service.
func TestSSHService_Start_Success(t *testing.T) {
	// Setup mocks
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTT := new(mocks.MQTTMiddleware)
	mockFile := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Expected calls
	mockDeviceInfo.On("GetDeviceID").Return("test-device")
	mockFile.On("ReadFileRaw", "private.key").Return([]byte(privateKey), nil)
	mockFile.On("ReadFileRaw", "public.key").Return([]byte(hostKey), nil)
	mockMQTT.On("Subscribe", "ssh/test-device", byte(1), mock.Anything).Return(nil)

	// Create service
	service := services.NewSSHService(
		"ssh",
		1,
		"testuser",
		"private.key",
		"public.key",
		mockDeviceInfo,
		mockMQTT,
		mockFile,
		logger,
		5,
		30*time.Second,
	)

	// Test
	err := service.Start()

	// Verify
	assert.NoError(t, err)
	mockDeviceInfo.AssertExpectations(t)
	mockMQTT.AssertExpectations(t)
	mockFile.AssertExpectations(t)
}

// TestSSHService_Start_PrivateKeyError tests the error handling when reading the private key fails.
func TestSSHService_Start_PrivateKeyError(t *testing.T) {
	// Setup mocks
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTT := new(mocks.MQTTMiddleware)
	mockFile := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Expected calls
	mockFile.On("ReadFileRaw", "private.key").Return([]byte{}, errors.New("read error"))

	// Create service
	service := services.NewSSHService(
		"ssh",
		1,
		"testuser",
		"private.key",
		"public.key",
		mockDeviceInfo,
		mockMQTT,
		mockFile,
		logger,
		5,
		30*time.Second,
	)

	// Test
	err := service.Start()

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read SSH private key")
	mockFile.AssertExpectations(t)
}

// TestSSHService_Start_PublicKeyError tests the error handling when reading the public key fails.
func TestSSHService_Start_PublicKeyError(t *testing.T) {
	// Setup mocks
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTT := new(mocks.MQTTMiddleware)
	mockFile := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Expected calls
	mockFile.On("ReadFileRaw", "private.key").Return([]byte(privateKey), nil)
	mockFile.On("ReadFileRaw", "public.key").Return([]byte{}, errors.New("read error"))

	// Create service
	service := services.NewSSHService(
		"ssh",
		1,
		"testuser",
		"private.key",
		"public.key",
		mockDeviceInfo,
		mockMQTT,
		mockFile,
		logger,
		5,
		30*time.Second,
	)

	// Test
	err := service.Start()

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read SSH server public key")
	mockFile.AssertExpectations(t)
}

// TestSSHService_Start_SubscribeError tests the error handling when subscribing to the MQTT topic fails.
func TestSSHService_Start_SubscribeError(t *testing.T) {
	// Setup mocks
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTT := new(mocks.MQTTMiddleware)
	mockFile := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Expected calls
	mockDeviceInfo.On("GetDeviceID").Return("test-device")
	mockFile.On("ReadFileRaw", "private.key").Return([]byte(privateKey), nil)
	mockFile.On("ReadFileRaw", "public.key").Return([]byte(hostKey), nil)
	mockMQTT.On("Subscribe", "ssh/test-device", byte(1), mock.Anything).Return(errors.New("subscribe error"))

	// Create service
	service := services.NewSSHService(
		"ssh",
		1,
		"testuser",
		"private.key",
		"public.key",
		mockDeviceInfo,
		mockMQTT,
		mockFile,
		logger,
		5,
		30*time.Second,
	)

	// Test
	err := service.Start()

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "subscribe error")
	mockDeviceInfo.AssertExpectations(t)
	mockMQTT.AssertExpectations(t)
	mockFile.AssertExpectations(t)
}

// TestSSHService_Stop_Success tests the successful stop of the SSH service.
func TestSSHService_Stop_Success(t *testing.T) {
	// Setup mocks
	mockDeviceInfo := new(mocks.DeviceInfoInterface)
	mockMQTT := new(mocks.MQTTMiddleware)
	mockFile := new(mocks.FileOperations)
	logger := zerolog.Nop()

	// Expected calls for Start
	mockDeviceInfo.On("GetDeviceID").Return("test-device")
	mockFile.On("ReadFileRaw", "private.key").Return([]byte(privateKey), nil)
	mockFile.On("ReadFileRaw", "public.key").Return([]byte(hostKey), nil)
	mockMQTT.On("Subscribe", "ssh/test-device", byte(1), mock.Anything).Return(nil)

	// Expected calls for Stop
	mockMQTT.On("Unsubscribe", "ssh/test-device").Return(nil)

	// Create and start service
	service := services.NewSSHService(
		"ssh",
		1,
		"testuser",
		"private.key",
		"public.key",
		mockDeviceInfo,
		mockMQTT,
		mockFile,
		logger,
		5,
		30*time.Second,
	)
	err := service.Start()
	assert.NoError(t, err)

	// Test Stop
	err = service.Stop()

	// Verify
	assert.NoError(t, err)
	mockMQTT.AssertExpectations(t)
}
