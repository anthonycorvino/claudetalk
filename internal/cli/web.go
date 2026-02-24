package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/corvino/claudetalk/internal/runner"
	"github.com/corvino/claudetalk/internal/web"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

func newWebCmd() *cobra.Command {
	var (
		port     int
		claudeBin string
	)

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Open the web UI with local Claude spawning",
		Long: `Starts a local web UI that connects to a remote ClaudeTalk server.
Chat messages are relayed to the remote server, but "Ask Claude" spawns
a Claude Code instance locally on your machine.

Your friends just need this binary — no Go or other dependencies required.

Example:
  claudetalk web --server https://claudetalk.fly.dev
  claudetalk web -s http://localhost:8080 -p 3000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWeb(flagServer, port, claudeBin)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 3000, "local web UI port")
	cmd.Flags().StringVar(&claudeBin, "claude-bin", "", "path to claude CLI binary")
	return cmd
}

func runWeb(remoteServer string, port int, claudeBin string) error {
	remote, err := url.Parse(remoteServer)
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}

	localAddr := fmt.Sprintf("http://localhost:%d", port)

	// Create runner for local Claude spawning.
	r := runner.New(runner.Config{
		ClaudeBin: claudeBin,
		ServerURL: remoteServer, // Claude's MCP tools talk to the REMOTE server.
	})

	mux := http.NewServeMux()

	// Intercept spawn/stop — handle locally.
	mux.HandleFunc("POST /api/rooms/{room}/spawn", func(w http.ResponseWriter, req *http.Request) {
		handleLocalSpawn(w, req, r)
	})
	mux.HandleFunc("POST /api/rooms/{room}/stop", func(w http.ResponseWriter, req *http.Request) {
		handleLocalStop(w, req, r)
	})

	// Proxy WebSocket connections to remote server.
	mux.HandleFunc("GET /ws/{room}", func(w http.ResponseWriter, req *http.Request) {
		proxyWebSocket(w, req, remote)
	})

	// Proxy all other API calls to remote server.
	proxy := httputil.NewSingleHostReverseProxy(remote)
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		log.Printf("proxy error: %v", err)
		http.Error(w, fmt.Sprintf("proxy error: %v", err), http.StatusBadGateway)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, req *http.Request) {
		req.Host = remote.Host
		proxy.ServeHTTP(w, req)
	})

	// Serve embedded web UI.
	staticFS, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		return fmt.Errorf("embedded static fs: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/" {
			http.NotFound(w, req)
			return
		}
		data, _ := web.StaticFS.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	handler := corsMiddlewareWeb(mux)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Println()
		fmt.Println("============================================================")
		fmt.Println()
		fmt.Println("  ClaudeTalk Web UI")
		fmt.Println()
		fmt.Printf("  Local UI:      %s\n", localAddr)
		fmt.Printf("  Chat server:   %s\n", remoteServer)
		fmt.Println("  Claude:        spawns locally on your machine")
		fmt.Println()
		fmt.Println("============================================================")
		fmt.Println()
		fmt.Println("Press Ctrl+C to shut down.")
		fmt.Println()

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-stop
	fmt.Println("\nShutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	fmt.Println("Stopped.")
	return nil
}

func handleLocalSpawn(w http.ResponseWriter, r *http.Request, rnr *runner.Runner) {
	roomName := r.PathValue("room")
	if roomName == "" {
		writeJSONWeb(w, http.StatusBadRequest, map[string]string{"error": "room name required"})
		return
	}

	var req struct {
		Sender string `json:"sender"`
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONWeb(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
		return
	}
	if req.Sender == "" || req.Prompt == "" {
		writeJSONWeb(w, http.StatusBadRequest, map[string]string{"error": "sender and prompt required"})
		return
	}

	_, cancel, err := rnr.Sessions().Start(roomName, req.Sender)
	if err != nil {
		writeJSONWeb(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	claudeName := req.Sender + "'s Claude"

	go func() {
		defer cancel()
		defer rnr.Sessions().End(roomName, req.Sender)

		params := runner.SpawnParams{
			Room:   roomName,
			Sender: req.Sender,
			Prompt: req.Prompt,
		}

		if err := rnr.Spawn(params); err != nil {
			log.Printf("spawn error room=%s sender=%s: %v", roomName, req.Sender, err)
		}
	}()

	writeJSONWeb(w, http.StatusAccepted, map[string]string{"status": "spawning", "claude": claudeName})
}

func handleLocalStop(w http.ResponseWriter, r *http.Request, rnr *runner.Runner) {
	roomName := r.PathValue("room")

	var req struct {
		Sender string `json:"sender"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONWeb(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if err := rnr.Sessions().Stop(roomName, req.Sender); err != nil {
		writeJSONWeb(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSONWeb(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// proxyWebSocket proxies a WebSocket connection to the remote server.
func proxyWebSocket(w http.ResponseWriter, r *http.Request, remote *url.URL) {
	// Build remote WebSocket URL.
	wsScheme := "ws"
	if remote.Scheme == "https" {
		wsScheme = "wss"
	}
	remoteURL := wsScheme + "://" + remote.Host + r.URL.Path + "?" + r.URL.RawQuery

	// Connect to remote.
	remoteConn, _, err := websocket.DefaultDialer.Dial(remoteURL, nil)
	if err != nil {
		log.Printf("ws proxy: failed to connect to remote: %v", err)
		http.Error(w, "failed to connect to remote server", http.StatusBadGateway)
		return
	}
	defer remoteConn.Close()

	// Upgrade local connection.
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	localConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws proxy: upgrade failed: %v", err)
		return
	}
	defer localConn.Close()

	// Bidirectional relay.
	done := make(chan struct{}, 2)

	// Remote → Local
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			msgType, data, err := remoteConn.ReadMessage()
			if err != nil {
				return
			}
			if err := localConn.WriteMessage(msgType, data); err != nil {
				return
			}
		}
	}()

	// Local → Remote
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			msgType, data, err := localConn.ReadMessage()
			if err != nil {
				return
			}
			if err := remoteConn.WriteMessage(msgType, data); err != nil {
				return
			}
		}
	}()

	<-done
}

func writeJSONWeb(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func corsMiddlewareWeb(next http.Handler) http.Handler {
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
