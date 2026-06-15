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

type AutomationSessionManager interface {
	GetAutomationRun(context.Context, string) (model.AutomationRun, error)
}

type AutomationTokenValidator interface {
	ValidateAutomationToken(context.Context, string, string) (model.AutomationClaims, error)
}

type Service struct {
	sm                    SessionManager
	sts                   TokenValidator
	automationSM          AutomationSessionManager
	automationSTS         AutomationTokenValidator
	connector             connection.ShellConnector
	executor              connection.CommandExecutor
	pod                   model.ProxyPod
	mu                    sync.RWMutex
	runtimes              map[string]*Runtime
	rootCtx               context.Context
	interval              time.Duration
	rejoinGrace           time.Duration
	scrollbackLimit       int
	automationTimeout     time.Duration
	automationOutputBytes int
}

func New(
	sm SessionManager,
	sts TokenValidator,
	connector connection.ShellConnector,
	pod model.ProxyPod,
	interval time.Duration,
	rejoinGrace time.Duration,
	scrollbackLimit int,
	automationOptions ...any,
) *Service {
	if rejoinGrace <= 0 {
		rejoinGrace = 5 * time.Minute
	}
	if scrollbackLimit <= 0 {
		scrollbackLimit = 256 * 1024
	}
	var executor connection.CommandExecutor
	if candidate, ok := connector.(connection.CommandExecutor); ok {
		executor = candidate
	}
	automationSM, _ := sm.(AutomationSessionManager)
	automationSTS, _ := sts.(AutomationTokenValidator)
	automationTimeout := 30 * time.Second
	automationOutputBytes := 256 * 1024
	for _, option := range automationOptions {
		switch value := option.(type) {
		case connection.CommandExecutor:
			executor = value
		case time.Duration:
			if value > 0 {
				automationTimeout = value
			}
		case int:
			if value > 0 {
				automationOutputBytes = value
			}
		}
	}
	return &Service{
		sm:                    sm,
		sts:                   sts,
		automationSM:          automationSM,
		automationSTS:         automationSTS,
		connector:             connector,
		executor:              executor,
		pod:                   pod,
		runtimes:              make(map[string]*Runtime),
		rootCtx:               context.Background(),
		interval:              interval,
		rejoinGrace:           rejoinGrace,
		scrollbackLimit:       scrollbackLimit,
		automationTimeout:     automationTimeout,
		automationOutputBytes: automationOutputBytes,
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
