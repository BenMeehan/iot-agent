package mocks

import (
	"github.com/stretchr/testify/mock"
)

// MockFileOperations is a mock implementation of the FileOperations interface
type MockFileOperations struct {
	mock.Mock
}

func (m *MockFileOperations) ReadFile(filePath string) (string, error) {
	args := m.Called(filePath)
	return args.String(0), args.Error(1)
}

func (m *MockFileOperations) WriteFile(filePath string, data string) error {
	args := m.Called(filePath, data)
	return args.Error(0)
}
