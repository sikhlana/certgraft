package upstream

import "go.uber.org/fx"

// Module provides the Client.
var Module = fx.Module("upstream",
	fx.Provide(New),
)
