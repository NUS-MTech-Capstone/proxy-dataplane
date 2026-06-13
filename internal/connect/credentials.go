package connect

import (
	"context"
	"errors"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type CredentialProvider interface {
	PrivateKey(context.Context, string) ([]byte, error)
}

type EnvCredentials struct {
	PrivateKeyPEM string
}

func (p EnvCredentials) PrivateKey(_ context.Context, _ string) ([]byte, error) {
	if p.PrivateKeyPEM == "" {
		return nil, errors.New("SSH_PRIVATE_KEY_PEM is required")
	}
	return []byte(p.PrivateKeyPEM), nil
}

type secretsManagerClient interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

type SecretsManagerCredentials struct {
	client secretsManagerClient
}

func NewSecretsManagerCredentials(ctx context.Context, region string) (*SecretsManagerCredentials, error) {
	options := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		options = append(options, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	return &SecretsManagerCredentials{client: secretsmanager.NewFromConfig(cfg)}, nil
}

func (p *SecretsManagerCredentials) PrivateKey(ctx context.Context, secretID string) ([]byte, error) {
	if secretID == "" {
		return nil, errors.New("credential secret ID is required")
	}
	output, err := p.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &secretID})
	if err != nil {
		return nil, err
	}
	if output.SecretString != nil {
		return []byte(*output.SecretString), nil
	}
	if len(output.SecretBinary) == 0 {
		return nil, errors.New("secret contains no private key")
	}
	return append([]byte(nil), output.SecretBinary...), nil
}
