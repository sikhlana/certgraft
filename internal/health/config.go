package health

import "github.com/sikhlana/pkg/http/httpserver"

// DefaultPort is the health server's port. The container doesn't publish it.
const DefaultPort = 9090

// Config is the health server's operator config.
type Config struct {
	Server httpserver.Config `mapstructure:"server"`
}
