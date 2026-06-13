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
	CloseSession(context.Context, string, string, string) error
}

type TokenValidator interface {
	ValidateToken(context.Context, string, string, string) (model.SessionClaims, error)
}

type Service struct {
	sm              SessionManager
	sts             TokenValidator
	connector       connection.ShellConnector
	pod             model.ProxyPod
	mu              sync.RWMutex
	runtimes        map[string]*Runtime
	rootCtx         context.Context
	interval        time.Duration
	rejoinGrace     time.Duration
	scrollbackLimit int
}

func New(
	sm SessionManager,
	sts TokenValidator,
	connector connection.ShellConnector,
	pod model.ProxyPod,
	interval time.Duration,
	rejoinGrace time.Duration,
	scrollbackLimit int,
) *Service {
	if rejoinGrace <= 0 {
		rejoinGrace = 5 * time.Minute
	}
	if scrollbackLimit <= 0 {
		scrollbackLimit = 256 * 1024
	}
	return &Service{
		sm:              sm,
		sts:             sts,
		connector:       connector,
		pod:             pod,
		runtimes:        make(map[string]*Runtime),
		rootCtx:         context.Background(),
		interval:        interval,
		rejoinGrace:     rejoinGrace,
		scrollbackLimit: scrollbackLimit,
	}
}

func (s *Service) runtime(sessionID string) (*Runtime, bool) {
	s.mu.RLock()
	runtime, ok := s.runtimes[sessionID]
	s.mu.RUnlock()
	return runtime, ok
}

func (s *Service) removeRuntime(sessionID string, runtime *Runtime) {
	s.mu.Lock()
	if s.runtimes[sessionID] == runtime {
		delete(s.runtimes, sessionID)
	}
	s.mu.Unlock()
}
