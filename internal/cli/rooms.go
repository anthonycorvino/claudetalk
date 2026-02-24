package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRoomsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rooms",
		Short: "List active rooms on the server",
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := getRooms(flagServer)
			if err != nil {
				return err
			}

			if len(list.Rooms) == 0 {
				fmt.Println("no active rooms")
				return nil
			}

			fmt.Printf("%-20s %8s %8s %8s\n", "ROOM", "CLIENTS", "MSGS", "LAST SEQ")
			for _, r := range list.Rooms {
				fmt.Printf("%-20s %8d %8d %8d\n", r.Name, r.Clients, r.MessageCount, r.LastSeq)
			}
			return nil
		},
	}
}
