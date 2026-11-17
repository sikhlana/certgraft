// Package app assembles the program's fx graph. Every command builds its app from New, so they all share one graph
// and differ only in the runners they opt in to.
package app

import (
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/cli"
	"github.com/sikhlana/pkg/http/httpserver"
	"github.com/sikhlana/pkg/kernel"
	"github.com/sikhlana/pkg/log"
	"github.com/sikhlana/pkg/telemetry"
	"github.com/sikhlana/pkg/telemetry/trace"

	"github.com/sikhlana/certgraft/internal/config"
	"github.com/sikhlana/certgraft/internal/health"
	"github.com/sikhlana/certgraft/internal/patch"
	"github.com/sikhlana/certgraft/internal/registry"
	"github.com/sikhlana/certgraft/internal/roots"
	"github.com/sikhlana/certgraft/internal/store"
	"github.com/sikhlana/certgraft/internal/upstream"
)

// Name is the program's name: its root command, its config file and env prefix, and its telemetry service name.
const Name = "certgraft"

// New returns the program's kernel. Its Command and Run are the one way every command runs an app.
func New() kernel.App {
	return kernel.New(kernel.Options{
		Format: log.FormatJSON,
		Config: Config,
		Shared: Modules(),
	})
}

// Config loads the operator config, given --config's path, empty for the search.
func Config(path string) fx.Option {
	return config.Provide(Name, path)
}

// Modules returns the graph every command shares, in three tiers.
func Modules() fx.Option {
	return fx.Options(
		// Infrastructure.
		telemetry.Module,
		trace.Module,
		telemetry.WithServiceName(Name),
		telemetry.WithServiceVersion(cli.ReadBuildInfo().Version),
		log.WithMiddleware(telemetry.LogMiddleware{}),
		httpserver.Module,
		roots.Module,
		store.Module,
		upstream.Module,

		// Domain.
		patch.Module,

		// Service.
		health.Module,
		registry.Module,
	)
}
