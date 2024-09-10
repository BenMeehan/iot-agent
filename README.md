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

## Rules
1. Please use camel case for variables and constants
2. Please use snake case for files and folders

### URLS

https://www.emqx.com/en/mqtt/public-mqtt5-broker

okay now, ill send you my code. please vet it. I mean add proper casing and meaningful naming of variables, comments and logs. Dont over do the comments please, keep it readable