package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	flagServer string
	flagRoom   string
	flagSender string
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "claudetalk",
		Short: "CLI for ClaudeTalk - real-time communication between Claude Code instances",
	}

	// Resolve defaults: flags > env vars > .claudetalk config > hardcoded defaults.
	defaultServer := "http://localhost:8080"
	defaultRoom := ""
	defaultSender := ""

	if cfg := loadConfig(); cfg != nil {
		if cfg.Server != "" {
			defaultServer = cfg.Server
		}
		if cfg.Room != "" {
			defaultRoom = cfg.Room
		}
		if cfg.Sender != "" {
			defaultSender = cfg.Sender
		}
	}

	root.PersistentFlags().StringVarP(&flagServer, "server", "s", envOrDefault("CLAUDETALK_SERVER", defaultServer), "server URL")
	root.PersistentFlags().StringVarP(&flagRoom, "room", "r", envOrDefault("CLAUDETALK_ROOM", defaultRoom), "room name")
	root.PersistentFlags().StringVarP(&flagSender, "name", "n", envOrDefault("CLAUDETALK_SENDER", defaultSender), "sender name")

	root.AddCommand(
		newSendCmd(),
		newRecvCmd(),
		newPollCmd(),
		newWatchCmd(),
		newRoomsCmd(),
		newStatusCmd(),
		newHostCmd(),
		newJoinCmd(),
		newConverseCmd(),
		newDigestCmd(),
		newMCPServeCmd(),
		newDaemonCmd(),
		newWebCmd(),
	)

	return root
}

// Execute runs the CLI.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
