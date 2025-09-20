package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"

	"github.com/benmeehan/iot-agent/internal/constants"
	mqtt_middleware "github.com/benmeehan/iot-agent/internal/middlewares/mqtt"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	http_utils "github.com/benmeehan/iot-agent/pkg/httpUtils"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/jwt"
)

// UpdateService struct with FSM
type UpdateService struct {
	JwtManager                     jwt.JWTManagerInterface
	SubTopic                       string
	SharedAcknowledgementMqttTopic string
	AcknowledgementURL             string
	DeviceInfo                     identity.DeviceInfoInterface
	QOS                            int
	MqttClient                     mqtt_middleware.MQTTMiddleware
	Logger                         zerolog.Logger
	FileClient                     file.FileOperations

	state               constants.UpdateState
	validTransitions    map[constants.UpdateState][]constants.UpdateState
	bootPartition       models.Partition
	activePartition     models.Partition
	inactivePartition   models.Partition
	dataPartition       models.Partition
	manifest            models.Manifest
	ackChannel          chan models.Ack
	wg                  sync.WaitGroup
	metadataFile        string
	fileMetadataContent models.PartitionMetadata
	updateMetadata      models.UpdatesMetaData
	updateMux           sync.Mutex
	ackRequestChannel   chan *models.Ack // channel for sending ACK requests

	// Internal state for managing service lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// NewUpdateService creates and returns a new instance of UpdateService.
func NewUpdateService(jwtManager jwt.JWTManagerInterface, subTopic string, sharedAcknowledgementMqttTopic string, acknowledgementURL string, deviceInfo identity.DeviceInfoInterface, qos int,
	mqttClient mqtt_middleware.MQTTMiddleware, fileClient file.FileOperations, logger zerolog.Logger,
	metadataFile string) *UpdateService {

	return &UpdateService{
		JwtManager:                     jwtManager,
		SubTopic:                       subTopic,
		SharedAcknowledgementMqttTopic: sharedAcknowledgementMqttTopic,
		AcknowledgementURL:             acknowledgementURL,
		DeviceInfo:                     deviceInfo,
		QOS:                            qos,
		MqttClient:                     mqttClient,
		FileClient:                     fileClient,
		Logger:                         logger,
		metadataFile:                   metadataFile,

		state: constants.UpdateStateIdle, // Assuming an initial state
		validTransitions: map[constants.UpdateState][]constants.UpdateState{
			constants.UpdateStateIdle:        {constants.UpdateStateDownloading, constants.UpdateStateInstalling, constants.UpdateStateVerifying, constants.UpdateStateFailure},
			constants.UpdateStateDownloading: {constants.UpdateStateInstalling, constants.UpdateStateFailure},
			constants.UpdateStateInstalling:  {constants.UpdateStateVerifying, constants.UpdateStateFailure},
			constants.UpdateStateVerifying:   {constants.UpdateStateSuccess, constants.UpdateStateFailure},
			constants.UpdateStateSuccess:     {constants.UpdateStateIdle},
			constants.UpdateStateFailure:     {constants.UpdateStateIdle},
		},
	}
}

// Start initiates the MQTT listener for update commands
func (u *UpdateService) Start() error {
	u.ackChannel = make(chan models.Ack)
	u.ackRequestChannel = make(chan *models.Ack, 10) // buffered channel to accomodate http requests

	// Check if system is linux
	if runtime.GOOS != "linux" {
		return fmt.Errorf("current system is not linux. update service is incompatible with %s", runtime.GOOS)
	}

	// Verify system partition and write metadata file
	if err := u.verifySystemPartition(); err != nil {
		return fmt.Errorf("error verifying system partitions. update service can't be run in this system. error: %v", err)
	}

	u.wg.Add(1)
	go u.runAckHandler()

	u.SubscribeMQTTEndpoint()

	// Checking metadata file exists
	isFileExists, err := u.FileClient.IsFileExists(u.metadataFile)
	if isFileExists {
		metadataFileContent, err := u.FileClient.ReadFile(u.metadataFile)
		if err != nil {
			return fmt.Errorf("error reading metadata file: %v", err)
		}

		if err := json.Unmarshal([]byte(metadataFileContent), &u.fileMetadataContent); err != nil {
			return fmt.Errorf("error unmashal metadata file while reading file: %v", err)
		}

		u.updateMetadata = u.fileMetadataContent.Update
		// Check if ManifestData is empty before unmarshaling
		if len(u.updateMetadata.ManifestData) == 0 {
			u.manifest = models.Manifest{}
		} else {
			if err := json.Unmarshal([]byte(u.updateMetadata.ManifestData), &u.manifest); err != nil {
				return fmt.Errorf("error unmashal metadata content: %v", err)
			}
		}

		//u.state = constants.UpdateStateInstalling

		prevState := u.fileMetadataContent.Update.Status

		if prevState == string(constants.UpdateStateDownloading) || prevState == string(constants.UpdateStateInstalling) || prevState == string(constants.UpdateStateVerifying) {
			// u.SubscribeMQTTEndpoint()
			if prevState == string(constants.UpdateStateDownloading) {
				u.setState(constants.UpdateStateDownloading)
			} else if prevState == string(constants.UpdateStateInstalling) {
				u.setState(constants.UpdateStateInstalling)
			} else if prevState == string(constants.UpdateStateVerifying) {
				u.setState(constants.UpdateStateVerifying)
			}

			u.UpdateProcssFlow()
			// Send acknowledgment data
			requestPayload := &models.Ack{
				UpdateId: u.fileMetadataContent.Update.UpdateId,
				DeviceId: u.DeviceInfo.GetDeviceID(),
				Status:   prevState,
			}
			// u.handleAckMessages(requestPayload)
			u.ackRequestChannel <- requestPayload
		}
	} else {
		// If file exists and can't read because of permission issues
		if err != nil {
			return fmt.Errorf("unable to read metadata file: %v", err)
		}

		// File does not exist
		if err := u.FileClient.WriteJsonFile(u.metadataFile, u.fileMetadataContent); err != nil {
			return fmt.Errorf("unable to write metadata file: %v", err)
		}
	}

	return nil
}

// Stop gracefully shuts down the update service and unsubscribes from MQTT topics.
func (u *UpdateService) Stop() error {
	// Unsubscribe from acknowledgment mqtt topic
	ackTopic := "ack" + "/" + u.DeviceInfo.GetDeviceID()
	err := u.MqttClient.Unsubscribe(ackTopic)
	if err != nil {
		u.Logger.Error().Err(err).Msg("Failed to unsubscribe MQTT topic: " + ackTopic)
		return fmt.Errorf("failed to unsubscribe MQTT topic: %s", ackTopic)
	}

	// Unsubscribe from device id mqtt topic
	topic := u.SubTopic + "/" + u.DeviceInfo.GetDeviceID()
	err = u.MqttClient.Unsubscribe(topic)
	if err != nil {
		u.Logger.Error().Err(err).Msg("Failed to unsubscribe MQTT topic: " + topic)
		return fmt.Errorf("failed to unsubscribe MQTT topic: %s", ackTopic)
	}

	u.Logger.Info().Msg("Closing ackRequestChannel, ackChannel, WaitGroup")
	close(u.ackRequestChannel)
	close(u.ackChannel)
	u.wg.Wait()
	u.Logger.Info().Msg("UpdateService stopped")

	return nil
}

func (u *UpdateService) SubscribeMQTTEndpoint() {
	topic := u.SubTopic + "/" + u.DeviceInfo.GetDeviceID()

	err := u.MqttClient.Subscribe(topic, byte(u.QOS), u.InitiateUpdate)
	if err != nil {
		u.Logger.Error().Err(err).Msg("Failed to subscribe MQTT topic: " + topic)
	} else {
		u.Logger.Info().Str("topic", topic).Msg(fmt.Sprintf("Subscribed MQTT topic: %s", topic))
	}
}

func (u *UpdateService) verifySystemPartition() error {
	// Get all mounted partitions
	mounts, err := getMounts()
	if err != nil {
		return fmt.Errorf("error reading mounts: %v", err)
	}

	// Get PARTUUIDs for all devices
	partuuidMap, err := getPARTUUIDs()
	if err != nil {
		return fmt.Errorf("uuid unavailable: %v", err)
	}

	// Assign PARTUUIDs to partitions
	for i, partition := range mounts {
		if partuuid, ok := partuuidMap[partition.Device]; ok {
			mounts[i].PARTUUID = partuuid
		}
	}

	// Identify active, inactive, and data partitions
	var bootPartition, activePartition, inactivePartition, dataPartition models.Partition
	possibleBootPaths := []string{"/boot"}
	possibleInactivePaths := []string{"/a", "/b"}
	possibleDataPaths := []string{"/data", "/userdata"}

	for _, mount := range mounts {
		if mount.MountPoint == "/" {
			activePartition = mount
		} else if contains(possibleInactivePaths, mount.MountPoint) {
			inactivePartition = mount
		} else if contains(possibleDataPaths, mount.MountPoint) {
			dataPartition = mount
		} else if contains(possibleBootPaths, mount.MountPoint) {
			bootPartition = mount
		}
	}

	// Set the active, data partition to the receiver struct
	u.bootPartition = bootPartition
	u.activePartition = activePartition
	u.inactivePartition = inactivePartition
	u.dataPartition = dataPartition

	// Print partition information in the requested format
	if activePartition.Device != "" {
		uuid := activePartition.PARTUUID
		if uuid == "" {
			uuid = "N/A"
		}
	} else {
		fmt.Errorf("active partition not found")
	}

	if inactivePartition.Device != "" {
		uuid := inactivePartition.PARTUUID
		if uuid == "" {
			uuid = "N/A"
		}
	} else {
		fmt.Errorf("inactive partition not found")
	}
	if dataPartition.Device != "" {
		uuid := dataPartition.PARTUUID
		if uuid == "" {
			uuid = "N/A"
		}
	} else {
		fmt.Errorf("data partition not found")
	}

	// Create /data/updates and /data/mnt/inactive
	err = os.MkdirAll(u.dataPartition.MountPoint+"/updates", 0755) // 0755 sets permissions (read/write/execute for owner, read/execute for group/others)
	if err != nil {
		return fmt.Errorf("error creating directory: %v", err)
	}

	err = os.MkdirAll(u.dataPartition.MountPoint+"/mnt/inactive", 0755) // 0755 sets permissions (read/write/execute for owner, read/execute for group/others)
	if err != nil {
		return fmt.Errorf("error creating directory: %v", err)
	}

	// Storing metadata file with path in data partition
	u.metadataFile = dataPartition.MountPoint + "/updates/" + u.metadataFile
	u.fileMetadataContent = models.PartitionMetadata{
		TimeStamp:         time.Now().UTC(),
		ActivePartition:   activePartition,
		InActivePartition: inactivePartition,
	}

	return nil
}

// getMounts reads /proc/mounts to get device-to-mountpoint mappings
func getMounts() ([]models.Partition, error) {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/mounts: %v", err)
	}
	defer file.Close()

	var partitions []models.Partition
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}

		// Filter for block devices (e.g., /dev/mmcblk0p3, /dev/sda1)
		if strings.HasPrefix(fields[0], "/dev/") {
			partitions = append(partitions, models.Partition{
				Device:     fields[0],
				MountPoint: fields[1],
				PARTUUID:   "", // PARTUUID will be filled by getPARTUUIDs
			})
		}
	}

	return partitions, scanner.Err()
}

// getPARTUUIDs runs blkid to get UUIDs for all block devices
func getPARTUUIDs() (map[string]string, error) {
	partuuidMap := make(map[string]string)
	cmd := exec.Command("blkid", "-s", "PARTUUID")
	output, err := cmd.Output()
	if err != nil {
		return partuuidMap, fmt.Errorf("blkid not available or failed: %v", err)
	}

	// Parse blkid output
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		sections := strings.SplitN(line, ": ", 2)
		if len(sections) != 2 {
			continue
		}
		device := sections[0]
		partuuidSection := strings.TrimSpace(sections[1])
		if strings.HasPrefix(partuuidSection, "PARTUUID=") {
			partuuid := strings.TrimPrefix(partuuidSection, "PARTUUID=")
			partuuid = strings.Trim(partuuid, `"`)
			partuuidMap[device] = partuuid
		}
	}
	return partuuidMap, nil
}

// contains checks if a slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// InitiateUpdate processes incoming MQTT update commands
func (u *UpdateService) InitiateUpdate(client MQTT.Client, msg MQTT.Message) {
	if u.ctx != nil {
		u.Logger.Info().Msg("Update is alread running")
		return
	}
	u.ctx, u.cancel = context.WithCancel(context.Background())

	// Parse UpdateCommandPayload
	var payload models.UpdateCommandPayload

	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		u.Logger.Error().Err(err).Msg("Failed to parse update command payload")
		return
	}

	// Create the update metadata
	u.updateMetadata = models.UpdatesMetaData{
		TimeStamp:      time.Now().UTC(),
		UpdateId:       payload.UpdateId,
		FileUrl:        payload.FileUrl,
		FileName:       payload.FileName,
		FileSize:       payload.FileSize,
		Version:        payload.UpdateVersion,
		SHA256Checksum: payload.SHA256Checksum,
		Status:         string(u.state),
		ErrorLog:       "",
		ManifestData:   payload.ManifestData,
	}

	// Old version
	var localMetadataFileContent models.PartitionMetadata
	oldVersion := "0.0.0"
	isFileExists, err := u.FileClient.IsFileExists(u.metadataFile)
	if isFileExists && err == nil {
		metadataFileContent, err := u.FileClient.ReadFile(u.metadataFile)
		if err != nil {
			u.Logger.Error().Err(err).Msg("error reading metadata file")
			return
		}

		if err := json.Unmarshal([]byte(metadataFileContent), &localMetadataFileContent); err != nil {
			u.Logger.Error().Err(err).Msg("error unmashal metadata file while checking version")
			return
		}

		if localMetadataFileContent.Update.Version != "" {
			oldVersion = localMetadataFileContent.Update.Version
		}
	}

	// Check if the version is new
	isNewVersionUpdate, err := u.isNewVersion(oldVersion, payload.UpdateVersion)
	if err != nil {
		u.failedExecution(err, "error validating version")
		return
	}

	if !isNewVersionUpdate && localMetadataFileContent.Update.Status != string(constants.UpdateStateFailure) {
		u.failedExecution(fmt.Errorf("version should be higher than old version"), fmt.Sprintf("%s less/equal to %s", oldVersion, payload.UpdateVersion))
		return
	}

	// Proceed with the downloading state
	if err := u.setState(constants.UpdateStateDownloading); err != nil {
		u.failedExecution(err, "error updating state from idle to downloading")
		return
	}

	// send downloading mqtt message
	requestPayload := &models.Ack{
		UpdateId: payload.UpdateId,
		DeviceId: u.DeviceInfo.GetDeviceID(),
		Status:   string(u.state),
	}

	u.UpdateProcssFlow() // Start the consumer goroutine first
	// u.Logger.Info().Interface("payload", requestPayload).Msg("Sending to ackRequestChannel")
	select {
	case u.ackRequestChannel <- requestPayload:
		// u.Logger.Info().Msg("Successfully sent to ackRequestChannel")
	default:
		u.Logger.Warn().Msg("ackRequestChannel is full or closed")
		u.failedExecution(fmt.Errorf("channel send failed"), "ackRequestChannel is full or closed")
		return
	}
}

func (u *UpdateService) UpdateProcssFlow() {

	retry := 1
	// sharedAckTopic := u.SharedAcknowledgementMqttTopic
	u.wg.Add(1)
	// go routine for proceeding with next step
	go func() {
		defer u.wg.Done()

		for {
			select {
			case ack := <-u.ackChannel:
				if ack.Error != "" {
					// Do not retry if there is error from cloud
					if !strings.Contains(ack.Error, "connection timeout error") {
						u.failedCloudResponse(fmt.Errorf("error: server error"), ack.Error)
						return
					}
				} else if ack.Status == string(constants.UpdateStateDownloading) {
					retry = 0

					u.updateMetadata.Status = string(u.state)
					u.fileMetadataContent.Update = u.updateMetadata
					u.FileClient.WriteJsonFile(u.metadataFile, u.fileMetadataContent)

					// Create /data/updates and /data/mnt/inactive
					err := os.MkdirAll(u.dataPartition.MountPoint+"/updates", 0755) // 0755 sets permissions (read/write/execute for owner, read/execute for group/others)
					if err != nil {
						u.failedExecution(err, "error creating directory")
						return
					}

					err = os.MkdirAll(u.dataPartition.MountPoint+"/mnt/inactive", 0755) // 0755 sets permissions (read/write/execute for owner, read/execute for group/others)
					if err != nil {
						u.failedExecution(err, "error creating directory")
						return
					}

					// Check if the space on dataPartition > fileSize
					cmdStr := fmt.Sprintf("df -m %s | awk 'NR==2 {print $4}'", u.dataPartition.MountPoint)
					cmd := exec.Command("sh", "-c", cmdStr)

					output, err := executeCommandAndGetOutput(cmd)
					if err != nil {
						u.failedExecution(err, "error while getting free device space")
						return
					}

					updateLocationSpace, err := strconv.ParseFloat(strings.TrimSpace(output), 32)
					if err != nil {
						u.failedExecution(err, "invalid bytes number")
						return
					}
					fileSizeInt, err := strconv.ParseFloat(u.fileMetadataContent.Update.FileSize, 32)
					if updateLocationSpace < fileSizeInt {
						u.failedExecution(err, "insufficient space in device")
						return
					}

					// Download file the file and send mqtt message for installing
					if err := http_utils.DownloadFileByPresignedURL(u.updateMetadata.FileUrl, u.dataPartition.MountPoint+"/updates/"+u.updateMetadata.FileName); err != nil {
						u.failedExecution(err, "failed to download update file")
						return
					}

					// Vadlidate file checksum
					checksumValue, err := u.FileClient.GetFileHash(u.dataPartition.MountPoint + "/updates/" + u.updateMetadata.FileName)
					if err != nil {
						u.failedExecution(err, "error validating checksum")
						return
					}

					if checksumValue != u.updateMetadata.SHA256Checksum {
						u.failedExecution(err, "incorrect checksum")
						return
					}

					// Update state to installing and send mqtt request
					if err := u.setState(constants.UpdateStateInstalling); err != nil {
						u.failedExecution(err, "error updating state from downloading to installing")
						return
					}

					// send installing mqtt message
					requestPayload := &models.Ack{
						UpdateId: u.updateMetadata.UpdateId,
						DeviceId: u.DeviceInfo.GetDeviceID(),
						Status:   string(u.state),
					}
					u.ackRequestChannel <- requestPayload
				} else if ack.Status == string(constants.UpdateStateInstalling) {
					retry = 0

					u.updateMetadata.Status = string(u.state)
					u.fileMetadataContent.Update = u.updateMetadata
					u.FileClient.WriteJsonFile(u.metadataFile, u.fileMetadataContent)

					// starting os, file, folder updates
					if !u.installUpdates() {
						return
					}

					// Update state to verifying
					if err := u.setState(constants.UpdateStateVerifying); err != nil {
						u.failedExecution(err, "error updating state from installing to verifying")
						return
					}

					// update metadata file
					u.updateMetadata.Status = string(u.state)
					u.fileMetadataContent.Update = u.updateMetadata
					u.FileClient.WriteJsonFile(u.metadataFile, u.fileMetadataContent)

					// Reboot if os update was executed, else send mqtt message to verifying
					if u.manifest.OSUpdate != "" {
						rebootCmd := exec.Command("reboot")
						rebootCmdOutput := executeCommmandAndGetStatusCode(rebootCmd)
						if rebootCmdOutput == -1 || rebootCmdOutput != 0 {
							u.failedExecution(fmt.Errorf("reboot failed"), "error running reboot command")
						}
						return
					} else {
						// send verifying mqtt message
						requestPayload := &models.Ack{
							UpdateId: u.updateMetadata.UpdateId,
							DeviceId: u.DeviceInfo.GetDeviceID(),
							Status:   string(u.state),
						}

						u.ackRequestChannel <- requestPayload
					}

				} else if ack.Status == string(constants.UpdateStateVerifying) {
					if u.manifest.OSUpdate != "" && u.activePartition.PARTUUID != u.fileMetadataContent.InActivePartition.PARTUUID {
						u.failedExecution(fmt.Errorf("boot failed"), "failed to switch partition")
						return
					}

					if err := u.setState(constants.UpdateStateSuccess); err != nil {
						u.failedExecution(err, "error updating state from installing to verifying")
						return
					}

					// send success mqtt message
					requestPayload := &models.Ack{
						UpdateId: u.updateMetadata.UpdateId,
						DeviceId: u.DeviceInfo.GetDeviceID(),
						Status:   string(u.state),
					}

					u.ackRequestChannel <- requestPayload
				} else if ack.Status == string(constants.UpdateStateSuccess) {
					retry = 0

					u.updateMetadata.Status = string(u.state)
					u.fileMetadataContent.Update = u.updateMetadata
					u.FileClient.WriteJsonFile(u.metadataFile, u.fileMetadataContent)

					// Update the state to idle for next update
					u.setState(constants.UpdateStateIdle)

					if u.ctx != nil {
						u.cancel()
						u.ctx = nil
						u.cancel = nil
					}

					return
				}

			case <-time.After(10 * time.Second):
				retry++
				time.Sleep(5 * time.Second)

				requestPayload := &models.Ack{
					UpdateId: u.updateMetadata.UpdateId,
					DeviceId: u.DeviceInfo.GetDeviceID(),
					Status:   string(u.state),
				}
				// u.Logger.Info().Interface("payload", requestPayload).Msg("Sending retry ACK to ackRequestChannel")
				select {
				case u.ackRequestChannel <- requestPayload:
					// u.Logger.Info().Msg("Successfully sent retry ACK")
				default:
					u.Logger.Warn().Msg("ackRequestChannel is full or closed")
					u.failedExecution(fmt.Errorf("channel send failed"), "ackRequestChannel is full or closed")
					return
				}

				if retry > 5 {
					u.failedExecution(fmt.Errorf("retried 5 times for response"), "max retries reached")
					return
				}
			}
		}
	}()
}

// handleAckMessages processes incoming Acknowledgment message
func (u *UpdateService) runAckHandler() {
	u.Logger.Info().Msg("runAckHandler started")
	defer u.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			u.Logger.Error().Interface("panic", r).Msg("Panic in runAckHandler")
		}
	}()
	for requestPayload := range u.ackRequestChannel {
		// u.Logger.Info().Interface("payload", requestPayload).Msg("Received ACK request")
		u.handleAckMessages(requestPayload)
	}
}

// handleAckMessages processes incoming Acknowledgment message
func (u *UpdateService) handleAckMessages(requestPayload *models.Ack) {
	var ackResponse models.AckResponse

	requestPayloadBytes, err := json.Marshal(&requestPayload)
	if err != nil {
		u.Logger.Error().Err(err).Msg("failed to marshal request payload")
		ackResponse.Message.Error = err.Error()
	}

	status_code, dataBytes, err := http_utils.GetAcknowledgementResponse(u.JwtManager.GetJWT(), u.AcknowledgementURL, string(requestPayloadBytes))
	if err != nil {
		u.Logger.Error().Err(err).Msg("failed to get response")
		ackResponse.Message.Error = err.Error()
	}

	if status_code == 0 {
		ackResponse.Message.Error = fmt.Sprintf("%s; connection timeout error", err.Error())
	} else if status_code == 200 {
		if err := json.Unmarshal([]byte(dataBytes), &ackResponse); err != nil {
			u.Logger.Error().Err(err).Msg("failed to parse success acknowledgement message")
			ackResponse.Message.Error = err.Error()
		}
	} else {
		if err := json.Unmarshal([]byte(dataBytes), &ackResponse); err != nil {
			u.Logger.Error().Err(err).Msg("failed to parse failure acknowledgement message")
			ackResponse.Message.Error = err.Error()
		} else {
			fmt.Println("RES: ", ackResponse)
			ackResponse.Message.Error = fmt.Sprintf("STAUS: %s; %s: %s", strconv.Itoa(status_code), ackResponse.Status, ackResponse.Message.Error)
		}
	}

	u.ackChannel <- ackResponse.Message
}

// setState sets the current update state and saves it to disk
func (u *UpdateService) setState(newState constants.UpdateState) error {
	if !u.isValidTransition(newState) {
		err := errors.New("invalid state transition")
		u.Logger.Error().
			Str("currentState", string(u.state)).
			Str("newState", string(newState)).
			Err(err).
			Msg("State transition denied")
		return err
	}

	u.state = newState

	u.Logger.Info().Str("state", string(newState)).Msg("Update state updated")
	return nil
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

// isNewVersion checks if the new version is newer than the current version using semantic versioning
func (u *UpdateService) isNewVersion(currentVersionStr, newVersion string) (bool, error) {

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

// installUpdates will update and send mqtt message for success
func (u *UpdateService) installUpdates() bool {

	updateZipFileLocation := u.dataPartition.MountPoint + "/updates/" + u.fileMetadataContent.Update.FileName
	if _, err := os.Stat(updateZipFileLocation); os.IsNotExist(err) {
		u.failedExecution(err, "unable to locate zip file")
		return false
	}

	// Unzip the .zip file
	cmd := exec.Command("unzip", "-o", updateZipFileLocation, "-d", u.dataPartition.MountPoint+"/updates") // -o to overwrite if extract already exists
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMessage := ""
		// Delete the extracted file
		_, deleteErr := u.deleteExtractedFile(updateZipFileLocation)
		if deleteErr != nil {
			errMessage += fmt.Sprintf("error deleting partially extracted file: %v; ", deleteErr)
		}

		// Check if the error is an ExitError
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Get the exit code
			exitCode := exitErr.ExitCode()
			if exitCode == 50 {
				errMessage += fmt.Sprintf("error code(%d): %s; ", exitCode, string(output))
			} else {
				errMessage += fmt.Sprintf("error code(%d): %s; ", exitCode, string(output))
			}
		}
		errMessage += "error extracting zip file"
		u.failedExecution(err, errMessage)
		return false
	}

	// Parse the manifest
	if err := json.Unmarshal([]byte(u.updateMetadata.ManifestData), &u.manifest); err != nil {
		u.failedExecution(err, "error parsing manifest data")
		return false
	}

	// OS Update
	if u.manifest.OSUpdate != "" {
		// Check if update file size is less than inactive partition size
		ext := filepath.Ext(u.fileMetadataContent.Update.FileName)
		updateFile := strings.TrimSuffix(u.fileMetadataContent.Update.FileName, ext)
		updateFilePath := u.dataPartition.MountPoint + "/updates/" + updateFile
		getUpdatefileSizeCmdStr := fmt.Sprintf("du -hb %s | awk '{print $1/1024/1024}'", updateFilePath)
		getUpdatefileSizeCmd := exec.Command("sh", "-c", getUpdatefileSizeCmdStr)
		updatefileSizeStr, err := executeCommandAndGetOutput(getUpdatefileSizeCmd)
		if err != nil {
			u.failedExecution(err, "failed to get update file size")
			return false
		}

		getInactivePartitionSizeCmdStr := fmt.Sprintf("blockdev --getsize64 %s | awk '{print $1/1024/1024}'", u.fileMetadataContent.InActivePartition.Device)
		getInactivePartitionSizeCmd := exec.Command("sh", "-c", getInactivePartitionSizeCmdStr)
		inactivePartitionSizeStr, err := executeCommandAndGetOutput(getInactivePartitionSizeCmd)
		if err != nil {
			u.failedExecution(err, "failed to get inactive partition size")
			return false
		}

		updatefileSizeInt, err := strconv.ParseFloat(strings.TrimSpace(updatefileSizeStr), 32)
		if err != nil {
			u.failedExecution(err, "failed to parse update file size")
			return false
		}

		inactivePartitionSizeInt, err := strconv.ParseFloat(strings.TrimSpace(inactivePartitionSizeStr), 32)
		if err != nil {
			u.failedExecution(err, "failed to parse inactive partition size")
			return false
		}

		if updatefileSizeInt > inactivePartitionSizeInt {
			u.failedExecution(fmt.Errorf("update file size bigger than partition size"), fmt.Sprintf("update file %s should be less than %fMB", updateFile, strings.TrimSpace(inactivePartitionSizeStr)))
			return false
		}

		_, err = u.initiateOSUpdate(u.dataPartition.MountPoint + "/updates/" + u.manifest.OSUpdate)
		if err != nil {
			u.failedExecution(err, "error installing os update")
			return false
		}

		// update cmdline.txt to in-active partition
		cpCmd := exec.Command("cp", u.bootPartition.MountPoint+"/cmdline.txt", u.bootPartition.MountPoint+"/cmdline.txt.bak")
		cpCmdOutput := executeCommmandAndGetStatusCode(cpCmd)
		if cpCmdOutput == -1 || cpCmdOutput != 0 {
			u.failedExecution(err, "unable to backup cmdline.txt")
			return false
		}

		sedArg1 := fmt.Sprintf("s/PARTUUID=[0-9a-fA-F-]\\+/PARTUUID=%s/", u.inactivePartition.PARTUUID)
		sedArg2 := fmt.Sprintf("%s/cmdline.txt", u.bootPartition.MountPoint)
		updateCmdlineCmd := exec.Command("sed", "-i", sedArg1, sedArg2)
		updateCmdlineCmdOutput := executeCommmandAndGetStatusCode(updateCmdlineCmd)
		if updateCmdlineCmdOutput == -1 || updateCmdlineCmdOutput != 0 {
			u.failedExecution(err, "unable to update cmdline.txt")
			return false
		}
	}

	// Folder Update
	for _, manifestFileUpdate := range u.manifest.Updates.FolderUpdates {
		from_path := u.dataPartition.MountPoint + "/updates/" + manifestFileUpdate.Update
		to_path := manifestFileUpdate.Path

		if manifestFileUpdate.Overwrite {
			cmd := exec.Command("ionice", "rsync", "-a", "--info=progress2", "--delete", from_path, to_path)
			_, err := executeCommandAndWait(cmd)
			if err != nil {
				u.failedExecution(err, "error on folder update")
				return false
			}
		} else {
			cmd := exec.Command("ionice", "rsync", "-a", "--info=progress2", from_path, to_path)
			_, err := executeCommandAndWait(cmd)
			if err != nil {
				u.failedExecution(err, "error on folder update")
				return false
			}
		}
	}

	// File Update
	for _, manifestFileUpdate := range u.manifest.Updates.FileUpdates {
		from_path := u.dataPartition.MountPoint + "/updates/" + manifestFileUpdate.Update
		to_path := manifestFileUpdate.Path

		cmd := exec.Command("ionice", "rsync", "-a", "--info=progress2", from_path, to_path)
		_, err := executeCommandAndWait(cmd)
		if err != nil {
			u.failedExecution(err, "error on file update")
			return false
		}
	}

	return true
}

// initiateOSUpdate will update system files in device
func (u *UpdateService) initiateOSUpdate(OSUpdateFile string) (bool, error) {
	// Setup loop device and mount the OS update image

	checkMountCmd := exec.Command("findmnt", u.inactivePartition.Device)
	statuscode := executeCommmandAndGetStatusCode(checkMountCmd)
	if statuscode == 0 {
		unmountInactivePartitionCmd := exec.Command("umount", u.inactivePartition.Device)
		_, err := executeCommandAndGetOutput(unmountInactivePartitionCmd)
		if err != nil {
			return false, err
		}
	} else if statuscode > 0 {
		return false, fmt.Errorf("error running findmnt command, status code: %d", statuscode)
	} else {
		return false, fmt.Errorf("findmnt command not found")
	}

	updateInactivePartitionCmd := exec.Command("dd", "if="+OSUpdateFile, "of="+u.inactivePartition.Device, "bs=4M", "conv=fsync")
	ok, err := executeCommandAndWait(updateInactivePartitionCmd)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("failed to update inactive partition")
	}

	syncCmd := exec.Command("sync")
	_, err = executeCommandAndGetOutput(syncCmd)
	if err != nil {
		return false, err
	}

	return true, nil
}

// failedExecution will update the state, metadata file and send the error mqtt message
func (u *UpdateService) failedExecution(err error, errorMessage string) {
	u.setState(constants.UpdateStateFailure)
	u.updateMetadata.Status = string(u.state)
	u.updateMetadata.ErrorLog = fmt.Sprintf("%v: %s", err, errorMessage)
	u.fileMetadataContent.Update = u.updateMetadata
	u.FileClient.WriteJsonFile(u.metadataFile, u.fileMetadataContent)

	// update cloud with acknowledgement message on failure
	requestPayload := &models.Ack{
		UpdateId: u.updateMetadata.UpdateId,
		DeviceId: u.DeviceInfo.GetDeviceID(),
		Status:   string(u.state),
		Error:    u.updateMetadata.ErrorLog,
	}

	var ackResponse models.AckResponse
	requestPayloadBytes, err := json.Marshal(&requestPayload)
	if err != nil {
		u.Logger.Error().Err(err).Msg("failed to marshal request payload")
		ackResponse.Message.Error = err.Error()
	}

	status_code, dataBytes, err := http_utils.GetAcknowledgementResponse(u.JwtManager.GetJWT(), u.AcknowledgementURL, string(requestPayloadBytes))
	if err != nil {
		u.Logger.Error().Err(err).Msg("failed to get response")
		ackResponse.Message.Error = err.Error()
	}

	if status_code == 0 {
		ackResponse.Message.Error = fmt.Sprintf("%s; connection timeout error", err.Error())
	} else if status_code == 200 {
		if err := json.Unmarshal([]byte(dataBytes), &ackResponse); err != nil {
			u.Logger.Error().Err(err).Msg("failed to parse success acknowledgement message")
			ackResponse.Message.Error = err.Error()
		}
	} else {
		if err := json.Unmarshal([]byte(dataBytes), &ackResponse); err != nil {
			u.Logger.Error().Err(err).Msg("failed to parse failure acknowledgement message")
			ackResponse.Message.Error = fmt.Sprintf("%s; %s", err.Error(), u.updateMetadata.ErrorLog)
		} else {
			ackResponse.Message.Error = fmt.Sprintf("%s; %s", ackResponse.Status, ackResponse.Message.Error)
		}
	}

	u.updateMetadata.ErrorLog = ackResponse.Message.Error
	u.fileMetadataContent.Update = u.updateMetadata
	u.FileClient.WriteJsonFile(u.metadataFile, u.fileMetadataContent)

	u.Logger.Error().Err(err).Msg(u.updateMetadata.ErrorLog)
	u.setState(constants.UpdateStateIdle)

	if u.ctx != nil {
		u.cancel()
		u.ctx = nil
		u.cancel = nil
	}
}

// failedCloudResponse will update the log in device
func (u *UpdateService) failedCloudResponse(err error, errorMessage string) {
	u.setState(constants.UpdateStateFailure)
	u.updateMetadata.Status = string(u.state)
	u.updateMetadata.ErrorLog = fmt.Sprintf("%v; %s", err, errorMessage)
	u.fileMetadataContent.Update = u.updateMetadata
	u.FileClient.WriteJsonFile(u.metadataFile, u.fileMetadataContent)

	u.Logger.Error().Err(err).Msg(u.updateMetadata.ErrorLog)
	u.setState(constants.UpdateStateIdle)

	if u.ctx != nil {
		u.cancel()
		u.ctx = nil
		u.cancel = nil
	}
}

// deleteExtractedFile will update system files in device
func (u *UpdateService) deleteExtractedFile(extractedFilePath string) (bool, error) {
	err := os.Remove(extractedFilePath)
	if err != nil {
		return false, err
	}

	return true, nil
}

func executeCommandAndGetOutput(cmd *exec.Cmd) (string, error) {
	output, err := cmd.CombinedOutput() // Captures both stdout and stderr
	if err != nil {
		return "", fmt.Errorf("command: %s; error:%v", cmd.String(), err)
	}
	return fmt.Sprint(string(output)), nil
}

func executeCommandAndWait(cmd *exec.Cmd) (bool, error) {
	var outBuf, errBuf bytes.Buffer

	// Multi-writer: terminal + memory buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &outBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &errBuf)

	// Run command
	err := cmd.Run()

	// Check error
	if err != nil {
		// Check if err is greater than 300 characters, trim and return last 100 characters
		errMsg := errBuf.String()
		if len(errMsg) > 300 {
			errMsg = errMsg[len(errMsg)-100:]
		}
		if errBuf.Len() > 0 {
			return false, fmt.Errorf("error running command[%s]: %s", cmd.String(), errMsg)
		}
		if outBuf.Len() > 0 {
			fmt.Print(outBuf.String())
		}
	}

	return true, nil
}

// executeCommmandAndGetStatusCode return status code for a command
// return 0 if success or error status code or -1 if command does not exist
func executeCommmandAndGetStatusCode(cmd *exec.Cmd) int {
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus()
			}
		} else {
			return -1
		}
	}
	return 0
}
