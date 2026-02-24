package server

import "sync"

// Hub manages all active rooms.
type Hub struct {
	mu         sync.RWMutex
	rooms      map[string]*Room
	maxHistory int
}

// NewHub creates a new Hub with the given max history per room.
func NewHub(maxHistory int) *Hub {
	if maxHistory <= 0 {
		maxHistory = 1000
	}
	return &Hub{
		rooms:      make(map[string]*Room),
		maxHistory: maxHistory,
	}
}

// GetOrCreateRoom returns the room with the given name, creating it if needed.
func (h *Hub) GetOrCreateRoom(name string) *Room {
	h.mu.RLock()
	r, ok := h.rooms[name]
	h.mu.RUnlock()
	if ok {
		return r
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	// Double-check after acquiring write lock.
	if r, ok = h.rooms[name]; ok {
		return r
	}
	r = NewRoom(name, h.maxHistory)
	h.rooms[name] = r
	return r
}

// GetRoom returns a room or nil if it doesn't exist.
func (h *Hub) GetRoom(name string) *Room {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rooms[name]
}

// ListRooms returns info about all active rooms.
func (h *Hub) ListRooms() []RoomSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]RoomSnapshot, 0, len(h.rooms))
	for _, r := range h.rooms {
		out = append(out, r.Snapshot())
	}
	return out
}

// RoomCount returns the number of active rooms.
func (h *Hub) RoomCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms)
}
