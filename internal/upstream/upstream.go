// Package upstream is the only way out of the registry: it maps request names onto the allowlisted registries and
// fetches from them anonymously, trusting the extra roots as well as the system ones.
package upstream

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"go.uber.org/fx"

	"github.com/sikhlana/certgraft/internal/roots"
)

// DockerHub is the registry a name without a registry host belongs to.
const DockerHub = "docker.io"

// ErrNotAllowed is a request naming a registry outside the allowlist.
var ErrNotAllowed = errors.New("upstream registry not on the allowlist")

// Allowed reports whether host is a registry the registry fetches from. Adding one is a line here and a rule in the
// source policy.
func Allowed(host string) bool {
	switch host {
	case DockerHub, "gcr.io", "ghcr.io", "quay.io", "mcr.microsoft.com":
		return true
	default:
		return false
	}
}

// ParseRepository maps a request's repository name onto its upstream repository. A first segment that looks like a
// host (it has a dot or a port) names the registry and must be allowlisted; any other name is a Docker Hub repository,
// which is what a registry mirror request always is.
func ParseRepository(n string) (name.Repository, error) {
	host, _, _ := strings.Cut(n, "/")

	if !strings.ContainsAny(host, ".:") {
		n = DockerHub + "/" + n
	} else if !Allowed(host) {
		return name.Repository{}, fmt.Errorf("%s: %w", host, ErrNotAllowed)
	}

	repo, err := name.NewRepository(n)
	if err != nil {
		return name.Repository{}, fmt.Errorf("parsing repository: %w", err)
	}

	return repo, nil
}

// Unreachable reports whether err means upstream couldn't give an answer: no connection, a server error or a rate
// limit. A registry that answers with a client error (an unknown tag, say) is reachable.
func Unreachable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}

	if te, ok := errors.AsType[*transport.Error](err); ok {
		return te.StatusCode >= http.StatusInternalServerError || te.StatusCode == http.StatusTooManyRequests
	}

	return true
}

// DialFunc opens a connection; tests use it to route every upstream host to a local server.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Params is what New takes from the graph.
type Params struct {
	fx.In

	Roots *roots.Set
	Dial  DialFunc `optional:"true"`
}

// Client fetches from the allowlisted registries.
type Client struct {
	transport http.RoundTripper
}

// New returns a Client whose TLS trusts the system roots plus the extra roots: the registry itself pulls through
// the proxy that re-signs TLS.
func New(p Params) (*Client, error) {
	pool, err := p.Roots.Pool()
	if err != nil {
		return nil, err
	}

	dial := p.Dial

	if dial == nil {
		dial = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}

	return &Client{transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dial,
		TLSClientConfig:       &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}}, nil
}

// Head returns the descriptor ref currently points at, without counting as a pull.
func (c *Client) Head(ctx context.Context, ref name.Reference) (*v1.Descriptor, error) {
	d, err := remote.Head(ref, c.options(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("checking %s upstream: %w", ref, err)
	}

	return d, nil
}

// Get fetches ref's manifest. For a digest reference, the content is verified against the digest.
func (c *Client) Get(ctx context.Context, ref name.Reference) (*remote.Descriptor, error) {
	d, err := remote.Get(ref, c.options(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("fetching %s upstream: %w", ref, err)
	}

	return d, nil
}

// Layer returns the blob ref names, fetched lazily.
func (c *Client) Layer(ctx context.Context, ref name.Digest) (v1.Layer, error) {
	l, err := remote.Layer(ref, c.options(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("fetching blob %s upstream: %w", ref, err)
	}

	return l, nil
}

func (c *Client) options(ctx context.Context) []remote.Option {
	return []remote.Option{
		remote.WithContext(ctx),
		remote.WithTransport(c.transport),
		remote.WithAuth(authn.Anonymous),
		remote.WithUserAgent("certgraft"),
	}
}
