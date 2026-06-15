package model

import "time"

const SessionStatusActive = "ACTIVE"

type Session struct {
	SessionID       string    `json:"sessionId"`
	UserID          string    `json:"userId"`
	HostID          string    `json:"hostId"`
	CRID            string    `json:"crId"`
	ConnectionType  string    `json:"connectionType"`
	OwnerPodID      string    `json:"ownerPodId"`
	OwnerInstanceID string    `json:"ownerInstanceId"`
	WebsocketURL    string    `json:"websocketUrl"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
	LastHeartbeatAt time.Time `json:"lastHeartbeatAt"`
	LeaseExpiresAt  time.Time `json:"leaseExpiresAt"`
}

type SessionClaims struct {
	SessionID          string `json:"sessionId"`
	UserID             string `json:"userId"`
	HostID             string `json:"hostId"`
	CRID               string `json:"crId"`
	ConnectionType     string `json:"connectionType"`
	Action             string `json:"action"`
	Mode               string `json:"mode"`
	Address            string `json:"address,omitempty"`
	Username           string `json:"username,omitempty"`
	CredentialSecretID string `json:"credentialSecretId,omitempty"`
	HostPublicKey      string `json:"hostPublicKey,omitempty"`
}

type ProxyPod struct {
	PodID           string    `json:"podId"`
	InstanceID      string    `json:"instanceId"`
	PodIP           string    `json:"podIp"`
	WebsocketURL    string    `json:"websocketUrl"`
	OperationsURL   string    `json:"operationsUrl"`
	Status          string    `json:"status"`
	ActiveSessions  int       `json:"activeSessions"`
	MaxSessions     int       `json:"maxSessions"`
	LastHeartbeatAt time.Time `json:"lastHeartbeatAt,omitempty"`
}

type AutomationRun struct {
	RunID           string    `json:"runId"`
	IncidentID      string    `json:"incidentId"`
	HostID          string    `json:"hostId"`
	OwnerPodID      string    `json:"ownerPodId"`
	OwnerInstanceID string    `json:"ownerInstanceId"`
	OperationsURL   string    `json:"operationsUrl"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type AutomationClaims struct {
	RunID              string   `json:"runId"`
	IncidentID         string   `json:"incidentId"`
	HostID             string   `json:"hostId"`
	Phase              string   `json:"phase"`
	AllowedOperations  []string `json:"allowedOperations"`
	PlanHash           string   `json:"planHash,omitempty"`
	OwnerPodID         string   `json:"ownerPodId"`
	OwnerInstanceID    string   `json:"ownerInstanceId"`
	Address            string   `json:"address"`
	Username           string   `json:"username"`
	CredentialSecretID string   `json:"credentialSecretId"`
	HostPublicKey      string   `json:"hostPublicKey"`
	ServiceName        string   `json:"serviceName"`
	HealthURL          string   `json:"healthUrl"`
	HelperPath         string   `json:"helperPath"`
}

type OperationRequest struct {
	OperationID string            `json:"operationId"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	PlanHash    string            `json:"planHash,omitempty"`
}

type OperationResult struct {
	OperationID string `json:"operationId"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	ExitCode    int    `json:"exitCode"`
}
