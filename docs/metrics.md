# Metrics Service Documentation

## Overview

The Metrics Service is responsible for collecting and reporting system and process metrics. It gathers CPU usage, memory consumption, and I/O operations from both system-wide and individual process levels. The collected data is then published via MQTT for further processing and analysis.

## Flow of Execution

![](./images/metrics.png)

### Service Initialization

When the Metrics Service starts, it first checks if an instance is already running. If another instance is detected, the service logs a warning and exits. If no active instance is found, it proceeds with loading and validating the metrics configuration. If the configuration is invalid, an error is logged, and the service terminates. If the configuration is valid, the service registers the default metrics collectors required for monitoring.

### Process Monitoring

After initializing default collectors, the service checks if process monitoring is enabled in the configuration. If enabled, it registers the Process Metrics Collector. If disabled, it skips the process monitoring step and proceeds with the initialization of the collection loop.

### Metrics Collection Loop

The collection loop starts by waiting for the defined interval before gathering system and process metrics. If no metrics are collected, a failure is logged. If the data is successfully retrieved, additional metadata, such as the device ID and JWT token, is added to the collected metrics. The service then attempts to publish the metrics data via MQTT.

### Publishing Metrics

If the MQTT publishing operation is successful, the service logs the successful transmission of the metrics data. If the publishing operation fails, an error is logged, and the service retries the operation. Regardless of success or failure, the collection loop continues to the next cycle.

### Service Termination

The collection loop continues executing until a stop signal is received. Once a stop signal is detected, the service performs cleanup operations and exits gracefully. The cleanup ensures that any allocated resources are properly released before the service shuts down.