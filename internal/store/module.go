package store

import "go.uber.org/fx"

// Params is what the Store's provider takes from the graph.
type Params struct {
	fx.In

	Config Config
}

// Module provides the Store in Config.Dir.
var Module = fx.Module("store",
	fx.Provide(provide),
)

func provide(p Params) (*Store, error) {
	return Open(p.Config.Dir)
}
