package upstream_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/certgraft/internal/upstream"
)

func TestParseRepository(t *testing.T) {
	tests := []struct {
		in, registry, repo string
	}{
		{in: "library/alpine", registry: "index.docker.io", repo: "library/alpine"},
		{in: "docker.io/library/node", registry: "index.docker.io", repo: "library/node"},
		{in: "docker/dockerfile", registry: "index.docker.io", repo: "docker/dockerfile"},
		{in: "gcr.io/distroless/static", registry: "gcr.io", repo: "distroless/static"},
		{in: "ghcr.io/a/b/c", registry: "ghcr.io", repo: "a/b/c"},
		{in: "quay.io/x/y", registry: "quay.io", repo: "x/y"},
		{in: "mcr.microsoft.com/dotnet/sdk", registry: "mcr.microsoft.com", repo: "dotnet/sdk"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			r, err := upstream.ParseRepository(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.registry, r.RegistryStr())
			assert.Equal(t, tt.repo, r.RepositoryStr())
		})
	}
}

func TestParseRepositoryRejectsUnlisted(t *testing.T) {
	for _, in := range []string{"example.com/foo", "localhost:5000/foo", "registry-1.docker.io/library/x"} {
		t.Run(in, func(t *testing.T) {
			_, err := upstream.ParseRepository(in)
			require.ErrorIs(t, err, upstream.ErrNotAllowed)
		})
	}
}

func TestUnreachable(t *testing.T) {
	assert.False(t, upstream.Unreachable(nil))
	assert.True(t, upstream.Unreachable(errors.New("dial tcp: connection refused")))
	assert.True(t, upstream.Unreachable(&transport.Error{StatusCode: http.StatusBadGateway}))
	assert.True(t, upstream.Unreachable(&transport.Error{StatusCode: http.StatusTooManyRequests}))
	assert.False(t, upstream.Unreachable(&transport.Error{StatusCode: http.StatusNotFound}))
}
