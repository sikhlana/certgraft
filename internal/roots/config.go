package roots

// DefaultDir is where the container mounts the roots, read-only.
const DefaultDir = "/etc/certgraft/roots"

// Config is where the roots are read from.
type Config struct {
	// Dir holds one or more PEM files; every file must contain a certificate.
	Dir string `mapstructure:"dir" validate:"required"`
}
