package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/benmeehan/iot-agent/internal/constants"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	"github.com/benmeehan/iot-agent/pkg/mqtt"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/ssh"
)

// SSHService manages SSH connections and port forwarding via MQTT.
type SSHService struct {
	DeviceInfo        identity.DeviceInfoInterface
	MqttClient        mqtt.MQTTClient
	FileClient        file.FileOperations
	Logger            zerolog.Logger
	SubTopic          string
	SSHUser           string
	PrivateKeyPath    string
	QOS               int
	ctx               context.Context
	cancel            context.CancelFunc
	cachedPrivateKey  ssh.Signer
	activeConns       int32
	activeListeners   int32
	clientPool        map[string]models.SSHClientWrapper
	clientPoolMutex   sync.RWMutex
	listeners         map[int]net.Listener
	listenersMutex    sync.RWMutex
	wg                sync.WaitGroup
	MaxListeners      int
	MaxSSHConnections int
	ConnectionTimeout time.Duration
	AutoDisconnect    time.Duration
}

// NewSSHService initializes a new SSHService instance.
func NewSSHService(subTopic string, deviceInfo identity.DeviceInfoInterface, mqttClient mqtt.MQTTClient,
	logger zerolog.Logger, sshUser string, privateKeyPath string,
	fileClient file.FileOperations, qos int, maxListeners, maxSSHConnections int,
	connectionTimeout, autoDisconnect time.Duration) *SSHService {

	ctx, cancel := context.WithCancel(context.Background())

	if maxListeners == 0 {
		maxListeners = constants.MaxListeners
	}
	if maxSSHConnections == 0 {
		maxSSHConnections = constants.MaxSSHConnections
	}
	if connectionTimeout == 0 {
		connectionTimeout = constants.ConnectionTimeout
	}
	if autoDisconnect == 0 {
		autoDisconnect = constants.AutoDisconnect
	}

	return &SSHService{
		SubTopic:          subTopic,
		DeviceInfo:        deviceInfo,
		MqttClient:        mqttClient,
		Logger:            logger,
		SSHUser:           sshUser,
		PrivateKeyPath:    privateKeyPath,
		FileClient:        fileClient,
		QOS:               qos,
		ctx:               ctx,
		cancel:            cancel,
		clientPool:        make(map[string]models.SSHClientWrapper),
		listeners:         make(map[int]net.Listener),
		MaxListeners:      maxListeners,
		MaxSSHConnections: maxSSHConnections,
		ConnectionTimeout: connectionTimeout,
		AutoDisconnect:    autoDisconnect,
	}
}

// Start initializes the SSH service and subscribes to MQTT topics.
func (s *SSHService) Start() error {
	s.Logger.Info().Msg("Starting SSH service...")
	key, err := s.FileClient.ReadFile(s.PrivateKeyPath)
	if err != nil {
		s.Logger.Error().Err(err).Str("path", s.PrivateKeyPath).Msg("Failed to read SSH private key")
		return fmt.Errorf("failed to read SSH private key: %w", err)
	}

	privateKey, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to parse SSH private key")
		return fmt.Errorf("failed to parse SSH private key: %w", err)
	}
	s.cachedPrivateKey = privateKey
	s.Logger.Debug().Msg("SSH private key loaded successfully")

	topic := fmt.Sprintf("%s/%s", s.SubTopic, s.DeviceInfo.GetDeviceID())
	if err := s.subscribeToTopic(topic); err != nil {
		return err
	}

	go s.cleanupExpiredConnections()
	s.Logger.Info().Msg("SSH service started successfully")
	return nil
}

func (s *SSHService) Stop() error {
	s.Logger.Info().Msg("Stopping SSH service...")
	s.cancel()

	s.listenersMutex.Lock()
	for port, listener := range s.listeners {
		s.Logger.Debug().Int("port", port).Msg("Closing listener")
		if err := listener.Close(); err != nil {
			s.Logger.Warn().Err(err).Int("port", port).Msg("Error closing listener")
		}
	}
	s.listeners = make(map[int]net.Listener)
	s.listenersMutex.Unlock()

	s.Logger.Debug().Msg("Waiting for all goroutines to finish")
	s.wg.Wait()

	s.clientPoolMutex.Lock()
	for backendHost, wrapper := range s.clientPool {
		s.Logger.Debug().Str("backend", backendHost).Msg("Closing SSH connection")
		if err := wrapper.Client.Close(); err != nil {
			s.Logger.Warn().Err(err).Str("backend", backendHost).Msg("Error closing SSH connection")
		}
	}
	s.clientPool = make(map[string]models.SSHClientWrapper)
	s.clientPoolMutex.Unlock()

	topic := fmt.Sprintf("%s/%s", s.SubTopic, s.DeviceInfo.GetDeviceID())
	s.MqttClient.Unsubscribe(topic).Wait()

	s.Logger.Info().Msg("SSH service stopped successfully")
	return nil
}

// cleanupExpiredConnections periodically removes stale SSH connections.
func (s *SSHService) cleanupExpiredConnections() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			s.Logger.Debug().Msg("Stopping cleanup of expired connections")
			return
		case <-ticker.C:
			now := time.Now()
			s.clientPoolMutex.Lock()
			for backendHost, wrapper := range s.clientPool {
				if now.Sub(wrapper.StartTime) > s.AutoDisconnect {
					s.Logger.Info().Str("backend", backendHost).Msg("Auto-disconnecting stale SSH connection")
					if err := wrapper.Client.Close(); err != nil {
						s.Logger.Warn().Err(err).Str("backend", backendHost).Msg("Error closing stale connection")
					}
					delete(s.clientPool, backendHost)
				}
			}
			s.clientPoolMutex.Unlock()
		}
	}
}

// monitorSSHConnection checks the health of the SSH connection.
func (s *SSHService) monitorSSHConnection(backendHost string, client *ssh.Client) {
	s.wg.Add(1)
	defer s.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			s.Logger.Debug().Str("backend", backendHost).Msg("Stopping SSH connection monitor")
			return
		case <-ticker.C:
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				s.Logger.Warn().Str("backend", backendHost).Err(err).Msg("SSH connection lost")
				s.clientPoolMutex.Lock()
				delete(s.clientPool, backendHost)
				s.clientPoolMutex.Unlock()
				client.Close()
				atomic.AddInt32(&s.activeConns, -1)
				return
			}
		}
	}
}

// subscribeToTopic subscribes to the MQTT topic for SSH requests.
func (s *SSHService) subscribeToTopic(topic string) error {
	s.Logger.Info().Str("topic", topic).Msg("Subscribing to MQTT topic")
	token := s.MqttClient.Subscribe(topic, byte(s.QOS), s.handleSSHRequest)
	token.Wait()
	if err := token.Error(); err != nil {
		s.Logger.Error().Err(err).Str("topic", topic).Msg("Failed to subscribe to MQTT topic")
		return err
	}
	s.Logger.Debug().Str("topic", topic).Msg("Subscribed to MQTT topic successfully")
	return nil
}

// handleSSHRequest processes incoming SSH requests from MQTT.
func (s *SSHService) handleSSHRequest(client MQTT.Client, msg MQTT.Message) {
	var request models.SSHRequest
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		s.Logger.Error().Err(err).Bytes("payload", msg.Payload()).Msg("Invalid SSH request payload")
		return
	}

	s.Logger.Debug().
		Int("local_port", request.LocalPort).
		Int("remote_port", request.RemotePort).
		Int("backend_port", request.BackendPort).
		Str("backend_host", request.BackendHost).
		Str("server_id", request.ServerID).
		Msg("Received SSH request")

	if err := s.startReverseSSH(request.LocalPort, request.RemotePort, request.BackendPort, request.BackendHost, request.ServerID); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to start reverse SSH")
	}
}

// startReverseSSH establishes or reuses an SSH connection for port forwarding.
func (s *SSHService) startReverseSSH(localPort, remotePort, backendPort int, backendHost, serverId string) error {
	if atomic.LoadInt32(&s.activeConns) >= int32(s.MaxSSHConnections) {
		s.Logger.Warn().Int("max", s.MaxSSHConnections).Msg("Maximum SSH connections reached")
		return fmt.Errorf("maximum SSH connections reached: %d", s.MaxSSHConnections)
	}

	s.clientPoolMutex.RLock()
	client, found := s.clientPool[backendHost]
	s.clientPoolMutex.RUnlock()

	if !found {
		config := &ssh.ClientConfig{
			User:            s.SSHUser,
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(s.cachedPrivateKey)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         s.ConnectionTimeout,
		}

		connStr := fmt.Sprintf("%s:%d", backendHost, backendPort)
		newClient, err := ssh.Dial("tcp", connStr, config)
		if err != nil {
			s.Logger.Error().Err(err).Str("backend", connStr).Msg("Failed to establish SSH connection")
			return fmt.Errorf("failed to establish SSH connection: %w", err)
		}

		s.clientPoolMutex.Lock()
		s.clientPool[backendHost] = models.SSHClientWrapper{Client: newClient, StartTime: time.Now()}
		s.clientPoolMutex.Unlock()

		atomic.AddInt32(&s.activeConns, 1)
		s.Logger.Info().Str("backend", backendHost).Msg("Established new SSH connection")

		go s.monitorSSHConnection(backendHost, newClient)
		client = models.SSHClientWrapper{Client: newClient, StartTime: time.Now()}
	}

	listener, err := client.Client.Listen("tcp", fmt.Sprintf("localhost:%d", remotePort))
	if err != nil {
		s.Logger.Error().Err(err).Int("remote_port", remotePort).Msg("Failed to setup port forwarding")
		atomic.AddInt32(&s.activeConns, -1)
		return fmt.Errorf("failed to setup port forwarding: %w", err)
	}

	s.listenersMutex.Lock()
	s.listeners[localPort] = listener
	s.listenersMutex.Unlock()

	s.Logger.Info().
		Int("local_port", localPort).
		Int("remote_port", remotePort).
		Str("backend", backendHost).
		Msg("Port forwarding started")

	s.publishDeviceReply(s.DeviceInfo.GetDeviceID(), serverId, localPort, remotePort)
	go s.acceptConnections(listener, localPort)
	return nil
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
		s.Logger.Error().Err(err).Str("topic", replyTopic).Msg("Failed to publish DeviceReply message")
	} else {
		s.Logger.Debug().Str("topic", replyTopic).Msg("Published DeviceReply message")
	}
}

// acceptConnections listens for incoming connections and forwards them.
func (s *SSHService) acceptConnections(listener net.Listener, localPort int) {
	s.wg.Add(1)
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			s.Logger.Debug().Int("local_port", localPort).Msg("Stopping acceptConnections due to shutdown")
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				if s.ctx.Err() != nil {
					s.Logger.Debug().Int("local_port", localPort).Msg("Accept stopped due to context cancellation")
					return
				}
				s.Logger.Error().Err(err).Int("local_port", localPort).Msg("Listener accept failed")
				return
			}

			if atomic.LoadInt32(&s.activeListeners) >= int32(s.MaxListeners) {
				s.Logger.Warn().Int("max", s.MaxListeners).Msg("Maximum listeners reached, dropping connection")
				conn.Close()
				continue
			}

			atomic.AddInt32(&s.activeListeners, 1)
			s.Logger.Debug().Int("local_port", localPort).Msg("Accepted new connection")
			s.wg.Add(1)
			go func() {
				defer atomic.AddInt32(&s.activeListeners, -1)
				defer s.wg.Done()
				s.forwardConnection(conn, localPort)
			}()
		}
	}
}

// forwardConnection forwards data between the local and remote connections
func (s *SSHService) forwardConnection(conn net.Conn, localPort int) {
	defer conn.Close()

	localConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", localPort))
	if err != nil {
		s.Logger.Error().Err(err).Int("local_port", localPort).Msg("Failed to connect to local service")
		return
	}
	defer localConn.Close()

	s.Logger.Debug().Int("local_port", localPort).Msg("Forwarding connection established")

	// Use a channel to signal when the goroutine completes
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := io.Copy(localConn, conn); err != nil && s.ctx.Err() == nil {
			s.Logger.Warn().Err(err).Int("local_port", localPort).Msg("Error forwarding remote to local")
		}
	}()

	// Main forwarding loop with context cancellation
	select {
	case <-s.ctx.Done():
		s.Logger.Debug().Int("local_port", localPort).Msg("Stopping forwarding due to shutdown")
		conn.Close() // Close the connection to unblock io.Copy
		localConn.Close()
	case <-done:
		// If the goroutine finishes first, proceed with the second io.Copy
		if _, err := io.Copy(conn, localConn); err != nil && s.ctx.Err() == nil {
			s.Logger.Warn().Err(err).Int("local_port", localPort).Msg("Error forwarding local to remote")
		}
	}

	// Wait for the goroutine to finish if it hasn't already
	select {
	case <-done:
	case <-s.ctx.Done():
		s.Logger.Info().Int("local_port", localPort).Msg("Forcing closure of forwarding goroutine")
	}

	s.Logger.Debug().Int("local_port", localPort).Msg("Finished forwarding connection")
}
