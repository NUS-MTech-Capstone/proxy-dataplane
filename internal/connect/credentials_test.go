package connect

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type fakeSecretsManager struct {
	output *secretsmanager.GetSecretValueOutput
}

func (f fakeSecretsManager) GetSecretValue(
	context.Context,
	*secretsmanager.GetSecretValueInput,
	...func(*secretsmanager.Options),
) (*secretsmanager.GetSecretValueOutput, error) {
	return f.output, nil
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
