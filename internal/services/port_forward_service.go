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
	s.Logger.Info().Str("topic", msg.Topic()).Msg("Received port forward request")

	var request models.PortForwardRequest
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		s.Logger.Error().Err(err).Msg("Invalid port forward request")
		return
	}

	s.Logger.Info().Str("user_id", request.UserID).Int("local_port", request.LocalPort).Int("remote_port", request.RemotePort).Msg("Processing port forward request")

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

// handleTunnel establishes a tunnel to the server and forwards data between the local service and the server.
func (s *PortForwardService) handleTunnel(request models.PortForwardRequest) {
	defer s.tunnels.Done()

	localConn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", request.LocalPort), 5*time.Second)
	if err != nil {
		s.Logger.Error().Err(err).Int("local_port", request.LocalPort).Msg("Failed to connect to local service")
		return
	}
	defer localConn.Close()

	var client *http.Client
	var scheme string
	if s.UseTLS {
		tr := &http2.Transport{
			TLSClientConfig: &tls.Config{NextProtos: []string{"h2"}},
		}
		client = &http.Client{Transport: tr}
		scheme = "https"
	} else {
		tr := &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		}
		client = &http.Client{Transport: tr}
		scheme = "http"
	}

	tunnelURL := fmt.Sprintf("%s://%s/tunnel/%s/%d/%d/%s",
		scheme, s.ServerAddress, s.DeviceInfo.GetDeviceID(),
		request.LocalPort, request.RemotePort, request.UserID,
	)

	pr, pw := io.Pipe()
	defer pr.Close()

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

	// ✅ Local cancel context for early shutdown
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer pw.Close()
		n, err := io.Copy(pw, localConn)
		s.Logger.Info().Int64("bytes", n).Err(err).Msg("Local to server copy finished")
		cancel() // 🔥 Shutdown when this direction ends
	}()

	go func() {
		defer wg.Done()
		n, err := io.Copy(localConn, resp.Body)
		s.Logger.Info().Int64("bytes", n).Err(err).Msg("Server to local copy finished")
		cancel() // 🔥 Shutdown when this direction ends
	}()

	<-ctx.Done() // 💡 Wait for first error/close
	s.Logger.Info().Msg("Tunnel closed (either direction ended)")
	wg.Wait()
}
