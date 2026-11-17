// Package cmd wires the command tree. Commands only pick the runners their app runs; behaviour lives in internal/.
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/sikhlana/pkg/cli"

	"github.com/sikhlana/certgraft/internal/app"
)

// Root returns the program's command tree.
func Root() *cobra.Command {
	k := app.New()

	root := k.RootCommand(app.Name, "Serve upstream container images with extra CA roots added")
	root.AddCommand(
		cli.NewVersionCommand(),
		newServeCommand(k),
		newPruneCommand(k),
	)

	return root
}
