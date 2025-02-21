package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/ssh"
)

// SSHService handles reverse SSH tunneling
type SSHService struct {
	SubTopic        string
	DeviceInfo      identity.DeviceInfoInterface
	MqttClient      mqtt.MQTTClient
	Logger          zerolog.Logger
	BackendHost     string
	BackendPort     int
	SSHUser         string
	PrivateKeyPath  string
	FileClient      file.FileOperations
	QOS             int
	listeners       map[int]net.Listener
	clientPool      cmap.ConcurrentMap[string, *ssh.Client]
	clientListeners cmap.ConcurrentMap[string, int]
	stopChan        chan struct{}
}

// NewSSHService creates and returns a new instance of SSHService.
func NewSSHService(subTopic string, deviceInfo identity.DeviceInfoInterface, mqttClient mqtt.MQTTClient,
	logger zerolog.Logger, backendHost string, backendPort int, sshUser string, privateKeyPath string,
	fileClient file.FileOperations, qos int) *SSHService {

	return &SSHService{
		SubTopic:        subTopic,
		DeviceInfo:      deviceInfo,
		MqttClient:      mqttClient,
		Logger:          logger,
		BackendHost:     backendHost,
		BackendPort:     backendPort,
		SSHUser:         sshUser,
		PrivateKeyPath:  privateKeyPath,
		FileClient:      fileClient,
		QOS:             qos,
		listeners:       make(map[int]net.Listener),
		clientPool:      cmap.New[*ssh.Client](),
		clientListeners: cmap.New[int](),
		stopChan:        make(chan struct{}),
	}
}

// Start subscribes to MQTT topics and listens for SSH requests
func (s *SSHService) Start() error {
	s.Logger.Info().Msg("Starting SSH service...")

	topic := fmt.Sprintf("%s/%s", s.SubTopic, s.DeviceInfo.GetDeviceID())
	if err := s.subscribeToTopic(topic); err != nil {
		return err
	}

	s.listeners = make(map[int]net.Listener)
	s.clientPool = cmap.New[*ssh.Client]()
	s.clientListeners = cmap.New[int]()
	s.stopChan = make(chan struct{})

	return nil
}

// Stop gracefully shuts down the SSH service
func (s *SSHService) Stop() error {
	s.Logger.Info().Msg("Stopping SSH service...")

	// Close all active SSH connections
	s.clientPool.IterCb(func(key string, client *ssh.Client) {
		s.Logger.Info().Str("backend_host", key).Msg("Closing SSH connection")
		err := client.Close()
		if err != nil {
			s.Logger.Error().Err(err).Msg("Failed to close SSH connection")
		}
		s.clientPool.Remove(key)
	})

	// Close all active listeners
	for port, listener := range s.listeners {
		s.Logger.Info().Int("port", port).Msg("Closing SSH listener")
		err := listener.Close()
		if err != nil {
			s.Logger.Error().Err(err).Msg("Failed to close listener")
			return err
		}
		delete(s.listeners, port)
	}

	// Unsubscribe from MQTT topic
	topic := fmt.Sprintf("%s/%s", s.SubTopic, s.DeviceInfo.GetDeviceID())
	s.Logger.Info().Str("topic", topic).Msg("Unsubscribing from MQTT topic")
	token := s.MqttClient.Unsubscribe(topic)
	token.Wait()

	if err := token.Error(); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to unsubscribe from MQTT topic")
		return err
	}

	// Close stop channel
	close(s.stopChan)
	s.Logger.Info().Msg("SSH service stopped")
	return nil
}

// subscribeToTopic subscribes to the MQTT topic for SSH requests
func (s *SSHService) subscribeToTopic(topic string) error {
	s.Logger.Info().Str("topic", topic).Msg("Subscribing to MQTT topic")

	token := s.MqttClient.Subscribe(topic, byte(s.QOS), s.handleSSHRequest)
	token.Wait()
	if err := token.Error(); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to subscribe to SSH request topic")
		return err
	}

	s.Logger.Info().Msg("Successfully subscribed to SSH request topic")
	return nil
}

// handleSSHRequest processes the incoming MQTT message with the requested port
func (s *SSHService) handleSSHRequest(client MQTT.Client, msg MQTT.Message) {
	var request models.SSHRequest
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to unmarshal SSH request message")
		return
	}

	s.Logger.Info().
		Int("local_port", request.LocalPort).
		Int("remote_port", request.RemotePort).
		Str("backend_host", request.BackendHost).
		Msg("Received reverse SSH port request")

	if err := s.startReverseSSH(request.LocalPort, request.RemotePort, request.BackendHost, request.ServerID); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to start reverse SSH")
	}
}

// startReverseSSH sets up the reverse SSH tunnel
func (s *SSHService) startReverseSSH(localPort, remotePort int, backendHost, serverId string) error {
	s.Logger.Info().Str("private_key_path", s.PrivateKeyPath).Msg("Reading private SSH key")
	key, err := s.FileClient.ReadFile(s.PrivateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read private SSH key: %w", err)
	}

	privateKey, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return fmt.Errorf("failed to parse private SSH key: %w", err)
	}

	config := &ssh.ClientConfig{
		User:            s.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(privateKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	client, exists := s.clientPool.Get(backendHost)
	if !exists {
		connStr := fmt.Sprintf("%s:%d", backendHost, s.BackendPort)
		s.Logger.Info().Str("connection_string", connStr).Msg("Establishing SSH connection")

		client, err = ssh.Dial("tcp", connStr, config)
		if err != nil {
			return fmt.Errorf("failed to establish SSH connection: %w", err)
		}

		s.clientPool.Set(backendHost, client)
		s.clientListeners.Set(backendHost, 0)
	}

	listener, err := client.Listen("tcp", fmt.Sprintf("localhost:%d", localPort))
	if err != nil {
		return fmt.Errorf("failed to set up port forwarding: %w", err)
	}

	s.listeners[remotePort] = listener
	s.Logger.Info().
		Int("local_port", localPort).
		Int("remote_port", remotePort).
		Msg("Port forwarding established")

	s.publishDeviceReply(s.DeviceInfo.GetDeviceID(), serverId, localPort, remotePort)

	go s.acceptConnections(listener, remotePort)

	return nil
}

// acceptConnections handles incoming connections on the listener
func (s *SSHService) acceptConnections(listener net.Listener, remotePort int) {
	defer listener.Close()

	for {
		select {
		case <-s.stopChan:
			s.Logger.Info().Int("remote_port", remotePort).Msg("Stopping listener")
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				s.Logger.Error().Err(err).Msg("Error accepting connection")
				return
			}
			go s.forwardConnection(conn, remotePort)
		}
	}
}

// forwardConnection forwards traffic between local and remote ports
func (s *SSHService) forwardConnection(conn net.Conn, remotePort int) {
	defer conn.Close()

	localConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", remotePort))
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to connect to local service")
		return
	}
	defer localConn.Close()

	go io.Copy(localConn, conn)
	io.Copy(conn, localConn)

	s.Logger.Info().
		Int("local_port", remotePort).
		Msg("Finished forwarding connection")
}

// publishDeviceReply sends a DeviceReply message over MQTT
func (s *SSHService) publishDeviceReply(deviceID, serverId string, localPort, remotePort int) {
	reply := models.DeviceReply{
		DeviceID: deviceID,
		Lport:    localPort,
		Rport:    remotePort,
	}

	payload, err := json.Marshal(reply)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to marshal DeviceReply message")
		return
	}

	replyTopic := fmt.Sprintf("%s/%s", s.SubTopic, serverId)
	token := s.MqttClient.Publish(replyTopic, byte(s.QOS), false, payload)
	token.Wait()

	if err := token.Error(); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to publish DeviceReply message")
	} else {
		s.Logger.Info().Msg("Published DeviceReply message")
	}
}
