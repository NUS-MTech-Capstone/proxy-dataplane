package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

type SessionManager struct {
	baseURL string
	client  *http.Client
}

func NewSessionManager(baseURL string) *SessionManager {
	return &SessionManager{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *SessionManager) RegisterPod(ctx context.Context, pod model.ProxyPod) error {
	return c.post(ctx, "/proxy-pods/register", pod, nil)
}

func (c *SessionManager) HeartbeatPod(ctx context.Context, pod model.ProxyPod) error {
	return c.post(ctx, "/proxy-pods/heartbeat", pod, nil)
}

func (c *SessionManager) GetSession(ctx context.Context, sessionID string) (model.Session, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/sessions/"+sessionID, nil)
	if err != nil {
		return model.Session{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return model.Session{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.Session{}, fmt.Errorf("session-manager returned status %d", resp.StatusCode)
	}
	var session model.Session
	return session, json.NewDecoder(resp.Body).Decode(&session)
}

func (c *SessionManager) HeartbeatSession(ctx context.Context, sessionID, podID, instanceID string) error {
	return c.post(ctx, "/sessions/"+sessionID+"/heartbeat", map[string]string{"ownerPodId": podID, "ownerInstanceId": instanceID}, nil)
}

func (c *SessionManager) CloseSession(ctx context.Context, sessionID string) error {
	return c.post(ctx, "/sessions/"+sessionID+"/close", map[string]string{}, nil)
}

func (c *SessionManager) post(ctx context.Context, path string, req any, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("session-manager returned status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
