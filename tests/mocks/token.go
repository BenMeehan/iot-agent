package mocks

import (
	"time"

	"github.com/stretchr/testify/mock"
)

// MockToken is a mock implementation of the mqtt.Token interface
type MockToken struct {
	mock.Mock
}

// Error returns the error associated with the token
func (m *MockToken) Error() error {
	args := m.Called()
	return args.Error(0)
}

// Wait waits for the token to complete
func (m *MockToken) Wait() bool {
	args := m.Called()
	return args.Bool(0)
}

// Done channel returns the done channel for the token
func (m *MockToken) Done() <-chan struct{} {
	args := m.Called()
	return args.Get(0).(<-chan struct{})
}

// Completed returns true if the token has completed
func (m *MockToken) Completed() bool {
	args := m.Called()
	return args.Bool(0)
}

// WaitTimeout waits for the token to complete or timeout
func (m *MockToken) WaitTimeout(timeout time.Duration) bool {
	args := m.Called(timeout)
	return args.Bool(0)
}
