package roots

import "go.uber.org/fx"

// Params is what the Set's provider takes from the graph.
type Params struct {
	fx.In

	Config Config
}

// Module provides the Set loaded from Config.Dir; a missing or certificate-less file stops the app.
var Module = fx.Module("roots",
	fx.Provide(provide),
)

func provide(p Params) (*Set, error) {
	return Load(p.Config.Dir)
}
