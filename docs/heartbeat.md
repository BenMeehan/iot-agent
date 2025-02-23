# Heartbeat Service Documentation

## Overview
The Heartbeat Service is responsible for periodically sending heartbeat messages to an MQTT broker. This service ensures that a device signals its active status at regular intervals, facilitating reliable monitoring and health checks.

The Heartbeat Service operates by running a loop that sends a structured heartbeat message at a configurable interval. Each message contains essential information, including the device ID, timestamp, current status, and a JWT token for authentication. The service utilizes an MQTT client to publish these messages to a predefined topic.

## Flow of Execution

![](./images/heartbeat.png)

### Initialization
To create an instance of the Heartbeat Service, various dependencies must be provided, including the MQTT client, identity manager, and JWT manager. The constructor function, `NewHeartbeatService`, initializes the service with parameters such as the publication topic, interval, and quality of service (QoS) settings. Once initialized, the service is ready to start sending heartbeat messages.

### Starting the Service
Calling the `Start` method launches the heartbeat loop in a separate goroutine. This method first checks whether the service is already running to prevent multiple instances. If the service is not already running, it initializes a new execution context and begins the loop. A log entry is generated to indicate that the service has started successfully.

### Sending Heartbeat Messages
The core functionality resides within the `runHeartbeatLoop` method. A ticker is used to trigger the heartbeat message at the configured interval. On each tick, the service constructs a heartbeat message containing the device ID, current timestamp, and a status flag indicating that the device is alive. The message is then serialized into JSON format before being published to the designated MQTT topic. If the message is successfully published, a debug log confirms the action. If an error occurs during serialization or transmission, an error log is recorded.

### Stopping the Service
The `Stop` method gracefully terminates the heartbeat service. If the service is currently running, it cancels the execution context and waits for the active goroutine to complete. A log message confirms the successful shutdown of the service.