package server

import (
	"log"
	"net/http"
	"time"
)

// New creates a configured HTTP server with all routes registered.
// fileStore may be nil to disable file storage.
func New(hub *Hub, addr string, fileStore *FileStore) *http.Server {
	mux := http.NewServeMux()
	h := &Handlers{
		Hub:       hub,
		FileStore: fileStore,
		StartTime: time.Now(),
	}

	// REST API routes.
	mux.HandleFunc("GET /api/health", h.Health)
	mux.HandleFunc("GET /api/rooms", h.ListRooms)
	mux.HandleFunc("POST /api/rooms/{room}/messages", h.SendMessage)
	mux.HandleFunc("GET /api/rooms/{room}/messages/latest", h.LatestMessages)
	mux.HandleFunc("GET /api/rooms/{room}/messages", h.GetMessages)

	// File routes.
	mux.HandleFunc("POST /api/rooms/{room}/files", h.UploadFile)
	mux.HandleFunc("GET /api/rooms/{room}/files/{id}", h.DownloadFile)
	mux.HandleFunc("GET /api/rooms/{room}/files", h.ListFiles)

	// Participant route.
	mux.HandleFunc("GET /api/rooms/{room}/participants", h.ListParticipants)

	// WebSocket route.
	mux.HandleFunc("GET /ws/{room}", h.HandleWS)

	// Wrap with logging middleware.
	handler := loggingMiddleware(corsMiddleware(mux))

	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Microsecond))
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
