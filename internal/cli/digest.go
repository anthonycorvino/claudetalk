package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/corvino/claudetalk/internal/protocol"
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
			content := buildDigest(list.Room, list.Messages)

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

func buildDigest(room string, messages []protocol.Envelope) string {
	var b strings.Builder

	now := time.Now().Local().Format("2006-01-02 15:04")

	fmt.Fprintf(&b, "# ClaudeTalk Digest — %s\n\n", now)
	fmt.Fprintf(&b, "**Room**: %s\n", room)

	// Collect unique senders.
	senders := map[string]bool{}
	for _, env := range messages {
		if env.Type != protocol.TypeSystem {
			senders[env.Sender] = true
		}
	}
	names := make([]string, 0, len(senders))
	for name := range senders {
		names = append(names, name)
	}
	fmt.Fprintf(&b, "**Participants**: %s\n", strings.Join(names, ", "))

	if len(messages) > 0 {
		first := messages[0].Timestamp.Local().Format("15:04:05")
		last := messages[len(messages)-1].Timestamp.Local().Format("15:04:05")
		fmt.Fprintf(&b, "**Time range**: %s — %s\n", first, last)
	}
	fmt.Fprintf(&b, "**Messages**: %d\n", len(messages))
	fmt.Fprintf(&b, "\n---\n\n## Transcript\n\n")

	// Write each message.
	for _, env := range messages {
		ts := env.Timestamp.Local().Format("15:04:05")

		if env.Type == protocol.TypeSystem {
			fmt.Fprintf(&b, "*[%s] %s*\n\n", ts, env.Payload.Text)
			continue
		}

		// Sender line with optional conversation metadata.
		sender := fmt.Sprintf("**%s**", env.Sender)
		if to := env.Metadata["to"]; to != "" {
			sender += fmt.Sprintf(" → **%s**", to)
		}

		switch env.Type {
		case protocol.TypeText:
			fmt.Fprintf(&b, "[%s] %s: %s", ts, sender, env.Payload.Text)
		case protocol.TypeCode:
			fmt.Fprintf(&b, "[%s] %s shared code", ts, sender)
			if env.Payload.FilePath != "" {
				fmt.Fprintf(&b, " (%s)", env.Payload.FilePath)
			}
			fmt.Fprintf(&b, ":\n```%s\n%s\n```", env.Payload.Language, env.Payload.Code)
		case protocol.TypeDiff:
			fmt.Fprintf(&b, "[%s] %s shared diff", ts, sender)
			if env.Payload.FilePath != "" {
				fmt.Fprintf(&b, " (%s)", env.Payload.FilePath)
			}
			fmt.Fprintf(&b, ":\n```diff\n%s\n```", env.Payload.Diff)
		default:
			fmt.Fprintf(&b, "[%s] %s: %s", ts, sender, env.Payload.Text)
		}

		// Conversation indicators.
		if env.Metadata["expecting_reply"] == "true" {
			fmt.Fprintf(&b, " *(reply expected)*")
		} else if env.Metadata["expecting_reply"] == "false" {
			fmt.Fprintf(&b, " *(conversation complete)*")
		}

		fmt.Fprintf(&b, "\n\n")
	}

	fmt.Fprintf(&b, "---\n\n## Insights\n\n")
	fmt.Fprintf(&b, "*Add your key takeaways, decisions, and action items here.*\n\n")
	fmt.Fprintf(&b, "- \n")

	return b.String()
}

func writeDigestFile(path, content string) error {
	// If file exists, append with a separator.
	if existing, err := os.ReadFile(path); err == nil {
		content = string(existing) + "\n\n---\n\n" + content
	}
	return os.WriteFile(path, []byte(content), 0644)
}
