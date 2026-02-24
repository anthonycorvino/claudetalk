package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/corvino/claudetalk/internal/protocol"
	"github.com/gorilla/websocket"
)

// WSConn is a persistent WebSocket connection with automatic reconnect.
type WSConn struct {
	serverURL string
	room      string
	name      string

	events chan protocol.ServerEvent
	done   chan struct{}
	once   sync.Once
}

// NewWSConn creates a new persistent WebSocket connection.
func NewWSConn(serverURL, room, name string) *WSConn {
	return &WSConn{
		serverURL: serverURL,
		room:      room,
		name:      name,
		events:    make(chan protocol.ServerEvent, 64),
		done:      make(chan struct{}),
	}
}

// Events returns the channel of server events.
func (ws *WSConn) Events() <-chan protocol.ServerEvent {
	return ws.events
}

// Close stops the connection loop.
func (ws *WSConn) Close() {
	ws.once.Do(func() {
		close(ws.done)
	})
}

// Run connects to the WebSocket and reconnects on failure with exponential backoff.
// This blocks until Close() is called.
func (ws *WSConn) Run() {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ws.done:
			return
		default:
		}

		err := ws.connect()
		if err != nil {
			log.Printf("websocket connection error: %v", err)
		}

		// Check if we should stop.
		select {
		case <-ws.done:
			return
		default:
		}

		log.Printf("reconnecting in %s...", backoff)
		select {
		case <-time.After(backoff):
		case <-ws.done:
			return
		}

		// Exponential backoff.
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (ws *WSConn) connect() error {
	wsURL, err := ws.buildWSURL()
	if err != nil {
		return err
	}

	log.Printf("connecting to %s", wsURL)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	log.Printf("connected to room %q as %q (daemon mode)", ws.room, ws.name)

	// Reset backoff on successful connect (handled by caller).
	for {
		select {
		case <-ws.done:
			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return nil
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		var event protocol.ServerEvent
		if err := json.Unmarshal(data, &event); err != nil {
			log.Printf("failed to unmarshal server event: %v", err)
			continue
		}

		select {
		case ws.events <- event:
		default:
			log.Printf("event channel full, dropping event: %s", event.Event)
		}
	}
}

func (ws *WSConn) buildWSURL() (string, error) {
	u, err := url.Parse(ws.serverURL)
	if err != nil {
		return "", fmt.Errorf("parse server URL: %w", err)
	}

	// Convert http(s) to ws(s).
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}

	u.Path = fmt.Sprintf("/ws/%s", ws.room)
	q := u.Query()
	q.Set("sender", ws.name)
	q.Set("mode", "daemon")
	q.Set("role", "daemon")
	u.RawQuery = q.Encode()

	return u.String(), nil
}
