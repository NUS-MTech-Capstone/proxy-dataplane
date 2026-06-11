package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/session_manager"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/sts"
)

func TestValidateSessionRejectsAuthenticatedUserMismatch(t *testing.T) {
	stsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"allowed":true}`))
	}))
	defer stsServer.Close()

	sessionManagerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"sessionId":"sess-1",
			"userId":"owner-123",
			"ownerPodId":"pod-1",
			"ownerInstanceId":"instance-1"
		}`))
	}))
	defer sessionManagerServer.Close()

	svc := New(
		session_manager.NewSessionManager(sessionManagerServer.URL),
		sts.NewSTS(stsServer.URL),
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Second,
	)

	err := svc.ValidateSession(t.Context(), "sess-1", "token", "other-user")
	if err != ErrUserMismatch {
		t.Fatalf("ValidateSession() error = %v, want %v", err, ErrUserMismatch)
	}
}
