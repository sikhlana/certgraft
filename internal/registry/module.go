package registry

import (
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/http/httpserver"
)

// Module provides the registry server, its handler and the service behind it.
var Module = fx.Module("registry",
	fx.Provide(NewService, NewHandler),
	httpserver.ProvideServer(ServerName, newSpec),
	httpserver.ProvideMiddleware(ServerName, newTracing),
)
