package patch

import "go.uber.org/fx"

// Module provides the Patcher.
var Module = fx.Module("patch",
	fx.Provide(New),
)
