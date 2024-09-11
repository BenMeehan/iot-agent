# IoT Agent

An IoT agent designed to be modular and configurable, supporting a variety of services.

## Running the Project

To run the project, use:
```sh
go run cmd/agent/main.go
```

## Architecture
![arch.png](./.github/images/agent-arch.png)

## Adding a New Service

1. **Update Configuration**:
   - Add necessary configurations in `config/config.yaml`.

2. **Create Service Logic**:
   - Add a new file in `internal/services` (e.g., `new_service.go`).
   - Implement the service logic similar to existing services (e.g., `heartbeatService.go`).

## Services Overview

### Registration Service

- **Purpose**: Handles the registration of IoT devices with the server.
- **Configuration Parameters**:
  - `PubTopic`: The MQTT topic to publish registration messages.
  - `DeviceSecretFile`: Path to the file containing device secrets.
  - `ClientID`: The unique client identifier used for MQTT communication.
  - `QOS`: Quality of Service level for MQTT messages.
- **Behavior**: Publishes registration data to the MQTT broker to register the device. If the device.json file contains a deviceID then, the device is already registered and we use that. Else, we do a secure registration through a PSK hash for authentication.

### Heartbeat Service

- **Purpose**: Periodically sends heartbeat messages to indicate the device is active.
- **Configuration Parameters**:
  - `PubTopic`: The MQTT topic to publish heartbeat messages.
  - `Interval`: Interval in seconds between heartbeat messages.
  - `DeviceID`: The unique identifier for the device.
  - `QOS`: Quality of Service level for MQTT messages.
- **Behavior**: Sends heartbeat messages at regular intervals to the MQTT broker to indicate that the device is still operational.

## Rules

1. **Naming Conventions**:
   - Use camel case for variables and constants (e.g., `deviceId`, `maxRetries`).
   - Use snake case for file names and folder names (e.g., `heartbeat_service.go`, `internal/services`).

2. **Code Style**:
   - Ensure code readability with clear, concise naming and consistent formatting.
   - Include meaningful comments where necessary but avoid over-commenting. Comments should explain why something is done, not just what is done.

3. **Logging**:
   - Use structured logging with a clear message and relevant context.
   - Ensure logs are informative and useful for debugging purposes.

## URLs

- [EMQX Public MQTT5 Broker](https://www.emqx.com/en/mqtt/public-mqtt5-broker)
