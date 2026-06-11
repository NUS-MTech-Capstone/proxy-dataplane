package httpapi

import (
	"bufio"
	"net"
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
