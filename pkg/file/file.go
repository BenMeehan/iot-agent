package file

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"

	"gopkg.in/yaml.v3"
)

// FileOperations defines methods for reading from and writing to files.
type FileOperations interface {
	IsFileExists(filePath string) (bool, error)
	ReadFile(filePath string) (string, error)
	ReadFileRaw(filePath string) ([]byte, error)
	ReadJsonFile(filePath string, v any) error
	ReadYamlFile(filePath string, v any) error
	WriteFile(filePath string, data string) error
	WriteFileRaw(filePath string, data []byte) error
	WriteJsonFile(filePath string, data any) error
	WriteYamlFile(filePath string, data any) error
	GetFileMultipartFormData(filePath string) (*multipart.FileHeader, error)
	GetFileHash(filePath string) (string, error)
}

// FileService implements the FileOperations interface using standard file operations.
type FileService struct{}

// NewFileService creates a new instance of FileService.
func NewFileService() *FileService {
	return &FileService{}
}

// IsFileExist checks if the file exists and returns boolean and error
func (fs *FileService) IsFileExists(filePath string) (bool, error) {
	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return false, nil
	}

	// checking err == nil because of permission related error
	return err == nil, err
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
	tempFile := filePath + ".tmp"

	file, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ") // Optional: Pretty-print

	if err := encoder.Encode(data); err != nil {
		os.Remove(tempFile) // Clean up partial file
		return err
	}

	return os.Rename(tempFile, filePath) // Atomic file update
}

// WriteYamlFile writes the YAML data to the file at filePath.
func (fs *FileService) WriteYamlFile(filePath string, data any) error {
	tempFile := filePath + ".tmp"

	file, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	defer encoder.Close() // Ensure the encoder is properly closed

	if err := encoder.Encode(data); err != nil {
		os.Remove(tempFile) // Clean up partial file
		return err
	}

	return os.Rename(tempFile, filePath) // Atomic file update
}

// GetFileMultipartFormData returns multipart.FileHeader of given file path
func (fs *FileService) GetFileMultipartFormData(filePath string) (*multipart.FileHeader, error) {
	// Open the local file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	// Create a buffer to store the multipart form data
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Create a form file field
	part, err := writer.CreateFormFile("file", filePath)
	if err != nil {
		return nil, fmt.Errorf("error createing form file: %v", err)
	}

	// Copy the file content to the form field
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("error copying file content: %v", err)
	}

	// Close the writer to finalize the form
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("error closing writer: %v", err)
	}

	// Create a reader for the multipart form data
	reader := bytes.NewReader(body.Bytes())

	// Parse the multipart form
	multipartReader, err := multipart.NewReader(reader, writer.Boundary()).ReadForm(10 << 20) // 10MB max memory
	if err != nil {
		return nil, fmt.Errorf("error parsing multipart form: %v", err)
	}

	// Get the file header from the parsed form
	fileHeaders, ok := multipartReader.File["file"]
	if !ok || len(fileHeaders) == 0 {
		return nil, fmt.Errorf("no file found in form")
	}

	return fileHeaders[0], nil

}

// GetFileHash returns SHA256 hast of given file path
func (fs *FileService) GetFileHash(filePath string) (string, error) {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	// Create a new SHA-256 hasher
	hasher := sha256.New()

	// Copy the file contents to the hasher
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("error reading file contents: %v", err)
	}

	// Compute the hash
	hash := hasher.Sum(nil)

	// Convert the hash to a hexadecimal string
	return fmt.Sprintf("%x", hash), nil
}
