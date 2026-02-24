package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/corvino/claudetalk/internal/protocol"
)

// Handlers holds references needed by HTTP handlers.
type Handlers struct {
	Hub       *Hub
	FileStore *FileStore
	StartTime time.Time
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
