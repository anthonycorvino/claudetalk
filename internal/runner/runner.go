package runner

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// Config holds configuration for the runner.
type Config struct {
	ClaudeBin string // Path to claude CLI binary (default: "claude")
	WorkDir   string // Working directory for claude processes
	ServerURL string // URL of the local server (e.g. http://localhost:8080)
}

// Runner spawns local Claude Code instances with MCP tools.
type Runner struct {
	claudeBin string
	workDir   string
	serverURL string
	session   *SessionManager
}

// New creates a runner that spawns local Claude Code processes.
func New(cfg Config) *Runner {
	claudeBin := cfg.ClaudeBin
	if claudeBin == "" {
		// Try to find claude in PATH first, then common locations.
		if path, err := exec.LookPath("claude"); err == nil {
			claudeBin = path
		} else {
			home, _ := os.UserHomeDir()
			candidates := []string{
				filepath.Join(home, ".local", "bin", "claude"),
				filepath.Join(home, ".local", "bin", "claude.exe"),
			}
			for _, c := range candidates {
				if _, err := os.Stat(c); err == nil {
					claudeBin = c
					break
				}
			}
			if claudeBin == "" {
				claudeBin = "claude"
			}
		}
		log.Printf("runner: using claude binary: %s", claudeBin)
	}
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	serverURL := cfg.ServerURL
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	return &Runner{
		claudeBin: claudeBin,
		workDir:   workDir,
		serverURL: serverURL,
		session:   NewSessionManager(),
	}
}

// Sessions returns the runner's session manager.
func (r *Runner) Sessions() *SessionManager {
	return r.session
}

// SpawnParams holds parameters for spawning a Claude instance.
type SpawnParams struct {
	Room   string
	Sender string
	ConvID string // conversation thread ID; used for concurrent session tracking
	Prompt string
}

// Spawn launches a local Claude Code process with MCP tools connected to the chatroom.
// Blocks until Claude exits.
func (r *Runner) Spawn(params SpawnParams) error {
	claudeName := params.Sender + "'s Claude"

	// Write temp MCP config pointing at local server.
	configPath, err := r.writeMCPConfig(params.Room, claudeName)
	if err != nil {
		return fmt.Errorf("write mcp config: %w", err)
	}
	defer os.Remove(configPath)

	// Build the prompt with context.
	prompt := r.buildPrompt(params)

	log.Printf("spawning local claude for %s in room %s", params.Sender, params.Room)

	args := []string{
		"--mcp-config", configPath,
		"--print",
		"--dangerously-skip-permissions",
		"-p", prompt,
	}

	cmd := exec.Command(r.claudeBin, args...)
	cmd.Dir = r.workDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	// Remove CLAUDECODE env var so nested claude can run.
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude exited with error: %w", err)
	}

	log.Printf("claude completed for %s in room %s", params.Sender, params.Room)
	return nil
}

// mcpConfig is the JSON structure for Claude Code's --mcp-config.
type mcpConfig struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

type mcpServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func (r *Runner) writeMCPConfig(room, senderName string) (string, error) {
	// Find the claudetalk binary.
	claudetalkBin, err := os.Executable()
	if err != nil {
		claudetalkBin = "claudetalk"
	}

	cfg := mcpConfig{
		MCPServers: map[string]mcpServerConfig{
			"claudetalk": {
				Command: claudetalkBin,
				Args: []string{
					"mcp-serve",
					"--server", r.serverURL,
					"--room", room,
					"--name", senderName,
				},
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("claudetalk-web-mcp-%s.json", uuid.New().String()))
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return "", err
	}

	return tmpFile, nil
}

func (r *Runner) buildPrompt(params SpawnParams) string {
	var sb strings.Builder

	claudeName := params.Sender + "'s Claude"
	sb.WriteString(fmt.Sprintf("You are %q in the ClaudeTalk room %q.\n\n", claudeName, params.Room))
	sb.WriteString("MCP tools available: send_message, converse, get_messages, list_files, list_participants.\n\n")
	sb.WriteString("Your user's request:\n")
	sb.WriteString(params.Prompt)
	sb.WriteString("\n\n━━━ INSTRUCTIONS ━━━\n")
	sb.WriteString("- Use get_messages to read recent context first.\n")
	sb.WriteString("- ALWAYS use send_message to communicate with your user — they read the chat, not the terminal.\n")
	sb.WriteString("  If you need to ask them something, post it with send_message. They will reply in the chat.\n")
	sb.WriteString("  After asking, call get_messages to poll for their reply before continuing.\n")
	sb.WriteString("- To start or continue a directed conversation with another Claude, use the `converse` tool.\n")
	sb.WriteString("- To find other Claudes: call list_participants and look for names ending in \"'s Claude\".\n")
	sb.WriteString("- The `converse` tool sets metadata so the other Claude is automatically notified and spawned to reply.\n")
	sb.WriteString("- Omit `done` (or set done=false) to keep the conversation going. Set done=true only to end it.\n")

	return sb.String()
}

// filterEnv returns env vars with the named key removed.
func filterEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}
