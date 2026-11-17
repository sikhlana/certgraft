package registry_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/certgraft/internal/patch"
	"github.com/sikhlana/certgraft/internal/registry"
	"github.com/sikhlana/certgraft/internal/roots"
	"github.com/sikhlana/certgraft/internal/roots/rootstest"
	"github.com/sikhlana/certgraft/internal/store"
	"github.com/sikhlana/certgraft/internal/upstream"
)

var upstreamHosts = []string{
	"index.docker.io", "registry-1.docker.io", "gcr.io", "ghcr.io", "quay.io", "mcr.microsoft.com", "example.com",
}

// fakeUpstream is one in-memory registry answering, over TLS, for every upstream host. Its certificate is signed
// by a test CA that stands in for the proxy's root.
type fakeUpstream struct {
	ca    rootstest.CA
	srv   *httptest.Server
	dials atomic.Int64
	down  atomic.Bool
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()

	u := &fakeUpstream{ca: rootstest.NewCA(t, "Proxy Root")}
	reg := ggcrregistry.New()
	u.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// While down, drop every connection without an answer, including kept-alive ones.
		if u.down.Load() {
			if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
				_ = conn.Close()
			}

			return
		}

		reg.ServeHTTP(w, r)
	}))
	u.srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{u.ca.ServerCert(t, upstreamHosts...)},
		MinVersion:   tls.VersionTLS12,
	}
	u.srv.StartTLS()
	t.Cleanup(u.srv.Close)

	return u
}

// dial sends every connection to the fake upstream.
func (u *fakeUpstream) dial(ctx context.Context, network, _ string) (net.Conn, error) {
	u.dials.Add(1)

	var d net.Dialer

	return d.DialContext(ctx, network, u.srv.Listener.Addr().String())
}

// push writes img (or an index) to ref on the fake upstream.
func (u *fakeUpstream) push(t *testing.T, ref string, img v1.Image) v1.Hash {
	t.Helper()

	r, err := name.ParseReference(ref)
	require.NoError(t, err)
	require.NoError(t, remote.Write(r, img, u.options(t)...))

	h, err := img.Digest()
	require.NoError(t, err)

	return h
}

func (u *fakeUpstream) pushIndex(t *testing.T, ref string, idx v1.ImageIndex) v1.Hash {
	t.Helper()

	r, err := name.ParseReference(ref)
	require.NoError(t, err)
	require.NoError(t, remote.WriteIndex(r, idx, u.options(t)...))

	h, err := idx.Digest()
	require.NoError(t, err)

	return h
}

func (u *fakeUpstream) options(t *testing.T) []remote.Option {
	t.Helper()

	pool := x509.NewCertPool()
	pool.AddCert(u.ca.Cert)

	return []remote.Option{remote.WithContext(t.Context()), remote.WithTransport(&http.Transport{
		DialContext:     u.dial,
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	})}
}

// local is the registry under test, served over plain HTTP on loopback.
type local struct {
	srv   *httptest.Server
	logs  *syncBuffer
	set   *roots.Set
	store string
}

func newLocal(t *testing.T, u *fakeUpstream, storeDir string, extra ...rootstest.CA) *local {
	t.Helper()

	certs := []*x509.Certificate{u.ca.Cert}

	for _, ca := range extra {
		certs = append(certs, ca.Cert)
	}

	return newLocalWithRoots(t, u, storeDir, roots.New(certs))
}

func newLocalWithRoots(t *testing.T, u *fakeUpstream, storeDir string, set *roots.Set) *local {
	t.Helper()

	up, err := upstream.New(upstream.Params{Roots: set, Dial: u.dial})
	require.NoError(t, err)

	st, err := store.Open(storeDir)
	require.NoError(t, err)

	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))

	svc := registry.NewService(registry.ServiceParams{
		Upstream: up, Store: st, Patcher: patch.New(patch.Params{Roots: set}), Roots: set, Logger: logger,
	})
	h := registry.NewHandler(registry.HandlerParams{Service: svc, Logger: logger})

	srv := httptest.NewServer(registry.NewRouter(logger, h))
	t.Cleanup(srv.Close)

	return &local{srv: srv, logs: logs, set: set, store: storeDir}
}

func (l *local) ref(t *testing.T, ref string) name.Reference {
	t.Helper()

	r, err := name.ParseReference(strings.TrimPrefix(l.srv.URL, "http://")+"/"+ref, name.Insecure)
	require.NoError(t, err)

	return r
}

func (l *local) get(t *testing.T, ref string) *remote.Descriptor {
	t.Helper()

	d, err := remote.Get(l.ref(t, ref), remote.WithContext(t.Context()))
	require.NoError(t, err)

	return d
}

func (l *local) image(t *testing.T, ref string) v1.Image {
	t.Helper()

	img, err := remote.Image(l.ref(t, ref), remote.WithContext(t.Context()))
	require.NoError(t, err)

	return img
}

func (l *local) raw(t *testing.T, path string) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, l.srv.URL+path, nil)
	require.NoError(t, err)

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	return res.StatusCode, string(body)
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) count(substr string) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return strings.Count(b.buf.String(), substr)
}

// fixtureImage is a one-layer image holding files (path → content).
func fixtureImage(t *testing.T, files map[string]string, env ...string) v1.Image {
	t.Helper()

	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)

	for p, body := range files {
		require.NoError(
			t,
			tw.WriteHeader(&tar.Header{Name: p, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}),
		)
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())

	data := buf.Bytes()

	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
	require.NoError(t, err)

	img, err := mutate.AppendLayers(empty.Image, l)
	require.NoError(t, err)

	img, err = mutate.Config(img, v1.Config{Env: env})
	require.NoError(t, err)

	return img
}

func busybox(t *testing.T, marker string) v1.Image {
	t.Helper()

	return fixtureImage(t, map[string]string{"bin/sh": marker, "etc/passwd": "root:x:0:0"})
}

func layerDigests(t *testing.T, img v1.Image) []v1.Hash {
	t.Helper()

	ls, err := img.Layers()
	require.NoError(t, err)

	out := make([]v1.Hash, len(ls))

	for i, l := range ls {
		out[i], err = l.Digest()
		require.NoError(t, err)
	}

	return out
}

// extractFile reads one file from img's merged filesystem, fetching every blob through the registry.
func extractFile(t *testing.T, img v1.Image, path string) string {
	t.Helper()

	rc := mutate.Extract(img)
	defer rc.Close()

	tr := tar.NewReader(rc)

	for {
		hdr, err := tr.Next()

		if errors.Is(err, io.EOF) {
			return ""
		}

		require.NoError(t, err)

		if hdr.Name == path {
			body, readErr := io.ReadAll(tr)
			require.NoError(t, readErr)

			return string(body)
		}
	}
}
