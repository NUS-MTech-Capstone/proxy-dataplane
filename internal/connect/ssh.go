package connect

import (
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
