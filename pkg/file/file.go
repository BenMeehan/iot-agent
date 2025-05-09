package file

import (
	"encoding/json"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// FileOperations defines methods for reading from and writing to files.
type FileOperations interface {
	ReadFile(filePath string) (string, error)
	ReadFileRaw(filePath string) ([]byte, error)
	ReadJsonFile(filePath string, v any) error
	ReadYamlFile(filePath string, v any) error
	WriteFile(filePath string, data string) error
	WriteFileRaw(filePath string, data []byte) error
	WriteJsonFile(filePath string, data any) error
	WriteYamlFile(filePath string, data any) error
}

// FileService implements the FileOperations interface using standard file operations.
type FileService struct{}

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
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(file)
}

// ReadJsonFile reads and unmarshals JSON data from the given file.
func (fs *FileService) ReadJsonFile(filePath string, v any) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	return decoder.Decode(v)
}

// ReadYamlFile reads and unmarshals YAML data from the given file.
func (fs *FileService) ReadYamlFile(filePath string, v any) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	return decoder.Decode(v)
}

// WriteFile writes the data string to the file at filePath.
func (fs *FileService) WriteFile(filePath string, data string) error {
	return os.WriteFile(filePath, []byte(data), 0600)
}

// WriteFileRaw writes the data byte array to the file at filePath.
func (fs *FileService) WriteFileRaw(filePath string, data []byte) error {
	return os.WriteFile(filePath, data, 0600)
}

// WriteJsonFile writes the JSON data to the file at filePath.
func (fs *FileService) WriteJsonFile(filePath string, data any) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// WriteYamlFile writes the YAML data to the file at filePath.
func (fs *FileService) WriteYamlFile(filePath string, data any) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	defer encoder.Close()
	return encoder.Encode(data)
}
