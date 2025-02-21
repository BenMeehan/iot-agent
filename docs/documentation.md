# IoT Agent Documentation

## Overview
This IoT agent is responsible for handling multiple services such as device registration, heartbeat monitoring, command execution, metrics collection, location tracking, and secure communication through MQTT. The agent is designed to ensure reliable communication with an IoT platform, providing encryption, authentication, and secure messaging.

## Main Components
### 1. `main.go`
This is the entry point of the application, responsible for setting up dependencies, initializing services, and handling graceful shutdowns.

#### Key Responsibilities
- Configures logging levels.
- Starts a profiler for performance monitoring.
- Loads configuration from a YAML file.
- Initializes core components such as MQTT, encryption, JWT, and file operations.
- Registers and starts various services through the `ServiceRegistry`.
- Implements a graceful shutdown mechanism to stop services cleanly upon receiving termination signals.

#### Important Functions
- **`startProfiler()`**: Starts an HTTP server on port 6060 for `pprof` profiling.
- **`getLogLevel()`**: Reads the `LOG_LEVEL` environment variable and returns the corresponding logging level.
- **`main()`**:
  - Initializes logging.
  - Loads configuration.
  - Creates MQTT client.
  - Loads device information.
  - Initializes encryption and JWT management.
  - Registers and starts services.
  - Listens for OS signals to handle graceful shutdowns.

### 2. `registry/service_registry.go`
This file defines the `ServiceRegistry`, which manages the lifecycle of various services.

#### Key Responsibilities
- Registers available services.
- Starts services in a structured manner, ensuring proper initialization.
- Stops services gracefully in case of failures or shutdown.
- Ensures dependency injection of key components like MQTT, encryption, JWT, and file handling.

#### Key Structs and Methods
- **`ServiceRegistry`**
  - **Attributes:**
    - `services`: A map storing registered services.
    - `serviceKeys`: A slice maintaining the order of registered services.
    - `mqttClient`: MQTT communication client.
    - `fileClient`: Handles file operations.
    - `encryptionManager`: Manages encryption operations.
    - `jwtManager`: Handles JWT authentication.
    - `Logger`: Logging instance.
  
  - **Methods:**
    - `NewServiceRegistry()`: Initializes the service registry with dependencies.
    - `RegisterService(name string, svc Service)`: Registers a new service if not already present.
    - `StartServices() error`: Starts all registered services in order and handles failures.
    - `StopServices() error`: Stops all services in reverse order to ensure a clean shutdown.
    - `RegisterServices(config *utils.Config, deviceInfo identity.DeviceInfoInterface) error`: Creates and registers services dynamically based on configuration.
    - `isEnabled(name string, config *utils.Config) bool`: Checks if a service is enabled in the configuration.

### 3. Services
Each service is responsible for a specific functionality within the IoT agent. The `ServiceRegistry` initializes and manages these services dynamically.

#### Registered Services
- **`RegistrationService`**: Handles device registration with the IoT platform.
- **`HeartbeatService`**: Sends periodic heartbeat signals to indicate the device is online.
- **`MetricsService`**: Collects and transmits device metrics such as CPU usage, memory consumption, and network statistics.
- **`CommandService`**: Executes remote commands received via MQTT and sends back responses.
- **`SSHService`**: Manages secure SSH tunnels for remote access to devices.
- **`LocationService`**: Provides device location tracking through GPS or Google Geolocation API.
- **`UpdateService`**: Handles firmware or software updates on the device.

#### Service Lifecycle Management
1. **Registration**:
   - Services are dynamically instantiated based on the configuration.
   - Required dependencies (MQTT, encryption, JWT, file operations) are injected.
2. **Startup**:
   - Services are started in the order they were registered.
   - If a service fails, previously started services are stopped.
3. **Shutdown**:
   - Services are stopped in reverse order to ensure a clean shutdown.
   - Any errors encountered during shutdown are logged.

## Error Handling and Logging
- The application uses `zerolog` for structured logging.
- Logs are categorized into `Info`, `Warn`, and `Error` levels.
- Errors encountered during startup/shutdown are logged and handled appropriately.
- If a service fails during startup, the system attempts to roll back and stop already running services.

## Configuration Management
- Configuration is loaded from `configs/config.yaml` using the `utils.LoadConfig()` function.
- The config file contains parameters for MQTT, security, device identity, and services.
- Essential validation checks ensure that required fields are present before initializing services.

## Security Features
- **Encryption**:
  - Uses an AES encryption manager to secure stored data.
  - The encryption key is loaded from the configured key file.
- **JWT Authentication**:
  - JWT tokens are used for authentication with the IoT platform.
  - The JWT manager ensures token integrity and secure access.
- **MQTT Security**:
  - The MQTT client supports TLS certificates for secure communication.
  - Connection credentials are securely stored and managed.