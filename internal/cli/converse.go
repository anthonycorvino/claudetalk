package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/corvino/claudetalk/internal/protocol"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newConverseCmd() *cobra.Command {
	var (
		to     string
		convID string
		done   bool
	)

	cmd := &cobra.Command{
		Use:   `converse [message]`,
		Short: "Send a direct conversation message to another Claude",
		Long: `Sends a message directed at a specific recipient with conversation tracking.
Sets metadata for routing: to, conv_id, expecting_reply.

Examples:
  claudetalk converse --to kruz-claude "What is sessionStore used for?"
  claudetalk converse --to alice-claude --conv <id> --done "It's a session cache..."`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagRoom == "" {
				return fmt.Errorf("room is required (use -r or CLAUDETALK_ROOM)")
			}
			if flagSender == "" {
				return fmt.Errorf("sender name is required (use -n or CLAUDETALK_SENDER)")
			}
			if to == "" {
				return fmt.Errorf("--to is required (recipient name)")
			}
			if len(args) == 0 {
				return fmt.Errorf("message is required")
			}

			message := strings.Join(args, " ")

			// Generate conv_id if not provided.
			if convID == "" {
				convID = uuid.New().String()
			}

			expectingReply := "true"
			if done {
				expectingReply = "false"
			}

			metadata := map[string]string{
				"to":              to,
				"conv_id":         convID,
				"expecting_reply": expectingReply,
			}

			req := protocol.SendRequest{
				Sender:   flagSender,
				Type:     protocol.TypeText,
				Payload:  protocol.NewTextPayload(message),
				Metadata: metadata,
			}

			env, err := postMessage(flagServer, flagRoom, req)
			if err != nil {
				return err
			}

			// Print conv_id to stdout for Claude to capture.
			fmt.Println(convID)

			// Status to stderr.
			fmt.Fprintf(os.Stderr, "sent conversation message #%d to %s in room %q\n", env.SeqNum, to, env.Room)
			if done {
				fmt.Fprintf(os.Stderr, "conversation marked as complete\n")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&to, "to", "", "recipient name (required)")
	cmd.Flags().StringVar(&convID, "conv", "", "conversation ID (auto-generated if omitted)")
	cmd.Flags().BoolVar(&done, "done", false, "mark conversation as complete (no reply expected)")

	return cmd
}
