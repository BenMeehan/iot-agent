package constants

const (
	DefaultOutputSizeLimit  = 1024 * 1024 // 1MB
	DefaultMaxExecutionTime = 30          // 30 seconds
)

// Command statuses
const (
	// CommandStatusFailed indicates that the command execution has failed
	CommandStatusFailed = "failed"
	// CommandStatusSuccess indicates that the command execution was successful
	CommandStatusSuccess = "success"
	// CommandStatusPending indicates that the command is pending execution
	CommandStatusPending = "pending"
	// CommandStatusInProgress indicates that the command is currently being executed
	CommandStatusInProgress = "in_progress"
)
