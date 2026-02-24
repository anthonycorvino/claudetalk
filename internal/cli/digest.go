package cli

import (
	"fmt"
	"os"

	"github.com/corvino/claudetalk/internal/protocol"
	"github.com/corvino/claudetalk/internal/synopsis"
	"github.com/spf13/cobra"
)

func newDigestCmd() *cobra.Command {
	var (
		outputFile string
		latest     int
		after      int64
	)

	cmd := &cobra.Command{
		Use:   "digest",
		Short: "Save conversation transcript and insights to a markdown file",
		Long: `Fetches messages from the room and writes them as a formatted markdown
transcript. Useful for recording conversations, insights, and decisions
after a multi-Claude discussion.

Examples:
  claudetalk digest                          # Save latest 50 messages to claudetalk-digest.md
  claudetalk digest -o session-notes.md      # Custom output file
  claudetalk digest --latest 100             # Save latest 100 messages
  claudetalk digest --after 25               # Save messages after seq #25`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagRoom == "" {
				return fmt.Errorf("room is required (use -r or CLAUDETALK_ROOM)")
			}

			// Fetch messages.
			var list *protocol.MessageList
			var err error
			if after > 0 {
				list, err = getMessages(flagServer, flagRoom, after, 1000)
			} else {
				list, err = getLatestMessages(flagServer, flagRoom, latest)
			}
			if err != nil {
				return err
			}

			if list.Count == 0 {
				fmt.Fprintln(os.Stderr, "no messages to digest")
				return nil
			}

			// Build markdown content.
			content := synopsis.Build(list.Room, list.Messages)

			// Write or append to file.
			if err := writeDigestFile(outputFile, content); err != nil {
				return fmt.Errorf("write %s: %w", outputFile, err)
			}

			fmt.Fprintf(os.Stderr, "wrote %d messages to %s\n", list.Count, outputFile)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFile, "output", "o", "claudetalk-digest.md", "output file path")
	cmd.Flags().IntVar(&latest, "latest", 50, "number of latest messages to include")
	cmd.Flags().Int64Var(&after, "after", 0, "include messages after this sequence number (overrides --latest)")

	return cmd
}

func writeDigestFile(path, content string) error {
	// If file exists, append with a separator.
	if existing, err := os.ReadFile(path); err == nil {
		content = string(existing) + "\n\n---\n\n" + content
	}
	return os.WriteFile(path, []byte(content), 0644)
}
