package health

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/sikhlana/pkg/http/httpserver"
)

// ServerName names the health server; a command runs it with httpserver.ProvideRunner(ServerName).
const ServerName = "health"

// Params is what the server's spec takes from the graph.
type Params struct {
	fx.In

	Config Config
	Logger *slog.Logger
}

// Module provides the health server, built only in the commands that run it.
var Module = fx.Module("health",
	httpserver.ProvideServer(ServerName, newSpec),
)

func newSpec(p Params) httpserver.Spec {
	return httpserver.Spec{Config: p.Config.Server, Handler: newRouter(p.Logger)}
}
