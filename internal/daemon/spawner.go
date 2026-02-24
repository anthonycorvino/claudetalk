package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/corvino/claudetalk/internal/protocol"
	"github.com/google/uuid"
)

// mcpConfig is the JSON structure for Claude Code's --mcp-config.
type mcpConfig struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

type mcpServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Spawner manages launching Claude Code instances.
type Spawner struct {
	claudeBin    string
	workDir      string
	serverURL    string
	room         string
	name         string
	maxConcurrent int

	sem chan struct{} // Semaphore for concurrency control
	mu  sync.Mutex
}

// NewSpawner creates a new Claude Code spawner.
func NewSpawner(claudeBin, workDir, serverURL, room, name string, maxConcurrent int) *Spawner {
	if claudeBin == "" {
		claudeBin = "claude"
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return &Spawner{
		claudeBin:     claudeBin,
		workDir:       workDir,
		serverURL:     serverURL,
		room:          room,
		name:          name,
		maxConcurrent: maxConcurrent,
		sem:           make(chan struct{}, maxConcurrent),
	}
}

// Spawn launches a Claude Code instance with the given spawn request.
// This runs synchronously and blocks until Claude exits.
func (s *Spawner) Spawn(req *protocol.SpawnReq) error {
	// Acquire semaphore.
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	// Generate temp MCP config.
	configPath, err := s.writeMCPConfig()
	if err != nil {
		return fmt.Errorf("write mcp config: %w", err)
	}
	defer os.Remove(configPath)

	// Build the prompt.
	prompt := s.buildPrompt(req)

	log.Printf("spawning claude for: %s", req.Reason)

	// Build command.
	args := []string{
		"--mcp-config", configPath,
		"--print",
		"-p", prompt,
	}

	cmd := exec.Command(s.claudeBin, args...)
	cmd.Dir = s.workDir
	cmd.Stdout = os.Stderr // Claude's output goes to daemon's stderr for visibility
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude exited with error: %w", err)
	}

	log.Printf("claude completed for: %s", req.Reason)
	return nil
}

func (s *Spawner) writeMCPConfig() (string, error) {
	// Find the claudetalk binary path.
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
					"--server", s.serverURL,
					"--room", s.room,
					"--name", s.name,
				},
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("claudetalk-mcp-%s.json", uuid.New().String()))
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return "", err
	}

	return tmpFile, nil
}

func (s *Spawner) buildPrompt(req *protocol.SpawnReq) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("You are %q in the ClaudeTalk room %q.\n", s.name, s.room))
	sb.WriteString("You have MCP tools to interact with the chatroom: send_message, converse, get_messages, send_file, get_file, list_files, list_participants.\n\n")

	// Add context messages.
	if len(req.Context) > 0 {
		sb.WriteString("Recent conversation context:\n")
		for _, env := range req.Context {
			ts := env.Timestamp.Format("15:04:05")
			fmt.Fprintf(&sb, "[#%d %s] %s", env.SeqNum, ts, env.Sender)
			if to := env.Metadata["to"]; to != "" {
				fmt.Fprintf(&sb, " → %s", to)
			}
			fmt.Fprintf(&sb, ": %s\n", env.Payload.Text)
		}
		sb.WriteString("\n")
	}

	// Add the trigger message.
	if req.Trigger != nil {
		sb.WriteString("You received a direct message that requires your response:\n")
		fmt.Fprintf(&sb, "From: %s\n", req.Trigger.Sender)
		fmt.Fprintf(&sb, "Message: %s\n", req.Trigger.Payload.Text)
		if convID := req.Trigger.Metadata["conv_id"]; convID != "" {
			fmt.Fprintf(&sb, "Conversation ID: %s\n", convID)
		}
		sb.WriteString("\nPlease respond using the converse tool with the sender's name and conversation ID.\n")
	}

	return sb.String()
}
