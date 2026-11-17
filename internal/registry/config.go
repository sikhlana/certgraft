package registry

import (
	"time"

	"github.com/sikhlana/pkg/http/httpserver"
)

const (
	// DefaultPort is the registry server's port inside the container; the host publishes it on 127.0.0.1:5555.
	DefaultPort = 8080

	// DefaultWriteTimeout bounds one registry response. A first request for an image patches it, which reads every
	// upstream layer, and blobs stream from upstream, so httpserver's default is far too short.
	DefaultWriteTimeout = 30 * time.Minute
)

// Config is the registry server's operator config.
type Config struct {
	Server httpserver.Config `mapstructure:"server"`
}
