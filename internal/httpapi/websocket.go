package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/protocol"
)

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := parseSessionID(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !s.webSocketOriginAllowed(r.Header.Get("Origin")) {
		writeError(w, http.StatusForbidden, "websocket origin is not allowed")
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		writeError(w, http.StatusUnauthorized, "session token is required")
		return
	}

	cognitoSubject := ""
	if user, ok := cognitoUserFromContext(r.Context()); ok {
		cognitoSubject = user.Subject
	}
	userID, err := s.svc.ResolveSessionUserID(r.Context(), sessionID, token, cognitoSubject)
	if err != nil {
		slog.WarnContext(r.Context(), "SessionAttachAuthFailed",
			"requestId", observability.RequestID(r.Context()),
			"sessionId", sessionID,
			"error", err,
		)
		writeError(w, http.StatusUnauthorized, "missing or invalid Cognito identity")
		return
	}

	attachment, err := s.svc.Attach(r.Context(), sessionID, token, userID)
	if err != nil {
		slog.WarnContext(r.Context(), "SessionAttachFailed",
			"requestId", observability.RequestID(r.Context()),
			"sessionId", sessionID,
			"userId", userID,
			"error", err,
		)
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	conn, err := upgrade(w, r)
	if err != nil {
		attachment.Close()
		slog.WarnContext(r.Context(), "WebSocketUpgradeFailed",
			"requestId", observability.RequestID(r.Context()),
			"sessionId", sessionID,
			"userId", userID,
			"error", err,
		)
		return
	}
	defer conn.Close()
	defer attachment.Close()
	disconnectReason := "unknown"
	defer func() {
		observability.EmitCount("proxy-dataplane", "WebSocketDisconnects", map[string]string{
			"Component": "websocket",
			"Reason":    disconnectReason,
		})
		slog.InfoContext(r.Context(), "WebSocketDisconnected",
			"requestId", observability.RequestID(r.Context()),
			"sessionId", sessionID,
			"userId", userID,
			"reason", disconnectReason,
		)
	}()
	slog.InfoContext(r.Context(), "SessionAttached",
		"requestId", observability.RequestID(r.Context()),
		"sessionId", sessionID,
		"userId", userID,
	)

	localOutput := make(chan protocol.Packet, 8)
	readDone := make(chan error, 1)
	go func() {
		for {
			data, err := conn.ReadBinary()
			if err != nil {
				readDone <- err
				return
			}
			packet, err := protocol.Parse(data)
			if err == nil && packet.Type == protocol.ClientClose {
				disconnectReason = "client_close"
			}
			if err == nil && packet.Type == protocol.ClientEndSession {
				disconnectReason = "client_end_session"
			}
			if err == nil {
				err = attachment.Handle(packet)
			}
			if err != nil {
				select {
				case localOutput <- protocol.Packet{Type: protocol.Error, Data: []byte(err.Error())}:
				default:
				}
			}
		}
	}()

	for {
		select {
		case <-r.Context().Done():
			disconnectReason = "context_cancelled"
			return
		case <-attachment.Done():
			if disconnectReason == "unknown" {
				disconnectReason = "runtime_closed"
			}
			return
		case err := <-readDone:
			if err == io.EOF {
				disconnectReason = "client_close"
			} else if err != nil {
				disconnectReason = "read_error"
				slog.WarnContext(r.Context(), "WebSocketReadFailed",
					"requestId", observability.RequestID(r.Context()),
					"sessionId", sessionID,
					"userId", userID,
					"errorKind", observability.ErrorKind(err),
					"error", err,
				)
			}
			return
		case packet := <-localOutput:
			if err := conn.WriteBinary(protocol.Encode(packet)); err != nil {
				disconnectReason = "write_error"
				return
			}
		case packet := <-attachment.Output():
			if err := conn.WriteBinary(protocol.Encode(packet)); err != nil {
				disconnectReason = "write_error"
				return
			}
		}
	}
}

func parseSessionID(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "sessions" || parts[2] != "ws" {
		return "", false
	}
	return parts[1], true
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(header, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(header, prefix))
	}
	return ""
}
