package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
)

const maxAgentResponseBytes = 1 << 20

var validHostID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)

type Agent struct {
	urlTemplate string
	client      *http.Client
}

func NewAgent(urlTemplate string, timeout time.Duration) *Agent {
	return &Agent{
		urlTemplate: strings.TrimRight(urlTemplate, "/"),
		client:      &http.Client{Timeout: timeout},
	}
}

func (a *Agent) Execute(ctx context.Context, command connection.Command) (connection.Result, error) {
	if strings.TrimSpace(command.Value) == "" {
		return connection.Result{}, errors.New("command is required")
	}
	endpoint, err := a.endpoint(command.HostID)
	if err != nil {
		return connection.Result{}, err
	}
	body, err := json.Marshal(map[string]string{"command": command.Value})
	if err != nil {
		return connection.Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return connection.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DDM-Session-ID", command.SessionID)
	req.Header.Set("X-DDM-User-ID", command.UserID)
	req.Header.Set("X-DDM-Host-ID", command.HostID)
	req.Header.Set("X-DDM-CR-ID", command.CRID)
	req.Header.Set("Authorization", "Bearer "+command.Token)

	resp, err := a.client.Do(req)
	if err != nil {
		return connection.Result{}, fmt.Errorf("execute through ddm-agent: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxAgentResponseBytes)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(limited)
		return connection.Result{}, fmt.Errorf("ddm-agent returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var result connection.Result
	if err := json.NewDecoder(limited).Decode(&result); err != nil {
		return connection.Result{}, fmt.Errorf("decode ddm-agent response: %w", err)
	}
	return result, nil
}

func (a *Agent) endpoint(hostID string) (string, error) {
	if strings.Contains(a.urlTemplate, "{hostId}") && !validHostID.MatchString(hostID) {
		return "", errors.New("hostId cannot be used in ddm-agent URL")
	}
	rawURL := strings.ReplaceAll(a.urlTemplate, "{hostId}", hostID)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid ddm-agent URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("ddm-agent URL must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil {
		return "", errors.New("ddm-agent URL must contain a host and no user info")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/execute"
	return parsed.String(), nil
}
