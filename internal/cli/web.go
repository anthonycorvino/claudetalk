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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/corvino/claudetalk/internal/protocol"
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
		proxyWebSocket(w, req, remote, r)
	})

	// Serve embedded web UI.
	staticFS, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		return fmt.Errorf("embedded static fs: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Proxy all other requests — API calls go to remote, root serves index.html.
	proxy := httputil.NewSingleHostReverseProxy(remote)
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		log.Printf("proxy error: %v", err)
		http.Error(w, fmt.Sprintf("proxy error: %v", err), http.StatusBadGateway)
	}
	proxyHandler := func(w http.ResponseWriter, req *http.Request) {
		req.Host = remote.Host
		proxy.ServeHTTP(w, req)
	}
	mux.HandleFunc("GET /api/", proxyHandler)
	mux.HandleFunc("POST /api/", proxyHandler)

	// Serve index.html at root, 404 everything else.
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

	_, cancel, err := rnr.Sessions().Start(roomName, req.Sender, "")
	if err != nil {
		writeJSONWeb(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	claudeName := req.Sender + "'s Claude"

	go func() {
		defer cancel()
		defer rnr.Sessions().End(roomName, req.Sender, "")

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
// It also starts a daemon-mode watcher for the user's Claude so that directed
// messages trigger automatic local spawns.
func proxyWebSocket(w http.ResponseWriter, r *http.Request, remote *url.URL, rnr *runner.Runner) {
	room := r.PathValue("room")
	sender := r.URL.Query().Get("sender")

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

	// Start daemon watcher so directed messages trigger local Claude spawns.
	// The watcher's lifetime is tied to this browser connection.
	watcherDone := make(chan struct{})
	if rnr != nil && room != "" && sender != "" {
		go startWatcher(remote, room, sender, rnr, watcherDone)
	}

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
	close(watcherDone) // Stop the watcher when the browser disconnects.
}

// startWatcher opens a daemon-mode WebSocket connection to the remote server as
// "{sender}'s Claude" and listens for spawn events. When a spawn event arrives,
// it launches a local Claude process to respond. Runs until done is closed.
func startWatcher(remote *url.URL, room, sender string, rnr *runner.Runner, done <-chan struct{}) {
	claudeName := sender + "'s Claude"

	// Build daemon WebSocket URL.
	wsScheme := "ws"
	if remote.Scheme == "https" {
		wsScheme = "wss"
	}
	u := *remote
	u.Scheme = wsScheme
	u.Path = "/ws/" + url.PathEscape(room)
	q := url.Values{}
	q.Set("sender", claudeName)
	q.Set("mode", "daemon")
	q.Set("role", "daemon")
	u.RawQuery = q.Encode()
	wsURL := u.String()

	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-done:
			return
		default:
		}

		if err := runWatcherConn(wsURL, room, sender, claudeName, rnr, done); err != nil {
			log.Printf("watcher(%s): %v", claudeName, err)
		}

		select {
		case <-done:
			return
		case <-time.After(backoff):
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// runWatcherConn runs a single WebSocket connection for the watcher.
// When a spawn event arrives while a session is already active for that conv_id,
// the latest spawn request is queued and replayed once the active session ends.
func runWatcherConn(wsURL, room, sender, claudeName string, rnr *runner.Runner, done <-chan struct{}) error {
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	log.Printf("watcher connected: %s in room %s", claudeName, room)

	// pendingSpawns holds the latest queued spawn per conv_id when a session is active.
	var pendingMu sync.Mutex
	pendingSpawns := map[string]*protocol.SpawnReq{}

	// trySpawn attempts to start a Claude session for the given conv_id.
	// If a session is already active, the request is queued and will be replayed
	// automatically when the active session ends.
	var trySpawn func(convID string, req *protocol.SpawnReq)
	trySpawn = func(convID string, req *protocol.SpawnReq) {
		_, cancel, err := rnr.Sessions().Start(room, sender, convID)
		if err != nil {
			// Session already active — queue this spawn for after it ends.
			pendingMu.Lock()
			pendingSpawns[convID] = req
			pendingMu.Unlock()
			log.Printf("watcher: queued spawn for %s conv=%s (session active)", sender, convID)
			return
		}

		go func() {
			defer cancel()
			defer func() {
				// End the session first, then replay any queued spawn.
				rnr.Sessions().End(room, sender, convID)
				pendingMu.Lock()
				pending := pendingSpawns[convID]
				delete(pendingSpawns, convID)
				pendingMu.Unlock()
				if pending != nil {
					log.Printf("watcher: replaying queued spawn for %s conv=%s", sender, convID)
					trySpawn(convID, pending)
				}
			}()

			params := runner.SpawnParams{
				Room:   room,
				Sender: sender,
				ConvID: convID,
				Prompt: buildWatcherPrompt(claudeName, room, req),
			}
			if err := rnr.Spawn(params); err != nil {
				log.Printf("watcher: spawn error for %s: %v", claudeName, err)
			}
		}()
	}

	msgs := make(chan []byte, 64)
	readErr := make(chan error, 1)

	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}
			select {
			case msgs <- data:
			default:
			}
		}
	}()

	for {
		select {
		case <-done:
			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return nil
		case err := <-readErr:
			return fmt.Errorf("read: %w", err)
		case data := <-msgs:
			var event protocol.ServerEvent
			if err := json.Unmarshal(data, &event); err != nil {
				continue
			}
			if event.Event == "spawn" && event.Spawn != nil {
				convID := ""
				if event.Spawn.Trigger != nil {
					convID = event.Spawn.Trigger.Metadata["conv_id"]
				}
				trySpawn(convID, event.Spawn)
			}
		}
	}
}

// buildWatcherPrompt builds a prompt for a watcher-triggered Claude spawn.
func buildWatcherPrompt(claudeName, room string, req *protocol.SpawnReq) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("You are %q in the ClaudeTalk room %q.\n\n", claudeName, room))

	// Group thread context.
	isGroup := len(req.Participants) > 1
	if isGroup {
		sb.WriteString("This is a GROUP conversation thread. All participants:\n")
		for _, p := range req.Participants {
			sb.WriteString("  • " + p + "\n")
		}
		sb.WriteString("When you reply, ALL participants in this thread are automatically notified and may respond.\n\n")
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
			sb.WriteString("   All other thread participants will be notified regardless.\n")
		}
		sb.WriteString("3. The context above is current — no need to call get_messages first.\n")
		sb.WriteString("4. To CONTINUE: omit `done`. All participants are notified automatically.\n")
		sb.WriteString("5. To END: set done=true only when the topic is genuinely exhausted.\n")
		sb.WriteString("6. Be concise and substantive.\n")
	}

	return sb.String()
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
