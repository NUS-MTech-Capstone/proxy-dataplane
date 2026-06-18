package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
)

func (s *Server) operation(w http.ResponseWriter, r *http.Request) {
	runID, ok := parseAutomationRunID(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "capability token is required")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req model.OperationRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || req.OperationID == "" {
		writeError(w, http.StatusBadRequest, "valid operationId is required")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request must contain one JSON object")
		return
	}
	result, err := s.svc.ExecuteOperation(r.Context(), runID, token, req)
	if err != nil {
		slog.WarnContext(r.Context(), "AutomationOperationFailed",
			"requestId", observability.RequestID(r.Context()),
			"runId", runID,
			"operationId", req.OperationID,
			"error", err,
		)
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	slog.InfoContext(r.Context(), "AutomationOperationCompleted",
		"requestId", observability.RequestID(r.Context()),
		"runId", runID,
		"operationId", req.OperationID,
		"status", "completed",
		"exitCode", result.ExitCode,
	)
	writeJSON(w, http.StatusOK, result)
}

func parseAutomationRunID(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "automation" || parts[1] != "runs" || parts[3] != "operations" {
		return "", false
	}
	return parts[2], parts[2] != ""
}
