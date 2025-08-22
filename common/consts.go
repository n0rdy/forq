package common

const (
	// message statuses:
	ReadyStatus      = 0
	ProcessingStatus = 1
	FailedStatus     = 2

	// OS:
	WindowsOS = "windows"
	LinuxOS   = "linux"
	MacOS     = "darwin"

	// reasons to move message to DLQ:
	MaxAttemptsReachedFailureReason = "max_attempts_reached"
	MessageExpiredFailureReason     = "message_expired"
)
