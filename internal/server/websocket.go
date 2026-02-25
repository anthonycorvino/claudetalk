package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/corvino/claudetalk/internal/protocol"
	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	maxMsgSize = 64 * 1024
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Client represents a WebSocket connection in a room.
type Client struct {
	room    *Room
	conn    *websocket.Conn
	send    chan protocol.Envelope
	rawSend chan []byte // raw JSON frames; all writes go through writePump
	sender  string
	mode    string // "legacy" or "daemon"
	role    string // "daemon", "user", etc.
}

// Send queues an envelope for delivery to this client.
func (c *Client) Send(env protocol.Envelope) {
	select {
	case c.send <- env:
	default:
		// Client too slow; drop message.
	}
}

// SendEvent sends a ServerEvent to a daemon client. For legacy clients, this is a no-op.
func (c *Client) SendEvent(event protocol.ServerEvent) {
	if c.mode != "daemon" {
		return
	}
	c.sendRaw(event)
}

// sendRaw queues an arbitrary JSON value for delivery via writePump.
// Safe to call from any goroutine.
func (c *Client) sendRaw(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.rawSend <- data:
	default:
		// Client too slow; drop.
	}
}

// readPump reads messages from the WebSocket and posts them to the room.
func (c *Client) readPump() {
	defer func() {
		c.room.UnregisterClient(c)
		c.room.UntrackParticipant(c.sender)
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMsgSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		var req protocol.SendRequest
		err := c.conn.ReadJSON(&req)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("ws read error room=%s sender=%s: %v", c.room.name, c.sender, err)
			}
			return
		}
		sender := req.Sender
		if sender == "" {
			sender = c.sender
		}
		msgType := req.Type
		if msgType == "" {
			msgType = protocol.TypeText
		}
		c.room.AddMessage(sender, msgType, req.Payload, req.Metadata)
	}
}

// writePump sends messages from the send channel to the WebSocket.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case env, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if c.mode == "daemon" {
				// Daemon clients receive ServerEvent wrappers.
				event := protocol.ServerEvent{
					Event:   "message",
					Message: &env,
				}
				if err := c.conn.WriteJSON(event); err != nil {
					return
				}
			} else {
				// Legacy clients receive bare envelopes.
				if err := c.conn.WriteJSON(env); err != nil {
					return
				}
			}

			// After sending the message, trigger spawn events for all relevant daemon clients.
			// For group conv_id threads, this notifies every thread participant except the sender.
			if targets, allParticipants := c.room.GetConvSpawnTargets(env); len(targets) > 0 {
				ctx := c.room.LatestMessages(30)
				daemonClients := c.room.GetDaemonClients(targets)
				log.Printf("spawn dispatch: targets=%v daemonClients=%d sender=%s", targets, len(daemonClients), env.Sender)
				for name, dc := range daemonClients {
					if dc == c {
						log.Printf("spawn dispatch: skipping %s (self)", name)
						continue
					}
					log.Printf("spawn dispatch: sending spawn event to %s", name)
					spawnEvent := protocol.ServerEvent{
						Event: "spawn",
						Spawn: &protocol.SpawnReq{
							Reason:       "directed_message",
							Trigger:      &env,
							Context:      ctx,
							Participants: allParticipants,
						},
					}
					dc.sendRaw(spawnEvent)
				}
			}

			// Fire server-side hooks for non-daemon participants (e.g. host-mode spawned Claudes).
			if hookTargets, hookParticipants := c.room.GetHookSpawnTargets(env); len(hookTargets) > 0 {
				hookCtx := c.room.LatestMessages(30)
				for name, hook := range hookTargets {
					name, hook := name, hook // capture loop vars
					log.Printf("spawn dispatch: hook for %s", name)
					go hook(&protocol.SpawnReq{
						Reason:       "directed_message",
						Trigger:      &env,
						Context:      hookCtx,
						Participants: hookParticipants,
					})
				}
			}
		case data, ok := <-c.rawSend:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ServeWS upgrades an HTTP connection to WebSocket and registers the client.
func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request, roomName, sender string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	mode := r.URL.Query().Get("mode")
	role := r.URL.Query().Get("role")
	if mode == "" {
		mode = "legacy"
	}
	if role == "" {
		role = "user"
	}

	room := hub.GetOrCreateRoom(roomName)
	client := &Client{
		room:    room,
		conn:    conn,
		send:    make(chan protocol.Envelope, 256),
		rawSend: make(chan []byte, 64),
		sender:  sender,
		mode:    mode,
		role:    role,
	}
	room.RegisterClient(client)

	// Track all clients as participants.
	room.TrackParticipant(sender, role, client)

	// Announce join.
	room.AddMessage("system", protocol.TypeSystem, protocol.Payload{
		Text: sender + " joined the room",
	}, nil)

	go client.writePump()
	go client.readPump()
}
