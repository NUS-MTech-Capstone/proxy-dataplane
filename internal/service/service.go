package service

import (
	"context"
	"sync"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

type SessionManager interface {
	RegisterPod(context.Context, model.ProxyPod) error
	HeartbeatPod(context.Context, model.ProxyPod) error
	GetSession(context.Context, string) (model.Session, error)
	HeartbeatSession(context.Context, string, string, string) error
	CloseSession(context.Context, string) error
}

type TokenValidator interface {
	ValidateToken(context.Context, string, string) error
}

type Service struct {
	sm       SessionManager
	sts      TokenValidator
	executor connection.Executor
	pod      model.ProxyPod
	mu       sync.RWMutex
	active   map[string]context.CancelFunc
	interval time.Duration
}

func New(sm SessionManager, sts TokenValidator, executor connection.Executor, pod model.ProxyPod, interval time.Duration) *Service {
	return &Service{
		sm:       sm,
		sts:      sts,
		executor: executor,
		pod:      pod,
		active:   make(map[string]context.CancelFunc),
		interval: interval,
	}
}
