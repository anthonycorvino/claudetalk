package runner

import (
	"context"
	"fmt"
	"sync"
)

// sessionKey uniquely identifies a session by room + sender.
type sessionKey struct {
	Room   string
	Sender string
}

// activeSession tracks a running Claude session.
type activeSession struct {
	cancel context.CancelFunc
}

// SessionManager prevents double-spawn per user per room.
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
// Returns an error if a session is already active for this user/room.
func (sm *SessionManager) Start(room, sender string) (context.Context, context.CancelFunc, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	key := sessionKey{Room: room, Sender: sender}
	if _, ok := sm.sessions[key]; ok {
		return nil, nil, fmt.Errorf("Claude session already active for %s in room %s", sender, room)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sm.sessions[key] = &activeSession{cancel: cancel}
	return ctx, cancel, nil
}

// End cleans up a session.
func (sm *SessionManager) End(room, sender string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	key := sessionKey{Room: room, Sender: sender}
	delete(sm.sessions, key)
}

// Stop cancels an active session.
func (sm *SessionManager) Stop(room, sender string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	key := sessionKey{Room: room, Sender: sender}
	s, ok := sm.sessions[key]
	if !ok {
		return fmt.Errorf("no active Claude session for %s in room %s", sender, room)
	}
	s.cancel()
	delete(sm.sessions, key)
	return nil
}
