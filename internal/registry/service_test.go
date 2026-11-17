package registry_test

import (
	"crypto/x509"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/certgraft/internal/patch"
	"github.com/sikhlana/certgraft/internal/roots"
	"github.com/sikhlana/certgraft/internal/roots/rootstest"
)

func TestUnlistedUpstreamMakesNoConnection(t *testing.T) {
	u := newFakeUpstream(t)
	l := newLocal(t, u, t.TempDir())

	status, body := l.raw(t, "/v2/example.com/foo/manifests/latest")

	assert.Equal(t, http.StatusNotFound, status)
	assert.Contains(t, body, `"code":"NAME_UNKNOWN"`)
	assert.Zero(t, u.dials.Load(), "no connection to example.com or anywhere else")
}

func TestPingAndUnknownPaths(t *testing.T) {
	l := newLocal(t, newFakeUpstream(t), t.TempDir())

	status, _ := l.raw(t, "/v2/")
	assert.Equal(t, http.StatusOK, status)

	status, body := l.raw(t, "/v2/docker.io/library/x/tags/list")
	assert.Equal(t, http.StatusNotFound, status)
	assert.Contains(t, body, "UNSUPPORTED")
}

func TestBothURLShapes(t *testing.T) {
	u := newFakeUpstream(t)
	u.push(t, "index.docker.io/library/alpine:3.21", busybox(t, "alpine"))
	u.push(t, "gcr.io/distroless/static:nonroot", busybox(t, "distroless"))

	l := newLocal(t, u, t.TempDir())

	policy := l.get(t, "docker.io/library/alpine:3.21")
	mirror := l.get(t, "library/alpine:3.21")
	assert.Equal(t, policy.Digest, mirror.Digest, "the policy and mirror shapes are the same Hub image")

	img := l.image(t, "gcr.io/distroless/static:nonroot")
	assert.Equal(t, string(u.ca.PEM()), extractFile(t, img, "etc/ssl/certs/ca-certificates.crt"))
}

func TestPassThroughIsByteIdentical(t *testing.T) {
	u := newFakeUpstream(t)
	frontend := fixtureImage(t, map[string]string{"bin/dockerfile-frontend": "elf"})
	want := u.push(t, "index.docker.io/docker/dockerfile:1", frontend)

	dir := t.TempDir()
	l := newLocal(t, u, dir)

	d := l.get(t, "docker.io/docker/dockerfile:1")
	assert.Equal(t, want, d.Digest)

	upRaw, err := frontend.RawManifest()
	require.NoError(t, err)
	assert.Equal(t, upRaw, d.Manifest)

	img, err := d.Image()
	require.NoError(t, err)
	assert.Equal(t, "elf", extractFile(t, img, "bin/dockerfile-frontend"), "blobs stream from upstream")

	blobs, err := os.ReadDir(filepath.Join(dir, "blobs", "sha256"))
	require.NoError(t, err)
	assert.Empty(t, blobs, "nothing stored for a pass-through image")
}

func TestIndexEveryPlatformPatched(t *testing.T) {
	u := newFakeUpstream(t)
	amd, arm := busybox(t, "amd64"), busybox(t, "arm64")
	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: amd, Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}},
		mutate.IndexAddendum{Add: arm, Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}},
		mutate.IndexAddendum{
			Add:         busybox(t, "attestation"),
			Platform:    &v1.Platform{OS: "unknown", Architecture: "unknown"},
			Annotations: map[string]string{"vnd.docker.reference.type": "attestation-manifest"},
		},
	)
	u.pushIndex(t, "index.docker.io/library/node:22-alpine", idx)

	l := newLocal(t, u, t.TempDir())

	got, err := l.get(t, "docker.io/library/node:22-alpine").ImageIndex()
	require.NoError(t, err)

	im, err := got.IndexManifest()
	require.NoError(t, err)
	require.Len(t, im.Manifests, 2, "same platforms, attestation dropped")

	for i, upImg := range []v1.Image{amd, arm} {
		desc := im.Manifests[i]
		assert.Equal(t, []string{"amd64", "arm64"}[i], desc.Platform.Architecture)

		img, imgErr := got.Image(desc.Digest)
		require.NoError(t, imgErr)

		base := layerDigests(t, upImg)
		patched := layerDigests(t, img)
		require.Len(t, patched, len(base)+1)
		assert.Equal(t, base, patched[:len(base)], "upstream layers plus one")

		assert.Equal(t, string(u.ca.PEM()), extractFile(t, img, "etc/ssl/certs/ca-certificates.crt"))

		cf, cfErr := img.ConfigFile()
		require.NoError(t, cfErr)
		assert.Equal(t, l.set.Fingerprint(), cf.Config.Labels[patch.LabelPatched])
	}
}

func TestPatchFailureServesUnpatchedAndLogsOnce(t *testing.T) {
	u := newFakeUpstream(t)
	upImg := fixtureImage(t, map[string]string{
		"bin/sh":                        "x",
		"opt/java/lib/security/cacerts": "not a keystore",
	}, "JAVA_HOME=/opt/java")
	u.push(t, "index.docker.io/library/eclipse-temurin:25-jre-alpine", upImg)

	l := newLocal(t, u, t.TempDir())
	img := l.image(t, "docker.io/library/eclipse-temurin:25-jre-alpine")

	cf, err := img.ConfigFile()
	require.NoError(t, err)
	assert.Equal(t, patch.LabelFailed, cf.Config.Labels[patch.LabelPatched])
	assert.Equal(t, layerDigests(t, upImg), layerDigests(t, img), "served unpatched")

	assert.Equal(t, 1, l.logs.count("patching failed"))
	assert.Equal(t, 1, l.logs.count(`"trust_point":"java trust store /opt/java/lib/security/cacerts"`))
	assert.Equal(t, 1, l.logs.count("library/eclipse-temurin@sha256:"), "the line names the image")
}

func TestDigestPin(t *testing.T) {
	u := newFakeUpstream(t)
	upImg := busybox(t, "pinned")
	h := u.push(t, "index.docker.io/library/node:22-alpine", upImg)

	l := newLocal(t, u, t.TempDir())

	t.Run("known digest", func(t *testing.T) {
		img := l.image(t, "docker.io/library/node:sha256-"+h.Hex)

		base := layerDigests(t, upImg)
		assert.Equal(t, base, layerDigests(t, img)[:len(base)])
		assert.Len(t, layerDigests(t, img), len(base)+1)
	})

	t.Run("unknown digest", func(t *testing.T) {
		missing := strings.Repeat("ab", 32)
		status, body := l.raw(t, "/v2/docker.io/library/node/manifests/sha256-"+missing)

		assert.Equal(t, http.StatusNotFound, status)
		assert.Contains(t, body, "MANIFEST_UNKNOWN")
		assert.Contains(t, body, "sha256:"+missing)
	})
}

func TestMovedTagServesNewDigest(t *testing.T) {
	u := newFakeUpstream(t)
	v1img, v2img := busybox(t, "v1"), busybox(t, "v2")
	u.push(t, "quay.io/x/app:latest", v1img)

	l := newLocal(t, u, t.TempDir())
	first := l.get(t, "quay.io/x/app:latest")

	u.push(t, "quay.io/x/app:latest", v2img)
	second := l.get(t, "quay.io/x/app:latest")

	assert.NotEqual(t, first.Digest, second.Digest)

	img, err := second.Image()
	require.NoError(t, err)
	assert.Equal(t, layerDigests(t, v2img)[0], layerDigests(t, img)[0], "a patch of the new digest")
}

func TestUpstreamUnreachable(t *testing.T) {
	u := newFakeUpstream(t)
	u.push(t, "ghcr.io/a/b:1", busybox(t, "b"))
	u.push(t, "ghcr.io/a/c:1", busybox(t, "c"))

	l := newLocal(t, u, t.TempDir())
	cached := l.get(t, "ghcr.io/a/b:1")

	u.down.Store(true)

	t.Run("with a cached patch", func(t *testing.T) {
		d := l.get(t, "ghcr.io/a/b:1")
		assert.Equal(t, cached.Digest, d.Digest)
		assert.Equal(t, 1, l.logs.count("upstream unreachable"), "one degraded-mode line")
	})

	t.Run("without one", func(t *testing.T) {
		status, _ := l.raw(t, "/v2/ghcr.io/a/c/manifests/1")
		assert.Equal(t, http.StatusBadGateway, status, "upstream's failure reaches the client")
	})
}

func TestRootSetChangeRepatches(t *testing.T) {
	u := newFakeUpstream(t)
	u.push(t, "mcr.microsoft.com/dotnet/runtime:9.0", busybox(t, "dotnet"))

	dir := t.TempDir()
	before := newLocal(t, u, dir).get(t, "mcr.microsoft.com/dotnet/runtime:9.0")

	after := newLocal(t, u, dir, rootstest.NewCA(t, "Another Root"))
	d := after.get(t, "mcr.microsoft.com/dotnet/runtime:9.0")
	assert.NotEqual(t, before.Digest, d.Digest)

	img, err := d.Image()
	require.NoError(t, err)

	cf, err := img.ConfigFile()
	require.NoError(t, err)
	assert.Equal(t, after.set.Fingerprint(), cf.Config.Labels[patch.LabelPatched])
}

func TestUpstreamTLSNeedsTheExtraRoot(t *testing.T) {
	u := newFakeUpstream(t)
	u.push(t, "gcr.io/distroless/static:nonroot", busybox(t, "x"))

	other := roots.New([]*x509.Certificate{rootstest.NewCA(t, "Unrelated Root").Cert})
	l := newLocalWithRoots(t, u, t.TempDir(), other)

	status, body := l.raw(t, "/v2/gcr.io/distroless/static/manifests/nonroot")
	assert.Equal(t, http.StatusBadGateway, status, "the proxy-signed upstream is untrusted without its root")
	assert.Contains(t, body, "certificate")
}
