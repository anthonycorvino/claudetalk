package runner

import (
	"context"
	"fmt"
	"sync"
)

// sessionKey uniquely identifies a session by room + sender + conv_id.
// When ConvID is set, multiple concurrent sessions are allowed for the same
// user (one per conversation thread). Empty ConvID = one-at-a-time.
type sessionKey struct {
	Room   string
	Sender string
	ConvID string
}

// activeSession tracks a running Claude session.
type activeSession struct {
	cancel context.CancelFunc
}

// SessionManager tracks active Claude spawns, allowing multiple concurrent
// sessions per user when they are in different conversation threads.
type SessionManager struct {
	mu       sync.Mutex
	sessions map[sessionKey]*activeSession
}

// NewSessionManager creates a new session manager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[sessionKey]*activeSession),
	}
}

// Start creates a new cancellable context for a session.
// Returns an error if a session with the same (room, sender, convID) is already active.
func (sm *SessionManager) Start(room, sender, convID string) (context.Context, context.CancelFunc, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	key := sessionKey{Room: room, Sender: sender, ConvID: convID}
	if _, ok := sm.sessions[key]; ok {
		return nil, nil, fmt.Errorf("Claude session already active for %s in room %s (conv: %s)", sender, room, convID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sm.sessions[key] = &activeSession{cancel: cancel}
	return ctx, cancel, nil
}

// End cleans up a specific session.
func (sm *SessionManager) End(room, sender, convID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	key := sessionKey{Room: room, Sender: sender, ConvID: convID}
	delete(sm.sessions, key)
}

// Stop cancels all active sessions for a user in a room (across all conv threads).
func (sm *SessionManager) Stop(room, sender string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	found := false
	for key, s := range sm.sessions {
		if key.Room == room && key.Sender == sender {
			s.cancel()
			delete(sm.sessions, key)
			found = true
		}
	}
	if !found {
		return fmt.Errorf("no active Claude session for %s in room %s", sender, room)
	}
	return nil
}
