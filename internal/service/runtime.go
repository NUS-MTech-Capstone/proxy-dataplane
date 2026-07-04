package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/protocol"
)

type Runtime struct {
	service *Service
	session model.Session
	shell   connection.Shell
	ctx     context.Context
	cancel  context.CancelFunc

	mu         sync.Mutex
	clients    map[*Attachment]struct{}
	writer     *Attachment
	scrollback []byte
	graceTimer *time.Timer
	closed     bool
	closeOnce  sync.Once
}

type Attachment struct {
	runtime *Runtime
	mode    string
	userID  string
	output  chan protocol.Packet
	done    chan struct{}
	once    sync.Once
}

func newRuntime(service *Service, session model.Session, shell connection.Shell) *Runtime {
	ctx, cancel := context.WithCancel(service.rootCtx)
	return &Runtime{
		service: service,
		session: session,
		shell:   shell,
		ctx:     ctx,
		cancel:  cancel,
		clients: make(map[*Attachment]struct{}),
	}
}

func (r *Runtime) Start() {
	go r.copyOutput(r.shell.Stdout())
	go r.copyOutput(r.shell.Stderr())
	go r.heartbeatLoop()
	go func() {
		err := r.shell.Wait()
		if err != nil && r.ctx.Err() == nil {
			r.Broadcast(protocol.Packet{Type: protocol.Error, Data: []byte(err.Error())})
		}
		r.Close(err)
	}()
}

func (r *Runtime) Attach(mode, userID string) (*Attachment, error) {
	if mode != "owner" && mode != "observer" {
		return nil, errors.New("attachment mode must be owner or observer")
	}
	attachment := &Attachment{
		runtime: r,
		mode:    mode,
		userID:  userID,
		output:  make(chan protocol.Packet, 64),
		done:    make(chan struct{}),
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, ErrRuntimeNotFound
	}
	if mode == "owner" {
		if r.writer != nil {
			r.mu.Unlock()
			return nil, ErrWriterAlreadyAttached
		}
		r.writer = attachment
		if r.graceTimer != nil {
			r.graceTimer.Stop()
			r.graceTimer = nil
		}
	}
	r.clients[attachment] = struct{}{}
	scrollback := append([]byte(nil), r.scrollback...)
	r.mu.Unlock()

	if len(scrollback) > 0 {
		attachment.send(protocol.Packet{Type: protocol.Output, Data: scrollback})
	}
	r.Broadcast(protocol.Packet{Type: protocol.SessionEvent, Data: []byte(mode + " attached")})
	r.broadcastAttachmentSnapshot()
	return attachment, nil
}

func (a *Attachment) Output() <-chan protocol.Packet { return a.output }
func (a *Attachment) Done() <-chan struct{}          { return a.done }

func (a *Attachment) Handle(packet protocol.Packet) error {
	switch packet.Type {
	case protocol.Input:
		if !a.runtime.isWriter(a) {
			return errors.New("attachment is not the active session writer")
		}
		_, err := a.runtime.shell.Write(packet.Data)
		return err
	case protocol.Resize:
		if !a.runtime.isWriter(a) {
			if a.mode == "observer" {
				return nil
			}
			return errors.New("attachment is not the active session writer")
		}
		cols, rows, err := protocol.ParseResize(packet.Data)
		if err != nil {
			return err
		}
		return a.runtime.shell.Resize(cols, rows)
	case protocol.ClientClose:
		a.Close()
		return nil
	case protocol.ClientEndSession:
		if !a.runtime.isWriter(a) {
			return errors.New("attachment is not the active session writer")
		}
		a.runtime.Close(nil)
		return nil
	default:
		return errors.New("unsupported terminal packet")
	}
}

func (r *Runtime) isWriter(attachment *Attachment) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && r.writer == attachment
}

func (a *Attachment) Close() {
	a.once.Do(func() {
		a.runtime.detach(a)
		close(a.done)
	})
}

func (a *Attachment) send(packet protocol.Packet) bool {
	packet.Data = append([]byte(nil), packet.Data...)
	select {
	case <-a.done:
		return false
	case a.output <- packet:
		return true
	default:
		return false
	}
}

func (r *Runtime) detach(attachment *Attachment) {
	r.mu.Lock()
	if _, ok := r.clients[attachment]; !ok {
		r.mu.Unlock()
		return
	}
	delete(r.clients, attachment)
	wasWriter := r.writer == attachment
	if wasWriter {
		r.writer = nil
		if !r.closed {
			r.graceTimer = time.AfterFunc(r.service.rejoinGrace, func() {
				r.Close(errors.New("rejoin grace period expired"))
			})
		}
	}
	r.mu.Unlock()
	r.Broadcast(protocol.Packet{Type: protocol.SessionEvent, Data: []byte(attachment.mode + " detached")})
	r.broadcastAttachmentSnapshot()
}

func (r *Runtime) broadcastAttachmentSnapshot() {
	observers := r.observerUserIDs()
	payload, err := json.Marshal(struct {
		Type      string   `json:"type"`
		Observers []string `json:"observers"`
	}{
		Type:      "attachments",
		Observers: observers,
	})
	if err != nil {
		return
	}
	r.Broadcast(protocol.Packet{Type: protocol.SessionEvent, Data: payload})
}

func (r *Runtime) observerUserIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[string]struct{})
	for client := range r.clients {
		if client.mode != "observer" || client.userID == "" {
			continue
		}
		seen[client.userID] = struct{}{}
	}
	observers := make([]string, 0, len(seen))
	for userID := range seen {
		observers = append(observers, userID)
	}
	sort.Strings(observers)
	return observers
}

func (r *Runtime) Broadcast(packet protocol.Packet) {
	r.mu.Lock()
	if packet.Type == protocol.Output {
		r.scrollback = append(r.scrollback, packet.Data...)
		if overflow := len(r.scrollback) - r.service.scrollbackLimit; overflow > 0 {
			r.scrollback = append([]byte(nil), r.scrollback[overflow:]...)
		}
	}
	clients := make([]*Attachment, 0, len(r.clients))
	for client := range r.clients {
		clients = append(clients, client)
	}
	r.mu.Unlock()
	for _, client := range clients {
		if !client.send(packet) {
			client.Close()
		}
	}
}

func (r *Runtime) copyOutput(reader io.Reader) {
	buffer := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			r.Broadcast(protocol.Packet{Type: protocol.Output, Data: append([]byte(nil), buffer[:n]...)})
		}
		if err != nil {
			return
		}
	}
}

func (r *Runtime) heartbeatLoop() {
	heartbeat := func() bool {
		err := r.service.sm.HeartbeatSession(
			r.ctx, r.session.SessionID, r.service.pod.PodID, r.service.pod.InstanceID,
		)
		if err == nil || r.ctx.Err() != nil {
			return true
		}
		slog.WarnContext(r.ctx, "SessionHeartbeatFailed",
			"sessionId", r.session.SessionID,
			"userId", r.session.UserID,
			"hostId", r.session.HostID,
			"crId", r.session.CRID,
			"podId", r.service.pod.PodID,
			"instanceId", r.service.pod.InstanceID,
			"errorKind", observability.ErrorKind(err),
			"error", err,
		)
		observability.EmitCount("proxy-dataplane", "HeartbeatMisses", map[string]string{"Component": "session", "Reason": observability.ErrorKind(err)})
		var terminal interface{ Terminal() bool }
		if errors.As(err, &terminal) && terminal.Terminal() {
			r.Close(err)
			return false
		}
		return true
	}
	if !heartbeat() {
		return
	}
	ticker := time.NewTicker(r.service.interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			if !heartbeat() {
				return
			}
		}
	}
}

func (r *Runtime) Close(reason error) {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		if r.graceTimer != nil {
			r.graceTimer.Stop()
		}
		clients := make([]*Attachment, 0, len(r.clients))
		for client := range r.clients {
			clients = append(clients, client)
		}
		r.clients = make(map[*Attachment]struct{})
		r.writer = nil
		r.mu.Unlock()

		if reason != nil {
			packet := protocol.Packet{Type: protocol.Error, Data: []byte(reason.Error())}
			for _, client := range clients {
				client.send(packet)
			}
		}
		r.cancel()
		_ = r.shell.Close()
		for _, client := range clients {
			client.once.Do(func() { close(client.done) })
		}
		r.service.removeRuntime(r.session.SessionID, r)

		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.service.sm.CloseSession(
			closeCtx, r.session.SessionID, r.service.pod.PodID, r.service.pod.InstanceID,
		); err != nil {
			slog.WarnContext(closeCtx, "SessionCloseFailed",
				"sessionId", r.session.SessionID,
				"userId", r.session.UserID,
				"hostId", r.session.HostID,
				"crId", r.session.CRID,
				"podId", r.service.pod.PodID,
				"instanceId", r.service.pod.InstanceID,
				"errorKind", observability.ErrorKind(err),
				"error", err,
			)
		}
	})
}
