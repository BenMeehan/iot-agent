package file

import (
	"os"

	"github.com/sirupsen/logrus"
)

// FileOperations defines methods for reading from and writing to files.
type FileOperations interface {
	ReadFile(filePath string) (string, error)
	WriteFile(filePath string, data string) error
}

// FileService implements the FileOperations interface using standard file operations.
type FileService struct{
	Logger *logrus.Logger
}

// NewFileService creates a new instance of FileService.
func NewFileService(logger *logrus.Logger) *FileService {
	return &FileService{
		Logger: logger,
	}
}

// ReadFile reads the contents of the file at filePath and returns it as a string.
func (fs *FileService) ReadFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFile writes the data string to the file at filePath.
func (fs *FileService) WriteFile(filePath string, data string) error {
	return os.WriteFile(filePath, []byte(data), 0644)
}
