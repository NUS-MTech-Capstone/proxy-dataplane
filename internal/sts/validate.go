package sts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type validateResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

func (c *Client) ValidateToken(ctx context.Context, sessionID, userID, mode, token string) error {
	body, err := json.Marshal(map[string]string{
		"sessionId": sessionID,
		"userId":    userID,
		"mode":      mode,
		"token":     token,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tokens/validate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sts returned status %d", resp.StatusCode)
	}
	var parsed validateResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return err
	}
	if !parsed.Allowed {
		return fmt.Errorf("token rejected: %s", parsed.Reason)
	}
	return nil
}
