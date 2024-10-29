package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

// SSHService defines the structure of the SSH service
type SSHService struct {
	SubTopic       string
	DeviceInfo     identity.DeviceInfoInterface
	MqttClient     mqtt.MQTTClient
	Logger         *logrus.Logger
	BackendHost    string
	BackendPort    int
	SSHUser        string
	SSHKey         []byte
	PrivateKeyPath string
	FileClient     file.FileOperations
	QOS            int
	sshClient      *ssh.Client
	listeners      map[int]net.Listener
}

// SSHRequest represents the structure of the MQTT message that contains the SSH port request
type SSHRequest struct {
	Lport int `json:"lport"`
	Rport int `json:"rport"`
}

// Start listens for incoming SSH port requests via MQTT and starts the reverse SSH process
func (s *SSHService) Start() error {
	s.Logger.Info("Starting SSH service...")

	topic := fmt.Sprintf("%s/%s", s.SubTopic, s.DeviceInfo.GetDeviceID())
	s.Logger.WithField("topic", topic).Info("Subscribing to MQTT topic")

	token := s.MqttClient.Subscribe(topic, byte(s.QOS), s.handleSSHRequest)
	token.Wait()
	if token.Error() != nil {
		s.Logger.WithError(token.Error()).Error("Failed to subscribe to SSH request topic")
		return token.Error()
	}
	s.Logger.Info("Successfully subscribed to SSH request topic")

	if err := s.establishSSHConnection(); err != nil {
		s.Logger.WithError(err).Error("Failed to establish SSH connection")
		return err
	}

	s.listeners = make(map[int]net.Listener)

	s.Logger.Info("SSH connection established successfully")

	return nil
}

// establishSSHConnection creates a single SSH connection for reuse
func (s *SSHService) establishSSHConnection() error {
	s.Logger.WithField("privatekey_path", s.PrivateKeyPath).Info("Reading private SSH key")

	key, err := s.FileClient.ReadFile(s.PrivateKeyPath)
	if err != nil {
		s.Logger.WithError(err).Error("Failed to read private SSH key")
		return err
	}

	s.Logger.Info("Parsing private SSH key")
	privateKey, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		s.Logger.WithError(err).Error("Failed to parse private SSH key")
		return err
	}

	config := &ssh.ClientConfig{
		User: s.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(privateKey),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	connStr := fmt.Sprintf("%s:%d", s.BackendHost, s.BackendPort)
	s.Logger.WithField("connection_string", connStr).Info("Establishing SSH connection")

	client, err := ssh.Dial("tcp", connStr, config)
	if err != nil {
		s.Logger.WithError(err).Error("Failed to establish SSH connection")
		return err
	}

	s.sshClient = client // Store the SSH client for reuse
	s.Logger.Info("SSH connection established successfully")
	return nil
}

// handleSSHRequest processes the incoming MQTT message with the requested port
func (s *SSHService) handleSSHRequest(client MQTT.Client, msg MQTT.Message) {
	var request SSHRequest
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		s.Logger.WithError(err).Error("Failed to unmarshal SSH request message")
		return
	}

	s.Logger.WithField("remote_port", request.Rport).Info("Received Reverse SSH port request")

	// Start reverse SSH process
	err := s.startReverseSSH(request.Lport, request.Rport)
	if err != nil {
		s.Logger.WithError(err).Error("Failed to start reverse SSH")
		return
	}
}

// startReverseSSH establishes the reverse SSH connection with port forwarding using crypto/ssh
func (s *SSHService) startReverseSSH(lport, rport int) error {
	if s.sshClient == nil {
		s.Logger.Error("SSH connection is not established")
		return fmt.Errorf("SSH connection is not established")
	}

	// Check if listener for the given remote port already exists
	if _, exists := s.listeners[rport]; exists {
		s.Logger.WithField("remote_port", rport).Warn("Listener already exists for this remote port, ignoring request")
		return nil // or return a specific error if desired
	}

	listener, err := s.sshClient.Listen("tcp", fmt.Sprintf("localhost:%d", rport))
	if err != nil {
		s.Logger.WithError(err).Error("Failed to set up port forwarding")
		return err
	}

	s.listeners[rport] = listener

	s.Logger.WithFields(logrus.Fields{
		"local_port":  lport,
		"remote_port": rport,
	}).Info("Port forwarding established")

	// Start a goroutine to handle incoming connections for this listener
	go func() {
		defer func() {
			listener.Close()
			delete(s.listeners, rport) // Remove listener from map once closed
			s.Logger.WithField("remote_port", rport).Info("Port forwarding listener closed")
		}()

		for {
			conn, err := listener.Accept()
			if err != nil {
				s.Logger.WithError(err).Error("Error accepting connection")
				break // Exit on accept error, listener will close
			}

			go s.forwardConnection(conn, lport)
		}
	}()

	return nil
}

// forwardConnection forwards traffic between local and remote ports
func (s *SSHService) forwardConnection(conn net.Conn, lport int) {
	s.Logger.WithField("local_port", lport).Info("Accepting new reverse SSH connection")
	defer conn.Close()

	localConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", lport))
	if err != nil {
		s.Logger.WithError(err).Error("Failed to connect to local service")
		return
	}
	defer localConn.Close()

	s.Logger.WithField("local_port", lport).Info("Forwarding connection to local service")

	// Use goroutines to copy data in both directions
	go func() {
		if _, err := io.Copy(localConn, conn); err != nil {
			s.Logger.WithError(err).Error("Error forwarding data from remote to local")
		}
	}()

	if _, err := io.Copy(conn, localConn); err != nil {
		s.Logger.WithError(err).Error("Error forwarding data from local to remote")
	}
	s.Logger.WithField("local_port", lport).Info("Finished forwarding connection")
}
