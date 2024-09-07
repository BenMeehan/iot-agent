# IOT - Agent
An IOT agent designed to be modular and configurable, supporting a variety of services.

## Running the project
To run, do
```
go run cmd/agent/main.go
```

## To add a new service
1. Add necessary configurations in config/config.yaml
2. Add a new file in internal/services, similar to heartbeatService.go and write your logic there.

### URLS

https://www.emqx.com/en/mqtt/public-mqtt5-broker