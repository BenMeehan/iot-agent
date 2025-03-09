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
	// Dependencies
	DeviceInfo identity.DeviceInfoInterface
	MqttClient mqtt.MQTTClient
	FileClient file.FileOperations
	Logger     zerolog.Logger

	// Configuration
	SubTopic       string
	SSHUser        string
	PrivateKeyPath string
	QOS            int

	// Internal state
	ctx              context.Context
	cancel           context.CancelFunc
	cachedPrivateKey ssh.Signer
	activeConns      int32
	activeListeners  int32
	clientPool       map[string]models.SSHClientWrapper
	clientPoolMutex  sync.RWMutex
	listeners        map[int]net.Listener
	listenersMutex   sync.RWMutex
	wg               sync.WaitGroup

	// Configuration
	MaxListeners      int
	MaxSSHConnections int
	ConnectionTimeout time.Duration
	ForwardTimeout    time.Duration
	AutoDisconnect    time.Duration
}

// ---- Initialization ----

// NewSSHService initializes a new SSHService instance.
func NewSSHService(subTopic string, deviceInfo identity.DeviceInfoInterface, mqttClient mqtt.MQTTClient,
	logger zerolog.Logger, sshUser string, privateKeyPath string,
	fileClient file.FileOperations, qos int, maxListeners, maxSSHConnections int,
	connectionTimeout, forwardTimeout, autoDisconnect time.Duration) *SSHService {

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
	if forwardTimeout == 0 {
		forwardTimeout = constants.ForwardTimeout
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
		ForwardTimeout:    forwardTimeout,
		AutoDisconnect:    autoDisconnect,
	}
}

// ---- Service Lifecycle ----

// Start initializes the SSH service and subscribes to MQTT topics.
func (s *SSHService) Start() error {
	s.Logger.Info().Msg("Starting SSH service...")

	// Load SSH private key
	key, err := s.FileClient.ReadFile(s.PrivateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH private key: %w", err)
	}

	privateKey, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return fmt.Errorf("failed to parse SSH private key: %w", err)
	}
	s.cachedPrivateKey = privateKey

	topic := fmt.Sprintf("%s/%s", s.SubTopic, s.DeviceInfo.GetDeviceID())

	if err := s.subscribeToTopic(topic); err != nil {
		return err
	}

	go s.cleanupExpiredConnections()
	return nil
}
func (s *SSHService) Stop() error {
	s.Logger.Info().Msg("Stopping SSH service...")
	s.cancel()

	// Close all active listeners to unblock `listener.Accept()`
	s.listenersMutex.Lock()
	for port, listener := range s.listeners {
		s.Logger.Info().Int("port", port).Msg("Closing listener")
		_ = listener.Close()
	}
	s.listeners = make(map[int]net.Listener)
	s.listenersMutex.Unlock()

	s.Logger.Info().Msg("Waiting for all goroutines to finish...") // Debug log
	s.wg.Wait()
	s.Logger.Info().Msg("All goroutines finished, shutting down.")

	// Close all active SSH connections
	s.clientPoolMutex.Lock()
	for _, wrapper := range s.clientPool {
		wrapper.Client.Close()
	}
	s.clientPool = make(map[string]models.SSHClientWrapper)
	s.clientPoolMutex.Unlock()

	// Unsubscribe from MQTT topic
	topic := fmt.Sprintf("%s/%s", s.SubTopic, s.DeviceInfo.GetDeviceID())
	s.MqttClient.Unsubscribe(topic).Wait()

	s.Logger.Info().Msg("SSH service stopped.")
	return nil
}

// ---- Connection Management ----

// cleanupExpiredConnections periodically removes stale SSH connections.
func (s *SSHService) cleanupExpiredConnections() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			s.clientPoolMutex.Lock()
			for backendHost, wrapper := range s.clientPool {
				if now.Sub(wrapper.StartTime) > s.AutoDisconnect {
					s.Logger.Info().Str("backend", backendHost).Msg("Auto-disconnecting stale SSH connection")
					wrapper.Client.Close()
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
			return
		case <-ticker.C:
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				s.Logger.Warn().Str("backend", backendHost).Msg("SSH connection lost")
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

// ---- MQTT Handling ----

// subscribeToTopic subscribes to the MQTT topic for SSH requests.
func (s *SSHService) subscribeToTopic(topic string) error {
	s.Logger.Info().Str("topic", topic).Msg("Subscribing to MQTT topic")

	token := s.MqttClient.Subscribe(topic, byte(s.QOS), s.handleSSHRequest)
	token.Wait()
	if err := token.Error(); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to subscribe to SSH request topic")
		return err
	}
	return nil
}

// handleSSHRequest processes incoming SSH requests from MQTT.
func (s *SSHService) handleSSHRequest(client MQTT.Client, msg MQTT.Message) {
	var request models.SSHRequest
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		s.Logger.Error().Err(err).Msg("Invalid SSH request payload")
		return
	}

	if err := s.startReverseSSH(request.LocalPort, request.RemotePort, request.BackendPort, request.BackendHost, request.ServerID); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to start reverse SSH")
	}
}

// ---- SSH Connection Establishment ----

// startReverseSSH establishes or reuses an SSH connection for port forwarding.
func (s *SSHService) startReverseSSH(localPort, remotePort, backendPort int, backendHost, serverId string) error {
	if atomic.LoadInt32(&s.activeConns) >= int32(s.MaxSSHConnections) {
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
			return fmt.Errorf("failed to establish SSH connection: %w", err)
		}

		s.clientPoolMutex.Lock()
		s.clientPool[backendHost] = models.SSHClientWrapper{Client: newClient, StartTime: time.Now()}
		s.clientPoolMutex.Unlock()

		atomic.AddInt32(&s.activeConns, 1)

		go s.monitorSSHConnection(backendHost, newClient)
		client = models.SSHClientWrapper{Client: newClient, StartTime: time.Now()}
	}

	listener, err := client.Client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", remotePort))
	if err != nil {
		atomic.AddInt32(&s.activeConns, -1)
		return fmt.Errorf("failed to setup port forwarding: %w", err)
	}

	s.listenersMutex.Lock()
	s.listeners[localPort] = listener
	s.listenersMutex.Unlock()

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
		s.Logger.Error().Err(err).Msg("Failed to publish DeviceReply message")
	} else {
		s.Logger.Info().Msg("Published DeviceReply message")
	}
}

// ---- Connection Forwarding ----

// acceptConnections listens for incoming connections and forwards them.
func (s *SSHService) acceptConnections(listener net.Listener, localPort int) {
	s.wg.Add(1)
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done(): // Exit if context is canceled
			s.Logger.Info().Msg("Stopping acceptConnections due to shutdown.")
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				if s.ctx.Err() != nil {
					return // Context is canceled, exit cleanly
				}
				s.Logger.Error().Err(err).Msg("Listener accept failed")
				return
			}

			if atomic.LoadInt32(&s.activeListeners) >= int32(s.MaxListeners) {
				conn.Close()
				continue
			}

			atomic.AddInt32(&s.activeListeners, 1)
			s.wg.Add(1)
			go func() {
				defer atomic.AddInt32(&s.activeListeners, -1)
				defer s.wg.Done()
				s.forwardConnection(conn, localPort)
			}()
		}
	}
}

// Forwards data between the local and remote connections
func (s *SSHService) forwardConnection(conn net.Conn, localPort int) {
	defer conn.Close()
	localConn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", localPort), s.ConnectionTimeout)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to connect to local service")
		return
	}
	defer localConn.Close()

	// Enforce timeout for active connections
	_ = conn.SetDeadline(time.Now().Add(s.ForwardTimeout))
	_ = localConn.SetDeadline(time.Now().Add(s.ForwardTimeout))

	if _, err := io.Copy(localConn, conn); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to forward data")
	}
	if _, err := io.Copy(conn, localConn); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to forward data")
	}
}
