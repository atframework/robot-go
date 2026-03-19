package conn

import (
	conn "github.com/atframework/robot-go/conn"
	"github.com/gorilla/websocket"
)

type WebSocketConnection struct {
	conn *websocket.Conn
}

func NewWebSocketConnection(c *websocket.Conn) *WebSocketConnection {
	return &WebSocketConnection{conn: c}
}

// DialWebSocket is a ConnectFunc that creates a WebSocket connection.
func DialWebSocket(url string) (conn.Connection, error) {
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	return NewWebSocketConnection(c), nil
}

func (w *WebSocketConnection) ReadMessage() ([]byte, error) {
	_, data, err := w.conn.ReadMessage()
	return data, err
}

func (w *WebSocketConnection) WriteMessage(data []byte) error {
	return w.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (w *WebSocketConnection) Close() error {
	return w.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

func (w *WebSocketConnection) IsValid() bool {
	return w.conn != nil
}

func (w *WebSocketConnection) IsUnexpectedCloseError(err error) bool {
	return websocket.IsUnexpectedCloseError(err,
		websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure)
}
