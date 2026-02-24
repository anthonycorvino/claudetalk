package synopsis

import (
	"fmt"
	"strings"
	"time"

	"github.com/corvino/claudetalk/internal/protocol"
)

// Build creates a markdown digest from a room's messages.
func Build(room string, messages []protocol.Envelope) string {
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
