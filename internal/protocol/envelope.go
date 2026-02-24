package protocol

import "time"

// Envelope wraps a message with metadata assigned by the server.
type Envelope struct {
	ID        string            `json:"id"`
	Room      string            `json:"room"`
	Sender    string            `json:"sender"`
	Timestamp time.Time         `json:"timestamp"`
	Type      string            `json:"type"`
	Payload   Payload           `json:"payload"`
	SeqNum    int64             `json:"seq"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// SendRequest is the JSON body for POST /api/rooms/{room}/messages.
type SendRequest struct {
	Sender   string            `json:"sender"`
	Type     string            `json:"type"`
	Payload  Payload           `json:"payload"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MessageList is the response for message list endpoints.
type MessageList struct {
	Room     string     `json:"room"`
	Messages []Envelope `json:"messages"`
	Count    int        `json:"count"`
}

// RoomInfo describes an active room.
type RoomInfo struct {
	Name        string `json:"name"`
	Clients     int    `json:"clients"`
	MessageCount int   `json:"message_count"`
	LastSeq     int64  `json:"last_seq"`
}

// RoomList is the response for GET /api/rooms.
type RoomList struct {
	Rooms []RoomInfo `json:"rooms"`
}

// HealthResponse is the response for GET /api/health.
type HealthResponse struct {
	Status    string  `json:"status"`
	Uptime    string  `json:"uptime"`
	UptimeSec float64 `json:"uptime_seconds"`
	Rooms     int     `json:"rooms"`
}

// FileInfo describes a file shared in a room.
type FileInfo struct {
	ID          string    `json:"id"`
	Room        string    `json:"room"`
	Sender      string    `json:"sender"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	Description string    `json:"description,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
	URL         string    `json:"url,omitempty"`
}

// FileList is the response for file listing endpoints.
type FileList struct {
	Room  string     `json:"room"`
	Files []FileInfo `json:"files"`
	Count int        `json:"count"`
}

// ServerEvent is the discriminated union sent to daemon WebSocket clients.
type ServerEvent struct {
	Event   string    `json:"event"`
	Message *Envelope `json:"message,omitempty"`
	File    *FileInfo `json:"file,omitempty"`
	Spawn   *SpawnReq `json:"spawn,omitempty"`
}

// SpawnReq tells a daemon to spawn a Claude Code instance.
type SpawnReq struct {
	Reason       string     `json:"reason"`
	Trigger      *Envelope  `json:"trigger"`
	Context      []Envelope `json:"context"`
	Participants []string   `json:"participants,omitempty"` // all members of this conv thread (group convos)
}

// ParticipantInfo describes a connected participant.
type ParticipantInfo struct {
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	JoinedAt  time.Time `json:"joined_at"`
	Connected bool      `json:"connected"`
}

// ParticipantList is the response for participant listing endpoints.
type ParticipantList struct {
	Room         string            `json:"room"`
	Participants []ParticipantInfo `json:"participants"`
}
