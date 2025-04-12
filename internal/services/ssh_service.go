package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"
	mqtt_middleware "github.com/benmeehan/iot-agent/internal/middlewares/mqtt"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/ssh"
)

// SSHService manages SSH connections and port forwarding via MQTT.
type SSHService struct {
	// Configuration
	topic string
	qos   int

	// SSH user credentials
	sshUser               string
	privateKeyPath        string
	serverPublicKeyPath   string
	cachedPrivateKey      ssh.Signer
	cachedServerPublicKey ssh.PublicKey

	// Dependencies
	deviceInfo     identity.DeviceInfoInterface
	mqttMiddleware mqtt_middleware.MQTTAuthMiddleware
	fileClient     file.FileOperations
	logger         zerolog.Logger

	/// Connection settings
	connectionTimeout time.Duration
	maxSSHConnections int
	sshClients        map[string]*ssh.Client
	sshClientsMux     sync.Mutex

	// Internal state
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewSSHService initializes a new SSHService instance.
func NewSSHService(subTopic string, qos int, sshUser, privateKeyPath, serverPublicKeyPath string, deviceInfo identity.DeviceInfoInterface,
	mqttMiddleware mqtt_middleware.MQTTAuthMiddleware, fileClient file.FileOperations, logger zerolog.Logger, maxSSHConnections int,
	connectionTimeout time.Duration) *SSHService {

	ctx, cancel := context.WithCancel(context.Background())

	if maxSSHConnections == 0 {
		maxSSHConnections = constants.MaxSSHConnections
	}
	if connectionTimeout == 0 {
		connectionTimeout = constants.ConnectionTimeout
	}

	return &SSHService{
		topic:               subTopic,
		qos:                 qos,
		sshUser:             sshUser,
		privateKeyPath:      privateKeyPath,
		serverPublicKeyPath: serverPublicKeyPath,
		deviceInfo:          deviceInfo,
		mqttMiddleware:      mqttMiddleware,
		fileClient:          fileClient,
		logger:              logger,
		connectionTimeout:   connectionTimeout,
		maxSSHConnections:   maxSSHConnections,
		sshClients:          make(map[string]*ssh.Client),
		ctx:                 ctx,
		cancel:              cancel,
	}
}

// Start initializes the SSH service by loading SSH keys and subscribing to the MQTT topic.
func (s *SSHService) Start() error {
	s.logger.Info().Msg("Starting SSH service...")

	// Read and parse the private key
	key, err := s.fileClient.ReadFileRaw(s.privateKeyPath)
	if err != nil {
		s.logger.Error().Err(err).Str("path", s.privateKeyPath).Msg("Failed to read SSH private key")
		return fmt.Errorf("failed to read SSH private key: %w", err)
	}

	privateKey, err := ssh.ParsePrivateKey(key)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to parse SSH private key")
		return fmt.Errorf("failed to parse SSH private key: %w", err)
	}
	s.cachedPrivateKey = privateKey
	s.logger.Debug().Msg("SSH private key loaded successfully")

	// Read and parse the server public key
	serverKey, err := s.fileClient.ReadFileRaw(s.serverPublicKeyPath)
	if err != nil {
		s.logger.Error().Err(err).Str("path", s.serverPublicKeyPath).Msg("Failed to read SSH server public key")
		return fmt.Errorf("failed to read SSH server public key: %w", err)
	}

	// Validate key content
	if len(serverKey) == 0 {
		err := errors.New("empty public key file")
		s.logger.Error().Err(err).Str("path", s.serverPublicKeyPath).Msg("Invalid SSH server public key")
		return fmt.Errorf("invalid SSH server public key: %w", err)
	}

	// Parse the public key in OpenSSH format
	publicKey, _, _, _, err := ssh.ParseAuthorizedKey(serverKey)
	if err != nil {
		s.logger.Error().Err(err).Str("path", s.serverPublicKeyPath).Msg("Failed to parse SSH server public key")
		return fmt.Errorf("failed to parse SSH server public key: %w", err)
	}
	s.cachedServerPublicKey = publicKey
	s.logger.Debug().Msg("SSH server public key loaded successfully")

	// Subscribe to MQTT topic
	topic := fmt.Sprintf("%s/%s", s.topic, s.deviceInfo.GetDeviceID())
	if err := s.subscribeToTopic(topic); err != nil {
		return err
	}

	s.logger.Info().Msg("SSH service started successfully")
	return nil
}

// Stop terminates the SSH service by unsubscribing from the MQTT topic and closing all SSH connections.
func (s *SSHService) Stop() error {
	s.logger.Info().Msg("Stopping SSH service...")

	// Cancel the context to signal all goroutines to stop
	s.cancel()

	// Unsubscribe from MQTT topic
	topic := fmt.Sprintf("%s/%s", s.topic, s.deviceInfo.GetDeviceID())
	if err := s.mqttMiddleware.Unsubscribe(topic); err != nil {
		s.logger.Error().Err(err).Str("topic", topic).Msg("Failed to unsubscribe from MQTT topic")
	}

	// Close all SSH connections
	s.sshClientsMux.Lock()
	defer s.sshClientsMux.Unlock()

	for host, client := range s.sshClients {
		if client != nil {
			if err := client.Close(); err != nil {
				s.logger.Error().Err(err).Str("host", host).Msg("Failed to close SSH connection")
			} else {
				s.logger.Debug().Str("host", host).Msg("Closed SSH connection")
			}
		}
		delete(s.sshClients, host)
	}

	// Wait for all goroutines to finish
	s.wg.Wait()

	s.logger.Info().Msg("SSH service stopped successfully")
	return nil
}

// subscribeToTopic subscribes to the MQTT topic for SSH requests.
func (s *SSHService) subscribeToTopic(topic string) error {
	s.logger.Info().Str("topic", topic).Msg("Subscribing to MQTT topic")
	err := s.mqttMiddleware.Subscribe(topic, byte(s.qos), s.handleSSHRequest)
	if err != nil {
		s.logger.Error().Err(err).Str("topic", topic).Msg("Failed to subscribe to MQTT topic")
		return err
	}
	s.logger.Debug().Str("topic", topic).Msg("Subscribed to MQTT topic successfully")
	return nil
}

// handleSSHRequest processes incoming SSH requests from MQTT.
func (s *SSHService) handleSSHRequest(client MQTT.Client, msg MQTT.Message) {
	var request models.SSHRequest
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		s.logger.Error().Err(err).Bytes("payload", msg.Payload()).Msg("Invalid SSH request payload")
		return
	}

	s.logger.Info().
		Int("local_port", request.LocalPort).
		Int("remote_port", request.RemotePort).
		Int("backend_port", request.BackendPort).
		Str("backend_host", request.BackendHost).
		Msg("Received SSH request")

	err := s.EstablishSSHTunnel(request)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to establish SSH tunnel")
		return
	}
	s.logger.Debug().Msg("SSH tunnel established successfully")
}

// createSSHClientConfig creates the SSH client configuration using the cached private key and server public key.
func (s *SSHService) createSSHClientConfig() *ssh.ClientConfig {
	config := &ssh.ClientConfig{
		User: s.sshUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(s.cachedPrivateKey),
		},
		// HostKeyCallback: ssh.FixedHostKey(s.cachedServerPublicKey),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // For testing purposes only, use a proper callback in production
		Timeout:         s.connectionTimeout,
	}

	return config
}

// EstablishSSHTunnel establishes an SSH tunnel to the backend server.
func (s *SSHService) EstablishSSHTunnel(request models.SSHRequest) error {
	s.logger.Info().Msg("Establishing SSH tunnel...")

	// Check if we've reached maximum connections
	s.sshClientsMux.Lock()
	if len(s.sshClients) >= s.maxSSHConnections {
		s.sshClientsMux.Unlock()
		return fmt.Errorf("maximum SSH connections (%d) reached", s.maxSSHConnections)
	}
	s.sshClientsMux.Unlock()

	serverAddr := fmt.Sprintf("%s:%d", request.BackendHost, request.BackendPort)

	// Check for existing connection
	s.sshClientsMux.Lock()
	client, exists := s.sshClients[request.BackendHost]
	s.sshClientsMux.Unlock()

	if !exists {
		// Create new connection
		clientConfig := s.createSSHClientConfig()

		var err error
		client, err = ssh.Dial("tcp", serverAddr, clientConfig)
		if err != nil {
			s.logger.Error().Err(err).Str("server_addr", serverAddr).Msg("Failed to establish SSH tunnel")
			return fmt.Errorf("failed to establish SSH tunnel: %w", err)
		}

		// Add to connections map
		s.sshClientsMux.Lock()
		s.sshClients[request.BackendHost] = client
		s.sshClientsMux.Unlock()

		s.logger.Debug().Str("server_addr", serverAddr).Msg("SSH tunnel established successfully")

		// Start handling channels for this connection
		go s.handleAgentChannels(client)
	}

	info := &models.AgentPortInfo{
		RemotePort: request.RemotePort,
		LocalPort:  request.LocalPort,
	}
	payload, err := json.Marshal(info)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to marshal SSH request payload")
		return fmt.Errorf("failed to marshal SSH request payload: %w", err)
	}

	ok, _, err := client.SendRequest("register", true, payload)
	if err != nil || !ok {
		return errors.New("failed to register tunnel")
	}

	s.logger.Info().Msgf("Registered tunnel: %s:%d ←→ localhost:%d", request.BackendHost, request.RemotePort, request.LocalPort)

	// Start keepalive monitoring in a goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.waitUntilDisconnected(client, request.BackendHost)
	}()

	return nil
}

// waitUntilDisconnected waits until the SSH client is disconnected or times out.
func (s *SSHService) waitUntilDisconnected(client *ssh.Client, backendHost string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			// Service is stopping, exit
			return
		case <-ticker.C:
			_, _, err := client.Conn.SendRequest("keepalive", true, nil)
			if err != nil {
				s.logger.Error().Err(err).Str("host", backendHost).Msg("SSH client disconnected")

				// Remove from connections map
				s.sshClientsMux.Lock()
				if existingClient, ok := s.sshClients[backendHost]; ok && existingClient == client {
					delete(s.sshClients, backendHost)
				}
				s.sshClientsMux.Unlock()

				return
			}
		}
	}
}

// handleAgentChannels listens for incoming channels on the SSH client and forwards them to the appropriate destination.
func (s *SSHService) handleAgentChannels(client *ssh.Client) {
	for newChannel := range client.HandleChannelOpen("direct-tcpip") {
		go func(nc ssh.NewChannel) {
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error().Msgf("Recovered from panic: %v", r)
					nc.Reject(ssh.Prohibited, "internal error")
				}
			}()

			var channelData struct {
				DestAddr string
				DestPort uint32
				SrcAddr  string
				SrcPort  uint32
			}

			if err := ssh.Unmarshal(nc.ExtraData(), &channelData); err != nil {
				nc.Reject(ssh.Prohibited, "invalid channel data")
				return
			}

			conn, err := net.DialTimeout("tcp", channelData.DestAddr, 5*time.Second)
			if err != nil {
				nc.Reject(ssh.ConnectionFailed, err.Error())
				return
			}
			defer conn.Close()

			channel, requests, err := nc.Accept()
			if err != nil {
				s.logger.Error().Err(err).Msg("Failed to accept channel")
				return
			}
			defer channel.Close()

			go ssh.DiscardRequests(requests)

			var wg sync.WaitGroup
			wg.Add(2)

			copyFunc := func(dst io.Writer, src io.Reader, name string) {
				defer wg.Done()
				_, err := io.Copy(dst, src)
				if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					s.logger.Error().Err(err).Str("name", name).Msg("Error during copy")
				}
			}

			go copyFunc(channel, conn, "local→remote")
			go copyFunc(conn, channel, "remote→local")

			wg.Wait()
		}(newChannel)
	}

	s.logger.Debug().Msg("SSH channel handler stopped")
}
