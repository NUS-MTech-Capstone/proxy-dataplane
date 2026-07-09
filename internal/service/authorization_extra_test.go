package service

import (
	"errors"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

func TestAuthorizeSessionRejectsOwnerMetadataAndActionModeMismatch(t *testing.T) {
	session := testSession()
	session.OwnerInstanceID = "other-instance"
	svc := newTestService(&fakeSessionManager{session: session}, &fakeConnector{}, time.Second)
	if _, err := svc.AuthorizeSession(t.Context(), "sess-1", "start", "owner-1"); !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("AuthorizeSession() error = %v, want %v", err, ErrOwnerMismatch)
	}

	session = testSession()
	validator := fakeTokenValidator{claims: map[string]model.SessionClaims{
		"bad-mode": testClaims("observe", "owner", "owner-1"),
	}}
	svc = New(
		&fakeSessionManager{session: session}, validator, &fakeConnector{},
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1", MaxSessions: 50},
		5*time.Millisecond, time.Second, 1024,
	)
	if _, err := svc.AuthorizeSession(t.Context(), "sess-1", "bad-mode", "owner-1"); err == nil {
		t.Fatal("AuthorizeSession() accepted invalid observe/owner token")
	}
}
