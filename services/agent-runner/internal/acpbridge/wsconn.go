package acpbridge

import (
	"context"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// wsConn adapts a *websocket.Conn to the Conn interface Registry needs —
// the only production implementation; tests substitute a fake instead of a
// real WebSocket.
type wsConn struct {
	conn *websocket.Conn
}

func (c wsConn) SendJSON(ctx context.Context, v any) error {
	return wsjson.Write(ctx, c.conn, v)
}

func (c wsConn) Close(code int, reason string) error {
	return c.conn.Close(websocket.StatusCode(code), reason)
}
