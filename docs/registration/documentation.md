# Registration Service Documentation

## Overview
The `RegistrationService` is responsible for managing the registration process of an IoT device with the backend system. It ensures that devices are properly authenticated, assigned a unique identifier, and receive a JWT for secure communication.

This service:
- Checks if the device is already registered.
- Verifies the validity of the device’s JWT token.
- Handles secure registration and re-registration through MQTT.
- Supports exponential backoff and retry mechanisms for failed registrations.
- Allows users to update metadata or trigger re-registration by modifying specific configuration files.

## Dependencies
The `RegistrationService` interacts with various components, including:
- **MQTT Broker**: Handles messaging between the device and backend.
- **JWT Manager**: Manages authentication tokens for the device.
- **Encryption Manager**: Encrypts sensitive payloads before transmission.
- **File Client**: Reads and writes configuration and authentication files.
- **Device Identity**: Provides unique device information (e.g., name, organization ID, metadata).

## Configuration Parameters
| Parameter         | Description |
|------------------|-------------|
| `PubTopic`       | MQTT topic for publishing registration requests. |
| `ClientID`       | Unique identifier of the IoT device. |
| `QOS`            | MQTT Quality of Service level. |
| `DeviceInfo`     | Interface to fetch device identity details. |
| `MqttClient`     | Interface to interact with the MQTT broker. |
| `FileClient`     | Handles reading and writing to the filesystem. |
| `JWTManager`     | Manages JWT tokens for authentication. |
| `EncryptionManager` | Encrypts and decrypts registration payloads. |
| `MaxBackoffSeconds` | Maximum delay between retry attempts. |
| `Logger`         | Logs events and errors. |

## Registration Flow
1. **Check for Existing Registration**
   - Reads `device_id` from `/config/devices.json`.
   - If `device_id` exists, checks JWT validity.
   - If the JWT is valid, registration is skipped.
2. **Re-Registration Handling**
   - If JWT is invalid, attempts to re-register using the existing `device_id`.
3. **New Registration**
   - If no `device_id` is found, prepares a registration request with device metadata.
   - Sends the request via MQTT.
   - Waits for the backend response.
   - Saves the received `device_id` and JWT token.
4. **Retry Mechanism**
   - Uses exponential backoff with jitter.
   - Retries until successful or `MaxBackoffSeconds` is reached.

![flow](./flow.png)

## Methods

### `Start()`
- Initiates the registration process.
- Checks if the device is already registered.
- If the JWT is invalid, attempts re-registration.
- Calls `retryRegistration()` to handle the registration process.

### `retryRegistration(payload models.RegistrationPayload)`
- Implements exponential backoff for failed registration attempts.
- Retries registration until successful or timeout is reached.

### `Register(payload models.RegistrationPayload)`
- Encrypts and publishes registration payload to MQTT.
- Subscribes to the response topic and waits for a response.
- If registration is successful, saves the `device_id` and JWT token.
- If no response is received within 10 seconds, registration fails.

## Updating Metadata or Re-Registering the Device
- **To update metadata**: Delete `jwt.bin` from `/secrets/`. The device will re-register and obtain a new JWT with updated metadata.
- **To force re-registration**: Remove the `device_id` value from `/config/devices.json`. The device will register as a new entity and receive a new `device_id`.

## Error Handling
- Logs errors when JWT validation fails, MQTT messages cannot be sent/received, or registration times out.
- Uses structured logging with context for easier debugging.

## Security Measures
- Encrypts registration payloads before sending over MQTT.
- Uses JWT tokens for authentication and secure communication.
- Limits retry attempts to prevent excessive load on the backend.
