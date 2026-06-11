package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type cognitoUserContextKey struct{}

type CognitoUser struct {
	Subject string         `json:"sub"`
	Email   string         `json:"email,omitempty"`
	Claims  map[string]any `json:"claims,omitempty"`
}

func cognitoMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := cognitoUserFromALBHeaders(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "missing or invalid Cognito identity")
			return
		}

		ctx := context.WithValue(r.Context(), cognitoUserContextKey{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func cognitoUserFromALBHeaders(r *http.Request) (CognitoUser, error) {
	claims, err := decodeJWTClaims(r.Header.Get("x-amzn-oidc-data"))
	if err != nil {
		return CognitoUser{}, err
	}

	subject, _ := claims["sub"].(string)
	if subject == "" {
		return CognitoUser{}, errors.New("sub claim is required")
	}

	email, _ := claims["email"].(string)
	return CognitoUser{
		Subject: subject,
		Email:   email,
		Claims:  claims,
	}, nil
}

func decodeJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode JWT payload: %w", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("decode JWT claims: %w", err)
	}
	return claims, nil
}

func cognitoUserFromContext(ctx context.Context) (CognitoUser, bool) {
	user, ok := ctx.Value(cognitoUserContextKey{}).(CognitoUser)
	return user, ok
}
