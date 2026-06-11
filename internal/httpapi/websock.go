package httpapi

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
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
	mode, ok := sessionMode(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "mode must be owner or observe")
		return
	}
	session, err := s.svc.AuthorizeSession(r.Context(), sessionID, token, user.Subject, mode)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if mode == "observer" {
		s.observe(w, r, sessionID, session.HostID, user.Subject)
		return
	}
	s.own(w, r, sessionID, token, user.Subject, session)
}

func (s *Server) own(w http.ResponseWriter, r *http.Request, sessionID, token, userID string, session model.Session) {
	sessionCtx, err := s.svc.StartSession(r.Context(), sessionID)
	if err != nil {
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
	defer s.svc.DisconnectSession(sessionID)
	go func() {
		<-sessionCtx.Done()
		_ = conn.Close()
	}()
	log.Printf("SessionStarted sessionId=%s userId=%s", sessionID, userID)

	runtime := targetsession.New(func(ctx context.Context, command string) (connection.Result, error) {
		return s.svc.ExecuteCommand(ctx, session, token, command)
	})
	welcome := runtime.Welcome(session.HostID)
	if err := conn.WriteText(welcome); err != nil {
		log.Printf("websocket welcome failed sessionId=%s error=%v", sessionID, err)
		return
	}
	s.svc.Publish(sessionID, welcome)
	for {
		msg, err := conn.ReadMessage()
		if err != nil {
			if err != io.EOF {
				log.Printf("websocket read failed sessionId=%s error=%v", sessionID, err)
			}
			return
		}
		output, executeErr := runtime.HandleInput(sessionCtx, msg)
		if executeErr != nil {
			output = append(output, []byte("command failed\r\n$ ")...)
		}
		if len(output) == 0 {
			continue
		}
		s.svc.Publish(sessionID, output)
		if err := conn.WriteText(output); err != nil {
			log.Printf("websocket write failed sessionId=%s error=%v", sessionID, err)
			return
		}
	}
}

func (s *Server) observe(w http.ResponseWriter, r *http.Request, sessionID, hostID, userID string) {
	outputs, unsubscribe := s.svc.Subscribe(sessionID)
	defer unsubscribe()

	conn, err := upgrade(w, r)
	if err != nil {
		log.Printf("observer websocket upgrade failed sessionId=%s error=%v", sessionID, err)
		return
	}
	defer conn.Close()
	log.Printf("ObserverJoined sessionId=%s userId=%s", sessionID, userID)

	if err := conn.WriteText([]byte("Observing session on " + hostID + "\r\n")); err != nil {
		return
	}
	disconnected := make(chan struct{})
	go func() {
		defer close(disconnected)
		_, _ = conn.ReadMessage()
	}()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-disconnected:
			return
		case output := <-outputs:
			if err := conn.WriteText(output); err != nil {
				return
			}
		}
	}
}

func sessionMode(r *http.Request) (string, bool) {
	switch r.URL.Query().Get("mode") {
	case "", "owner":
		return "owner", true
	case "observe", "observer":
		return "observer", true
	default:
		return "", false
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
