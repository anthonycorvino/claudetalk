package cli

import (
	"fmt"

	"github.com/corvino/claudetalk/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPServeCmd() *cobra.Command {
	var (
		server string
		room   string
		name   string
	)

	cmd := &cobra.Command{
		Use:    "mcp-serve",
		Short:  "Start the MCP stdio server for Claude Code integration",
		Long:   `Runs a Model Context Protocol (MCP) server over stdio. Claude Code connects to this as a subprocess to access chatroom tools (send_message, converse, get_messages, send_file, get_file, list_files, list_participants).`,
		Hidden: true, // Not typically called by users directly
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve from flags, then fall back to global flags, then config.
			if server == "" {
				server = flagServer
			}
			if room == "" {
				room = flagRoom
			}
			if name == "" {
				name = flagSender
			}

			if server == "" {
				return fmt.Errorf("server URL is required (use --server or .claudetalk config)")
			}
			if room == "" {
				return fmt.Errorf("room is required (use --room or .claudetalk config)")
			}
			if name == "" {
				return fmt.Errorf("name is required (use --name or .claudetalk config)")
			}

			return mcp.Serve(mcp.Config{
				ServerURL: server,
				Room:      room,
				Name:      name,
			})
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "server URL (overrides global --server)")
	cmd.Flags().StringVar(&room, "room", "", "room name (overrides global --room)")
	cmd.Flags().StringVar(&name, "name", "", "sender name (overrides global --name)")

	return cmd
}
