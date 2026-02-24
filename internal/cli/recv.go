package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/corvino/claudetalk/internal/protocol"
	"github.com/spf13/cobra"
)

func newRecvCmd() *cobra.Command {
	var (
		after  int64
		limit  int
		latest int
		format string
	)

	cmd := &cobra.Command{
		Use:   "recv",
		Short: "Receive messages from a room",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagRoom == "" {
				return fmt.Errorf("room is required (use -r or CLAUDETALK_ROOM)")
			}

			var list *protocol.MessageList
			var err error
			if latest > 0 {
				list, err = getLatestMessages(flagServer, flagRoom, latest)
			} else {
				list, err = getMessages(flagServer, flagRoom, after, limit)
			}
			if err != nil {
				return err
			}

			return printMessages(list, format)
		},
	}

	cmd.Flags().Int64Var(&after, "after", 0, "return messages after this sequence number")
	cmd.Flags().IntVar(&limit, "limit", 100, "max messages to return")
	cmd.Flags().IntVar(&latest, "latest", 0, "return the N most recent messages")
	cmd.Flags().StringVar(&format, "format", "plain", "output format: plain, json")

	return cmd
}

func printMessages(list *protocol.MessageList, format string) error {
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(list)
	}

	if list.Count == 0 {
		fmt.Println("no messages")
		return nil
	}

	for _, env := range list.Messages {
		fmt.Println(formatPlain(env))
	}
	return nil
}
