package services

import (
	"archive/zip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	MQTT "github.com/eclipse/paho.mqtt.golang"

	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/sirupsen/logrus"
)

type UpdateState string

const (
	StateDownloading UpdateState = "downloading"
	StateVerifying   UpdateState = "verifying"
	StateInstalling  UpdateState = "installing"
	StateSuccess     UpdateState = "success"
	StateFailure     UpdateState = "failure"
)

type UpdateCommandPayload struct {
	UpdateURL string `json:"update_url"`
}

// UpdateInstruction defines the structure for file operations in the update
type UpdateInstruction struct {
	TargetPath string `json:"target_path"`
	NewFile    string `json:"new_file"` // "-" for delete operations
}

// UpdateService struct with state and MQTT client
type UpdateService struct {
	SubTopic       string
	DeviceInfo     identity.DeviceInfoInterface
	QOS            int
	MqttClient     mqtt.MQTTClient
	Logger         *logrus.Logger
	StateFile      string
	UpdateFilePath string
	state          UpdateState
}

// Start initiates the MQTT listener for update commands
func (u *UpdateService) Start() error {
	topic := u.SubTopic + "/" + u.DeviceInfo.GetDeviceID()
	u.MqttClient.Subscribe(topic, byte(u.QOS), u.handleUpdateCommand)
	return nil
}

// handleUpdateCommand is triggered by MQTT to start the update process
func (u *UpdateService) handleUpdateCommand(client MQTT.Client, msg MQTT.Message) {
	u.Logger.Info("Received update")

	// Parse UpdateURL from MQTT message payload
	var payload UpdateCommandPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		u.setState(StateFailure)
		u.Logger.WithError(err).Error("Failed to parse update command payload")
		return
	}
	u.Logger.WithField("UpdateURL", payload.UpdateURL).Info("Parsed Update URL from command")

	// Start download, verification, and installation process
	u.setState(StateDownloading)
	if err := u.downloadUpdateFile(payload.UpdateURL); err != nil {
		u.setState(StateFailure)
		u.Logger.WithError(err).Error("Failed to download update file")
		return
	}

	u.setState(StateVerifying)
	if err := u.verifyAndDecryptUpdate(filepath.Join(u.UpdateFilePath, "encrypted_update.zip")); err != nil {
		u.setState(StateFailure)
		u.Logger.WithError(err).Error("Failed to verify update")
		return
	}

	instructionFile := filepath.Join(u.UpdateFilePath, "update_instructions.json")
	extractedDir := u.UpdateFilePath

	if err := u.applyUpdate(instructionFile, extractedDir); err != nil {
		u.setState(StateFailure)
		u.Logger.WithError(err).Error("Failed to apply update, starting rollback")

		// Rollback on failure
		u.setState("rollback")
		u.rollbackFiles([]UpdateInstruction{})
		u.Logger.Warn("Rollback completed")
		return
	}
	u.setState(StateSuccess)
	u.Logger.Info("Update installed successfully")
}

// downloadUpdateFile downloads the encrypted update file from the provided URL
func (u *UpdateService) downloadUpdateFile(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("error downloading update file")
	}

	file, err := os.Create(filepath.Join(u.UpdateFilePath, "encrypted_update.zip"))
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	return err
}

// verifyAndDecryptUpdate decrypts, verifies, and extracts the update files
func (u *UpdateService) verifyAndDecryptUpdate(encryptedFilePath string) error {
	decryptedFilePath := filepath.Join(u.UpdateFilePath, "decrypted_update.zip")

	if err := u.decryptFile(encryptedFilePath, decryptedFilePath); err != nil {
		return err
	}

	if err := u.verifyFileHash(decryptedFilePath); err != nil {
		return err
	}

	return u.unzipFile(decryptedFilePath, u.UpdateFilePath)
}

// decryptFile decrypts an AES-encrypted file and writes the result to outputPath
func (u *UpdateService) decryptFile(inputPath, outputPath string) error {
	key := []byte("32-byte-long-encryption-key-123")

	// Open the encrypted file
	encryptedFile, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer encryptedFile.Close()

	// Create the output file
	decryptedFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer decryptedFile.Close()

	// Initialize AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	// A unique IV; for now it's assumed to be the first 16 bytes of the file
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(encryptedFile, iv); err != nil {
		return err
	}

	stream := cipher.NewCFBDecrypter(block, iv)
	writer := &cipher.StreamWriter{S: stream, W: decryptedFile}

	// Copy and decrypt the content
	if _, err = io.Copy(writer, encryptedFile); err != nil {
		return err
	}
	return nil
}

// verifyFileHash compares the file's hash to the expected hash for integrity checking
func (u *UpdateService) verifyFileHash(filePath string) error {
	expectedHash := "expected-hash-here"

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}
	fileHash := hex.EncodeToString(hasher.Sum(nil))

	if fileHash != expectedHash {
		return errors.New("file hash does not match expected hash")
	}
	return nil
}

// unzipFile extracts a zip archive to the specified directory
func (u *UpdateService) unzipFile(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fPath := filepath.Join(dest, f.Name)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fPath, os.ModePerm); err != nil {
				return err
			}
			continue
		}

		if err := u.extractFile(f, fPath); err != nil {
			return err
		}
	}
	return nil
}

// extractFile extracts a single file from a zip archive
func (u *UpdateService) extractFile(f *zip.File, destPath string) error {
	srcFile, err := f.Open()
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	return err
}

// applyUpdate performs the update process according to parsed instructions
func (u *UpdateService) applyUpdate(instructionFile string, extractedDir string) error {
	// Step 1: Parse the update instruction file
	instructions, err := u.parseInstructions(instructionFile)
	if err != nil {
		return err
	}

	// Step 2: Backup existing files
	if err := u.backupFiles(instructions); err != nil {
		return err
	}

	// Step 3: Apply file operations (add/replace/delete)
	for _, inst := range instructions {
		switch {
		case inst.NewFile == "-": // Delete operation
			if err := os.Remove(inst.TargetPath); err != nil {
				u.Logger.WithError(err).Errorf("Failed to delete file %s", inst.TargetPath)
				u.rollbackFiles(instructions) // Rollback if any operation fails
				return err
			}
		case inst.TargetPath == "-": // New file
			srcPath := filepath.Join(extractedDir, inst.NewFile)
			destPath := filepath.Join(u.UpdateFilePath, inst.NewFile)
			if err := u.copyFile(srcPath, destPath); err != nil {
				u.rollbackFiles(instructions)
				return err
			}
		default: // Replace existing file
			srcPath := filepath.Join(extractedDir, inst.NewFile)
			if err := u.copyFile(srcPath, inst.TargetPath); err != nil {
				u.rollbackFiles(instructions)
				return err
			}
		}
	}

	u.Logger.Info("Update applied successfully")
	return nil
}

// parseInstructions reads and parses the JSON instruction file
func (u *UpdateService) parseInstructions(instructionFile string) ([]UpdateInstruction, error) {
	file, err := os.Open(instructionFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var instructions []UpdateInstruction
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&instructions); err != nil {
		return nil, err
	}
	return instructions, nil
}

// backupFiles creates backups of existing files that may be affected by the update
func (u *UpdateService) backupFiles(instructions []UpdateInstruction) error {
	backupDir := filepath.Join(u.UpdateFilePath, "backup")
	if err := os.MkdirAll(backupDir, os.ModePerm); err != nil {
		return err
	}

	for _, inst := range instructions {
		if inst.NewFile != "-" && inst.TargetPath != "-" {
			backupPath := filepath.Join(backupDir, filepath.Base(inst.TargetPath))
			if err := u.copyFile(inst.TargetPath, backupPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// rollbackFiles restores files from the backup in case of a failed update
func (u *UpdateService) rollbackFiles(instructions []UpdateInstruction) {
	backupDir := filepath.Join(u.UpdateFilePath, "backup")
	for _, inst := range instructions {
		if inst.NewFile != "-" && inst.TargetPath != "-" {
			backupPath := filepath.Join(backupDir, filepath.Base(inst.TargetPath))
			u.copyFile(backupPath, inst.TargetPath)
		}
	}
	u.Logger.Warn("Update rollback completed")
}

// copyFile copies a file from src to dst
func (u *UpdateService) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// setState sets the current update state and saves it to disk
func (u *UpdateService) setState(state UpdateState) {
	u.state = state
	stateData, _ := json.Marshal(struct{ State UpdateState }{state})
	_ = os.WriteFile(u.StateFile, stateData, 0644)
	u.Logger.WithField("state", state).Info("Update state updated")
}
