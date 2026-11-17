package patch_test

import (
	"archive/tar"
	"bytes"
	"crypto/x509"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/certgraft/internal/patch"
	"github.com/sikhlana/certgraft/internal/roots"
	"github.com/sikhlana/certgraft/internal/roots/rootstest"
)

type entry struct {
	name, body, link string
	typeflag         byte
}

func reg(name, body string) entry   { return entry{name: name, body: body, typeflag: tar.TypeReg} }
func dir(name string) entry         { return entry{name: name, typeflag: tar.TypeDir} }
func symlink(name, to string) entry { return entry{name: name, link: to, typeflag: tar.TypeSymlink} }

func layer(t *testing.T, entries ...entry) v1.Layer {
	t.Helper()

	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)

	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typeflag, Linkname: e.link, Mode: 0o644, Size: int64(len(e.body))}

		if e.typeflag == tar.TypeDir {
			hdr.Mode = 0o755
		}

		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(e.body))
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())

	data := buf.Bytes()

	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
	require.NoError(t, err)

	return l
}

func image(t *testing.T, env []string, layers ...v1.Layer) v1.Image {
	t.Helper()

	img, err := mutate.AppendLayers(empty.Image, layers...)
	require.NoError(t, err)

	img, err = mutate.Config(img, v1.Config{Env: env})
	require.NoError(t, err)

	return img
}

// files returns the patched image's merged filesystem, read by go-containerregistry rather than the patcher.
func files(t *testing.T, img v1.Image) map[string]entry {
	t.Helper()

	rc := mutate.Extract(img)
	defer rc.Close()

	out := map[string]entry{}
	tr := tar.NewReader(rc)

	for {
		hdr, err := tr.Next()

		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		body, err := io.ReadAll(tr)
		require.NoError(t, err)

		out[strings.TrimSuffix(hdr.Name, "/")] = entry{
			name:     hdr.Name,
			body:     string(body),
			link:     hdr.Linkname,
			typeflag: hdr.Typeflag,
		}
	}

	return out
}

type fixture struct {
	upstream rootstest.CA
	extra    rootstest.CA
	set      *roots.Set
	patcher  *patch.Patcher
}

func newFixture(t *testing.T) fixture {
	t.Helper()

	f := fixture{upstream: rootstest.NewCA(t, "Upstream Root"), extra: rootstest.NewCA(t, "Proxy Root")}
	f.set = roots.New([]*x509.Certificate{f.extra.Cert})
	f.patcher = patch.New(patch.Params{Roots: f.set})

	return f
}

func (f fixture) patch(t *testing.T, img v1.Image) patch.Result {
	t.Helper()

	res, err := f.patcher.Patch(img)
	require.NoError(t, err)

	return res
}

func config(t *testing.T, img v1.Image) v1.Config {
	t.Helper()

	cf, err := img.ConfigFile()
	require.NoError(t, err)

	return cf.Config
}

func layerCount(t *testing.T, img v1.Image) int {
	t.Helper()

	ls, err := img.Layers()
	require.NoError(t, err)

	return len(ls)
}

func TestDebianBundle(t *testing.T) {
	f := newFixture(t)
	img := image(t, nil, layer(t,
		reg("bin/sh", "x"), reg("etc/debian_version", "12"),
		reg("etc/ssl/certs/ca-certificates.crt", string(f.upstream.PEM())),
	))

	res := f.patch(t, img)
	require.Equal(t, patch.Patched, res.Outcome)

	fs := files(t, res.Image)
	assert.Equal(t, string(f.upstream.PEM())+string(f.extra.PEM()), fs["etc/ssl/certs/ca-certificates.crt"].body)
	assert.Equal(t, string(f.extra.PEM()), fs["usr/local/share/ca-certificates/certgraft-1.crt"].body)
	assert.Equal(t, layerCount(t, img)+1, layerCount(t, res.Image))
	assert.Equal(t, f.set.Fingerprint(), config(t, res.Image).Labels[patch.LabelPatched])
}

func TestAmazonLinuxBundleThroughSymlink(t *testing.T) {
	f := newFixture(t)
	target := "etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem"
	img := image(t, nil, layer(t,
		reg("bin/sh", "x"), reg("etc/system-release", "Amazon Linux release 2023"),
		reg(target, string(f.upstream.PEM())),
		symlink("etc/pki/tls/certs/ca-bundle.crt", "../../ca-trust/extracted/pem/tls-ca-bundle.pem"),
	))

	res := f.patch(t, img)
	require.Equal(t, patch.Patched, res.Outcome)

	fs := files(t, res.Image)
	assert.True(t, strings.HasSuffix(fs[target].body, string(f.extra.PEM())))
	assert.Equal(t, byte(tar.TypeSymlink), fs["etc/pki/tls/certs/ca-bundle.crt"].typeflag, "the link stays a link")
	assert.Equal(t, string(f.extra.PEM()), fs["etc/pki/ca-trust/source/anchors/certgraft-1.pem"].body)
	assert.NotContains(t, fs, "etc/ssl/certs/ca-certificates.crt", "no second bundle beside the distro's own")
}

func TestImageWithoutBundle(t *testing.T) {
	f := newFixture(t)

	res := f.patch(t, image(t, nil, layer(t, reg("bin/sh", "busybox"), reg("etc/passwd", "root:x:0:0"))))
	require.Equal(t, patch.Patched, res.Outcome)

	fs := files(t, res.Image)
	assert.Equal(t, string(f.extra.PEM()), fs["etc/ssl/certs/ca-certificates.crt"].body)
	assert.NotContains(t, fs, "usr/local/share/ca-certificates/certgraft-1.crt", "no distro tooling to feed")
}

func TestWhiteoutOverBundle(t *testing.T) {
	for name, wh := range map[string]string{
		"file whiteout":  "etc/ssl/certs/.wh.ca-certificates.crt",
		"opaque dir":     "etc/ssl/certs/.wh..wh..opq",
		"dir whiteout":   "etc/ssl/.wh.certs",
		"root-level dir": "etc/.wh.ssl",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			img := image(t, nil,
				layer(t, reg("bin/sh", "x"), reg("etc/ssl/certs/ca-certificates.crt", string(f.upstream.PEM()))),
				layer(t, reg(wh, "")),
			)

			res := f.patch(t, img)
			require.Equal(t, patch.Patched, res.Outcome)

			assert.Equal(t, string(f.extra.PEM()), files(t, res.Image)["etc/ssl/certs/ca-certificates.crt"].body,
				"the deleted bundle's roots don't come back")
		})
	}
}

func TestLoneBinaryPassesThrough(t *testing.T) {
	f := newFixture(t)

	res := f.patch(t, image(t, nil, layer(t, reg("bin/dockerfile-frontend", "elf"))))
	assert.Equal(t, patch.PassedThrough, res.Outcome)
	assert.Nil(t, res.Image)
}

func TestFailedTrustPointServesUnpatched(t *testing.T) {
	f := newFixture(t)
	img := image(t, []string{"JAVA_HOME=/opt/java"}, layer(t,
		reg("bin/sh", "x"), reg("etc/ssl/certs/ca-certificates.crt", string(f.upstream.PEM())),
		reg("opt/java/lib/security/cacerts", "not a keystore"),
	))

	res := f.patch(t, img)
	require.Equal(t, patch.Failed, res.Outcome)

	assert.Equal(t, "java trust store /opt/java/lib/security/cacerts", res.TrustPoint)
	require.Error(t, res.Err)
	assert.Equal(t, patch.LabelFailed, config(t, res.Image).Labels[patch.LabelPatched])
	assert.Equal(t, layerCount(t, img), layerCount(t, res.Image), "no layer added")
	assert.Equal(t, string(f.upstream.PEM()), files(t, res.Image)["etc/ssl/certs/ca-certificates.crt"].body)
}

func TestImageEnvIsKept(t *testing.T) {
	f := newFixture(t)
	img := image(t, []string{"SSL_CERT_FILE=/opt/certs/bundle.pem", "PYTHON_VERSION=3.13.1"}, layer(t,
		reg("bin/sh", "x"), reg("etc/ssl/certs/ca-certificates.crt", string(f.upstream.PEM())),
		reg("opt/certs/bundle.pem", string(f.upstream.PEM())),
	))

	res := f.patch(t, img)
	require.Equal(t, patch.Patched, res.Outcome)

	env := config(t, res.Image).Env
	assert.Contains(t, env, "SSL_CERT_FILE=/opt/certs/bundle.pem")
	assert.Equal(t, 1, countPrefix(env, "SSL_CERT_FILE="), "never overwritten or duplicated")
	assert.True(t, strings.HasSuffix(files(t, res.Image)["opt/certs/bundle.pem"].body, string(f.extra.PEM())))
}

func TestNodeEnv(t *testing.T) {
	f := newFixture(t)

	res := f.patch(t, image(t, []string{"NODE_VERSION=22.11.0"}, layer(t,
		reg("bin/sh", "x"), reg("etc/alpine-release", "3.21"),
		reg("etc/ssl/certs/ca-certificates.crt", string(f.upstream.PEM())),
	)))
	require.Equal(t, patch.Patched, res.Outcome)

	assert.Contains(t, config(t, res.Image).Env, "NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt")
}

func TestPython(t *testing.T) {
	want, err := os.ReadFile("sitecustomize.py")
	require.NoError(t, err)

	base := []entry{
		reg("bin/sh", "x"), reg("etc/ssl/certs/ca-certificates.crt", "x"),
		dir("usr/local/lib/python3.13/site-packages/"),
	}

	t.Run("writes sitecustomize and env", func(t *testing.T) {
		f := newFixture(t)

		res := f.patch(t, image(t, []string{"PYTHON_VERSION=3.13.1"}, layer(t, base...)))
		require.Equal(t, patch.Patched, res.Outcome)

		cfg := config(t, res.Image)

		for _, kv := range []string{
			"SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt",
			"REQUESTS_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt",
			"PIP_CERT=/etc/ssl/certs/ca-certificates.crt",
			"UV_SYSTEM_CERTS=1",
		} {
			assert.Contains(t, cfg.Env, kv)
		}

		assert.Equal(
			t,
			string(want),
			files(t, res.Image)["usr/local/lib/python3.13/site-packages/sitecustomize.py"].body,
		)
		assert.NotContains(t, cfg.Labels, patch.LabelPythonStrict)
	})

	t.Run("keeps an existing sitecustomize", func(t *testing.T) {
		f := newFixture(t)
		own := reg("usr/local/lib/python3.13/site-packages/sitecustomize.py", "# the image's own\n")

		res := f.patch(t, image(t, []string{"PYTHON_VERSION=3.13.1"}, layer(t, append(base, own)...)))
		require.Equal(t, patch.Patched, res.Outcome)

		assert.Equal(t, own.body, files(t, res.Image)[own.name].body)
		assert.Equal(t, "skipped", config(t, res.Image).Labels[patch.LabelPythonStrict])
	})
}

func countPrefix(list []string, prefix string) int {
	n := 0

	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}

	return n
}
