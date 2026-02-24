package server

import (
	"sync"
	"time"

	"github.com/corvino/claudetalk/internal/protocol"
	"github.com/google/uuid"
)

// RoomSnapshot holds a point-in-time view of a room for listing.
type RoomSnapshot struct {
	Name         string
	Clients      int
	MessageCount int
	LastSeq      int64
}

// participantState tracks a connected participant's daemon state.
type participantState struct {
	Name      string
	Role      string
	JoinedAt  time.Time
	Connected bool
	Client    *Client // The daemon client, if any
}

// Room holds messages and connected WebSocket clients.
type Room struct {
	name       string
	maxHistory int

	mu           sync.RWMutex
	messages     []protocol.Envelope
	seq          int64
	clients      map[*Client]struct{}
	participants map[string]*participantState
}

// NewRoom creates a room with the given name and history limit.
func NewRoom(name string, maxHistory int) *Room {
	return &Room{
		name:         name,
		maxHistory:   maxHistory,
		messages:     make([]protocol.Envelope, 0, 64),
		clients:      make(map[*Client]struct{}),
		participants: make(map[string]*participantState),
	}
}

// AddMessage stores a message, assigns server-side fields, broadcasts to WS clients, and returns the envelope.
func (r *Room) AddMessage(sender, msgType string, payload protocol.Payload, metadata map[string]string) protocol.Envelope {
	r.mu.Lock()
	r.seq++
	env := protocol.Envelope{
		ID:        uuid.New().String(),
		Room:      r.name,
		Sender:    sender,
		Timestamp: time.Now().UTC(),
		Type:      msgType,
		Payload:   payload,
		SeqNum:    r.seq,
		Metadata:  metadata,
	}
	r.messages = append(r.messages, env)
	// Trim if over max history.
	if len(r.messages) > r.maxHistory {
		excess := len(r.messages) - r.maxHistory
		r.messages = r.messages[excess:]
	}
	// Copy client set for broadcast outside lock.
	clients := make([]*Client, 0, len(r.clients))
	for c := range r.clients {
		clients = append(clients, c)
	}
	r.mu.Unlock()

	// Broadcast to WebSocket clients.
	for _, c := range clients {
		c.Send(env)
	}
	return env
}

// MessagesAfter returns messages with SeqNum > after, up to limit.
func (r *Room) MessagesAfter(after int64, limit int) []protocol.Envelope {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Find start index using the fact that seq numbers are monotonic.
	start := 0
	for i, m := range r.messages {
		if m.SeqNum > after {
			start = i
			break
		}
		if i == len(r.messages)-1 {
			return nil // All messages are <= after.
		}
	}

	if len(r.messages) == 0 {
		return nil
	}

	result := r.messages[start:]
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	out := make([]protocol.Envelope, len(result))
	copy(out, result)
	return out
}

// LatestMessages returns the last n messages.
func (r *Room) LatestMessages(n int) []protocol.Envelope {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if n <= 0 || len(r.messages) == 0 {
		return nil
	}
	start := len(r.messages) - n
	if start < 0 {
		start = 0
	}
	out := make([]protocol.Envelope, len(r.messages[start:]))
	copy(out, r.messages[start:])
	return out
}

// RegisterClient adds a WebSocket client to the room.
func (r *Room) RegisterClient(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[c] = struct{}{}
}

// UnregisterClient removes a WebSocket client from the room.
func (r *Room) UnregisterClient(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, c)
}

// Snapshot returns a point-in-time summary of this room.
func (r *Room) Snapshot() RoomSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return RoomSnapshot{
		Name:         r.name,
		Clients:      len(r.clients),
		MessageCount: len(r.messages),
		LastSeq:      r.seq,
	}
}

// TrackParticipant registers or updates a participant. If client is a daemon
// client, it stores the reference for spawn event delivery.
func (r *Room) TrackParticipant(name, role string, c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ps, ok := r.participants[name]; ok {
		ps.Connected = true
		ps.Role = role
		if role == "daemon" {
			ps.Client = c
		}
	} else {
		r.participants[name] = &participantState{
			Name:      name,
			Role:      role,
			JoinedAt:  time.Now().UTC(),
			Connected: true,
			Client:    c,
		}
	}
}

// UntrackParticipant marks a participant as disconnected.
func (r *Room) UntrackParticipant(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ps, ok := r.participants[name]; ok {
		ps.Connected = false
		ps.Client = nil
	}
}

// ListParticipants returns info about all known participants.
func (r *Room) ListParticipants() []protocol.ParticipantInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]protocol.ParticipantInfo, 0, len(r.participants))
	for _, ps := range r.participants {
		out = append(out, protocol.ParticipantInfo{
			Name:      ps.Name,
			Role:      ps.Role,
			JoinedAt:  ps.JoinedAt,
			Connected: ps.Connected,
		})
	}
	return out
}

// ShouldSpawn checks if a message should trigger a daemon spawn.
// Returns the target participant name, or "" if no spawn is needed.
func (r *Room) ShouldSpawn(env protocol.Envelope) string {
	// Only spawn for messages directed at a specific recipient.
	to := env.Metadata["to"]
	if to == "" {
		return ""
	}
	// Only spawn if the recipient expects a reply.
	if env.Metadata["expecting_reply"] != "true" {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ps, ok := r.participants[to]
	if !ok || !ps.Connected || ps.Role != "daemon" {
		return ""
	}
	return to
}

// GetDaemonClient returns the daemon client for a participant, if any.
func (r *Room) GetDaemonClient(name string) *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if ps, ok := r.participants[name]; ok && ps.Client != nil {
		return ps.Client
	}
	return nil
}
