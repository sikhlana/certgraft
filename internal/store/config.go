package store

// DefaultDir is the data volume's mount point in the container.
const DefaultDir = "/var/lib/certgraft"

// Config is where the cache lives.
type Config struct {
	// Dir is the cache directory; it persists across restarts.
	Dir string `mapstructure:"dir" validate:"required"`
}
