package config

import (
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/config/configloader"
)

// Provide loads and validates Config from the defaults, the config file at path (empty searches for one) and the
// environment, whose names take name as their prefix.
func Provide(name, path string) fx.Option {
	return configloader.Provide(configloader.Options[Config]{
		Name:     name,
		Filename: path,
		Defaults: defaults(),
	})
}
