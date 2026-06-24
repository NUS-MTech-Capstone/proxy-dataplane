package httpapi

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadMessageUnmasksClientPayload(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	conn := &wsConn{
		conn: server,
		rw:   bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)),
	}
	go func() {
		mask := [4]byte{1, 2, 3, 4}
		payload := []byte("whoami\r")
		frame := []byte{0x81, 0x80 | byte(len(payload))}
		frame = append(frame, mask[:]...)
		for i, b := range payload {
			frame = append(frame, b^mask[i%len(mask)])
		}
		_, _ = client.Write(frame)
	}()

	message, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(message), "whoami\r"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestReadMessageRejectsUnmaskedClientPayload(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	conn := &wsConn{
		conn: server,
		rw:   bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)),
	}
	go func() {
		_, _ = client.Write([]byte{0x81, 0x01, 'x'})
	}()

	if _, err := conn.ReadMessage(); err == nil {
		t.Fatal("ReadMessage() error = nil, want unmasked frame rejection")
	}
}

func TestReadBinaryHandlesPingThenClose(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	conn := &wsConn{
		conn: server,
		rw:   bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)),
	}
	go func() {
		writeMaskedFrame(t, client, 0x9, []byte("ping"))
		pong := make([]byte, 6)
		_, _ = io.ReadFull(client, pong)
		writeMaskedFrame(t, client, 0x8, nil)
	}()

	if _, err := conn.ReadBinary(); err == nil {
		t.Fatal("ReadBinary() error = nil, want close after ping")
	}
}

func TestWriteTextAndBinaryFrames(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	conn := &wsConn{
		conn: server,
		rw:   bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)),
	}
	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 7)
		_, _ = io.ReadFull(client, buf)
		done <- buf
	}()
	if err := conn.WriteText([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	frame := <-done
	if !bytes.Equal(frame, []byte{0x81, 0x05, 'h', 'e', 'l', 'l', 'o'}) {
		t.Fatalf("text frame = %v", frame)
	}

	go func() {
		buf := make([]byte, 130)
		_, _ = io.ReadFull(client, buf)
		done <- buf
	}()
	if err := conn.WriteBinary(bytes.Repeat([]byte{'x'}, 126)); err != nil {
		t.Fatal(err)
	}
	frame = <-done
	if len(frame) != 130 || frame[0] != 0x82 || frame[1] != 126 || binary.BigEndian.Uint16(frame[2:4]) != 126 {
		t.Fatalf("binary frame header = %v len=%d", frame[:4], len(frame))
	}
}

func TestReadMessageRejectsOversizedPayload(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	conn := &wsConn{
		conn: server,
		rw:   bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)),
	}
	go func() {
		_, _ = client.Write([]byte{0x82, 0x80 | 127, 0, 0, 0, 0, 0, 16, 0, 1, 1, 2, 3, 4})
	}()
	if _, err := conn.ReadBinary(); err == nil {
		t.Fatal("ReadBinary() error = nil, want oversized payload rejection")
	}
}

func writeMaskedFrame(t *testing.T, conn net.Conn, opcode byte, payload []byte) {
	t.Helper()
	mask := [4]byte{1, 2, 3, 4}
	frame := []byte{0x80 | opcode, 0x80 | byte(len(payload))}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%len(mask)])
	}
	if _, err := conn.Write(frame); err != nil {
		t.Error(err)
	}
}

func TestUpgradeRejectsMissingKeyAndUnsupportedHijacker(t *testing.T) {
	if _, err := upgrade(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sessions/s/ws", nil)); err == nil {
		t.Fatal("upgrade() accepted missing key")
	}
	req := httptest.NewRequest(http.MethodGet, "/sessions/s/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if _, err := upgrade(httptest.NewRecorder(), req); err == nil {
		t.Fatal("upgrade() accepted non-hijacker response writer")
	}
}

func TestUpgradeWritesHandshakeAndCloseClosesConn(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server))
	writer := &hijackWriter{conn: server, rw: rw, HeaderMap: make(http.Header)}
	req := httptest.NewRequest(http.MethodGet, "/sessions/s/ws", nil)
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 512)
		n, _ := client.Read(buf)
		done <- string(buf[:n])
	}()
	conn, err := upgrade(writer, req)
	if err != nil {
		t.Fatal(err)
	}
	response := <-done
	if !strings.Contains(response, "101 Switching Protocols") || !strings.Contains(response, "Sec-WebSocket-Accept") {
		t.Fatalf("handshake response = %q", response)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}

type hijackWriter struct {
	HeaderMap http.Header
	conn      net.Conn
	rw        *bufio.ReadWriter
}

func (w *hijackWriter) Header() http.Header { return w.HeaderMap }
func (w *hijackWriter) Write([]byte) (int, error) {
	return 0, nil
}
func (w *hijackWriter) WriteHeader(int) {}
func (w *hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, w.rw, nil
}
