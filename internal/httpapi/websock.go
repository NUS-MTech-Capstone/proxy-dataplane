package httpapi

import (
	"io"
	"log"
	"net/http"
	"strings"
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
	if err := s.svc.ValidateSession(r.Context(), sessionID, token, user.Subject); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	conn, err := upgrade(w, r)
	if err != nil {
		log.Printf("websocket upgrade failed sessionId=%s error=%v", sessionID, err)
		return
	}
	defer conn.Close()
	s.svc.StartSession(r.Context(), sessionID)
	defer s.svc.CloseSession(r.Context(), sessionID)
	log.Printf("SessionStarted sessionId=%s userId=%s", sessionID, user.Subject)
	_ = conn.WriteText([]byte("connected to DenDenMushi mock session\r\n"))
	for {
		msg, err := conn.ReadMessage()
		if err != nil {
			if err != io.EOF {
				log.Printf("websocket read failed sessionId=%s error=%v", sessionID, err)
			}
			return
		}
		if err := conn.WriteText(append([]byte("echo: "), msg...)); err != nil {
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
