package mocks

import "github.com/stretchr/testify/mock"

// MockDeviceInfo is a mock implementation of the DeviceInfoInterface
type MockDeviceInfo struct {
	mock.Mock
}

func (m *MockDeviceInfo) LoadDeviceInfo() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDeviceInfo) GetDeviceID() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockDeviceInfo) SaveDeviceID(deviceID string) error {
	args := m.Called(deviceID)
	return args.Error(0)
}
