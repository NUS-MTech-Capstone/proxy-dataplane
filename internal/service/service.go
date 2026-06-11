package service

import (
	"sync"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/session_manager"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/sts"
)

type Service struct {
	sm       *session_manager.Client
	sts      *sts.Client
	pod      model.ProxyPod
	mu       sync.RWMutex
	active   map[string]struct{}
	interval time.Duration
}

func New(sm *session_manager.Client, sts *sts.Client, pod model.ProxyPod, interval time.Duration) *Service {
	return &Service{sm: sm, sts: sts, pod: pod, active: make(map[string]struct{}), interval: interval}
}
