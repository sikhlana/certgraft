// Package config holds the operator config: every value an operator can change through the config file or the
// environment, and nothing else.
package config

import (
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/telemetry"
	"github.com/sikhlana/pkg/telemetry/trace"

	"github.com/sikhlana/certgraft/internal/health"
	"github.com/sikhlana/certgraft/internal/registry"
	"github.com/sikhlana/certgraft/internal/roots"
	"github.com/sikhlana/certgraft/internal/store"
)

// Config is the program's operator config. It embeds fx.Out, so each section reaches its module as that module's own
// Config type.
type Config struct {
	fx.Out

	Registry  registry.Config  `mapstructure:"registry"`
	Health    health.Config    `mapstructure:"health"`
	Roots     roots.Config     `mapstructure:"roots"`
	Store     store.Config     `mapstructure:"store"`
	Telemetry telemetry.Config `mapstructure:"telemetry"`
	Trace     trace.Config     `mapstructure:"trace"`
}
