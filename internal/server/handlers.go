package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/corvino/claudetalk/internal/protocol"
	"github.com/corvino/claudetalk/internal/runner"
	"github.com/corvino/claudetalk/internal/synopsis"
)

// Handlers holds references needed by HTTP handlers.
type Handlers struct {
	Hub        *Hub
	FileStore  *FileStore
	Runner     *runner.Runner
	StartTime  time.Time
	hookStates sync.Map // key: claudeName (string), value: *hostHookState
}

// hostHookState maintains pending spawn queues for a host-mode Claude so that
// directed replies from other Claudes trigger re-spawns automatically.
type hostHookState struct {
	mu            sync.Mutex
	pendingSpawns map[string]*protocol.SpawnReq
	rnr           *runner.Runner
	room          string
	sender        string
	claudeName    string
}

func (s *hostHookState) trySpawn(req *protocol.SpawnReq) {
	convID := ""
	if req.Trigger != nil {
		convID = req.Trigger.Metadata["conv_id"]
	}

	_, cancel, err := s.rnr.Sessions().Start(s.room, s.sender, convID)
	if err != nil {
		// Session already active — queue the latest request.
		s.mu.Lock()
		s.pendingSpawns[convID] = req
		s.mu.Unlock()
		log.Printf("host hook: queued spawn for %s conv=%s (session active)", s.sender, convID)
		return
	}

	go func() {
		defer cancel()
		defer func() {
			// End the session first, then replay any queued spawn.
			s.rnr.Sessions().End(s.room, s.sender, convID)
			s.mu.Lock()
			pending := s.pendingSpawns[convID]
			delete(s.pendingSpawns, convID)
			s.mu.Unlock()
			if pending != nil {
				log.Printf("host hook: replaying queued spawn for %s conv=%s", s.sender, convID)
				s.trySpawn(pending)
			}
		}()

		params := runner.SpawnParams{
			Room:   s.room,
			Sender: s.sender,
			ConvID: convID,
			Prompt: buildHostHookPrompt(s.claudeName, s.room, req),
		}
		if err := s.rnr.Spawn(params); err != nil {
			log.Printf("host hook: spawn error for %s: %v", s.claudeName, err)
		}
	}()
}

// buildHostHookPrompt builds a reply prompt for a host-mode Claude responding to a directed message.
func buildHostHookPrompt(claudeName, room string, req *protocol.SpawnReq) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("You are %q in the ClaudeTalk room %q.\n\n", claudeName, room))

	isGroup := len(req.Participants) > 1
	if isGroup {
		sb.WriteString("This is a GROUP conversation thread. All participants:\n")
		for _, p := range req.Participants {
			sb.WriteString("  • " + p + "\n")
		}
		sb.WriteString("When you reply, ALL participants in this thread are automatically notified.\n\n")
	}

	if len(req.Context) > 0 {
		sb.WriteString("Recent conversation context (newest at bottom):\n")
		for _, env := range req.Context {
			ts := env.Timestamp.Format("15:04:05")
			fmt.Fprintf(&sb, "[%s] %s", ts, env.Sender)
			if to := env.Metadata["to"]; to != "" {
				fmt.Fprintf(&sb, " → %s", to)
			}
			fmt.Fprintf(&sb, ": %s\n", env.Payload.Text)
		}
		sb.WriteString("\n")
	}

	if req.Trigger != nil {
		replyTo := req.Trigger.Sender
		convID := req.Trigger.Metadata["conv_id"]
		sb.WriteString("━━━ INCOMING MESSAGE ━━━\n")
		fmt.Fprintf(&sb, "From:            %s\n", replyTo)
		fmt.Fprintf(&sb, "Conversation ID: %s\n", convID)
		fmt.Fprintf(&sb, "Message:         %s\n", req.Trigger.Payload.Text)
		sb.WriteString("\n━━━ REPLY INSTRUCTIONS ━━━\n")
		sb.WriteString("1. You MUST reply using the `converse` tool — NEVER `send_message` for directed replies.\n")
		fmt.Fprintf(&sb, "2. Use: converse(to=%q, conv_id=%q, message=\"your reply\")\n", replyTo, convID)
		if isGroup {
			sb.WriteString("   In a group thread you may also change `to` to address a specific participant.\n")
		}
		sb.WriteString("3. The context above is current — no need to call get_messages first.\n")
		sb.WriteString("4. To CONTINUE: omit `done`. All participants are notified automatically.\n")
		sb.WriteString("5. To END: set done=true only when the topic is genuinely exhausted.\n")
		sb.WriteString("6. Be concise and substantive.\n")
	}

	return sb.String()
}

// Health handles GET /api/health.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(h.StartTime)
	resp := protocol.HealthResponse{
		Status:    "ok",
		Uptime:    uptime.Round(time.Second).String(),
		UptimeSec: uptime.Seconds(),
		Rooms:     h.Hub.RoomCount(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListRooms handles GET /api/rooms.
func (h *Handlers) ListRooms(w http.ResponseWriter, r *http.Request) {
	snapshots := h.Hub.ListRooms()
	rooms := make([]protocol.RoomInfo, len(snapshots))
	for i, s := range snapshots {
		rooms[i] = protocol.RoomInfo{
			Name:         s.Name,
			Clients:      s.Clients,
			MessageCount: s.MessageCount,
			LastSeq:      s.LastSeq,
		}
	}
	writeJSON(w, http.StatusOK, protocol.RoomList{Rooms: rooms})
}

// SendMessage handles POST /api/rooms/{room}/messages.
func (h *Handlers) SendMessage(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")
	if roomName == "" {
		writeError(w, http.StatusBadRequest, "room name required")
		return
	}

	var req protocol.SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if req.Sender == "" {
		writeError(w, http.StatusBadRequest, "sender required")
		return
	}
	if req.Type == "" {
		req.Type = protocol.TypeText
	}

	room := h.Hub.GetOrCreateRoom(roomName)
	env := room.AddMessage(req.Sender, req.Type, req.Payload, req.Metadata)
	writeJSON(w, http.StatusCreated, env)
}

// GetMessages handles GET /api/rooms/{room}/messages?after={seq}&limit={n}.
func (h *Handlers) GetMessages(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")
	if roomName == "" {
		writeError(w, http.StatusBadRequest, "room name required")
		return
	}

	room := h.Hub.GetRoom(roomName)
	if room == nil {
		writeJSON(w, http.StatusOK, protocol.MessageList{Room: roomName, Messages: []protocol.Envelope{}, Count: 0})
		return
	}

	after := int64(0)
	if v := r.URL.Query().Get("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid after parameter")
			return
		}
		after = n
	}

	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit parameter")
			return
		}
		limit = n
	}

	msgs := room.MessagesAfter(after, limit)
	if msgs == nil {
		msgs = []protocol.Envelope{}
	}
	writeJSON(w, http.StatusOK, protocol.MessageList{Room: roomName, Messages: msgs, Count: len(msgs)})
}

// LatestMessages handles GET /api/rooms/{room}/messages/latest?n={count}.
func (h *Handlers) LatestMessages(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")
	if roomName == "" {
		writeError(w, http.StatusBadRequest, "room name required")
		return
	}

	room := h.Hub.GetRoom(roomName)
	if room == nil {
		writeJSON(w, http.StatusOK, protocol.MessageList{Room: roomName, Messages: []protocol.Envelope{}, Count: 0})
		return
	}

	n := 10
	if v := r.URL.Query().Get("n"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "invalid n parameter")
			return
		}
		n = parsed
	}

	msgs := room.LatestMessages(n)
	if msgs == nil {
		msgs = []protocol.Envelope{}
	}
	writeJSON(w, http.StatusOK, protocol.MessageList{Room: roomName, Messages: msgs, Count: len(msgs)})
}

// HandleWS handles WS /ws/{room}?sender={name}.
func (h *Handlers) HandleWS(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")
	if roomName == "" {
		writeError(w, http.StatusBadRequest, "room name required")
		return
	}
	sender := r.URL.Query().Get("sender")
	if sender == "" {
		sender = "anonymous"
	}
	ServeWS(h.Hub, w, r, roomName, sender)
}

// UploadFile handles POST /api/rooms/{room}/files (multipart form).
func (h *Handlers) UploadFile(w http.ResponseWriter, r *http.Request) {
	if h.FileStore == nil {
		writeError(w, http.StatusServiceUnavailable, "file storage not configured")
		return
	}

	roomName := r.PathValue("room")
	if roomName == "" {
		writeError(w, http.StatusBadRequest, "room name required")
		return
	}

	// Parse multipart form (limit to FileStore's max + 1MB overhead).
	if err := r.ParseMultipartForm(h.FileStore.maxFileSize + 1024*1024); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("parse form: %v", err))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("missing file field: %v", err))
		return
	}
	defer file.Close()

	sender := r.FormValue("sender")
	if sender == "" {
		sender = "anonymous"
	}
	description := r.FormValue("description")
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	info, err := h.FileStore.Store(roomName, sender, header.Filename, contentType, description, header.Size, file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Broadcast a file notification message to the room.
	room := h.Hub.GetOrCreateRoom(roomName)
	text := fmt.Sprintf("shared file: %s", info.Filename)
	if description != "" {
		text += " — " + description
	}
	room.AddMessage(sender, protocol.TypeFile, protocol.Payload{Text: text, FilePath: info.Filename}, map[string]string{
		"file_id": info.ID,
	})

	writeJSON(w, http.StatusCreated, info)
}

// DownloadFile handles GET /api/rooms/{room}/files/{id}.
func (h *Handlers) DownloadFile(w http.ResponseWriter, r *http.Request) {
	if h.FileStore == nil {
		writeError(w, http.StatusServiceUnavailable, "file storage not configured")
		return
	}

	fileID := r.PathValue("id")
	if fileID == "" {
		writeError(w, http.StatusBadRequest, "file id required")
		return
	}

	info, err := h.FileStore.Get(fileID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	diskPath, err := h.FileStore.FilePath(fileID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", info.Filename))
	http.ServeFile(w, r, diskPath)
}

// ListFiles handles GET /api/rooms/{room}/files.
func (h *Handlers) ListFiles(w http.ResponseWriter, r *http.Request) {
	if h.FileStore == nil {
		writeError(w, http.StatusServiceUnavailable, "file storage not configured")
		return
	}

	roomName := r.PathValue("room")
	if roomName == "" {
		writeError(w, http.StatusBadRequest, "room name required")
		return
	}

	files := h.FileStore.List(roomName)
	if files == nil {
		files = []protocol.FileInfo{}
	}
	writeJSON(w, http.StatusOK, protocol.FileList{Room: roomName, Files: files, Count: len(files)})
}

// ListParticipants handles GET /api/rooms/{room}/participants.
func (h *Handlers) ListParticipants(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")
	if roomName == "" {
		writeError(w, http.StatusBadRequest, "room name required")
		return
	}

	room := h.Hub.GetRoom(roomName)
	if room == nil {
		writeJSON(w, http.StatusOK, protocol.ParticipantList{Room: roomName, Participants: []protocol.ParticipantInfo{}})
		return
	}

	participants := room.ListParticipants()
	writeJSON(w, http.StatusOK, protocol.ParticipantList{Room: roomName, Participants: participants})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// SpawnClaude handles POST /api/rooms/{room}/spawn.
func (h *Handlers) SpawnClaude(w http.ResponseWriter, r *http.Request) {
	if h.Runner == nil {
		writeError(w, http.StatusServiceUnavailable, "Claude runner not configured")
		return
	}

	roomName := r.PathValue("room")
	if roomName == "" {
		writeError(w, http.StatusBadRequest, "room name required")
		return
	}

	var req struct {
		Sender string `json:"sender"`
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if req.Sender == "" || req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "sender and prompt required")
		return
	}

	room := h.Hub.GetOrCreateRoom(roomName)
	claudeName := req.Sender + "'s Claude"

	// Ensure a spawn hook is registered so directed replies from other Claudes
	// trigger automatic re-spawns (mirrors watcher daemon behavior in web mode).
	hookVal, _ := h.hookStates.LoadOrStore(claudeName, &hostHookState{
		rnr:           h.Runner,
		room:          roomName,
		sender:        req.Sender,
		claudeName:    claudeName,
		pendingSpawns: make(map[string]*protocol.SpawnReq),
	})
	room.RegisterSpawnHook(claudeName, hookVal.(*hostHookState).trySpawn)

	// Try to start a session (no conv_id for user-initiated spawns).
	_, cancel, err := h.Runner.Sessions().Start(roomName, req.Sender, "")
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	// Post "thinking" system message.
	room.AddMessage("system", protocol.TypeSystem, protocol.Payload{
		Text: claudeName + " is thinking...",
	}, nil)

	// Track Claude as participant during session.
	room.TrackParticipant(claudeName, "claude", nil)

	// Launch local Claude Code process in background.
	go func() {
		defer cancel()
		defer h.Runner.Sessions().End(roomName, req.Sender, "")
		defer room.UntrackParticipant(claudeName)

		params := runner.SpawnParams{
			Room:   roomName,
			Sender: req.Sender,
			Prompt: req.Prompt,
		}

		if err := h.Runner.Spawn(params); err != nil {
			log.Printf("spawn error room=%s sender=%s: %v", roomName, req.Sender, err)
			room.AddMessage("system", protocol.TypeSystem, protocol.Payload{
				Text: claudeName + " encountered an error: " + err.Error(),
			}, nil)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "spawning", "claude": claudeName})
}

// StopClaude handles POST /api/rooms/{room}/stop.
func (h *Handlers) StopClaude(w http.ResponseWriter, r *http.Request) {
	if h.Runner == nil {
		writeError(w, http.StatusServiceUnavailable, "Claude runner not configured")
		return
	}

	roomName := r.PathValue("room")
	if roomName == "" {
		writeError(w, http.StatusBadRequest, "room name required")
		return
	}

	var req struct {
		Sender string `json:"sender"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if req.Sender == "" {
		writeError(w, http.StatusBadRequest, "sender required")
		return
	}

	if err := h.Runner.Sessions().Stop(roomName, req.Sender); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	claudeName := req.Sender + "'s Claude"
	room := h.Hub.GetRoom(roomName)
	if room != nil {
		room.UnregisterSpawnHook(claudeName)
		room.AddMessage("system", protocol.TypeSystem, protocol.Payload{
			Text: claudeName + " was stopped",
		}, nil)
	}
	h.hookStates.Delete(claudeName)

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// GenerateSynopsis handles POST /api/rooms/{room}/synopsis.
func (h *Handlers) GenerateSynopsis(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")
	if roomName == "" {
		writeError(w, http.StatusBadRequest, "room name required")
		return
	}

	room := h.Hub.GetRoom(roomName)
	if room == nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	msgs := room.LatestMessages(1000)
	if len(msgs) == 0 {
		writeError(w, http.StatusNotFound, "no messages in room")
		return
	}

	content := synopsis.Build(roomName, msgs)

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", roomName+"-synopsis.md"))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(content))
}
