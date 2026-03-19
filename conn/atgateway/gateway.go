package conn

import (
	"errors"
	"fmt"
	"sync"

	robotconn "github.com/atframework/robot-go/conn"
	v2 "github.com/atframework/robot-go/conn/atgateway/atframework/gateway/v2"
)

const clientCloseReason int32 = 4

// KickoffError is returned when the server kicks the connection.
type KickoffError struct {
	Reason    int32
	SubReason int32
	Message   string
}

func (e *KickoffError) Error() string {
	return fmt.Sprintf("gateway kickoff: reason=%d sub_reason=%d msg=%s",
		e.Reason, e.SubReason, e.Message)
}

// GatewayConnection wraps a transport-level Connection (TCP) with the
// atgateway v2 protocol, implementing the robot-go conn.Connection interface.
type GatewayConnection struct {
	inner   robotconn.Connection
	session *GatewaySession

	appMsgBuf [][]byte // buffered decoded application messages

	mu     sync.Mutex
	closed bool
}

// NewGatewayConnection creates a GatewayConnection around an existing transport connection.
// Call Handshake() before reading/writing application data.
func NewGatewayConnection(inner robotconn.Connection, config *GatewaySessionConfig) *GatewayConnection {
	return &GatewayConnection{
		inner:   inner,
		session: NewGatewaySession(config),
	}
}

// Handshake performs the ECDH key exchange with the server:
//  1. Send kKeyExchangeReq
//  2. Read kKeyExchangeRsp
//  3. Derive keys, decrypt session token
//  4. Send kConfirm
func (g *GatewayConnection) Handshake() error {
	// 1. Build and send the key exchange request.
	reqFrame, err := g.session.BuildKeyExchangeReq()
	if err != nil {
		return fmt.Errorf("build key exchange request: %w", err)
	}
	if err := g.inner.WriteMessage(reqFrame); err != nil {
		return fmt.Errorf("send key exchange request: %w", err)
	}

	// 2. Read frames until we get the handshake response.
	rspFrame, err := g.readNextFrame()
	if err != nil {
		return fmt.Errorf("read key exchange response: %w", err)
	}

	// 3. Process the response and build the confirm message.
	confirmFrame, err := g.session.HandleKeyExchangeRsp(rspFrame)
	if err != nil {
		return fmt.Errorf("handle key exchange response: %w", err)
	}

	// 4. Send the confirm message.
	if err := g.inner.WriteMessage(confirmFrame); err != nil {
		return fmt.Errorf("send confirm: %w", err)
	}

	return nil
}

// readNextFrame reads one complete protocol frame payload from the inner connection.
// The TCP transport layer handles framing, so each ReadMessage returns one frame.
func (g *GatewayConnection) readNextFrame() ([]byte, error) {
	return g.inner.ReadMessage()
}

// ========================= conn.Connection Interface =========================

// ReadMessage reads the next application-level message from the gateway.
// Protocol-level messages (ping, pong, kickoff) are handled transparently.
func (g *GatewayConnection) ReadMessage() ([]byte, error) {
	for {
		// Return buffered app messages first.
		if len(g.appMsgBuf) > 0 {
			data := g.appMsgBuf[0]
			g.appMsgBuf = g.appMsgBuf[1:]
			return data, nil
		}

		// Read one frame from the TCP transport.
		frame, err := g.inner.ReadMessage()
		if err != nil {
			return nil, err
		}

		msg, err := g.session.DecodeMessage(frame)
		if err != nil {
			return nil, fmt.Errorf("decode message: %w", err)
		}

		switch msg.Type {
		case v2.MsgTypePost:
			if msg.Post != nil && len(msg.Post.Data) > 0 {
				g.appMsgBuf = append(g.appMsgBuf, msg.Post.Data)
			}

		case v2.MsgTypePing:
			// Auto-reply with pong.
			if msg.Ping != nil {
				pongFrame := g.session.BuildPong(msg.Ping.Timepoint)
				_ = g.inner.WriteMessage(pongFrame)
			}

		case v2.MsgTypePong:
			// Silently consume pong messages.

		case v2.MsgTypeKickoff:
			ko := &KickoffError{}
			if msg.Kickoff != nil {
				ko.Reason = msg.Kickoff.Reason
				ko.SubReason = msg.Kickoff.SubReason
				ko.Message = msg.Kickoff.Message
			}
			return nil, ko

		case v2.MsgTypeHandshake:
			// Could be a key refresh (reconnect response); ignore for now.

		default:
			// Unknown message types are silently ignored.
		}
	}
}

// WriteMessage encrypts and sends application data through the gateway.
func (g *GatewayConnection) WriteMessage(data []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return errors.New("connection closed")
	}

	frame, err := g.session.EncodePost(data)
	if err != nil {
		return fmt.Errorf("encode post: %w", err)
	}
	return g.inner.WriteMessage(frame)
}

// Close closes the gateway connection and the underlying transport.
func (g *GatewayConnection) Close() error {
	g.mu.Lock()
	alreadyClosed := g.closed
	g.closed = true
	g.mu.Unlock()
	if alreadyClosed {
		return nil
	}

	if g.session.IsHandshakeDone() && g.inner.IsValid() {
		if frame, err := g.session.BuildKickoff(clientCloseReason, 0, "client closed"); err == nil {
			_ = g.inner.WriteMessage(frame)
		}
	}

	return g.inner.Close()
}

// IsValid returns true if the connection is established and the handshake is complete.
func (g *GatewayConnection) IsValid() bool {
	return g.inner.IsValid() && g.session.IsHandshakeDone() && !g.closed
}

// IsUnexpectedCloseError returns true if the error represents an unexpected close.
func (g *GatewayConnection) IsUnexpectedCloseError(err error) bool {
	var ko *KickoffError
	if errors.As(err, &ko) {
		return true
	}
	return g.inner.IsUnexpectedCloseError(err)
}

// ========================= Dial Functions =========================

// DialGateway establishes a TCP connection, performs the atgateway v2
// handshake, and returns a Connection that transparently encrypts/decrypts data.
func DialGateway(address string, config *GatewaySessionConfig) (robotconn.Connection, error) {
	inner, err := DialTCP(address)
	if err != nil {
		return nil, fmt.Errorf("dial tcp: %w", err)
	}

	gw := NewGatewayConnection(inner, config)
	if err := gw.Handshake(); err != nil {
		inner.Close()
		return nil, fmt.Errorf("gateway handshake: %w", err)
	}

	return gw, nil
}
