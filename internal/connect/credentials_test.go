package connect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type fakeSecretsManager struct {
	output *secretsmanager.GetSecretValueOutput
	err    error
}

func (f fakeSecretsManager) GetSecretValue(
	context.Context,
	*secretsmanager.GetSecretValueInput,
	...func(*secretsmanager.Options),
) (*secretsmanager.GetSecretValueOutput, error) {
	return f.output, f.err
}

func TestSecretsManagerCredentialsReturnsSecretString(t *testing.T) {
	value := "private-key"
	provider := &SecretsManagerCredentials{
		client: fakeSecretsManager{output: &secretsmanager.GetSecretValueOutput{SecretString: &value}},
	}
	key, err := provider.PrivateKey(t.Context(), "secret/host-1")
	if err != nil {
		t.Fatal(err)
	}
	if string(key) != value {
		t.Fatalf("key = %q, want %q", key, value)
	}
}

func TestSecretsManagerCredentialsPrivateKeyEdges(t *testing.T) {
	value := "private-key"
	tests := []struct {
		name    string
		id      string
		output  *secretsmanager.GetSecretValueOutput
		err     error
		want    string
		wantErr bool
	}{
		{name: "missing id", wantErr: true},
		{name: "secret string", id: "secret", output: &secretsmanager.GetSecretValueOutput{SecretString: &value}, want: value},
		{name: "secret binary", id: "secret", output: &secretsmanager.GetSecretValueOutput{SecretBinary: []byte(value)}, want: value},
		{name: "empty secret", id: "secret", output: &secretsmanager.GetSecretValueOutput{}, wantErr: true},
		{name: "client error", id: "secret", err: errors.New("denied"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &SecretsManagerCredentials{client: fakeSecretsManager{output: tt.output, err: tt.err}}
			key, err := provider.PrivateKey(t.Context(), tt.id)
			if tt.wantErr {
				if err == nil {
					t.Fatal("PrivateKey() error = nil")
				}
				return
			}
			if err != nil || string(key) != tt.want {
				t.Fatalf("PrivateKey() = %q, %v", key, err)
			}
		})
	}
}

func TestEnvCredentialsPrivateKey(t *testing.T) {
	if _, err := (EnvCredentials{}).PrivateKey(t.Context(), ""); err == nil {
		t.Fatal("PrivateKey() accepted empty env key")
	}
	key, err := (EnvCredentials{PrivateKeyPEM: "pem"}).PrivateKey(t.Context(), "")
	if err != nil || string(key) != "pem" {
		t.Fatalf("PrivateKey() = %q, %v", key, err)
	}
}

func TestNewCredentialProviderSelectsEnvStrategy(t *testing.T) {
	provider, err := NewCredentialProvider(t.Context(), "env", "pem", "")
	if err != nil {
		t.Fatal(err)
	}
	key, err := provider.PrivateKey(t.Context(), "")
	if err != nil || string(key) != "pem" {
		t.Fatalf("PrivateKey() = %q, %v", key, err)
	}
}

func TestNewCredentialProviderRejectsUnknownStrategy(t *testing.T) {
	_, err := NewCredentialProvider(t.Context(), "vault", "", "")
	if err == nil || !strings.Contains(err.Error(), "unsupported SSH_KEY_PROVIDER") {
		t.Fatalf("NewCredentialProvider() error = %v, want unsupported provider", err)
	}
}
