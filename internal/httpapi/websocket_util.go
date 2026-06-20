package httpapi

import (
	"bufio"
	"crypto/sha1" // #nosec G505 -- RFC 6455 requires SHA-1 for Sec-WebSocket-Accept, not for security.
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
const maxWebsocketPayload = 1 << 20

type wsConn struct {
	conn net.Conn
	rw   *bufio.ReadWriter
	mu   sync.Mutex
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
	hash := sha1.Sum([]byte(key + websocketGUID)) // #nosec G401 -- RFC 6455 requires SHA-1 for this handshake value.
	accept := base64.StdEncoding.EncodeToString(hash[:])
	fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\n")
	fmt.Fprintf(rw, "Upgrade: websocket\r\n")
	fmt.Fprintf(rw, "Connection: Upgrade\r\n")
	fmt.Fprintf(rw, "Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &wsConn{conn: conn, rw: rw}, nil
}

func (c *wsConn) Close() error {
	return c.conn.Close()
}

func (c *wsConn) ReadMessage() ([]byte, error) {
	return c.readMessage(false)
}

func (c *wsConn) ReadBinary() ([]byte, error) {
	return c.readMessage(true)
}

func (c *wsConn) readMessage(binaryOnly bool) ([]byte, error) {
	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(c.rw, header); err != nil {
			return nil, err
		}
		if header[0]&0x80 == 0 {
			return nil, errors.New("fragmented websocket messages are unsupported")
		}
		if header[0]&0x70 != 0 {
			return nil, errors.New("websocket extensions are unsupported")
		}
		opcode := header[0] & 0x0f
		masked := header[1]&0x80 != 0
		if !masked {
			return nil, errors.New("client websocket frames must be masked")
		}
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
		if length > maxWebsocketPayload {
			return nil, fmt.Errorf("websocket payload exceeds %d bytes", maxWebsocketPayload)
		}
		if opcode >= 0x8 && length > 125 {
			return nil, errors.New("websocket control payload exceeds 125 bytes")
		}
		var mask [4]byte
		if _, err := io.ReadFull(c.rw, mask[:]); err != nil {
			return nil, err
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.rw, payload); err != nil {
			return nil, err
		}
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
		switch opcode {
		case 0x1:
			if binaryOnly {
				return nil, errors.New("terminal protocol requires binary websocket frames")
			}
			return payload, nil
		case 0x2:
			return payload, nil
		case 0x8:
			return nil, io.EOF
		case 0x9:
			if err := c.writeFrame(0xA, payload); err != nil {
				return nil, err
			}
		case 0xA:
			continue
		default:
			return nil, fmt.Errorf("unsupported websocket opcode 0x%x", opcode)
		}
	}
}

func (c *wsConn) WriteText(payload []byte) error {
	return c.writeFrame(0x1, payload)
}

func (c *wsConn) WriteBinary(payload []byte) error {
	return c.writeFrame(0x2, payload)
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	header := []byte{0x80 | opcode}
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(length))
	case length <= 65535:
		var encoded [2]byte
		binary.BigEndian.PutUint16(encoded[:], uint16(length)) // #nosec G115 -- length is bounded to 65535 in this branch.
		header = append(header, 126)
		header = append(header, encoded[:]...)
	default:
		header = append(header, 127)
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], uint64(length))
		header = append(header, encoded[:]...)
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}
