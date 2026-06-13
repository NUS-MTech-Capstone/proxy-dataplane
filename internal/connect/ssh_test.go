package connect

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"golang.org/x/crypto/ssh"
)

func TestSSHOpenRequestsPTYAndStreamsShell(t *testing.T) {
	_, clientPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	_, hostPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPrivate)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyBlock, err := ssh.MarshalPrivateKey(clientPrivate, "")
	if err != nil {
		t.Fatal(err)
	}
	server := newTestSSHServer(t, clientSigner.PublicKey(), hostSigner)
	defer server.Close()

	connector := NewSSH(
		EnvCredentials{PrivateKeyPEM: string(pem.EncodeToMemory(privateKeyBlock))},
		time.Second,
	)
	shell, err := connector.Open(t.Context(), model.SessionClaims{
		ConnectionType: "ssh",
		Address:        server.Address(),
		Username:       "ubuntu",
		HostPublicKey:  strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hostSigner.PublicKey()))),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.Close()

	if _, err := shell.Write([]byte("whoami\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(shell.Stdout()).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "echo:whoami\n" {
		t.Fatalf("output = %q, want echo", line)
	}
	if err := shell.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	select {
	case size := <-server.resize:
		if size != [2]uint32{120, 40} {
			t.Fatalf("resize = %v, want 120x40", size)
		}
	case <-time.After(time.Second):
		t.Fatal("SSH server did not receive window change")
	}
}

type testSSHServer struct {
	listener net.Listener
	resize   chan [2]uint32
	done     chan struct{}
}

func newTestSSHServer(t *testing.T, allowedClientKey ssh.PublicKey, hostKey ssh.Signer) *testSSHServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &testSSHServer{
		listener: listener,
		resize:   make(chan [2]uint32, 1),
		done:     make(chan struct{}),
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) != string(allowedClientKey.Marshal()) {
				return nil, fmt.Errorf("unauthorized key")
			}
			return nil, nil
		},
	}
	config.AddHostKey(hostKey)
	go server.serve(config)
	return server
}

func (s *testSSHServer) Address() string { return s.listener.Addr().String() }

func (s *testSSHServer) Close() {
	_ = s.listener.Close()
	select {
	case <-s.done:
	case <-time.After(time.Second):
	}
}

func (s *testSSHServer) serve(config *ssh.ServerConfig) {
	defer close(s.done)
	rawConn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer rawConn.Close()
	_, channels, requests, err := ssh.NewServerConn(rawConn, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "session required")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			return
		}
		go s.serveSession(channel, channelRequests)
	}
}

func (s *testSSHServer) serveSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for request := range requests {
		switch request.Type {
		case "pty-req":
			var payload struct {
				Term          string
				Columns, Rows uint32
				Width, Height uint32
				Modes         string
			}
			_ = ssh.Unmarshal(request.Payload, &payload)
			_ = request.Reply(payload.Term == "xterm-256color" && payload.Columns == 80 && payload.Rows == 24, nil)
		case "window-change":
			var payload struct {
				Columns, Rows uint32
				Width, Height uint32
			}
			_ = ssh.Unmarshal(request.Payload, &payload)
			s.resize <- [2]uint32{payload.Columns, payload.Rows}
		case "shell":
			_ = request.Reply(true, nil)
			go func() {
				reader := bufio.NewReader(channel)
				for {
					line, err := reader.ReadString('\n')
					if len(line) > 0 {
						_, _ = io.WriteString(channel, "echo:"+line)
					}
					if err != nil {
						return
					}
				}
			}()
		default:
			_ = request.Reply(false, nil)
		}
	}
}

func TestEnvCredentialsRequiresPrivateKey(t *testing.T) {
	if _, err := (EnvCredentials{}).PrivateKey(context.Background(), "secret"); err == nil {
		t.Fatal("PrivateKey() accepted empty SSH_PRIVATE_KEY_PEM")
	}
}
