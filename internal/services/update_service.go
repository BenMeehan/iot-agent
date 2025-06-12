package services

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/benmeehan/iot-agent/internal/constants"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	"github.com/benmeehan/iot-agent/pkg/s3"
	MQTT "github.com/eclipse/paho.mqtt.golang"
)

// UpdateService struct with FSM
type UpdateService struct {
	SubTopic   string
	DeviceInfo identity.DeviceInfoInterface
	QOS        int
	MqttClient mqtt.MQTTClient
	Logger     zerolog.Logger
	S3         s3.ObjectStorageClient

	state            constants.UpdateState
	validTransitions map[constants.UpdateState][]constants.UpdateState
	dataPartition    models.Partition
	ackChannel       chan models.Ack
	wg               sync.WaitGroup
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
		state:      constants.UpdateStateIdle, // Assuming an initial state
		validTransitions: map[constants.UpdateState][]constants.UpdateState{
			constants.UpdateStateIdle:        {constants.UpdateStateDownloading},
			constants.UpdateStateDownloading: {constants.UpdateStateInstalling, constants.UpdateStateFailure},
			// constants.UpdateStateVerifying:   {constants.UpdateStateInstalling, constants.UpdateStateFailure},
			constants.UpdateStateInstalling: {constants.UpdateStateSuccess, constants.UpdateStateFailure},
			constants.UpdateStateSuccess:    {},
			constants.UpdateStateFailure:    {constants.UpdateStateIdle},
		},
	}
}

// Start initiates the MQTT listener for update commands
func (u *UpdateService) Start() error {
	u.ackChannel = make(chan models.Ack, 10000)
	u.wg.Add(1)
	// NEED TO RESUME FROM PREVIOUS STATE
	//u.setState(constants.UpdateStateIdle)
	fmt.Printf("Start state: %s\n", u.state)

	// Check if system is linux
	if runtime.GOOS != "linux" {
		return fmt.Errorf("Current system is not linux. Update service is incompatible with %s", runtime.GOOS)
	}
	// Verify system partition
	if err := u.verifySystemPartition(); err != nil {
		return fmt.Errorf("Error verifying system partitions! Update service can't be run in this system. Error: %v", err)
	}

	// Check for last updates

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
	token = u.MqttClient.Subscribe(topic, byte(u.QOS), u.handleUpdateCommand)
	token.Wait()
	if token.Error() != nil {
		u.Logger.Error().Err(token.Error()).Msg("Failed to subscribe MQTT topic: " + topic)
	} else {
		u.Logger.Info().Str("topic", topic).Msg(fmt.Sprintf("Subscribed MQTT topic: %s", topic))
	}

	u.Logger.Info().Msg("Starting wait group")
	u.wg.Wait()

	return nil
}

// Stop
func (u *UpdateService) Stop() error {
	return nil
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
	var activePartition, inactivePartition, dataPartition models.Partition
	possibleInactivePaths := []string{"/a", "/b"}
	possibleDataPaths := []string{"/data", "/userdata"}

	for _, mount := range mounts {
		if mount.MountPoint == "/" {
			activePartition = mount
		} else if contains(possibleInactivePaths, mount.MountPoint) {
			inactivePartition = mount
		} else if contains(possibleDataPaths, mount.MountPoint) {
			dataPartition = mount
		}
	}

	// Set the data partition to the receiver struct
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

	// Storing metadata file in data partition
	metaDataFile := dataPartition.MountPoint + "/updates-metadata.json"
	_, err = os.Stat(metaDataFile)
	if os.IsNotExist(err) {
		// Agent running for the first time
		var allPartitionMetadata []models.PartitionMetadata

		paritionMetadata := &models.PartitionMetadata{
			TimeStamp:         time.Now().UTC(),
			ActivePartition:   activePartition,
			InActivePartition: inactivePartition,
		}

		allPartitionMetadata = append(allPartitionMetadata, *paritionMetadata)

		jsonData, err := json.Marshal(allPartitionMetadata)
		if err != nil {
			return fmt.Errorf("Error marshaling JSON: %v", err)
		}

		err = os.WriteFile(metaDataFile, jsonData, 0644)
		if err != nil {
			return fmt.Errorf("Error marshaling JSON: %v", err)
		}

	} else {
		// Read metadata file and check for previous updates
		// var allPartitionMetadata []models.PartitionMetadata

		// metadataContent, err := os.ReadFile(metaDataFile)
		// if err != nil {
		// 	return fmt.Errorf("Error reading metadata file: %v", err)
		// }

		// err = json.Unmarshal(metadataContent, &allPartitionMetadata)
		// if err != nil {
		// 	return fmt.Errorf("Error unmarshaling JSON: %v", err)
		// }

		// lastMetadata := allPartitionMetadata[len(allPartitionMetadata) - 1]

		// lastMetadata.Updates[len(lastMetadata.Updates) - 1].Status !=

		// paritionMetadata := &models.PartitionMetadata{
		// 	TimeStamp:         time.Now().UTC(),
		// 	ActivePartition:   activePartition,
		// 	InActivePartition: inactivePartition,
		// }

		// allPartitionMetadata = append(allPartitionMetadata, *paritionMetadata)

		// jsonData, err := json.Marshal(allPartitionMetadata)
		// if err != nil {
		// 	return fmt.Errorf("Error marshaling JSON: %v", err)
		// }

		// err = os.WriteFile(metaDataFile, jsonData, 0644)
		// if err != nil {
		// 	return fmt.Errorf("Error writing metadata file: %v", err)
		// }

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

// handleUpdateCommand processes incoming MQTT update commands
func (u *UpdateService) handleUpdateCommand(client MQTT.Client, msg MQTT.Message) {
	// Parse UpdateCommandPayload
	var payload models.UpdateCommandPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		u.Logger.Error().Err(err).Msg("Failed to parse update command payload")
		return
	}

	u.Logger.Info().
		Str("UpdateURL", payload.FileUrl).
		Str("Version", payload.UpdateVersion).
		Msg("Parsed update command payload")

	// Proceed with the downloading state
	if err := u.setState(constants.UpdateStateDownloading); err != nil {
		u.Logger.Error().Err(err).Msg("Error updating state from idle to downloading")
		return
	}

	// Send mqtt message and get acknowledgment for downloading
	sharedTopic := "agent-updates"

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

	token := u.MqttClient.Publish(sharedTopic, byte(2), false, mqttPayloadBytes)
	token.Wait()
	if token.Error() != nil {
		u.Logger.Error().Err(token.Error()).Msg("Failed to publish for download message")
		return
	} else {
		u.Logger.Info().Str("status", "success").Msg("Published to " + sharedTopic)
	}

	// go routine for proceeding with next steps
	go func() {
		defer u.wg.Done()

		// Wait for acknowledgment
		// select {
		// case ack := <-u.ackChannel:
		for ack := range u.ackChannel {
			fmt.Println("ACK: ", ack)
			if ack.Status == string(constants.UpdateStateDownloading) {
				fmt.Println("DOWNLOADING...")
				// Download file the file and send mqtt message for installing
				if err := u.S3.DownloadFileByPresignedURL(payload.FileUrl, u.dataPartition.MountPoint+"/"+payload.FileName); err != nil {
					u.Logger.Error().Err(err).Msg("Failed to download file")
					return
				}

				// Update state to installing and send mqtt request
				if err := u.setState(constants.UpdateStateInstalling); err != nil {
					u.Logger.Error().Err(err).Msg("Error updating state from downloading to installing")
					return
				}

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

				token := u.MqttClient.Publish(sharedTopic, byte(2), false, mqttPayloadBytes)
				token.Wait()
				if token.Error() != nil {
					u.Logger.Error().Err(token.Error()).Msg("Failed to publish for installing message")
					return
				} else {
					u.Logger.Info().Str("status", "installing").Msg("Published to " + sharedTopic)
				}

			} else if ack.Status == string(constants.UpdateStateInstalling) {
				fmt.Println("INSTALLING...")
				// Install the update and send mqtt message for success
				updateZipFileLocation := u.dataPartition.MountPoint + "/" + payload.FileName
				if _, err := os.Stat(updateZipFileLocation); os.IsNotExist(err) {
					u.Logger.Error().Err(err).Msg(fmt.Sprintf("zip file does not exist in device: %s", err))
					return
				}

				// Unzip the .zip file
				fmt.Println(updateZipFileLocation, u.dataPartition.MountPoint)
				cmd := exec.Command("unzip", "-o", updateZipFileLocation, "-d", u.dataPartition.MountPoint) // -o to overwrite if extract already exists
				_, err := cmd.CombinedOutput()
				if err != nil {
					u.Logger.Error().Err(err).Msg(fmt.Sprintf("Error extracting the file: %v", err))
					return
				}

				updateFileLocation := updateZipFileLocation[:len(updateZipFileLocation)-4]
				fmt.Println(updateFileLocation)

				// Check if file has .deb extension
				if !strings.HasSuffix(strings.ToLower(updateFileLocation), ".deb") {
					u.Logger.Error().Err(err).Msg(fmt.Sprintf("Update file '%s' is not a .deb file", updateFileLocation))
					return
				}

				// Install .deb update file
				cmd = exec.Command("dpkg", "-i", updateFileLocation)
				_, err = cmd.CombinedOutput()
				if err != nil {
					u.Logger.Error().Err(err).Msg(fmt.Sprintf("Error installing update: %v", err))
					return
				}

				// Update state to success and send mqtt request
				if err := u.setState(constants.UpdateStateSuccess); err != nil {
					u.Logger.Error().Err(err).Msg("Error updating state from installing to success")
					return
				}

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

				token := u.MqttClient.Publish(sharedTopic, byte(2), false, mqttPayloadBytes)
				token.Wait()
				if token.Error() != nil {
					u.Logger.Error().Err(token.Error()).Msg("Failed to publish for success message")
					return
				} else {
					u.Logger.Info().Str("status", "success").Msg("Published to " + sharedTopic)
				}

			} else if ack.Status == string(constants.UpdateStateSuccess) {
				// Success and write the metadata file
				// metaDataFile := u.dataPartition.MountPoint + "/updates-metadata.json"
				// metadataContent, err := os.ReadFile(metaDataFile)
				// if err != nil {
				// 	return fmt.Errorf("Error reading metadata file: %v", err)
				// }
			} else {
				// Error when sending mqtt request
			}
			// case <-time.After(10 * time.Second):
			// 	// Retry

			// }
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

	fmt.Println(string(msg.Payload()))
	fmt.Println("ACK DATA: ", payload)
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
