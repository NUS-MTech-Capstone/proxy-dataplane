package session

import (
	"context"
	"errors"
	"strings"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
)

const prompt = "$ "
const maxCommandBytes = 16 * 1024

var ErrCommandTooLong = errors.New("command exceeds maximum length")

type CommandHandler func(context.Context, string) (connection.Result, error)

// Runtime adapts terminal keystrokes to the DDM Agent's line-oriented command
// endpoint. A streaming PTY transport can replace this without changing the
// public WebSocket handler.
type Runtime struct {
	line    []byte
	execute CommandHandler
}

func New(execute CommandHandler) *Runtime {
	return &Runtime{execute: execute}
}

func (r *Runtime) Welcome(hostID string) []byte {
	return []byte("connected to DenDenMushi target " + hostID + "\r\n" + prompt)
}

func (r *Runtime) HandleInput(ctx context.Context, input []byte) ([]byte, error) {
	var output []byte
	for _, b := range input {
		switch b {
		case '\r', '\n':
			if len(r.line) == 0 {
				if b == '\r' {
					output = append(output, "\r\n"...)
					output = append(output, prompt...)
				}
				continue
			}
			command := string(r.line)
			r.line = r.line[:0]
			output = append(output, "\r\n"...)
			result, err := r.execute(ctx, command)
			if err != nil {
				return output, err
			}
			output = appendTerminalLine(output, result.Stdout)
			output = appendTerminalLine(output, result.Stderr)
			output = append(output, prompt...)
		case 0x03:
			r.line = r.line[:0]
			output = append(output, "^C\r\n"...)
			output = append(output, prompt...)
		case 0x08, 0x7f:
			if len(r.line) > 0 {
				r.line = r.line[:len(r.line)-1]
				output = append(output, "\b \b"...)
			}
		default:
			if b >= 0x20 || b == '\t' {
				if len(r.line) >= maxCommandBytes {
					r.line = r.line[:0]
					return output, ErrCommandTooLong
				}
				r.line = append(r.line, b)
				output = append(output, b)
			}
		}
	}
	return output, nil
}

func appendTerminalLine(dst []byte, value string) []byte {
	if value == "" {
		return dst
	}
	dst = append(dst, value...)
	if !strings.HasSuffix(value, "\n") {
		dst = append(dst, "\r\n"...)
	}
	return dst
}
