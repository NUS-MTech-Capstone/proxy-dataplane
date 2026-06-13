package httpapi

import (
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/protocol"
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
	attachment, err := s.svc.Attach(r.Context(), sessionID, token, user.Subject)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	conn, err := upgrade(w, r)
	if err != nil {
		attachment.Close()
		log.Printf("websocket upgrade failed sessionId=%s error=%v", sessionID, err)
		return
	}
	defer conn.Close()
	defer attachment.Close()
	log.Printf("SessionAttached sessionId=%s userId=%s", sessionID, user.Subject)

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
			return
		case <-attachment.Done():
			return
		case err := <-readDone:
			if err != nil && err != io.EOF {
				log.Printf("websocket read failed sessionId=%s error=%v", sessionID, err)
			}
			return
		case packet := <-localOutput:
			if err := conn.WriteBinary(protocol.Encode(packet)); err != nil {
				return
			}
		case packet := <-attachment.Output():
			if err := conn.WriteBinary(protocol.Encode(packet)); err != nil {
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
