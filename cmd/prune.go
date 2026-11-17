package cmd

import (
	"github.com/spf13/cobra"

	"github.com/sikhlana/pkg/kernel"

	"github.com/sikhlana/certgraft/internal/store"
)

func newPruneCommand(k kernel.App) *cobra.Command {
	days := store.DefaultPruneDays

	c := &cobra.Command{
		Use:   "prune",
		Short: "Remove cached patches not requested within --days days",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req := store.PruneRequest{Days: days, Out: cmd.OutOrStdout()}

			return k.Run(cmd, store.WithPruneRequest(req), store.ProvidePruneRunner())
		},
	}
	c.Flags().IntVar(&days, "days", days, "remove entries last requested more than this many days ago")

	return c
}
