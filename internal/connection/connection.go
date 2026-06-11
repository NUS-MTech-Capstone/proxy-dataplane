package connection

import "context"

// Result is the transport-independent result of one command execution.
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode,omitempty"`
}

type Command struct {
	SessionID string
	UserID    string
	HostID    string
	CRID      string
	Token     string
	Value     string
}

// Executor connects the dataplane to a target and executes commands there.
// The host ID comes from the Session Manager record, not from the WebSocket.
type Executor interface {
	Execute(ctx context.Context, command Command) (Result, error)
}
