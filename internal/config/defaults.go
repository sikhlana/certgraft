package config

import (
	"github.com/sikhlana/pkg/http/httpserver"

	"github.com/sikhlana/certgraft/internal/health"
	"github.com/sikhlana/certgraft/internal/registry"
	"github.com/sikhlana/certgraft/internal/roots"
	"github.com/sikhlana/certgraft/internal/store"
)

// defaults returns the values an operator inherits when they set nothing.
func defaults() Config {
	return Config{
		Registry: registry.Config{
			Server: httpserver.Config{Port: registry.DefaultPort, WriteTimeout: registry.DefaultWriteTimeout},
		},
		Health: health.Config{Server: httpserver.Config{Port: health.DefaultPort}},
		Roots:  roots.Config{Dir: roots.DefaultDir},
		Store:  store.Config{Dir: store.DefaultDir},
	}
}
