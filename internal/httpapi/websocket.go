package httpapi

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type wsConn struct {
	conn net.Conn
	rw   *bufio.ReadWriter
}

func upgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("hijacking unsupported")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	hash := sha1.Sum([]byte(key + websocketGUID))
	accept := base64.StdEncoding.EncodeToString(hash[:])
	fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\n")
	fmt.Fprintf(rw, "Upgrade: websocket\r\n")
	fmt.Fprintf(rw, "Connection: Upgrade\r\n")
	fmt.Fprintf(rw, "Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	return &wsConn{conn: conn, rw: rw}, nil
}

func (c *wsConn) Close() error {
	return c.conn.Close()
}

func (c *wsConn) ReadMessage() ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.rw, header); err != nil {
		return nil, err
	}
	opcode := header[0] & 0x0f
	if opcode == 0x8 {
		return nil, io.EOF
	}
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var b [2]byte
		if _, err := io.ReadFull(c.rw, b[:]); err != nil {
			return nil, err
		}
		length = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err := io.ReadFull(c.rw, b[:]); err != nil {
			return nil, err
		}
		length = binary.BigEndian.Uint64(b[:])
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.rw, mask[:]); err != nil {
			return nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.rw, payload); err != nil {
		return nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return payload, nil
}

func (c *wsConn) WriteText(payload []byte) error {
	header := []byte{0x81}
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(length))
	case length <= 65535:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		header = append(header, 127, 0, 0, 0, 0, byte(length>>24), byte(length>>16), byte(length>>8), byte(length))
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}
