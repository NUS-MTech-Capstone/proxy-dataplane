package session_manager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

func (c *Client) GetSession(ctx context.Context, sessionID string) (model.Session, error) {
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

func (c *Client) HeartbeatSession(ctx context.Context, sessionID, podID, instanceID string) error {
	return c.post(ctx, "/sessions/"+sessionID+"/heartbeat", map[string]string{"ownerPodId": podID, "ownerInstanceId": instanceID}, nil)
}

func (c *Client) CloseSession(ctx context.Context, sessionID string) error {
	return c.post(ctx, "/sessions/"+sessionID+"/close", map[string]string{}, nil)
}

func (c *Client) post(ctx context.Context, path string, req any, out any) error {
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
