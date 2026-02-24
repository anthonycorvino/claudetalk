package cli

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"

	"github.com/corvino/claudetalk/internal/protocol"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	var noColor bool

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch a room for live messages via WebSocket",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagRoom == "" {
				return fmt.Errorf("room is required (use -r or CLAUDETALK_ROOM)")
			}
			sender := flagSender
			if sender == "" {
				sender = "watcher"
			}

			// Build WebSocket URL from HTTP server URL.
			wsURL := buildWSURL(flagServer, flagRoom, sender)

			fmt.Fprintf(os.Stderr, "connecting to %s ...\n", wsURL)
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer conn.Close()
			fmt.Fprintf(os.Stderr, "connected to room %q as %q\n", flagRoom, sender)

			// Handle Ctrl+C.
			interrupt := make(chan os.Signal, 1)
			signal.Notify(interrupt, os.Interrupt)

			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					var env protocol.Envelope
					err := conn.ReadJSON(&env)
					if err != nil {
						if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
							log.Printf("read error: %v", err)
						}
						return
					}
					if noColor {
					fmt.Println(formatPlain(env))
				} else {
					fmt.Println(formatColor(env))
				}
				}
			}()

			select {
			case <-done:
				return nil
			case <-interrupt:
				fmt.Fprintln(os.Stderr, "\ndisconnecting...")
				err := conn.WriteMessage(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				)
				if err != nil {
					return err
				}
				return nil
			}
		},
	}

	cmd.Flags().BoolVar(&noColor, "no-color", false, "disable colored output (useful for piping/logging)")

	return cmd
}

func buildWSURL(server, room, sender string) string {
	// Convert http(s) to ws(s).
	u := strings.TrimRight(server, "/")
	u = strings.Replace(u, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)
	return fmt.Sprintf("%s/ws/%s?sender=%s", u, room, url.QueryEscape(sender))
}
