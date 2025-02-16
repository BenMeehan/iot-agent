package file

import (
	"encoding/json"
	"os"
)

// FileOperations defines methods for reading from and writing to files.
type FileOperations interface {
	ReadFile(filePath string) (string, error)
	ReadFileRaw(filePath string) ([]byte, error)
	WriteFile(filePath string, data string) error
	WriteFileRaw(filePath string, data []byte) error
	WriteJsonFile(filePath string, data any) error
}

// FileService implements the FileOperations interface using standard file operations.
type FileService struct {
}

// NewFileService creates a new instance of FileService.
func NewFileService() *FileService {
	return &FileService{}
}

// ReadFile reads the contents of the file at filePath and returns it as a string.
func (fs *FileService) ReadFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadFileRaw reads the contents of the file at filePath and returns it as a byte array.
func (fs *FileService) ReadFileRaw(filePath string) ([]byte, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// WriteFile writes the data string to the file at filePath.
func (fs *FileService) WriteFile(filePath string, data string) error {
	return os.WriteFile(filePath, []byte(data), 0644)
}

// WriteFile writes the data string to the file at filePath.
func (fs *FileService) WriteFileRaw(filePath string, data []byte) error {
	return os.WriteFile(filePath, data, 0644)
}

// WriteJsonFile writes the json data string to the json file at filePath.
func (fs *FileService) WriteJsonFile(filePath string, data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, jsonData, 0644)
}
