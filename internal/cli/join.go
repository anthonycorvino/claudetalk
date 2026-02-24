package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Config is the .claudetalk project config written by "join".
type Config struct {
	Server string `json:"server"`
	Room   string `json:"room"`
	Sender string `json:"sender"`
}

const configFileName = ".claudetalk"

func newJoinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "join <url> [room] [name]",
		Short: "Connect to a friend's ClaudeTalk server",
		Long: `Connects to a ClaudeTalk server, verifies it's reachable, and writes
a .claudetalk config file in the current directory so all future commands
just work. Also writes CLAUDE.md so Claude Code knows how to use claudetalk.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			serverURL := ""
			room := ""
			sender := ""

			if len(args) >= 1 {
				serverURL = args[0]
			}
			if len(args) >= 2 {
				room = args[1]
			}
			if len(args) >= 3 {
				sender = args[2]
			}

			return runJoin(serverURL, room, sender)
		},
	}
	return cmd
}

func runJoin(serverURL, room, sender string) error {
	reader := bufio.NewReader(os.Stdin)

	// 1. Get the server URL.
	if serverURL == "" {
		fmt.Print("Paste the URL your friend shared: ")
		line, _ := reader.ReadString('\n')
		serverURL = strings.TrimSpace(line)
	}
	if serverURL == "" {
		return fmt.Errorf("server URL is required")
	}
	serverURL = strings.TrimRight(serverURL, "/")

	// 2. Health check.
	fmt.Printf("Connecting to %s ...\n", serverURL)
	health, err := getHealth(serverURL)
	if err != nil {
		return fmt.Errorf("could not reach server: %w\n\nPossible issues:\n  - The host hasn't started yet\n  - The URL is wrong or expired\n  - localtunnel may show a click-through page — open the URL in a browser first", err)
	}
	fmt.Printf("Connected! Server is %s (uptime: %s, %d rooms)\n", health.Status, health.Uptime, health.Rooms)

	// 3. Prompt for room and sender if needed.
	if room == "" {
		fmt.Print("Room name (e.g. myproject): ")
		line, _ := reader.ReadString('\n')
		room = strings.TrimSpace(line)
	}
	if room == "" {
		return fmt.Errorf("room name is required")
	}

	if sender == "" {
		fmt.Print("Your name (e.g. alice): ")
		line, _ := reader.ReadString('\n')
		sender = strings.TrimSpace(line)
	}
	if sender == "" {
		return fmt.Errorf("name is required")
	}

	// 4. Write .claudetalk config.
	cfg := Config{
		Server: serverURL,
		Room:   room,
		Sender: sender,
	}
	cfgBytes, _ := json.MarshalIndent(cfg, "", "  ")

	if err := os.WriteFile(configFileName, cfgBytes, 0644); err != nil {
		return fmt.Errorf("write %s: %w", configFileName, err)
	}
	fmt.Printf("Wrote %s\n", configFileName)

	// 5. Write CLAUDE.md (or append to existing one).
	claudeMDPath := "CLAUDE.md"
	if err := writeClaudeMD(claudeMDPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write %s: %v\n", claudeMDPath, err)
	} else {
		fmt.Printf("Wrote %s\n", claudeMDPath)
	}

	// 6. Print success.
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println()
	fmt.Println("  You're connected!")
	fmt.Println()
	fmt.Printf("  Server: %s\n", serverURL)
	fmt.Printf("  Room:   %s\n", room)
	fmt.Printf("  Name:   %s\n", sender)
	fmt.Println()
	fmt.Println("  Quick commands:")
	fmt.Printf("    claudetalk send \"hello everyone!\"\n")
	fmt.Printf("    claudetalk recv --latest 5\n")
	fmt.Printf("    claudetalk watch\n")
	fmt.Println()
	fmt.Println("  Claude Code will auto-detect the .claudetalk config.")
	fmt.Println("  Just start Claude in this directory and it knows what to do.")
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println()

	return nil
}

// writeClaudeMD writes the CLAUDE.md template. If one already exists,
// it appends the claudetalk section.
func writeClaudeMD(path string, cfg Config) error {
	content := claudeMDContent(cfg)

	// Check if CLAUDE.md already exists.
	existing, err := os.ReadFile(path)
	if err == nil {
		// Already exists — check if claudetalk section is already there.
		if strings.Contains(string(existing), "ClaudeTalk") {
			fmt.Printf("  %s already contains ClaudeTalk config — skipping\n", path)
			return nil
		}
		// Append.
		content = string(existing) + "\n\n" + content
	}

	return os.WriteFile(path, []byte(content), 0644)
}

// loadConfig reads a .claudetalk config from the current directory
// or any parent directory.
func loadConfig() *Config {
	dir, err := os.Getwd()
	if err != nil {
		return nil
	}

	for {
		path := filepath.Join(dir, configFileName)
		data, err := os.ReadFile(path)
		if err == nil {
			var cfg Config
			if json.Unmarshal(data, &cfg) == nil {
				return &cfg
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil
}

func claudeMDContent(cfg Config) string {
	return fmt.Sprintf(`# ClaudeTalk — Multi-Agent Communication

This project uses **ClaudeTalk** for real-time communication between Claude Code instances.
Check for messages regularly and respond when appropriate.

## Connection (auto-configured)

The .claudetalk config file in this directory sets:
- Server: %s
- Room: %s
- Sender: %s

All commands below auto-read this config — no flags needed.

## Commands

Send a message:
`+"```"+`
claudetalk send "your message here"
`+"```"+`

Send code:
`+"```"+`
claudetalk send -t code -f path/to/file.py
`+"```"+`

Send a diff:
`+"```"+`
git diff | claudetalk send -t diff
`+"```"+`

Receive messages:
`+"```"+`
claudetalk recv --latest 5
`+"```"+`

## Conversations (Direct Claude-to-Claude)

To ask another Claude a direct question:
`+"```"+`
claudetalk converse --to <recipient-name> "your question here"
`+"```"+`
This returns a conversation ID. When replying to a directed conversation:
`+"```"+`
claudetalk converse --to <sender-name> --conv <conv-id> "your answer"
`+"```"+`
To end a conversation (no further reply expected):
`+"```"+`
claudetalk converse --to <sender-name> --conv <conv-id> --done "final answer"
`+"```"+`

**When you see a message directed to you** (with `+"`→ your-name`"+` and `+"`(reply expected)`"+`):
1. Read the question carefully
2. Respond using `+"`converse --to <sender> --conv <id>`"+`
3. Use `+"`--done`"+` if your response completes the conversation

## Chatroom (Group Discussion)

When multiple Claudes are in the same room, use `+"`claudetalk send`"+` for group discussion.
All participants see all messages. This is for collaborative topics where everyone should chime in.

**Group discussion guidelines:**
- Stay on topic — respond only when you have something relevant to contribute
- Be concise — everyone has limited context
- Announce what you're working on so others don't duplicate effort
- When asked a group question, respond with your perspective

## Auto-Polling (MANDATORY)

**IMPORTANT: At the START of every response, run `+"`claudetalk poll`"+`. If there are new messages, read and respond to them before continuing with the user's request.**

The poll command is silent when there are no new messages, so it won't clutter your output. It automatically tracks which messages you've already seen.

## When to Check Messages

- **Every single response** — run `+"`claudetalk poll`"+` (this is mandatory, not optional)
- **After completing work** — send a summary of what you did
- **When stuck** — ask via claudetalk before guessing
- **Before modifying shared files** — check if someone else is working on them

## Guidelines

1. Be concise — other agents have limited context too
2. Use `+"`-t code`"+` for code, `+"`-t diff`"+` for diffs — not plain text walls
3. Announce what you're working on before starting
4. Send the actual diff when done: `+"`git diff | claudetalk send -t diff`"+`
`, cfg.Server, cfg.Room, cfg.Sender)
}
