package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

var helperPathPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

var readOnlyOperations = []string{
	"system_overview", "disk_usage", "memory_usage", "top_processes",
	"service_status", "service_logs", "health_check",
}

func (s *Service) ExecuteOperation(ctx context.Context, runID, token string, req model.OperationRequest) (model.OperationResult, error) {
	if s.executor == nil || s.automationSM == nil || s.automationSTS == nil {
		return model.OperationResult{}, errors.New("automation command execution is unavailable")
	}
	claims, err := s.automationSTS.ValidateAutomationToken(ctx, runID, token)
	if err != nil {
		return model.OperationResult{}, err
	}
	run, err := s.automationSM.GetAutomationRun(ctx, runID)
	if err != nil {
		return model.OperationResult{}, err
	}
	if run.Status != "ACTIVE" || !run.ExpiresAt.After(time.Now().UTC()) {
		return model.OperationResult{}, errors.New("automation run is not active")
	}
	if run.OwnerPodID != s.pod.PodID || run.OwnerInstanceID != s.pod.InstanceID ||
		claims.OwnerPodID != s.pod.PodID || claims.OwnerInstanceID != s.pod.InstanceID {
		return model.OperationResult{}, ErrOwnerMismatch
	}
	if claims.RunID != run.RunID || claims.IncidentID != run.IncidentID || claims.HostID != run.HostID {
		return model.OperationResult{}, errors.New("token claims do not match automation run")
	}
	if !slices.Contains(claims.AllowedOperations, req.OperationID) {
		return model.OperationResult{}, errors.New("operation is not allowed by capability token")
	}
	if len(req.Parameters) != 0 {
		return model.OperationResult{}, errors.New("operation does not accept parameters")
	}
	switch {
	case slices.Contains(readOnlyOperations, req.OperationID):
		if claims.Phase != "diagnose" && claims.Phase != "verify" {
			return model.OperationResult{}, errors.New("read-only operation is not allowed in this phase")
		}
	case req.OperationID == "restart_service":
		if claims.Phase != "remediate" || claims.PlanHash == "" || req.PlanHash == "" ||
			subtle.ConstantTimeCompare([]byte(claims.PlanHash), []byte(req.PlanHash)) != 1 {
			return model.OperationResult{}, errors.New("remediation plan hash mismatch")
		}
	default:
		return model.OperationResult{}, errors.New("unknown operation")
	}
	if !helperPathPattern.MatchString(claims.HelperPath) {
		return model.OperationResult{}, errors.New("invalid helper path in capability token")
	}
	command := fmt.Sprintf("sudo -- %s %s", claims.HelperPath, req.OperationID)
	operationCtx, cancel := context.WithTimeout(ctx, s.automationTimeout)
	defer cancel()
	result, err := s.executor.Execute(operationCtx, claims, command, s.automationOutputBytes)
	if err != nil {
		return model.OperationResult{}, err
	}
	result.OperationID = req.OperationID
	return result, nil
}
