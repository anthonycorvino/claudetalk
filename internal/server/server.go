package server

import (
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/corvino/claudetalk/internal/runner"
	"github.com/corvino/claudetalk/internal/web"
)

// New creates a configured HTTP server with all routes registered.
// fileStore may be nil to disable file storage.
// runner may be nil to disable Claude spawning.
func New(hub *Hub, addr string, fileStore *FileStore, r *runner.Runner) *http.Server {
	mux := http.NewServeMux()
	h := &Handlers{
		Hub:       hub,
		FileStore: fileStore,
		Runner:    r,
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

	// Claude runner routes.
	mux.HandleFunc("POST /api/rooms/{room}/spawn", h.SpawnClaude)
	mux.HandleFunc("POST /api/rooms/{room}/stop", h.StopClaude)
	mux.HandleFunc("POST /api/rooms/{room}/synopsis", h.GenerateSynopsis)

	// WebSocket route.
	mux.HandleFunc("GET /ws/{room}", h.HandleWS)

	// Serve embedded web UI (must be after API routes).
	staticFS, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		log.Fatalf("embedded static fs: %v", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", noCacheHandler(http.FileServer(http.FS(staticFS)))))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := web.StaticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

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

func noCacheHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		next.ServeHTTP(w, r)
	})
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
