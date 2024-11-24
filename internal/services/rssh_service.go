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
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

// SSHService defines the structure of the SSH service
type SSHService struct {
	SubTopic        string
	DeviceInfo      identity.DeviceInfoInterface
	MqttClient      mqtt.MQTTClient
	Logger          *logrus.Logger
	BackendHost     string
	BackendPort     int
	SSHUser         string
	PrivateKeyPath  string
	FileClient      file.FileOperations
	QOS             int
	listeners       map[int]net.Listener
	clientPool      cmap.ConcurrentMap[string, *ssh.Client]
	clientListeners cmap.ConcurrentMap[string, int]
}

// Start listens for incoming SSH port requests via MQTT and starts the reverse SSH process
func (s *SSHService) Start() error {
	s.Logger.Info("Starting SSH service...")

	topic := fmt.Sprintf("%s/%s", s.SubTopic, s.DeviceInfo.GetDeviceID())
	if err := s.subscribeToTopic(topic); err != nil {
		return err
	}

	s.listeners = make(map[int]net.Listener)
	s.clientPool = cmap.New[*ssh.Client]()
	s.clientListeners = cmap.New[int]()
	return nil
}

// subscribeToTopic subscribes to the specified MQTT topic
func (s *SSHService) subscribeToTopic(topic string) error {
	s.Logger.WithField("topic", topic).Info("Subscribing to MQTT topic")

	token := s.MqttClient.Subscribe(topic, byte(s.QOS), s.handleSSHRequest)
	token.Wait()
	if err := token.Error(); err != nil {
		s.Logger.WithError(err).Error("Failed to subscribe to SSH request topic")
		return err
	}

	s.Logger.Info("Successfully subscribed to SSH request topic")
	return nil
}

// logAndReturnError logs the error with a message and returns it
func (s *SSHService) logAndReturnError(message string, err error) error {
	s.Logger.WithError(err).Error(message)
	return err
}

// handleSSHRequest processes the incoming MQTT message with the requested port
func (s *SSHService) handleSSHRequest(client MQTT.Client, msg MQTT.Message) {
	var request models.SSHRequest
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		s.Logger.WithError(err).Error("Failed to unmarshal SSH request message")
		return
	}

	s.Logger.WithFields(logrus.Fields{
		"local_port":   request.LocalPort,
		"remote_port":  request.RemotePort,
		"backend_host": request.BackendHost,
	}).Info("Received reverse SSH port request")

	if err := s.startReverseSSH(request.LocalPort, request.RemotePort, request.BackendHost); err != nil {
		s.Logger.WithError(err).Error("Failed to start reverse SSH")
	}
}

// startReverseSSH establishes the reverse SSH connection with port forwarding
func (s *SSHService) startReverseSSH(localPort, remotePort int, backendHost string) error {
	// Read and parse the private SSH key
	s.Logger.WithField("private_key_path", s.PrivateKeyPath).Info("Reading private SSH key")
	key, err := s.FileClient.ReadFile(s.PrivateKeyPath)
	if err != nil {
		return s.logAndReturnError("Failed to read private SSH key", err)
	}

	privateKey, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return s.logAndReturnError("Failed to parse private SSH key", err)
	}

	// SSH client configuration
	config := &ssh.ClientConfig{
		User:            s.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(privateKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	// Get or create an SSH client for the backend host
	client, exists := s.clientPool.Get(backendHost)
	if !exists {
		connStr := fmt.Sprintf("%s:%d", backendHost, s.BackendPort)
		s.Logger.WithField("connection_string", connStr).Info("Establishing SSH connection")

		client, err = ssh.Dial("tcp", connStr, config)
		if err != nil {
			return s.logAndReturnError("Failed to establish SSH connection", err)
		}

		s.clientPool.Set(backendHost, client)
		s.clientListeners.Set(backendHost, 0)
	}

	// Increment listener count for the backend host
	s.clientListeners.Upsert(backendHost, 1, func(exists bool, value int, _ int) int {
		return value + 1
	})

	// Set up port forwarding
	listener, err := client.Listen("tcp", fmt.Sprintf("localhost:%d", remotePort))
	if err != nil {
		return s.logAndReturnError("Failed to set up port forwarding", err)
	}

	// Store the listener and log the success
	s.listeners[remotePort] = listener
	s.Logger.WithFields(logrus.Fields{
		"local_port":  localPort,
		"remote_port": remotePort,
	}).Info("Port forwarding established")

	// Handle incoming connections
	go s.acceptConnections(listener, localPort, remotePort, backendHost)

	return nil
}

// acceptConnections accepts incoming connections on the listener
func (s *SSHService) acceptConnections(listener net.Listener, localPort, remotePort int, backendHost string) {
	defer func() {
		listener.Close()
		delete(s.listeners, remotePort)

		v, _ := s.clientListeners.Get(backendHost)
		s.clientListeners.Set(backendHost, v-1)

		// Decrement the listener count for the backend host
		if count, ok := s.clientListeners.Get(backendHost); ok {
			newCount := count - 1
			if newCount > 0 {
				s.clientListeners.Set(backendHost, newCount)
			} else {
				// If no active listeners remain, close the SSH client
				s.Logger.WithField("backend_host", backendHost).Info("No active listeners, closing SSH client")
				if client, exists := s.clientPool.Get(backendHost); exists {
					client.Close()
					s.clientPool.Remove(backendHost)
				}
				s.clientListeners.Remove(backendHost)
			}
		}
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			s.Logger.WithError(err).Error("Error accepting connection")
			return // Exit on accept error
		}

		go s.forwardConnection(conn, localPort)
	}
}

// forwardConnection forwards traffic between local and remote ports
func (s *SSHService) forwardConnection(conn net.Conn, localPort int) {
	defer conn.Close()

	localConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", localPort))
	if err != nil {
		s.Logger.WithError(err).Error("Failed to connect to local service")
		return
	}
	defer localConn.Close()

	go func() {
		if _, err := io.Copy(localConn, conn); err != nil {
			s.Logger.WithError(err).Error("Error forwarding data from remote to local")
		}
	}()

	if _, err := io.Copy(conn, localConn); err != nil {
		s.Logger.WithError(err).Error("Error forwarding data from local to remote")
	}

	s.Logger.WithFields(logrus.Fields{
		"local_port":  localPort,
		"remote_port": conn.RemoteAddr().(*net.TCPAddr).Port,
	}).Info("Finished forwarding connection")
}
