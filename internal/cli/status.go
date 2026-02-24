package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check server health",
		RunE: func(cmd *cobra.Command, args []string) error {
			health, err := getHealth(flagServer)
			if err != nil {
				return fmt.Errorf("server unreachable: %w", err)
			}

			fmt.Printf("Status:  %s\n", health.Status)
			fmt.Printf("Uptime:  %s\n", health.Uptime)
			fmt.Printf("Rooms:   %d\n", health.Rooms)
			return nil
		},
	}
}
