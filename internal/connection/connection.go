package connection

import (
	"context"
	"io"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

type Shell interface {
	io.Writer
	Stdout() io.Reader
	Stderr() io.Reader
	Resize(cols, rows uint16) error
	Wait() error
	Close() error
}

type ShellConnector interface {
	Open(context.Context, model.SessionClaims) (Shell, error)
}
