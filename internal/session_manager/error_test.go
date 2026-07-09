package session_manager

import "testing"

func TestResponseErrorTerminalClassification(t *testing.T) {
	tests := []struct {
		status   int
		terminal bool
	}{
		{status: 400, terminal: true},
		{status: 404, terminal: true},
		{status: 409, terminal: true},
		{status: 500, terminal: false},
	}
	for _, tt := range tests {
		err := ResponseError{StatusCode: tt.status}
		if err.Terminal() != tt.terminal {
			t.Fatalf("ResponseError{%d}.Terminal() = %v, want %v", tt.status, err.Terminal(), tt.terminal)
		}
		if err.Error() == "" {
			t.Fatalf("ResponseError{%d}.Error() is empty", tt.status)
		}
	}
}
