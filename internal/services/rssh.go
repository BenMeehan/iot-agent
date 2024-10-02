package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"golang.org/x/crypto/ssh"
)

// Device configuration
var (
	mqttBroker    = "tcp://broker.emqx.io:1883"
	mqttUsername  = ""
	mqttPassword  = ""
	mqttTopic     = "/mqtt"
	deviceID      = "019214a8-7a41-7d09-bf2e-a27b82161829"
	sshUsername   = "ghost"
	sshPassword   = "ghost"
	sshServerAddr = "10.0.2.15:2000" // Address of your SSH server
	sshPrivateKey = ""
	sshPort       = "8080" // Default port to forward (can be overridden via MQTT)
)

func main() {
	// Start the MQTT client
	opts := mqtt.NewClientOptions().AddBroker(mqttBroker).SetClientID(deviceID)
	opts.SetUsername(mqttUsername)
	opts.SetPassword(mqttPassword)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Error connecting to MQTT broker: %v", token.Error())
	}
	defer client.Disconnect(250)

	// Subscribe to the MQTT topic to receive commands
	if token := client.Subscribe(mqttTopic, 0, handleMQTTMessage); token.Wait() && token.Error() != nil {
		log.Fatalf("Error subscribing to topic: %v", token.Error())
	}
	log.Println("Subscribed to MQTT topic, waiting for commands...")

	select {} // Keep the program running indefinitely
}

// handleMQTTMessage is the MQTT callback that handles incoming messages.
func handleMQTTMessage(client mqtt.Client, msg mqtt.Message) {
	payload := string(msg.Payload())
	log.Printf("Received MQTT message: %s", payload)

	// Parse the port from the message, assuming it follows a simple format: {"port": 8080}
	port := parsePortFromPayload(payload)
	if port != "" {
		sshPort = port
		log.Printf("Using port: %s for reverse SSH", port)
	}

	// Set up reverse SSH connection
	setupReverseSSH(sshPort)
}

// parsePortFromPayload extracts the port from the MQTT payload (assuming JSON structure)
func parsePortFromPayload(payload string) string {
	// Simple parsing of the JSON message, modify this to use a proper JSON parser if necessary
	if strings.Contains(payload, "port") {
		start := strings.Index(payload, ":") + 1
		end := strings.Index(payload, "}")
		return strings.TrimSpace(payload[start:end])
	}
	return ""
}

// setupReverseSSH sets up a reverse SSH tunnel using the provided port.
func setupReverseSSH(port string) {
	val, err := os.ReadFile("/home/ghost/Desktop/iot-project/iot-cloud/RSSH Service/cmd/priv.txt")
	if err != nil {
		log.Fatalf("Val: %v", err)
	}
	sshPrivateKey = string(val)
	config := &ssh.ClientConfig{
		User: sshUsername,
		Auth: []ssh.AuthMethod{
			ssh.Password(sshPassword),
			publicKeyFile("/home/ghost/Desktop/iot-project/iot-cloud/RSSH Service/cmd/priv.txt"), // Use private key for SSH authentication if needed
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // For simplicity, accept any host key
	}

	// Connect to the SSH server
	conn, err := ssh.Dial("tcp", sshServerAddr, config)
	if err != nil {
		log.Fatalf("Failed to establish SSH connection: %v", err)
	}
	defer conn.Close()

	log.Printf("Successfully connected to SSH server at %s", sshServerAddr)

	// Set up port forwarding (reverse tunnel)
	localPort, _ := strconv.ParseInt(port, 10, 32)  // The local port on the device to forward
	remotePort, _ := strconv.ParseInt(port, 10, 32) // The port on the SSH server that will forward traffic to the local port

	listener, err := conn.Listen("tcp", fmt.Sprintf("localhost:%d", remotePort))
	if err != nil {
		log.Fatalf("Failed to set up port forwarding: %v", err)
	}
	defer listener.Close()

	log.Printf("Port forwarding set up: localhost:%s -> %s on server", localPort, remotePort)

	// Wait for incoming connections on the forwarded port
	for {
		client, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept incoming connection: %v", err)
			continue
		}
		go handleClient(client, strconv.Itoa(int(localPort)))
	}
}

// handleClient forwards data between the SSH connection and the local port on the device
func handleClient(client net.Conn, localPort string) {
	defer client.Close()

	// Connect to the local service (e.g., localhost:8080)
	localConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%s", localPort))
	if err != nil {
		log.Printf("Failed to connect to local service on port %s: %v", localPort, err)
		return
	}
	defer localConn.Close()

	// Forward data between the client and local service
	go func() { _, _ = io.Copy(localConn, client) }()
	_, _ = io.Copy(client, localConn)
}

// publicKeyFile loads the private key for SSH authentication
func publicKeyFile(file string) ssh.AuthMethod {
	key, err := os.ReadFile(file)
	if err != nil {
		log.Fatalf("Failed to read SSH private key file: %v", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		log.Fatalf("Failed to parse SSH private key: %v", err)
	}

	return ssh.PublicKeys(signer)
}

// utility to execute shell command (optional for local services)
func executeCommand(command string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
