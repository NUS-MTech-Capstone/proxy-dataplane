package httpapi

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
	targetsession "github.com/NUS-MTech-Capstone/access-proxy/internal/session"
)

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := parseSessionID(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	user, ok := cognitoUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing Cognito identity")
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
	session, err := s.svc.AuthorizeSession(r.Context(), sessionID, token, user.Subject)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := s.svc.StartSession(r.Context(), sessionID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	conn, err := upgrade(w, r)
	if err != nil {
		s.svc.AbortSession(sessionID)
		log.Printf("websocket upgrade failed sessionId=%s error=%v", sessionID, err)
		return
	}
	defer conn.Close()
	defer s.svc.CloseSession(r.Context(), sessionID)
	log.Printf("SessionStarted sessionId=%s userId=%s", sessionID, user.Subject)

	runtime := targetsession.New(func(ctx context.Context, command string) (connection.Result, error) {
		return s.svc.ExecuteCommand(ctx, session, token, command)
	})
	if err := conn.WriteText(runtime.Welcome(session.HostID)); err != nil {
		log.Printf("websocket welcome failed sessionId=%s error=%v", sessionID, err)
		return
	}
	for {
		msg, err := conn.ReadMessage()
		if err != nil {
			if err != io.EOF {
				log.Printf("websocket read failed sessionId=%s error=%v", sessionID, err)
			}
			return
		}
		output, executeErr := runtime.HandleInput(r.Context(), msg)
		if executeErr != nil {
			output = append(output, []byte("command failed\r\n$ ")...)
		}
		if len(output) == 0 {
			continue
		}
		if err := conn.WriteText(output); err != nil {
			log.Printf("websocket write failed sessionId=%s error=%v", sessionID, err)
			return
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
