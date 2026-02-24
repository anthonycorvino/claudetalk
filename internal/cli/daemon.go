package cli

import (
	"fmt"

	"github.com/corvino/claudetalk/internal/daemon"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	var (
		claudeBin     string
		workDir       string
		maxConcurrent int
	)

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run as a background daemon that auto-spawns Claude when messages arrive",
		Long: `Connects to the ClaudeTalk server via WebSocket in daemon mode. When a directed
message arrives (converse --to <your-name>), the daemon automatically spawns a Claude Code
instance with MCP tools to read and respond to messages.

Before running daemon, use "claudetalk join" to configure your .claudetalk file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagServer == "" {
				return fmt.Errorf("server URL is required (use -s or .claudetalk config)")
			}
			if flagRoom == "" {
				return fmt.Errorf("room is required (use -r or .claudetalk config)")
			}
			if flagSender == "" {
				return fmt.Errorf("name is required (use -n or .claudetalk config)")
			}

			return daemon.Run(daemon.Config{
				ServerURL:     flagServer,
				Room:          flagRoom,
				Name:          flagSender,
				ClaudeBin:     claudeBin,
				WorkDir:       workDir,
				MaxConcurrent: maxConcurrent,
			})
		},
	}

	cmd.Flags().StringVar(&claudeBin, "claude-bin", "claude", "path to claude binary")
	cmd.Flags().StringVar(&workDir, "work-dir", "", "working directory for spawned Claude instances (default: current dir)")
	cmd.Flags().IntVar(&maxConcurrent, "max-concurrent", 1, "max concurrent Claude instances")

	return cmd
}
