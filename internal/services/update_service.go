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

	"github.com/Masterminds/semver/v3"
	MQTT "github.com/eclipse/paho.mqtt.golang"

	"github.com/benmeehan/iot-agent/internal/constants"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/sirupsen/logrus"
)

// UpdateService struct with FSM
type UpdateService struct {
	SubTopic         string
	DeviceInfo       identity.DeviceInfoInterface
	QOS              int
	MqttClient       mqtt.MQTTClient
	FileClient       file.FileOperations
	Logger           *logrus.Logger
	StateFile        string
	UpdateFilePath   string
	state            constants.UpdateState
	validTransitions map[constants.UpdateState][]constants.UpdateState
}

// Start initiates the MQTT listener for update commands
func (u *UpdateService) Start() error {
	u.InitializeStateMachine()

	// Ensure the state file exists
	if err := u.EnsureStateFileExists(); err != nil {
		return err
	}

	// Ensure the version file exists
	if err := u.EnsureVersionFileExists(); err != nil {
		return err
	}

	// Resume from the current state if possible
	if err := u.ResumeFromState(); err != nil {
		u.Logger.WithError(err).Error("Failed to resume update process")
	}

	// Subscribe to MQTT update commands
	topic := u.SubTopic + "/" + u.DeviceInfo.GetDeviceID()
	u.MqttClient.Subscribe(topic, byte(u.QOS), u.handleUpdateCommand)
	return nil
}

// InitializeStateMachine initializes valid state transitions
func (u *UpdateService) InitializeStateMachine() {
	u.validTransitions = map[constants.UpdateState][]constants.UpdateState{
		constants.StateEmpty: {constants.StateDownloading},

		// Normal flow from downloading to verifying to installing
		constants.StateDownloading: {constants.StateVerifying, constants.StateFailure},
		constants.StateVerifying:   {constants.StateInstalling, constants.StateFailure},

		// After installation, the update can either succeed or fail
		constants.StateInstalling: {constants.StateSuccess, constants.StateFailure},

		// Terminal states: cannot transition from success or failure
		constants.StateSuccess: {constants.StateDownloading},
		constants.StateFailure: {constants.StateDownloading},
	}
}

// isValidTransition checks if the transition between states is valid
func (u *UpdateService) isValidTransition(newState constants.UpdateState) bool {
	validStates, exists := u.validTransitions[u.state]
	if !exists {
		return false
	}
	for _, validState := range validStates {
		if newState == validState {
			return true
		}
	}
	return false
}

// EnsureStateFileExists ensures the state file exists and is initialized if missing
func (u *UpdateService) EnsureStateFileExists() error {
	// Check if the state file exists
	if _, err := os.Stat(u.StateFile); os.IsNotExist(err) {
		// Create the state file with an empty state
		initialState := struct{ State constants.UpdateState }{}

		if err := u.FileClient.WriteJsonFile(u.StateFile, initialState); err != nil {
			u.Logger.WithError(err).Error("Failed to create state file")
			return err
		}

		u.Logger.Info("State file created and initialized as empty")
	}
	return nil
}

// setState sets the current update state and saves it to disk
func (u *UpdateService) setState(newState constants.UpdateState) error {
	if !u.isValidTransition(newState) {
		err := errors.New("invalid state transition")
		u.Logger.WithFields(logrus.Fields{
			"currentState": u.state,
			"newState":     newState,
		}).WithError(err).Error("State transition denied")
		return err
	}

	u.state = newState
	stateData := struct{ State constants.UpdateState }{newState}
	if err := u.FileClient.WriteJsonFile(u.StateFile, stateData); err != nil {
		u.Logger.WithError(err).Error("Failed to persist state")
		return err
	}

	u.Logger.WithField("state", newState).Info("Update state updated")
	return nil
}

// ResumeFromState resumes the update process from the current state
func (u *UpdateService) ResumeFromState() error {
	// Read the current state from the state file
	stateData, err := u.FileClient.ReadFile(u.StateFile)
	if err != nil {
		u.Logger.WithError(err).Error("Failed to read state file, starting fresh")
		u.state = constants.StateDownloading // Default initial state
		return nil
	}

	var stateStruct struct{ State constants.UpdateState }
	if err := json.Unmarshal([]byte(stateData), &stateStruct); err != nil {
		u.Logger.WithError(err).Error("Failed to parse state file, starting fresh")
		u.state = constants.StateDownloading
		return nil
	}

	u.state = stateStruct.State
	u.Logger.WithField("state", u.state).Info("Resuming update process from state")

	// Continue the process based on the current state
	switch u.state {
	case constants.StateDownloading:
		// Re-download the update
		u.handleUpdateCommand(nil, nil)
		return nil
	case constants.StateVerifying:
		// Re-verify and decrypt the update
		return u.verifyAndDecryptUpdate(filepath.Join(u.UpdateFilePath, "encrypted_update.zip"))
	case constants.StateInstalling:
		// Re-apply the update
		instructionFile := filepath.Join(u.UpdateFilePath, "update_instructions.json")
		extractedDir := u.UpdateFilePath
		return u.applyUpdate(instructionFile, extractedDir)
	default:
		u.Logger.Info("No action required for terminal state")
		return nil
	}
}

// handleUpdateCommand updated to include version check with enhanced logging
func (u *UpdateService) handleUpdateCommand(client MQTT.Client, msg MQTT.Message) {
	u.Logger.Info("Received update command")

	// Parse UpdateCommandPayload
	var payload models.UpdateCommandPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		u.setState(constants.StateFailure)
		u.Logger.WithError(err).Error("Failed to parse update command payload")
		return
	}

	u.Logger.WithFields(logrus.Fields{
		"UpdateURL": payload.UpdateURL,
		"Version":   payload.Version,
	}).Info("Parsed update command payload")

	// Check if the version is newer
	isNew, err := u.isNewVersion(payload.Version)
	if err != nil {
		u.setState(constants.StateFailure)
		u.Logger.WithError(err).Error("Failed to check version")
		return
	}

	if !isNew {
		u.Logger.WithFields(logrus.Fields{
			"receivedVersion": payload.Version,
		}).Info("Received version is not newer or is equal to the current version. Update aborted.")
		return
	}

	// Proceed with the update process
	u.setState(constants.StateDownloading)
	if err := u.downloadUpdateFile(payload.UpdateURL); err != nil {
		u.setState(constants.StateFailure)
		u.Logger.WithError(err).Error("Failed to download update file")
		return
	}

	u.setState(constants.StateVerifying)
	if err := u.verifyAndDecryptUpdate(filepath.Join(u.UpdateFilePath, "encrypted_update.zip")); err != nil {
		u.setState(constants.StateFailure)
		u.Logger.WithError(err).Error("Failed to verify update")
		return
	}

	instructionFile := filepath.Join(u.UpdateFilePath, "update_instructions.json")
	extractedDir := u.UpdateFilePath

	u.setState(constants.StateInstalling)
	if err := u.applyUpdate(instructionFile, extractedDir); err != nil {
		u.setState(constants.StateFailure)
		u.Logger.WithError(err).Error("Failed to apply update, starting rollback")

		// Rollback on failure
		u.rollbackFiles([]models.UpdateInstruction{})
		return
	}

	u.setState(constants.StateSuccess)
	u.Logger.Info("Update installed successfully")
}

// downloadUpdateFile downloads the encrypted update file from the provided URL
func (u *UpdateService) downloadUpdateFile(url string) error {
	u.Logger.WithField("url", url).Info("Downloading update file...")

	resp, err := http.Get(url)
	if err != nil {
		u.Logger.WithError(err).Error("Error initiating download")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := errors.New("error downloading update file, received non-OK status")
		u.Logger.WithField("statusCode", resp.StatusCode).WithError(err).Error("Download failed")
		return err
	}

	// Ensure the update file path directory exists
	if err := os.MkdirAll(u.UpdateFilePath, os.ModePerm); err != nil {
		u.Logger.WithError(err).Error("Failed to create update file path directory")
		return err
	}

	file, err := os.Create(filepath.Join(u.UpdateFilePath, "encrypted_update.zip"))
	if err != nil {
		u.Logger.WithError(err).Error("Failed to create file for saving the update")
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		u.Logger.WithError(err).Error("Error writing the downloaded content to file")
		return err
	}

	u.Logger.Info("Update file downloaded successfully")
	return nil
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

	return u.unzipFile(encryptedFilePath, u.UpdateFilePath)
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
func (u *UpdateService) parseInstructions(instructionFile string) ([]models.UpdateInstruction, error) {
	file, err := os.Open(instructionFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var instructions []models.UpdateInstruction
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&instructions); err != nil {
		return nil, err
	}
	return instructions, nil
}

// backupFiles creates backups of existing files that may be affected by the update
func (u *UpdateService) backupFiles(instructions []models.UpdateInstruction) error {
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
func (u *UpdateService) rollbackFiles(instructions []models.UpdateInstruction) {
	backupDir := filepath.Join(u.UpdateFilePath, "backup")
	for _, inst := range instructions {
		if inst.NewFile != "-" && inst.TargetPath != "-" {
			backupPath := filepath.Join(backupDir, filepath.Base(inst.TargetPath))
			u.Logger.WithFields(logrus.Fields{
				"backupPath": backupPath,
				"targetPath": inst.TargetPath,
			}).Info("Rolling back file")
			if err := u.copyFile(backupPath, inst.TargetPath); err != nil {
				u.Logger.WithError(err).Errorf("Failed to restore file from backup: %s", inst.TargetPath)
			}
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

// readCurrentVersion reads the current version from configs/version.txt
func (u *UpdateService) readCurrentVersion() (string, error) {
	versionFile := filepath.Join("configs", "version.txt")
	data, err := u.FileClient.ReadFile(versionFile)
	if err != nil {
		return "", err
	}
	return data, nil
}

// isNewVersion checks if the new version is newer than the current version using semantic versioning
func (u *UpdateService) isNewVersion(newVersion string) (bool, error) {
	currentVersionStr, err := u.readCurrentVersion()
	if err != nil {
		return false, err
	}

	// Parse the current and new version strings into semver.Version
	currentVersion, err := semver.NewVersion(currentVersionStr)
	if err != nil {
		return false, errors.New("invalid current version format")
	}

	newVersionParsed, err := semver.NewVersion(newVersion)
	if err != nil {
		return false, errors.New("invalid new version format")
	}

	// Compare the versions
	return newVersionParsed.GreaterThan(currentVersion), nil
}

// EnsureVersionFileExists ensures the version.txt file exists and is initialized if missing
func (u *UpdateService) EnsureVersionFileExists() error {
	// Define the default version to initialize if the file does not exist
	defaultVersion := "0.0.1"

	// Check if the version.txt file exists
	versionFilePath := filepath.Join("configs", "version.txt")
	if _, err := os.Stat(versionFilePath); os.IsNotExist(err) {
		// Create the version.txt file with the default version
		if err := os.MkdirAll(filepath.Dir(versionFilePath), os.ModePerm); err != nil {
			u.Logger.WithError(err).Error("Failed to create directory for version file")
			return err
		}

		if err := u.FileClient.WriteFile(versionFilePath, defaultVersion); err != nil {
			u.Logger.WithError(err).Error("Failed to create version.txt file")
			return err
		}

		u.Logger.WithField("defaultVersion", defaultVersion).Info("version.txt file created and initialized")
	}
	return nil
}
