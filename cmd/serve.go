package cmd

import (
	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/http/httpserver"
	"github.com/sikhlana/pkg/kernel"

	"github.com/sikhlana/certgraft/internal/health"
	"github.com/sikhlana/certgraft/internal/registry"
)

func newServeCommand(k kernel.App) *cobra.Command {
	return k.Command("serve", "Serve the registry and health endpoints", serveRunners())
}

// serveRunners opts serve in to running both servers. The graph test runs the same set.
func serveRunners() fx.Option {
	return fx.Options(
		httpserver.ProvideRunner(registry.ServerName),
		httpserver.ProvideRunner(health.ServerName),
	)
}
