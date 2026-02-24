package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/corvino/claudetalk/internal/protocol"
	"github.com/spf13/cobra"
)

func newSendCmd() *cobra.Command {
	var (
		msgType  string
		filePath string
		language string
		body     string
	)

	cmd := &cobra.Command{
		Use:   "send [message]",
		Short: "Send a message to a room",
		Long: `Send a message to a room. Message content can come from:
  - Positional arguments (joined with spaces)
  - The --body flag
  - Stdin (if no args and no --body)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagRoom == "" {
				return fmt.Errorf("room is required (use -r or CLAUDETALK_ROOM)")
			}
			if flagSender == "" {
				return fmt.Errorf("sender name is required (use -n or CLAUDETALK_SENDER)")
			}

			// Determine content source.
			var content string
			switch {
			case body != "":
				content = body
			case len(args) > 0:
				content = strings.Join(args, " ")
			default:
				// Read from stdin.
				stat, _ := os.Stdin.Stat()
				if (stat.Mode() & os.ModeCharDevice) == 0 {
					b, err := io.ReadAll(os.Stdin)
					if err != nil {
						return fmt.Errorf("read stdin: %w", err)
					}
					content = string(b)
				} else {
					return fmt.Errorf("no message provided (use args, --body, or pipe to stdin)")
				}
			}

			content = strings.TrimRight(content, "\n")

			// Build payload based on type.
			if msgType == "" {
				msgType = protocol.TypeText
			}

			var payload protocol.Payload
			switch msgType {
			case protocol.TypeText:
				payload = protocol.NewTextPayload(content)
			case protocol.TypeCode:
				payload = protocol.NewCodePayload(content, filePath, language)
			case protocol.TypeDiff:
				payload = protocol.NewDiffPayload(content, filePath)
			default:
				payload = protocol.Payload{Text: content}
			}

			req := protocol.SendRequest{
				Sender:  flagSender,
				Type:    msgType,
				Payload: payload,
			}

			env, err := postMessage(flagServer, flagRoom, req)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "sent message #%d to room %q\n", env.SeqNum, env.Room)
			return nil
		},
	}

	cmd.Flags().StringVarP(&msgType, "type", "t", "", "message type: text, code, diff (default: text)")
	cmd.Flags().StringVarP(&filePath, "file", "f", "", "file path (for code/diff types)")
	cmd.Flags().StringVarP(&language, "lang", "l", "", "language (for code type; auto-detected from file if omitted)")
	cmd.Flags().StringVar(&body, "body", "", "message body (alternative to args/stdin)")

	return cmd
}
