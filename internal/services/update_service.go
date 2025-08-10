package services

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/benmeehan/iot-agent/pkg/s3"
)

// UpdateService struct with FSM
type UpdateService struct {
	SubTopic   string
	DeviceInfo identity.DeviceInfoInterface
	QOS        int
	MqttClient mqtt.MQTTClient
	Logger     zerolog.Logger
	S3         s3.ObjectStorageClient
	FileClient file.FileOperations

	state             constants.UpdateState
	validTransitions  map[constants.UpdateState][]constants.UpdateState
	bootPartition     models.Partition
	activePartition   models.Partition
	inactivePartition models.Partition
	dataPartition     models.Partition
	ackChannel        chan models.Ack
	wg                sync.WaitGroup
	metadataFile      string
	metadataContent   models.PartitionMetadata
	updateMetadata    models.UpdatesMetaData
}

// NewUpdateService creates and returns a new instance of UpdateService.
func NewUpdateService(subTopic string, deviceInfo identity.DeviceInfoInterface, qos int,
	mqttClient mqtt.MQTTClient, fileClient file.FileOperations, logger zerolog.Logger,
	stateFile string, updateFilePath string, s3 s3.ObjectStorageClient) *UpdateService {

	return &UpdateService{
		SubTopic:   subTopic,
		DeviceInfo: deviceInfo,
		QOS:        qos,
		MqttClient: mqttClient,
		Logger:     logger,
		S3:         s3,
		FileClient: fileClient,

		state: constants.UpdateStateIdle, // Assuming an initial state
		validTransitions: map[constants.UpdateState][]constants.UpdateState{
			constants.UpdateStateIdle:        {constants.UpdateStateDownloading, constants.UpdateStateFailure},
			constants.UpdateStateDownloading: {constants.UpdateStateInstalling, constants.UpdateStateFailure},
			// constants.UpdateStateVerifying:   {constants.UpdateStateInstalling, constants.UpdateStateFailure},
			constants.UpdateStateInstalling: {constants.UpdateStateVerifying, constants.UpdateStateFailure},
			constants.UpdateStateVerifying:  {constants.UpdateStateSuccess, constants.UpdateStateFailure},
			constants.UpdateStateSuccess:    {constants.UpdateStateIdle},
			constants.UpdateStateFailure:    {constants.UpdateStateIdle},
		},
	}
}

// Start initiates the MQTT listener for update commands
func (u *UpdateService) Start() error {
	u.ackChannel = make(chan models.Ack)

	fmt.Printf("Start state: %s\n", u.state)

	// Check if system is linux
	if runtime.GOOS != "linux" {
		return fmt.Errorf("Current system is not linux. Update service is incompatible with %s", runtime.GOOS)
	}

	// Verify system partition and write metadata file
	if err := u.verifySystemPartition(); err != nil {
		return fmt.Errorf("Error verifying system partitions! Update service can't be run in this system. Error: %v", err)
	}

	// NEED TO RESUME FROM PREVIOUS UPDATE STATE
	isFileExists, err := u.FileClient.IsFileExists(u.metadataFile)
	if isFileExists {
		metadataFileContent, err := u.FileClient.ReadFile(u.metadataFile)
		if err != nil {
			return fmt.Errorf("Error reading metadata file: %v", err)
		}

		if err := json.Unmarshal([]byte(metadataFileContent), &u.metadataContent); err != nil {
			return fmt.Errorf("Error unmashal metadata file: %v", err)
		}

		u.updateMetadata = u.metadataContent.Update
		u.state = constants.UpdateStateInstalling

		prevState := u.metadataContent.Update.Status
		if prevState == string(constants.UpdateStateDownloading) || prevState == string(constants.UpdateStateFailure) || prevState == "" {
			u.SubscribeMQTTEndpoint()
		} else if prevState == string(constants.UpdateStateInstalling) {
			u.SubscribeMQTTEndpoint()
			u.UpdateProcssFlow()

			// Send install acknowledgment data to ack channel
			mqttPayload := &models.StatusUpdatePayload{
				UpdateId: u.metadataContent.Update.UpdateId,
				DeviceId: u.DeviceInfo.GetDeviceID(),
				Status:   u.metadataContent.Update.Status,
			}
			mqttPayloadBytes, err := json.Marshal(&mqttPayload)
			if err != nil {
				u.setState(constants.UpdateStateFailure)
				// updateMetadata.Status = string(u.state)
				// updateMetadata.ErrorLog = fmt.Sprintf("Failed to marshal mqtt download payload: %v", err)
				// u.metadataContent.Update = updateMetadata
				u.metadataContent.Update.ErrorLog = fmt.Sprintf("%v", err)
				u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

				u.Logger.Error().Err(err).Msg("Failed to marshal mqtt payload")
				return err
			}

			sharedAckTopic := "agent-updates"

			token := u.MqttClient.Publish(sharedAckTopic, byte(2), false, mqttPayloadBytes)
			token.Wait()
			if token.Error() != nil {
				u.Logger.Error().Err(token.Error()).Msg("Failed to publish for download message")
				return token.Error()
			} else {
				u.Logger.Info().Str("status", "success").Msg("Published to " + sharedAckTopic)
			}

		} else if prevState == string(constants.UpdateStateVerifying) {
			// verify if update is installed
			if u.activePartition.PARTUUID == u.metadataContent.ActivePartition.PARTUUID {
				u.SubscribeMQTTEndpoint()
				u.UpdateProcssFlow()

				u.setState(constants.UpdateStateSuccess)

				// Send success acknowledgment data to ack channel
				mqttPayload := &models.StatusUpdatePayload{
					UpdateId: u.metadataContent.Update.UpdateId,
					DeviceId: u.DeviceInfo.GetDeviceID(),
					Status:   string(u.state),
				}
				mqttPayloadBytes, err := json.Marshal(&mqttPayload)
				if err != nil {
					u.setState(constants.UpdateStateFailure)
					// updateMetadata.Status = string(u.state)
					// updateMetadata.ErrorLog = fmt.Sprintf("Failed to marshal mqtt download payload: %v", err)
					// u.metadataContent.Update = updateMetadata
					u.metadataContent.Update.ErrorLog = fmt.Sprintf("%v", err)
					u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

					u.Logger.Error().Err(err).Msg("Failed to marshal mqtt payload")
					return err
				}

				sharedAckTopic := "agent-updates"

				token := u.MqttClient.Publish(sharedAckTopic, byte(2), false, mqttPayloadBytes)
				token.Wait()
				if token.Error() != nil {
					u.Logger.Error().Err(token.Error()).Msg("Failed to publish for download message")
					return token.Error()
				} else {
					u.Logger.Info().Str("status", "success").Msg("Published to " + sharedAckTopic)
				}

			} else {
				// update failed, partition cannot be used
			}
		} else if prevState == string(constants.UpdateStateSuccess) {
			u.SubscribeMQTTEndpoint()
		}
	} else {
		// If file exists and can't read because of permission issues
		if err != nil {
			return fmt.Errorf("Unable to read metadata file: %v", err)
		}

		// File does not exist
		if err := u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent); err != nil {
			return fmt.Errorf("Unable to write metadata file: %v", err)
		}
		u.SubscribeMQTTEndpoint()
	}

	// // Subscribe for acknowledgment messages
	// ackTopic := "ack" + "/" + u.DeviceInfo.GetDeviceID()
	// token := u.MqttClient.Subscribe(ackTopic, byte(u.QOS), u.handleAckMessages)
	// token.Wait()
	// if token.Error() != nil {
	// 	u.Logger.Error().Err(token.Error()).Msg("Failed to subscribe MQTT topic: " + ackTopic)
	// } else {
	// 	u.Logger.Info().Str("topic", ackTopic).Msg(fmt.Sprintf("Subscribed MQTT topic: %s", ackTopic))
	// }

	// // Subscribe from the MQTT topic and handle update
	// topic := u.SubTopic + "/" + u.DeviceInfo.GetDeviceID()
	// token = u.MqttClient.Subscribe(topic, byte(u.QOS), u.InitiateUpdate)
	// token.Wait()
	// if token.Error() != nil {
	// 	u.Logger.Error().Err(token.Error()).Msg("Failed to subscribe MQTT topic: " + topic)
	// } else {
	// 	u.Logger.Info().Str("topic", topic).Msg(fmt.Sprintf("Subscribed MQTT topic: %s", topic))
	// }

	// u.Logger.Info().Msg("Starting wait group")
	// u.wg.Wait()

	return nil
}

// Stop
func (u *UpdateService) Stop() error {
	return nil
}

func (u *UpdateService) SubscribeMQTTEndpoint() {
	// Subscribe for acknowledgment messages
	ackTopic := "ack" + "/" + u.DeviceInfo.GetDeviceID()
	token := u.MqttClient.Subscribe(ackTopic, byte(u.QOS), u.handleAckMessages)
	token.Wait()
	if token.Error() != nil {
		u.Logger.Error().Err(token.Error()).Msg("Failed to subscribe MQTT topic: " + ackTopic)
	} else {
		u.Logger.Info().Str("topic", ackTopic).Msg(fmt.Sprintf("Subscribed MQTT topic: %s", ackTopic))
	}

	// Subscribe from the MQTT topic and handle update
	topic := u.SubTopic + "/" + u.DeviceInfo.GetDeviceID()
	token = u.MqttClient.Subscribe(topic, byte(u.QOS), u.InitiateUpdate)
	token.Wait()
	if token.Error() != nil {
		u.Logger.Error().Err(token.Error()).Msg("Failed to subscribe MQTT topic: " + topic)
	} else {
		u.Logger.Info().Str("topic", topic).Msg(fmt.Sprintf("Subscribed MQTT topic: %s", topic))
	}

	u.Logger.Info().Msg("Starting wait group")
	u.wg.Wait()
}

func (u *UpdateService) verifySystemPartition() error {
	// Get all mounted partitions
	mounts, err := getMounts()
	if err != nil {
		return fmt.Errorf("Error reading mounts: %v", err)
	}

	// Get PARTUUIDs for all devices
	partuuidMap, err := getPARTUUIDs()
	if err != nil {
		return fmt.Errorf("Warning: %v. UUIDs will be unavailable.", err)
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
	fmt.Println("Partition Information:")
	if activePartition.Device != "" {
		uuid := activePartition.PARTUUID
		if uuid == "" {
			uuid = "N/A"
		}
		fmt.Printf("Active Partition: %s (mounted at %s, UUID: %s)\n",
			activePartition.Device, activePartition.MountPoint, uuid)
	} else {
		fmt.Println("Active Partition: Not found")
	}
	if inactivePartition.Device != "" {
		uuid := inactivePartition.PARTUUID
		if uuid == "" {
			uuid = "N/A"
		}
		fmt.Printf("Inactive Partition: %s (mounted at %s, UUID: %s)\n",
			inactivePartition.Device, inactivePartition.MountPoint, uuid)
	} else {
		fmt.Println("Inactive Partition: Not found")

		// // If in-active not found, reboot the device
		// rebootCmd := exec.Command("reboot")
		// executeCommandAndGetOutput(rebootCmd)
	}
	if dataPartition.Device != "" {
		uuid := dataPartition.PARTUUID
		if uuid == "" {
			uuid = "N/A"
		}
		fmt.Printf("Data Partition: %s (mounted at %s, UUID: %s)\n",
			dataPartition.Device, dataPartition.MountPoint, uuid)
	} else {
		fmt.Println("Data Partition: Not found")
	}

	// Create /data/updates and /data/mnt/inactive
	err = os.MkdirAll(u.dataPartition.MountPoint+"/updates", 0755) // 0755 sets permissions (read/write/execute for owner, read/execute for group/others)
	if err != nil {
		return fmt.Errorf("Error creating directory: %v\n", err)
	}
	fmt.Printf("Directory '%s' created successfully.\n", u.dataPartition.MountPoint+"/mnt/update")

	err = os.MkdirAll(u.dataPartition.MountPoint+"/mnt/inactive", 0755) // 0755 sets permissions (read/write/execute for owner, read/execute for group/others)
	if err != nil {
		return fmt.Errorf("Error creating directory: %v\n", err)
	}
	fmt.Printf("Directory '%s' created successfully.\n", u.dataPartition.MountPoint+"/mnt/inactive")

	// Storing metadata file in data partition
	u.metadataFile = dataPartition.MountPoint + "/updates/updates-metadata.json"
	u.metadataContent = models.PartitionMetadata{
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
	// Parse UpdateCommandPayload
	var payload models.UpdateCommandPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		u.Logger.Error().Err(err).Msg("Failed to parse update command payload")
		return
	}

	// u.Logger.Info().
	// 	Str("UpdateURL", payload.FileUrl).
	// 	Str("Version", payload.UpdateVersion).
	// 	Msg("Parsed update command payload")

	// Create the update metadata
	u.updateMetadata = models.UpdatesMetaData{
		TimeStamp:      time.Now().UTC(),
		UpdateId:       payload.ID,
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
	if isFileExists {
		metadataFileContent, err := u.FileClient.ReadFile(u.metadataFile)
		if err != nil {
			u.Logger.Error().Err(err).Msg("Error reading metadata file")
			return
		}

		if err := json.Unmarshal([]byte(metadataFileContent), &localMetadataFileContent); err != nil {
			u.Logger.Error().Err(err).Msg("Error unmashal metadata file")
			return
		}

		if localMetadataFileContent.Update.Version != "" {
			oldVersion = localMetadataFileContent.Update.Version
		}
	}

	// Check if the version is new
	isNewVersionUpdate, err := u.isNewVersion(oldVersion, payload.UpdateVersion)
	if err != nil {
		u.setState(constants.UpdateStateFailure)
		u.updateMetadata.Status = string(u.state)
		u.updateMetadata.ErrorLog = fmt.Sprintf("Error validating version: %v", err)
		u.metadataContent.Update = u.updateMetadata
		u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

		u.Logger.Error().Err(err).Msg("Error validating version")
		return
	}

	if !isNewVersionUpdate {
		return
	}

	// Proceed with the downloading state
	if err := u.setState(constants.UpdateStateDownloading); err != nil {
		u.Logger.Error().Err(err).Msg("Error updating state from idle to downloading")
		return
	}

	// Send mqtt message and get acknowledgment for downloading
	sharedAckTopic := "agent-updates"

	mqttPayload := &models.StatusUpdatePayload{
		UpdateId: payload.ID,
		DeviceId: u.DeviceInfo.GetDeviceID(),
		Status:   string(u.state),
	}
	mqttPayloadBytes, err := json.Marshal(&mqttPayload)
	if err != nil {
		u.Logger.Error().Err(err).Msg("Failed to marshal mqtt payload")
		return
	}

	token := u.MqttClient.Publish(sharedAckTopic, byte(2), false, mqttPayloadBytes)
	token.Wait()
	if token.Error() != nil {
		u.Logger.Error().Err(token.Error()).Msg("Failed to publish for download message")
		return
	} else {
		u.Logger.Info().Str("status", "success").Msg("Published to " + sharedAckTopic)
	}

	u.UpdateProcssFlow()
}
func (u *UpdateService) UpdateProcssFlow() {

	retry := 1
	sharedAckTopic := "agent-updates"
	u.wg.Add(1)
	// go routine for proceeding with next step
	go func() {
		defer u.wg.Done()

		for {
			select {
			case ack := <-u.ackChannel:
				// if !ok {
				// 	u.setState(constants.UpdateStateFailure)
				// 	u.updateMetadata.Status = string(u.state)
				// 	u.updateMetadata.ErrorLog = "Failed to get acklowledgment data from channel"
				// 	u.metadataContent.Update = u.updateMetadata
				// 	u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)
				// }

				// fmt.Println("ACK: ", ack)
				if ack.Status == string(constants.UpdateStateDownloading) {
					retry = 0

					fmt.Println("DOWNLOADING...")
					u.updateMetadata.Status = string(u.state)
					u.metadataContent.Update = u.updateMetadata
					u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

					// Create /data/updates and /data/mnt/inactive
					err := os.MkdirAll(u.dataPartition.MountPoint+"/updates", 0755) // 0755 sets permissions (read/write/execute for owner, read/execute for group/others)
					if err != nil {
						fmt.Printf("Error creating directory: %v\n", err)
						return
					}
					fmt.Printf("Directory '%s' created successfully.\n", u.dataPartition.MountPoint+"/mnt/update")

					err = os.MkdirAll(u.dataPartition.MountPoint+"/mnt/inactive", 0755) // 0755 sets permissions (read/write/execute for owner, read/execute for group/others)
					if err != nil {
						fmt.Printf("Error creating directory: %v\n", err)
						return
					}
					fmt.Printf("Directory '%s' created successfully.\n", u.dataPartition.MountPoint+"/mnt/inactive")

					// Check if the space on dataPartition > fileSize
					cmdStr := fmt.Sprintf("df -m %s | awk 'NR==2 {print $4}'", u.dataPartition.MountPoint)
					cmd := exec.Command("sh", "-c", cmdStr)

					err, output := executeCommandAndGetOutput(cmd)
					if err != nil {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = fmt.Sprintf("error getting free space: %v", err)
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(err).Msg(fmt.Sprintf("error getting free space: %v", err))
						return
					}

					updateLocationSpace, err := strconv.ParseFloat(strings.TrimSpace(output), 2)
					if err != nil {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = fmt.Sprintf("Invalid bytes number: %v", err)
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(err).Msg(fmt.Sprintf("Invalid bytes number: %v", err))
						return
					}
					fileSizeInt, err := strconv.ParseFloat(u.metadataContent.Update.FileSize, 2)
					if updateLocationSpace < fileSizeInt {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = fmt.Sprintf("insufficient space: %v", err)
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(err).Msg(fmt.Sprintf("insufficient space: %v", err))
						return
					}

					// Download file the file and send mqtt message for installing
					if err := u.S3.DownloadFileByPresignedURL(u.updateMetadata.FileUrl, u.dataPartition.MountPoint+"/updates/"+u.updateMetadata.FileName); err != nil {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = fmt.Sprintf("Error downloading update file: %v", err)
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(err).Msg("Failed to download file")
						return
					}

					// Check checksum
					// file, _ := u.FileClient.GetFileMultipartFormData(u.dataPartition.MountPoint + "/updates/" + u.updateMetadata.FileName)
					// fileContents, _ := file.Open()
					// sha256Hasher := sha256.New()
					// if _, err := io.Copy(sha256Hasher, fileContents); err != nil {
					// 	fmt.Println("CHECKSUM ERROR...")
					// 	return
					// }

					checksumValue, err := u.FileClient.GetFileHash(u.dataPartition.MountPoint + "/updates/" + u.updateMetadata.FileName)
					fmt.Println(checksumValue, ":::", u.updateMetadata.SHA256Checksum)
					if err != nil {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = fmt.Sprintf("Error validating checksum: %v", err)
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(err).Msg("Error updating state from downloading to installing")
						return
					}
					if checksumValue == u.updateMetadata.SHA256Checksum {
						fmt.Println("CHECKSUM MATCH...")
					} else {
						fmt.Println("CHECKSUM MISMATCH...")
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = fmt.Sprintf("Incorrect checksum: %v", err)
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(err).Msg("Error updating state from downloading to installing")
						return
					}

					// Update state to installing and send mqtt request
					if err := u.setState(constants.UpdateStateInstalling); err != nil {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = fmt.Sprintf("Error updating state from downloading to installing: %v", err)
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(err).Msg("Error updating state from downloading to installing")
						return
					}

					mqttPayload := &models.StatusUpdatePayload{
						UpdateId: u.updateMetadata.UpdateId,
						DeviceId: u.DeviceInfo.GetDeviceID(),
						Status:   string(u.state),
					}
					mqttPayloadBytes, err := json.Marshal(&mqttPayload)
					if err != nil {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = fmt.Sprintf("Failed to marshal mqtt download payload: %v", err)
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(err).Msg("Failed to marshal mqtt payload")
						return
					}

					fmt.Println("Sleeping")
					time.Sleep(20 * time.Second)

					token := u.MqttClient.Publish(sharedAckTopic, byte(2), false, mqttPayloadBytes)
					token.Wait()
					if token.Error() != nil {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = fmt.Sprintf("Failed to publish mqtt installing message: %v", err)
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(token.Error()).Msg("Failed to publish mqtt installing message")
						return
					} else {
						u.Logger.Info().Str("status", "installing").Msg("Published to " + sharedAckTopic)
					}

				} else if ack.Status == string(constants.UpdateStateInstalling) {
					retry = 0

					fmt.Println("INSTALLING...")

					// If in-active not found, reboot the device
					if u.inactivePartition.Device == "" {
						rebootCmd := exec.Command("reboot")
						executeCommandAndGetOutput(rebootCmd)
					}

					u.updateMetadata.Status = string(u.state)
					u.metadataContent.Update = u.updateMetadata
					u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

					// if !u.installDebPackage() {
					// 	return
					// }

					if !u.installUpdates() {
						fmt.Println("update failed")
					}

					// update cmdline.txt to in-active partition
					rebootCmd := exec.Command("reboot")
					rebootCmdOutput := executeCommmandAndGetStatusCode(rebootCmd)
					if rebootCmdOutput == -1 || rebootCmdOutput != 0 {
						fmt.Println("reboot failed")
					}
					return

					// Update state to verifying and send mqtt request
					if err := u.setState(constants.UpdateStateVerifying); err != nil {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = "Error updating state from installing to verifying"
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(err).Msg("Error updating state from installing to verifying")
						return
					}

					mqttPayload := &models.StatusUpdatePayload{
						UpdateId: u.updateMetadata.UpdateId,
						DeviceId: u.DeviceInfo.GetDeviceID(),
						Status:   string(u.state),
					}
					mqttPayloadBytes, err := json.Marshal(&mqttPayload)
					if err != nil {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = fmt.Sprintf("Failed to marshal install mqtt payload: %v", err)
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(err).Msg("Failed to marshal install mqtt payload")
						return
					}

					fmt.Println("Sleeping")
					time.Sleep(20 * time.Second)

					token := u.MqttClient.Publish(sharedAckTopic, byte(2), false, mqttPayloadBytes)
					token.Wait()
					if token.Error() != nil {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = "Failed to publish for verifying message"
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(token.Error()).Msg("Failed to publish for verifying message")
						return
					} else {
						u.Logger.Info().Str("status", "success").Msg("Published to " + sharedAckTopic)
					}

				} else if ack.Status == string(constants.UpdateStateVerifying) {
					fmt.Println("VERIFYING...")

					// update verifying in metadata
					u.updateMetadata.Status = string(u.state)
					u.metadataContent.Update = u.updateMetadata
					u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

					// device reboot
					cmd := exec.Command("shutdown", "-r", "now")
					_, err := cmd.CombinedOutput()
					if err != nil {
						u.setState(constants.UpdateStateFailure)
						u.updateMetadata.Status = string(u.state)
						u.updateMetadata.ErrorLog = fmt.Sprintf("Failed to reboot device: %v", err)
						u.metadataContent.Update = u.updateMetadata
						u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

						u.Logger.Error().Err(err).Msg(fmt.Sprintf("Failed to reboot device: %v", err))
						return
					}
				} else if ack.Status == string(constants.UpdateStateSuccess) {
					retry = 0

					fmt.Println("Success")
					u.updateMetadata.Status = string(u.state)
					u.metadataContent.Update = u.updateMetadata
					u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

					// Update the state to idle for next update
					u.setState(constants.UpdateStateIdle)
					return
					// Success and write the metadata file
					// metaDataFile := u.dataPartition.MountPoint + "/updates-metadata.json"
					// metadataContent, err := os.ReadFile(metaDataFile)
					// if err != nil {
					// 	return fmt.Errorf("Error reading metadata file: %v", err)
					// }
				} else {
					// Error when sending mqtt request
				}

			case <-time.After(1 * time.Second):
				retry++
				fmt.Println("RETRY: ", retry)
				time.Sleep(5 * time.Second)

				mqttPayload := &models.StatusUpdatePayload{
					UpdateId: u.updateMetadata.UpdateId,
					DeviceId: u.DeviceInfo.GetDeviceID(),
					Status:   string(u.state),
				}
				mqttPayloadBytes, err := json.Marshal(&mqttPayload)
				if err != nil {
					u.Logger.Error().Err(err).Msg("Failed to marshal mqtt payload")
					return
				}

				token := u.MqttClient.Publish(sharedAckTopic, byte(2), false, mqttPayloadBytes)
				token.Wait()
				if token.Error() != nil {
					u.Logger.Error().Err(token.Error()).Msg("Failed to publish for download message")
					return
				} else {
					u.Logger.Info().Str("status", "success").Msg("Published to " + sharedAckTopic)
				}

				if retry > 5 {
					u.setState(constants.UpdateStateFailure)
					u.updateMetadata.Status = string(u.state)
					u.updateMetadata.ErrorLog = "Max retries reached. Failed to get acklowledgment message."
					u.metadataContent.Update = u.updateMetadata
					u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)
					u.setState(constants.UpdateStateIdle)

					return
				}
			}
		}
	}()
	fmt.Println("Go routine end")
}

// handleAckMessages processes incoming Acknowledgment MQTT messages
func (u *UpdateService) handleAckMessages(client MQTT.Client, msg MQTT.Message) {
	// Parse Acknowledgment message
	var payload models.Ack
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		u.Logger.Error().Err(err).Msg("Failed to parse ack message payload")
		return
	}

	// fmt.Println(string(msg.Payload()))
	// fmt.Println("ACK DATA: ", payload)
	// Send acknowledgment to channel
	u.ackChannel <- payload
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
	// stateData := struct{ State constants.UpdateState }{newState}
	// if err := u.FileClient.WriteJsonFile(u.StateFile, stateData); err != nil {
	// 	u.Logger.Error().Err(err).Msg("Failed to persist state")
	// 	return err
	// }

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
	// currentVersionStr, err := u.readCurrentVersion()
	// if err != nil {
	// 	return false, err
	// }
	// fmt.Println(currentVersionStr)

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

// readCurrentVersion reads the current version from configs/version.txt
// func (u *UpdateService) readCurrentVersion() (string, error) {
// 	versionFile := filepath.Join("configs", "version.txt")
// 	data, err := u.FileClient.ReadFile(versionFile)
// 	if err != nil {
// 		return "", err
// 	}
// 	return data, nil
// }

func (u *UpdateService) installDebPackage() bool {
	// Install the update and send mqtt message for success
	updateZipFileLocation := u.dataPartition.MountPoint + "/" + u.metadataContent.Update.FileName
	if _, err := os.Stat(updateZipFileLocation); os.IsNotExist(err) {
		u.setState(constants.UpdateStateFailure)
		u.updateMetadata.Status = string(u.state)
		u.updateMetadata.ErrorLog = fmt.Sprintf("Zip file does not exist in device: %v", err)
		u.metadataContent.Update = u.updateMetadata
		u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

		u.Logger.Error().Err(err).Msg(fmt.Sprintf("Zip file does not exist in device: %v", err))
		return false
	}

	// Unzip the .zip file
	// fmt.Println(updateZipFileLocation, u.dataPartition.MountPoint)
	cmd := exec.Command("unzip", "-o", updateZipFileLocation, "-d", u.dataPartition.MountPoint) // -o to overwrite if extract already exists
	_, err := cmd.CombinedOutput()
	if err != nil {
		u.setState(constants.UpdateStateFailure)
		u.updateMetadata.Status = string(u.state)
		u.updateMetadata.ErrorLog = fmt.Sprintf("Error extracting the file: %v", err)
		u.metadataContent.Update = u.updateMetadata
		u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

		u.Logger.Error().Err(err).Msg(fmt.Sprintf("Error extracting the file: %v", err))

		return false
	}

	updateFileLocation := updateZipFileLocation[:len(updateZipFileLocation)-4]
	// fmt.Println(updateFileLocation)

	// Check if file has .deb extension
	if !strings.HasSuffix(strings.ToLower(updateFileLocation), ".deb") {
		u.setState(constants.UpdateStateFailure)
		u.updateMetadata.Status = string(u.state)
		u.updateMetadata.ErrorLog = fmt.Sprintf("Error updating file '%s' is not a .deb file", updateFileLocation)
		u.metadataContent.Update = u.updateMetadata
		u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

		u.Logger.Error().Err(err).Msg(fmt.Sprintf("Error updating file '%s' is not a .deb file", updateFileLocation))
		return false
	}

	// Install .deb update file
	cmd = exec.Command("dpkg", "-i", updateFileLocation)
	_, err = cmd.CombinedOutput()
	if err != nil {
		u.setState(constants.UpdateStateFailure)
		u.updateMetadata.Status = string(u.state)
		u.updateMetadata.ErrorLog = fmt.Sprintf("Error installing update: %v", err)
		u.metadataContent.Update = u.updateMetadata
		u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

		u.Logger.Error().Err(err).Msg(fmt.Sprintf("Error installing update: %v", err))
		return false
	}

	return true
}

// installUpdates will update and send mqtt message for success
func (u *UpdateService) installUpdates() bool {

	updateZipFileLocation := u.dataPartition.MountPoint + "/updates/" + u.metadataContent.Update.FileName
	if _, err := os.Stat(updateZipFileLocation); os.IsNotExist(err) {
		u.setState(constants.UpdateStateFailure)
		u.updateMetadata.Status = string(u.state)
		u.updateMetadata.ErrorLog = fmt.Sprintf("Zip file does not exist in device: %v", err)
		u.metadataContent.Update = u.updateMetadata
		u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)

		u.Logger.Error().Err(err).Msg(fmt.Sprintf("Zip file does not exist in device: %v", err))
		return false
	}

	// Unzip the .zip file
	// fmt.Println(updateZipFileLocation, u.dataPartition.MountPoint)
	// cmd := exec.Command("unzip", "-o", updateZipFileLocation, "-d", u.dataPartition.MountPoint+"/updates") // -o to overwrite if extract already exists
	// output, err := cmd.CombinedOutput()
	// if err != nil {
	// 	// Delete the extracted file
	// 	u.deleteExtractedFile(updateZipFileLocation)

	// 	u.updateMetadata.ErrorLog = fmt.Sprintf("Error extracting file: %v; MESSAGE: deleted extracted file;INFO: %s; ", err, string(output))

	// 	// Check if the error is an ExitError
	// 	if exitErr, ok := err.(*exec.ExitError); ok {
	// 		// Get the exit code
	// 		exitCode := exitErr.ExitCode()
	// 		fmt.Println(0)
	// 		if exitCode == 50 {
	// 			u.updateMetadata.ErrorLog = fmt.Sprintf("Error extracting file: %v; MESSAGE: deleted extracted file;INFO: %s; ", err, string(output))
	// 		} else {
	// 			fmt.Println(2)
	// 			u.updateMetadata.ErrorLog = fmt.Sprintf("Error extracting file: %v; MESSAGE: deleted extracted file;INFO: %s; ", err, string(output))
	// 		}
	// 	} else {
	// 		// Handle other types of errors (e.g., command not found)
	// 		fmt.Println(3)
	// 		u.updateMetadata.ErrorLog = fmt.Sprintf("Error extracting file: %v; MESSAGE: deleted extracted file;INFO: %s; ", err, string(output))
	// 	}

	// 	u.setState(constants.UpdateStateFailure)
	// 	u.updateMetadata.Status = string(u.state)
	// 	// u.updateMetadata.ErrorLog = fmt.Sprintf("4 Error extracting file: %v; Message: %s", err, string(output))
	// 	u.metadataContent.Update = u.updateMetadata

	// 	fmt.Println(u.metadataFile)
	// 	fmt.Println(u.metadataContent)
	// 	err = u.FileClient.WriteJsonFile(u.metadataFile, u.metadataContent)
	// 	if err != nil {
	// 		fmt.Println("ERROR:")
	// 		fmt.Println(err)
	// 		// handle - no space left on device error
	// 	}

	// 	u.Logger.Error().Err(err).Msg(fmt.Sprintf("4 Error extracting file: %v; Message: %s", err, string(output)))
	// 	return false
	// }

	// Parse the manifest
	var manifest models.Manifest
	if err := json.Unmarshal([]byte(u.updateMetadata.ManifestData), &manifest); err != nil {
		u.Logger.Error().Err(err).Msg(fmt.Sprintf("Error parsing manifest data: %v", err))
		return false
	}

	fmt.Println(manifest)

	// OS Update
	if manifest.OSUpdate != "" {
		// _, err := u.initiateOSUpdate(u.dataPartition.MountPoint + "/updates/" + manifest.OSUpdate)
		// if err != nil {
		// 	fmt.Println(err)
		// }

		// update cmdline.txt to in-active partition
		cpCmd := exec.Command("cp", u.bootPartition.MountPoint+"/cmdline.txt", u.bootPartition.MountPoint+"/cmdline.txt.bak")
		cpCmdOutput := executeCommmandAndGetStatusCode(cpCmd)
		if cpCmdOutput == -1 || cpCmdOutput != 0 {
			fmt.Println("error cp")
		}

		sedArg1 := fmt.Sprintf("\"s/PARTUUID=[0-9a-fA-F-]\\+/PARTUUID=%s/\"", u.inactivePartition.PARTUUID)
		sedArg2 := fmt.Sprintf("%s/cmdline.txt", u.bootPartition.MountPoint)
		updateCmdlineCmd := exec.Command("sed", "-i", sedArg1, sedArg2)
		updateCmdlineCmdOutput := executeCommmandAndGetStatusCode(updateCmdlineCmd)
		if updateCmdlineCmdOutput == -1 || updateCmdlineCmdOutput != 0 {
			fmt.Println("error sed")
		}
	}

	// Folder Update
	for _, manifestFileUpdate := range manifest.Updates.FolderUpdates {
		from_path := u.dataPartition.MountPoint + "/updates/" + manifestFileUpdate.Update
		to_path := manifestFileUpdate.Path
		fmt.Println(from_path, to_path)

		if manifestFileUpdate.Overwrite {
			cmd := exec.Command("ionice", "rsync", "-a", "--info=progress2", "--delete", from_path, to_path)
			executeCommandAndWait(cmd)
		} else {
			cmd := exec.Command("ionice", "rsync", "-a", "--info=progress2", from_path, to_path)
			executeCommandAndWait(cmd)
		}
	}

	// File Update
	for _, manifestFileUpdate := range manifest.Updates.FileUpdates {
		from_path := u.dataPartition.MountPoint + "/updates/" + manifestFileUpdate.Update
		to_path := manifestFileUpdate.Path
		fmt.Println(from_path, to_path)

		cmd := exec.Command("ionice", "rsync", "-a", "--info=progress2", u.dataPartition.MountPoint+"/updates/"+manifestFileUpdate.Update, manifestFileUpdate.Path)
		executeCommandAndWait(cmd)
	}

	return true

}

// initiateOSUpdate will update system files in device
func (u *UpdateService) initiateOSUpdate(OSUpdateFile string) (bool, error) {
	// Setup loop device and mount the OS update image

	checkMountCmd := exec.Command("findmnt", u.inactivePartition.Device)
	statuscode := executeCommmandAndGetStatusCode(checkMountCmd)
	fmt.Println(u.inactivePartition.Device)
	fmt.Println(statuscode)
	if statuscode == 0 {
		unmountInactivePartitionCmd := exec.Command("umount", u.inactivePartition.Device)
		err, _ := executeCommandAndGetOutput(unmountInactivePartitionCmd)
		if err != nil {
			return false, err
		}
	} else if statuscode > 0 {
		return false, fmt.Errorf("error running findmnt command, status code: %d", statuscode)
	} else {
		return false, fmt.Errorf("findmnt command not found")
	}

	executeCommandAndWait(exec.Command("ping", "-c", "5", "google.com"))

	updateInactivePartitionCmd := exec.Command("dd", "if="+OSUpdateFile, "of="+u.inactivePartition.Device, "bs=4M", "status=progress", "conv=fsync")
	ok, err := executeCommandAndWait(updateInactivePartitionCmd)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("Failed to update inactive partition")
	}

	syncCmd := exec.Command("sync")
	err, _ = executeCommandAndGetOutput(syncCmd)
	if err != nil {
		return false, err
	}

	return true, nil
}

// deleteExtractedFile will update system files in device
func (u *UpdateService) deleteExtractedFile(extractedFilePath string) (bool, error) {
	err := os.Remove(extractedFilePath)
	if err != nil {
		fmt.Println("Error deleting file:", err)
		return false, err
	}

	return true, nil
}

func executeCommandAndGetOutput(cmd *exec.Cmd) (error, string) {
	fmt.Printf("running cmd: %s\n", cmd.String())
	output, err := cmd.CombinedOutput() // Captures both stdout and stderr
	if err != nil {
		return fmt.Errorf("command: %s; error:%v\n", cmd.String(), err), ""
	}
	return nil, fmt.Sprint(string(output))
}

func executeCommandAndWait(cmd *exec.Cmd) (bool, error) {
	fmt.Printf("running cmd: %s\n", cmd.String())
	var outBuf, errBuf bytes.Buffer

	// Multi-writer: terminal + memory buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &outBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &errBuf)

	// Run command
	err := cmd.Run()

	// Check error
	if err != nil {
		fmt.Println("\nCommand failed:", err)

		// Print captured stderr/stdout so we can see the actual cause
		if errBuf.Len() > 0 {
			fmt.Println("Error details (stderr):")
			fmt.Print(errBuf.String())
			return false, fmt.Errorf("error: %s", errBuf.String())
		}
		if outBuf.Len() > 0 {
			fmt.Println("Partial output (stdout):")
			fmt.Print(outBuf.String())
		}
	}

	return true, nil
}

// func executeCommandAndWait(cmd *exec.Cmd) (bool, error) {
// 	// Create pipes to capture stdout and stderr
// 	stdoutPipe, err := cmd.StdoutPipe()
// 	if err != nil {
// 		return false, fmt.Errorf("Error setting up stdout pipe: %v\n", err)
// 	}
// 	stderrPipe, err := cmd.StderrPipe()
// 	if err != nil {
// 		return false, fmt.Errorf("Error setting up stderr pipe: %v\n", err)
// 	}

// 	// Start the command
// 	if err := cmd.Start(); err != nil {
// 		return false, fmt.Errorf("Error starting command: %v\n", err)
// 	}

// 	// Read and display stdout in real-time
// 	go func() {
// 		scanner := bufio.NewScanner(stdoutPipe)
// 		for scanner.Scan() {
// 			fmt.Println(scanner.Text())
// 		}
// 	}()

// 	// Read and display stderr in real-time
// 	go func() {
// 		scanner := bufio.NewScanner(stderrPipe)
// 		for scanner.Scan() {
// 			fmt.Fprintf(os.Stderr, "Error: %s\n", scanner.Text())
// 		}
// 	}()

// 	// Wait for the command to finish
// 	if err := cmd.Wait(); err != nil {
// 		return false, fmt.Errorf("command failed: %v\n", err)
// 	}

// 	return true, nil
// }

// executeCommmandAndGetStatusCode return status code for a command
// return 0 if success or error status code or -1 if command does not exist
func executeCommmandAndGetStatusCode(cmd *exec.Cmd) int {
	// err := cmd.Run()
	// if err != nil {
	// 	// Check if it's an ExitError to get the exit code
	// 	if exitError, ok := err.(*exec.ExitError); ok {
	// 		// Get the status code from Sys()
	// 		statusCode := exitError.Sys().(syscall.WaitStatus).ExitStatus()
	// 		return statusCode
	// 	} else {
	// 		// Could be an execution error, like command not found
	// 		return -1
	// 	}
	// } else {
	// 	return 0
	// }
	fmt.Printf("running cmd: %s\n", cmd.String())
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
