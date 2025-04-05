package services

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	mqtt_middleware "github.com/benmeehan/iot-agent/internal/middlewares/mqtt"
	"github.com/benmeehan/iot-agent/internal/models"
	"github.com/benmeehan/iot-agent/pkg/file"
	"github.com/benmeehan/iot-agent/pkg/identity"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
	"golang.org/x/net/http2"
)

// PortForwardService is a service that handles port forwarding requests from IoT devices to a backend server.
// It establishes HTTP/2 tunnels to the server and forwards data between the local service and the server.
type PortForwardService struct {
	// Configuration fields
	SubTopic      string
	QOS           int
	ServerAddress string
	UseTLS        bool

	// Dependencies
	DeviceInfo     identity.DeviceInfoInterface
	mqttMiddleware mqtt_middleware.MQTTAuthMiddleware
	FileClient     file.FileOperations
	Logger         zerolog.Logger

	// Internal state management
	ctx     context.Context
	cancel  context.CancelFunc
	tunnels sync.WaitGroup
}

// NewPortForwardService initializes a new PortForwardService instance.
func NewPortForwardService(subTopic string, qos int, serverAddress string, useTLS bool, deviceInfo identity.DeviceInfoInterface,
	mqttMiddleware mqtt_middleware.MQTTAuthMiddleware, fileClient file.FileOperations, logger zerolog.Logger) *PortForwardService {
	ctx, cancel := context.WithCancel(context.Background())
	return &PortForwardService{
		SubTopic:       subTopic,
		QOS:            qos,
		ServerAddress:  serverAddress,
		UseTLS:         useTLS,
		DeviceInfo:     deviceInfo,
		mqttMiddleware: mqttMiddleware,
		FileClient:     fileClient,
		Logger:         logger,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start initializes the PortForward service and subscribes to MQTT topics.
func (s *PortForwardService) Start() error {
	topic := fmt.Sprintf("%s/%s", s.SubTopic, s.DeviceInfo.GetDeviceID())
	s.Logger.Info().Str("topic", topic).Msg("Subscribing to MQTT topic")
	return s.mqttMiddleware.Subscribe(topic, byte(s.QOS), s.handlePortForwardRequest)
}

// Stop shuts down the Port Forward service and unsubscribes from MQTT.
func (s *PortForwardService) Stop() error {
	s.cancel()
	s.tunnels.Wait() // Wait for all tunnels to close
	topic := fmt.Sprintf("%s/%s", s.SubTopic, s.DeviceInfo.GetDeviceID())
	err := s.mqttMiddleware.Unsubscribe(topic)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to unsubscribe from MQTT topic")
	}
	return err
}

// handlePortForwardRequest handles an incoming MQTT port forward request.
func (s *PortForwardService) handlePortForwardRequest(client MQTT.Client, msg MQTT.Message) {
	var request models.PortForwardRequest
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		s.Logger.Error().Err(err).Msg("Invalid port forward request")
		return
	}

	// Check if service is shutting down
	select {
	case <-s.ctx.Done():
		s.Logger.Info().Msg("Service shutting down, rejecting new tunnel request")
		return
	default:
		s.tunnels.Add(1)
		go s.handleTunnel(request)
	}
}

func (s *PortForwardService) handleTunnel(request models.PortForwardRequest) {
	defer s.tunnels.Done()

	// Connect to local service
	localConn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", request.LocalPort), 5*time.Second)
	if err != nil {
		s.Logger.Error().Err(err).Int("local_port", request.LocalPort).Msg("Failed to connect to local service")
		return
	}
	defer localConn.Close()

	// Configure HTTP/2 client
	var client *http.Client
	var scheme string

	if s.UseTLS {
		// Secure TLS configuration for production
		tr := &http2.Transport{
			TLSClientConfig: &tls.Config{
				// use system CA pool for verification
				NextProtos: []string{"h2"},
			},
		}
		client = &http.Client{Transport: tr}
		scheme = "https"
	} else {
		// Cleartext HTTP/2 (h2c) for local testing
		tr := &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		}
		client = &http.Client{Transport: tr}
		scheme = "http"
	}

	tunnelURL := fmt.Sprintf("%s://%s/tunnel/%s/%d/%d/%s", scheme, s.ServerAddress, s.DeviceInfo.GetDeviceID(), request.LocalPort, request.RemotePort, request.UserID)

	// Create pipe for data transfer
	pr, pw := io.Pipe()
	defer pr.Close()

	// Handle local to server data transfer
	localToServerDone := make(chan struct{})
	go func() {
		defer pw.Close()
		defer close(localToServerDone)
		_, err := io.Copy(pw, localConn)
		if err != nil && s.ctx.Err() == nil {
			s.Logger.Warn().Err(err).Msg("Error copying local to server")
		}
	}()

	// Create and execute HTTP/2 request
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, tunnelURL, pr)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to create HTTP/2 tunnel request")
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to establish HTTP/2 tunnel")
		return
	}
	defer resp.Body.Close()

	s.Logger.Info().Str("url", tunnelURL).Msg("Tunnel established")

	// Handle server to local data transfer
	serverToLocalDone := make(chan struct{})
	go func() {
		defer close(serverToLocalDone)
		_, err = io.Copy(localConn, resp.Body)
		if err != nil && s.ctx.Err() == nil {
			s.Logger.Warn().Err(err).Msg("Error copying server to local")
		}
	}()

	// Wait for either completion or service shutdown
	select {
	case <-s.ctx.Done():
		s.Logger.Info().Msg("Tunnel closed (service shutdown)")
	case <-localToServerDone:
		s.Logger.Info().Msg("Tunnel closed (local to server connection ended)")
	case <-serverToLocalDone:
		s.Logger.Info().Msg("Tunnel closed (server to local connection ended)")
	}
}
