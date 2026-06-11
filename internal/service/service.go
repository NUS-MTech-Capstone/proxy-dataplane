package service

import (
	"context"
	"sync"
	"time"

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
	pod      model.ProxyPod
	mu       sync.RWMutex
	active   map[string]struct{}
	interval time.Duration
}

func New(sm SessionManager, sts TokenValidator, pod model.ProxyPod, interval time.Duration) *Service {
	return &Service{sm: sm, sts: sts, pod: pod, active: make(map[string]struct{}), interval: interval}
}
