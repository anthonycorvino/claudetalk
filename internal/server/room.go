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

	mu               sync.RWMutex
	messages         []protocol.Envelope
	seq              int64
	clients          map[*Client]struct{}
	participants     map[string]*participantState
	convParticipants map[string]map[string]struct{}            // conv_id → participant names
	spawnHooks       map[string]func(*protocol.SpawnReq) // name → hook for non-daemon participants
}

// NewRoom creates a room with the given name and history limit.
func NewRoom(name string, maxHistory int) *Room {
	return &Room{
		name:             name,
		maxHistory:       maxHistory,
		messages:         make([]protocol.Envelope, 0, 64),
		clients:          make(map[*Client]struct{}),
		participants:     make(map[string]*participantState),
		convParticipants: make(map[string]map[string]struct{}),
		spawnHooks:       make(map[string]func(*protocol.SpawnReq)),
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
	// Track conv_id participants for group thread broadcasting.
	if convID := env.Metadata["conv_id"]; convID != "" {
		if _, ok := r.convParticipants[convID]; !ok {
			r.convParticipants[convID] = make(map[string]struct{})
		}
		r.convParticipants[convID][env.Sender] = struct{}{}
		if to := env.Metadata["to"]; to != "" {
			r.convParticipants[convID][to] = struct{}{}
		}
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

// RegisterSpawnHook registers a function to call when a directed spawn event should
// be delivered to a participant who has no daemon WebSocket connection (e.g., host-mode).
func (r *Room) RegisterSpawnHook(name string, hook func(*protocol.SpawnReq)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spawnHooks[name] = hook
}

// UnregisterSpawnHook removes the spawn hook for a participant.
func (r *Room) UnregisterSpawnHook(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.spawnHooks, name)
}

// GetHookSpawnTargets returns hooks for participants who should receive spawn events
// but don't have a daemon WS client. Complements GetConvSpawnTargets for non-daemon participants.
func (r *Room) GetHookSpawnTargets(env protocol.Envelope) (hooks map[string]func(*protocol.SpawnReq), allParticipants []string) {
	if env.Metadata["to"] == "" || env.Metadata["expecting_reply"] != "true" {
		return nil, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	hooks = make(map[string]func(*protocol.SpawnReq))
	convID := env.Metadata["conv_id"]

	tryAdd := func(name string) {
		if name == env.Sender {
			return
		}
		hook, hasHook := r.spawnHooks[name]
		if !hasHook {
			return
		}
		// Skip if they already have a daemon client (GetConvSpawnTargets covers them).
		if ps, ok := r.participants[name]; ok && ps.Client != nil {
			return
		}
		hooks[name] = hook
	}

	tryAdd(env.Metadata["to"])

	if convID != "" {
		for name := range r.convParticipants[convID] {
			tryAdd(name)
		}
		for name := range r.convParticipants[convID] {
			allParticipants = append(allParticipants, name)
		}
	}

	return hooks, allParticipants
}

// GetConvSpawnTargets returns the set of daemon participants who should receive
// spawn events when this message arrives, plus all conv thread members for prompt context.
// For group threads (shared conv_id), ALL thread members except the sender are notified.
func (r *Room) GetConvSpawnTargets(env protocol.Envelope) (targets []string, allParticipants []string) {
	if env.Metadata["to"] == "" || env.Metadata["expecting_reply"] != "true" {
		return nil, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	convID := env.Metadata["conv_id"]
	targetSet := make(map[string]struct{})

	// Always include the primary `to` recipient if they're a connected daemon.
	if ps, ok := r.participants[env.Metadata["to"]]; ok && ps.Connected && ps.Role == "daemon" {
		targetSet[env.Metadata["to"]] = struct{}{}
	}

	// For conv_id threads, also notify every other thread participant.
	if convID != "" {
		for name := range r.convParticipants[convID] {
			if name == env.Sender {
				continue
			}
			ps, ok := r.participants[name]
			if !ok || !ps.Connected || ps.Role != "daemon" {
				continue
			}
			targetSet[name] = struct{}{}
		}
		// Build full participant list for the prompt.
		for name := range r.convParticipants[convID] {
			allParticipants = append(allParticipants, name)
		}
	}

	for name := range targetSet {
		targets = append(targets, name)
	}
	return targets, allParticipants
}

// GetDaemonClients returns the daemon *Client for each of the given participant names.
func (r *Room) GetDaemonClients(names []string) map[string]*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]*Client, len(names))
	for _, name := range names {
		if ps, ok := r.participants[name]; ok && ps.Client != nil {
			result[name] = ps.Client
		}
	}
	return result
}
