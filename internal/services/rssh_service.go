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
	MaxListeners      int
	MaxSSHConnections int
	ConnectionTimeout time.Duration
	AutoDisconnect    time.Duration
	activeConns       int32
	activeListeners   int32
	cachedPrivateKey  ssh.Signer
	ctx               context.Context
	cancel            context.CancelFunc
	clientPool        map[string]*ssh.Client
	clientPoolMutex   sync.RWMutex
	listeners         map[string]map[int]models.ListenerWrapper // backendHost -> localPort -> ListenerWrapper
	listenersMutex    sync.RWMutex
	wg                sync.WaitGroup
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
		clientPool:        make(map[string]*ssh.Client),
		listeners:         make(map[string]map[int]models.ListenerWrapper),
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

	go s.cleanupExpiredListeners()
	s.Logger.Info().Msg("SSH service started successfully")
	return nil
}

// Stop shuts down the SSH service and closes all active connections.
// TODO: Publish a message to the server to notify disconnects.
func (s *SSHService) Stop() error {
	s.Logger.Info().Msg("Stopping SSH service...")
	s.cancel()

	s.listenersMutex.Lock()
	for backendHost, listeners := range s.listeners {
		for port, wrapper := range listeners {
			s.Logger.Debug().Int("port", port).Str("backend", backendHost).Msg("Closing listener")
			if err := wrapper.Listener.Close(); err != nil {
				s.Logger.Warn().Err(err).Int("port", port).Str("backend", backendHost).Msg("Error closing listener")
			}
		}
	}
	s.listeners = make(map[string]map[int]models.ListenerWrapper)
	s.listenersMutex.Unlock()

	s.Logger.Debug().Msg("Waiting for all goroutines to finish")
	s.wg.Wait()

	s.clientPoolMutex.Lock()
	for backendHost, client := range s.clientPool {
		s.Logger.Debug().Str("backend", backendHost).Msg("Closing SSH connection")
		if err := client.Close(); err != nil {
			s.Logger.Warn().Err(err).Str("backend", backendHost).Msg("Error closing SSH connection")
		}
	}
	s.clientPool = make(map[string]*ssh.Client)
	s.clientPoolMutex.Unlock()

	topic := fmt.Sprintf("%s/%s", s.SubTopic, s.DeviceInfo.GetDeviceID())
	s.MqttClient.Unsubscribe(topic).Wait()

	s.Logger.Info().Msg("SSH service stopped successfully")
	return nil
}

// cleanupExpiredListeners periodically removes stale listeners and their SSH clients if no listeners remain.
func (s *SSHService) cleanupExpiredListeners() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			s.Logger.Debug().Msg("Stopping cleanup of expired listeners")
			return
		case <-ticker.C:
			now := time.Now()
			s.listenersMutex.Lock()
			s.clientPoolMutex.Lock()

			for backendHost, listeners := range s.listeners {
				for port, wrapper := range listeners {
					if now.Sub(wrapper.StartTime) > s.AutoDisconnect {
						s.Logger.Info().Int("port", port).Str("backend", backendHost).Msg("Closing expired listener")
						if err := wrapper.Listener.Close(); err != nil {
							s.Logger.Warn().Err(err).Int("port", port).Str("backend", backendHost).Msg("Error closing expired listener")
						}
						delete(listeners, port)
						atomic.AddInt32(&s.activeListeners, -1)
					}
				}
				// If no listeners remain for this backendHost, close the SSH client
				if len(listeners) == 0 {
					if client, exists := s.clientPool[backendHost]; exists {
						s.Logger.Info().Str("backend", backendHost).Msg("No listeners remain, closing SSH connection")
						if err := client.Close(); err != nil {
							s.Logger.Warn().Err(err).Str("backend", backendHost).Msg("Error closing SSH connection")
						}
						delete(s.clientPool, backendHost)
						delete(s.listeners, backendHost)
						atomic.AddInt32(&s.activeConns, -1)
					}
				}
			}

			s.clientPoolMutex.Unlock()
			s.listenersMutex.Unlock()
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
		s.clientPool[backendHost] = newClient
		s.clientPoolMutex.Unlock()

		atomic.AddInt32(&s.activeConns, 1)
		s.Logger.Info().Str("backend", backendHost).Msg("Established new SSH connection")
		client = newClient
	}

	listener, err := client.Listen("tcp", fmt.Sprintf("localhost:%d", remotePort))
	if err != nil {
		s.Logger.Error().Err(err).Int("remote_port", remotePort).Msg("Failed to setup port forwarding")
		return fmt.Errorf("failed to setup port forwarding: %w", err)
	}

	s.listenersMutex.Lock()
	if _, exists := s.listeners[backendHost]; !exists {
		s.listeners[backendHost] = make(map[int]models.ListenerWrapper)
	}
	s.listeners[backendHost][localPort] = models.ListenerWrapper{
		Listener:  listener,
		StartTime: time.Now(),
	}
	atomic.AddInt32(&s.activeListeners, 1)
	s.listenersMutex.Unlock()

	s.Logger.Info().
		Int("local_port", localPort).
		Int("remote_port", remotePort).
		Str("backend", backendHost).
		Msg("Port forwarding started")

	go s.acceptConnections(listener, localPort, backendHost)
	s.publishDeviceReply(s.DeviceInfo.GetDeviceID(), serverId, localPort, remotePort)
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
func (s *SSHService) acceptConnections(listener net.Listener, localPort int, backendHost string) {
	s.wg.Add(1)
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			s.Logger.Debug().Int("local_port", localPort).Str("backend", backendHost).Msg("Stopping acceptConnections due to shutdown")
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				if s.ctx.Err() != nil {
					s.Logger.Debug().Int("local_port", localPort).Str("backend", backendHost).Msg("Accept stopped due to context cancellation")
					return
				}
				s.Logger.Error().Err(err).Int("local_port", localPort).Str("backend", backendHost).Msg("Listener accept failed")
				return
			}

			if atomic.LoadInt32(&s.activeListeners) >= int32(s.MaxListeners) {
				s.Logger.Warn().Int("max", s.MaxListeners).Msg("Maximum listeners reached, dropping connection")
				conn.Close()
				continue
			}

			s.Logger.Debug().Int("local_port", localPort).Str("backend", backendHost).Msg("Accepted new connection")
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.forwardConnection(conn, localPort, backendHost)
			}()
		}
	}
}

// forwardConnection forwards data between the local and remote connections
func (s *SSHService) forwardConnection(conn net.Conn, localPort int, backendHost string) {
	defer conn.Close()

	localConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", localPort))
	if err != nil {
		s.Logger.Error().Err(err).Int("local_port", localPort).Str("backend", backendHost).Msg("Failed to connect to local service")
		return
	}
	defer localConn.Close()

	// Set read deadline so io.Copy doesn't block forever
	timeout := 2 * time.Minute
	conn.SetReadDeadline(time.Now().Add(timeout))
	localConn.SetReadDeadline(time.Now().Add(timeout))

	// Prevent lingering connections
	conn.(*net.TCPConn).SetLinger(0)
	localConn.(*net.TCPConn).SetLinger(0)

	s.Logger.Debug().Int("local_port", localPort).Str("backend", backendHost).Msg("Forwarding connection established")

	var wg sync.WaitGroup
	wg.Add(2)

	// Remote to local forwarding
	go func() {
		defer wg.Done()
		_, err := io.Copy(localConn, conn)
		if err != nil && s.ctx.Err() == nil {
			s.Logger.Warn().Err(err).Int("local_port", localPort).Str("backend", backendHost).Msg("Error forwarding remote to local")
		}
	}()

	// Local to remote forwarding
	go func() {
		defer wg.Done()
		_, err := io.Copy(conn, localConn)
		if err != nil && s.ctx.Err() == nil {
			s.Logger.Warn().Err(err).Int("local_port", localPort).Str("backend", backendHost).Msg("Error forwarding local to remote")
		}
	}()

	// Wait for either context cancellation or both directions to complete
	select {
	case <-s.ctx.Done():
		s.Logger.Debug().Int("local_port", localPort).Str("backend", backendHost).Msg("Stopping forwarding due to shutdown")
		conn.Close()
		localConn.Close()
		wg.Wait()
	case <-func() chan struct{} {
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		return done
	}():
	}

	s.Logger.Debug().Int("local_port", localPort).Str("backend", backendHost).Msg("Finished forwarding connection")
}
