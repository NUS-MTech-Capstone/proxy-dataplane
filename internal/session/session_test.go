package session

import (
	"context"
	"testing"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
)

func TestRuntimeExecutesCompletedLine(t *testing.T) {
	var command string
	runtime := New(func(_ context.Context, got string) (connection.Result, error) {
		command = got
		return connection.Result{Stdout: "user\n"}, nil
	})

	output, err := runtime.HandleInput(t.Context(), []byte("whoami\r"))
	if err != nil {
		t.Fatal(err)
	}
	if command != "whoami" {
		t.Fatalf("command = %q, want whoami", command)
	}
	if got, want := string(output), "whoami\r\nuser\n$ "; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRuntimeHandlesBackspace(t *testing.T) {
	runtime := New(func(_ context.Context, command string) (connection.Result, error) {
		if command != "pwd" {
			t.Fatalf("command = %q, want pwd", command)
		}
		return connection.Result{}, nil
	})

	if _, err := runtime.HandleInput(t.Context(), []byte{'p', 'w', 'x', 0x7f, 'd', '\r'}); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRejectsOversizedCommand(t *testing.T) {
	runtime := New(func(context.Context, string) (connection.Result, error) {
		t.Fatal("oversized command should not execute")
		return connection.Result{}, nil
	})
	input := make([]byte, maxCommandBytes+1)
	for i := range input {
		input[i] = 'a'
	}

	if _, err := runtime.HandleInput(t.Context(), input); err != ErrCommandTooLong {
		t.Fatalf("HandleInput() error = %v, want %v", err, ErrCommandTooLong)
	}
}
