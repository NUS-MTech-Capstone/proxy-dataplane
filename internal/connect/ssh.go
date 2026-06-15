package connect

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"golang.org/x/crypto/ssh"
)

type SSH struct {
	credentials CredentialProvider
	timeout     time.Duration
}

func NewSSH(credentials CredentialProvider, timeout time.Duration) *SSH {
	return &SSH{credentials: credentials, timeout: timeout}
}

func (c *SSH) Open(ctx context.Context, claims model.SessionClaims) (connection.Shell, error) {
	if claims.ConnectionType != "ssh" {
		return nil, errors.New("only ssh connectionType is supported")
	}
	privateKey, err := c.credentials.PrivateKey(ctx, claims.CredentialSecretID)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(claims.HostPublicKey))
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:              claims.Username,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback:   ssh.FixedHostKey(hostKey),
		HostKeyAlgorithms: hostKeyAlgorithms(hostKey),
		Timeout:           c.timeout,
	}
	dialer := net.Dialer{Timeout: c.timeout}
	rawConn, err := dialer.DialContext(ctx, "tcp", claims.Address)
	if err != nil {
		return nil, err
	}
	if c.timeout > 0 {
		_ = rawConn.SetDeadline(time.Now().Add(c.timeout))
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	clientConn, channels, requests, err := ssh.NewClientConn(rawConn, claims.Address, config)
	stopCancel()
	if err != nil {
		rawConn.Close()
		return nil, err
	}
	_ = rawConn.SetDeadline(time.Time{})
	client := ssh.NewClient(clientConn, channels, requests)
	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	if err := session.Shell(); err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	return &sshShell{
		client: client, session: session, stdin: stdin, stdout: stdout, stderr: stderr,
	}, nil
}

func (c *SSH) Execute(ctx context.Context, claims model.AutomationClaims, command string, maxOutputBytes int) (model.OperationResult, error) {
	client, err := c.openClient(ctx, claims.Address, claims.Username, claims.CredentialSecretID, claims.HostPublicKey)
	if err != nil {
		return model.OperationResult{}, err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return model.OperationResult{}, err
	}
	defer session.Close()
	stdout := newLimitedBuffer(maxOutputBytes)
	stderr := newLimitedBuffer(maxOutputBytes)
	session.Stdout = stdout
	session.Stderr = stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		_ = client.Close()
		<-done
		return model.OperationResult{}, ctx.Err()
	case err := <-done:
		result := model.OperationResult{Stdout: stdout.String(), Stderr: stderr.String()}
		if err == nil {
			return result, nil
		}
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitStatus()
			return result, nil
		}
		return model.OperationResult{}, err
	}
}

func (c *SSH) openClient(ctx context.Context, address, username, credentialSecretID, hostPublicKey string) (*ssh.Client, error) {
	privateKey, err := c.credentials.PrivateKey(ctx, credentialSecretID)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(hostPublicKey))
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User: username, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.FixedHostKey(hostKey), HostKeyAlgorithms: hostKeyAlgorithms(hostKey),
		Timeout: c.timeout,
	}
	dialer := net.Dialer{Timeout: c.timeout}
	rawConn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	if c.timeout > 0 {
		_ = rawConn.SetDeadline(time.Now().Add(c.timeout))
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	clientConn, channels, requests, err := ssh.NewClientConn(rawConn, address, config)
	stopCancel()
	if err != nil {
		rawConn.Close()
		return nil, err
	}
	_ = rawConn.SetDeadline(time.Time{})
	return ssh.NewClient(clientConn, channels, requests), nil
}

func hostKeyAlgorithms(hostKey ssh.PublicKey) []string {
	if hostKey.Type() == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{hostKey.Type()}
}

type sshShell struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	stderr  io.Reader
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	remaining int
	truncated bool
}

func newLimitedBuffer(limit int) *limitedBuffer {
	if limit <= 0 {
		limit = 256 * 1024
	}
	return &limitedBuffer{remaining: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	originalLength := len(p)
	if len(p) > b.remaining {
		p = p[:b.remaining]
		b.truncated = true
	}
	if len(p) > 0 {
		_, _ = b.buffer.Write(p)
		b.remaining -= len(p)
	}
	return originalLength, nil
}

func (b *limitedBuffer) String() string {
	if b.truncated {
		return b.buffer.String() + "\n[output truncated]\n"
	}
	return b.buffer.String()
}

func (s *sshShell) Write(data []byte) (int, error) { return s.stdin.Write(data) }
func (s *sshShell) Stdout() io.Reader              { return s.stdout }
func (s *sshShell) Stderr() io.Reader              { return s.stderr }
func (s *sshShell) Resize(cols, rows uint16) error {
	return s.session.WindowChange(int(rows), int(cols))
}
func (s *sshShell) Wait() error { return s.session.Wait() }
func (s *sshShell) Close() error {
	_ = s.stdin.Close()
	_ = s.session.Close()
	return s.client.Close()
}
