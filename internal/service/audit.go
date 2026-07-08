package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
)

const (
	auditQueueSize     = 256
	auditBatchSize     = 32
	auditFlushInterval = 250 * time.Millisecond
	auditAppendTimeout = 2 * time.Second
)

type AuditRecorder struct {
	service *Service
	session model.Session

	records chan model.AppendSessionAuditRecord
	done    chan struct{}

	mu             sync.Mutex
	line           []rune
	expectedEcho   []byte
	commandCounter int64
	currentCommand string
	disabled       bool
	escape         bool
	lastInputCR    bool
}

func NewAuditRecorder(service *Service, session model.Session) *AuditRecorder {
	recorder := &AuditRecorder{
		service: service,
		session: session,
		records: make(chan model.AppendSessionAuditRecord, auditQueueSize),
		done:    make(chan struct{}),
	}
	go recorder.run()
	return recorder
}

func (r *AuditRecorder) RecordOwnerInput(data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return
	}
	for _, ch := range string(data) {
		if r.escape {
			if ch >= '@' && ch <= '~' {
				r.escape = false
			}
			continue
		}
		switch ch {
		case '\x1b':
			r.escape = true
			r.lastInputCR = false
		case '\r':
			r.expectedEcho = append(r.expectedEcho, '\r', '\n')
			r.submitCommandLocked()
			r.lastInputCR = true
		case '\n':
			if !r.lastInputCR {
				r.expectedEcho = append(r.expectedEcho, '\r', '\n')
			}
			r.submitCommandLocked()
			r.lastInputCR = false
		case '\b', '\x7f':
			if len(r.line) > 0 {
				r.line = r.line[:len(r.line)-1]
			}
			r.expectedEcho = append(r.expectedEcho, '\b', ' ', '\b')
			r.lastInputCR = false
		case '\x15':
			r.line = nil
			r.lastInputCR = false
		case '\x17':
			r.removeWordLocked()
			r.lastInputCR = false
		case '\x03':
			commandID := r.currentCommand
			r.line = nil
			r.lastInputCR = false
			r.enqueueLocked(model.AppendSessionAuditRecord{
				RecordType:  model.AuditRecordControl,
				CommandID:   commandID,
				CommandText: "Ctrl-C",
				CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
				ByteCount:   len("Ctrl-C"),
			})
		default:
			if unicode.IsPrint(ch) {
				r.line = append(r.line, ch)
				r.expectedEcho = append(r.expectedEcho, string(ch)...)
			}
			r.lastInputCR = false
		}
	}
}

func (r *AuditRecorder) RecordOutput(data []byte) {
	if len(data) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return
	}
	data = r.stripExpectedEchoLocked(data)
	if len(data) == 0 {
		return
	}
	r.enqueueLocked(model.AppendSessionAuditRecord{
		RecordType:       model.AuditRecordOutput,
		CommandID:        r.currentCommand,
		OutputDataBase64: base64.StdEncoding.EncodeToString(data),
		CreatedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		ByteCount:        len(data),
	})
}

func (r *AuditRecorder) RecordSessionEvent(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return
	}
	r.enqueueLocked(model.AppendSessionAuditRecord{
		RecordType:  model.AuditRecordSessionEvent,
		CommandText: event,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		ByteCount:   len([]byte(event)),
	})
}

func (r *AuditRecorder) Close() {
	r.mu.Lock()
	r.disabled = true
	r.mu.Unlock()
	close(r.records)
	<-r.done
}

func (r *AuditRecorder) submitCommandLocked() {
	command := strings.TrimSpace(string(r.line))
	r.line = nil
	if command == "" {
		return
	}
	r.commandCounter++
	commandID := fmt.Sprintf("%s-%d", r.session.SessionID, r.commandCounter)
	r.currentCommand = commandID
	r.enqueueLocked(model.AppendSessionAuditRecord{
		RecordType:  model.AuditRecordCommand,
		CommandID:   commandID,
		CommandText: command,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		ByteCount:   len([]byte(command)),
	})
}

func (r *AuditRecorder) removeWordLocked() {
	for len(r.line) > 0 && unicode.IsSpace(r.line[len(r.line)-1]) {
		r.line = r.line[:len(r.line)-1]
	}
	for len(r.line) > 0 && !unicode.IsSpace(r.line[len(r.line)-1]) {
		r.line = r.line[:len(r.line)-1]
	}
}

func (r *AuditRecorder) stripExpectedEchoLocked(data []byte) []byte {
	for len(data) > 0 && len(r.expectedEcho) > 0 {
		if data[0] == r.expectedEcho[0] {
			data = data[1:]
			r.expectedEcho = r.expectedEcho[1:]
			continue
		}
		if r.expectedEcho[0] == '\r' && data[0] == '\n' {
			r.expectedEcho = r.expectedEcho[1:]
			continue
		}
		break
	}
	return data
}

func (r *AuditRecorder) enqueueLocked(record model.AppendSessionAuditRecord) {
	select {
	case r.records <- record:
	default:
		observability.EmitCount("proxy-dataplane", "SessionAuditDropped", map[string]string{"Component": "audit", "Reason": "queue_full"})
		slog.WarnContext(r.service.rootCtx, "SessionAuditDropped",
			"sessionId", r.session.SessionID,
			"userId", r.session.UserID,
			"hostId", r.session.HostID,
			"crId", r.session.CRID,
			"podId", r.service.pod.PodID,
			"instanceId", r.service.pod.InstanceID,
			"reason", "queue_full",
		)
	}
}

func (r *AuditRecorder) run() {
	defer close(r.done)
	ticker := time.NewTicker(auditFlushInterval)
	defer ticker.Stop()
	batch := make([]model.AppendSessionAuditRecord, 0, auditBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		r.flush(batch)
		batch = make([]model.AppendSessionAuditRecord, 0, auditBatchSize)
	}
	for {
		select {
		case record, ok := <-r.records:
			if !ok {
				flush()
				return
			}
			batch = append(batch, record)
			if len(batch) >= auditBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (r *AuditRecorder) flush(records []model.AppendSessionAuditRecord) {
	ctx, cancel := context.WithTimeout(r.service.rootCtx, auditAppendTimeout)
	defer cancel()
	if r.service.audit == nil {
		observability.EmitCount("proxy-dataplane", "SessionAuditAppendFailures", map[string]string{"Component": "audit", "Reason": "unconfigured"})
		slog.WarnContext(ctx, "SessionAuditAppendFailed",
			"sessionId", r.session.SessionID,
			"userId", r.session.UserID,
			"hostId", r.session.HostID,
			"crId", r.session.CRID,
			"podId", r.service.pod.PodID,
			"instanceId", r.service.pod.InstanceID,
			"errorKind", "unconfigured",
		)
		return
	}
	appended, err := r.service.audit.AppendSessionAuditRecords(ctx, r.session.SessionID, model.AppendSessionAuditRecordsRequest{
		OwnerPodID:      r.service.pod.PodID,
		OwnerInstanceID: r.service.pod.InstanceID,
		Records:         append([]model.AppendSessionAuditRecord(nil), records...),
	})
	if err != nil {
		observability.EmitCount("proxy-dataplane", "SessionAuditAppendFailures", map[string]string{"Component": "audit", "Reason": observability.ErrorKind(err)})
		slog.WarnContext(ctx, "SessionAuditAppendFailed",
			"sessionId", r.session.SessionID,
			"userId", r.session.UserID,
			"hostId", r.session.HostID,
			"crId", r.session.CRID,
			"podId", r.service.pod.PodID,
			"instanceId", r.service.pod.InstanceID,
			"errorKind", observability.ErrorKind(err),
			"error", err,
		)
		return
	}
	for _, record := range appended {
		if record.Truncated {
			r.mu.Lock()
			r.disabled = true
			r.mu.Unlock()
			observability.EmitCount("proxy-dataplane", "SessionAuditTruncated", map[string]string{"Component": "audit"})
			slog.WarnContext(ctx, "SessionAuditTruncated",
				"sessionId", r.session.SessionID,
				"userId", r.session.UserID,
				"hostId", r.session.HostID,
				"crId", r.session.CRID,
				"podId", r.service.pod.PodID,
				"instanceId", r.service.pod.InstanceID,
			)
			return
		}
	}
}
