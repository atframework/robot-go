package conn

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	robotconn "github.com/atframework/robot-go/conn"
)

// TCPConnection implements robotconn.Connection over a raw TCP stream.
// Reads decode the atgateway v2 wire format into payloads, while writes expect
// callers to provide a fully encoded frame.
type TCPConnection struct {
	conn    net.Conn
	readBuf []byte // 预分配的读缓冲区，避免每次 ReadMessage 分配

	mu     sync.Mutex
	closed bool
}

// DialTCP connects to the given TCP address and returns a Connection
// that frames messages using the atgateway v2 wire format.
func DialTCP(address string) (robotconn.Connection, error) {
	c, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}
	return &TCPConnection{conn: c, readBuf: make([]byte, 0, 4096)}, nil
}

// ReadMessage reads one complete framed message from the TCP stream.
func (t *TCPConnection) ReadMessage() ([]byte, error) {
	// Read the 8-byte frame header.
	var header [FrameHeaderSize]byte
	if _, err := io.ReadFull(t.conn, header[:]); err != nil {
		return nil, err
	}

	expectedHash := binary.LittleEndian.Uint32(header[0:4])
	msgLen := binary.LittleEndian.Uint32(header[4:8])

	// Read the payload, reusing pre-allocated buffer.
	if cap(t.readBuf) >= int(msgLen) {
		t.readBuf = t.readBuf[:msgLen]
	} else {
		t.readBuf = make([]byte, msgLen)
	}
	if _, err := io.ReadFull(t.conn, t.readBuf); err != nil {
		return nil, err
	}

	actualHash := MurmurHash3X86_32(t.readBuf, 0)
	if actualHash != expectedHash {
		return nil, fmt.Errorf("frame hash mismatch: expected 0x%08x, got 0x%08x", expectedHash, actualHash)
	}

	// 注意：返回的切片在下次 ReadMessage 调用后失效。
	// 调用方（GatewayConnection.readNextFrame）在 DecodeMessage 中同步消费后即可。
	return t.readBuf, nil
}

// WriteMessage writes a fully encoded wire-format frame to the TCP stream.
func (t *TCPConnection) WriteMessage(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return errors.New("connection closed")
	}

	_, err := t.conn.Write(data)
	return err
}

// Close closes the TCP connection.
func (t *TCPConnection) Close() error {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	return t.conn.Close()
}

// IsValid returns true if the connection has not been closed.
func (t *TCPConnection) IsValid() bool {
	return !t.closed
}

// IsUnexpectedCloseError returns true if the error is an unexpected close.
func (t *TCPConnection) IsUnexpectedCloseError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var ne *net.OpError
	return errors.As(err, &ne)
}
