package roots_test

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/certgraft/internal/roots"
	"github.com/sikhlana/certgraft/internal/roots/rootstest"
)

func TestLoad(t *testing.T) {
	a := rootstest.NewCA(t, "a")
	b := rootstest.NewCA(t, "b")

	set, err := roots.Load(rootstest.WriteDir(t, a, b))
	require.NoError(t, err)

	assert.Len(t, set.Certificates(), 2)
	assert.Len(t, set.PEM(), 2)
	assert.Len(t, set.Fingerprint(), 64)
}

func TestFingerprintIgnoresOrderAndDuplicates(t *testing.T) {
	a := rootstest.NewCA(t, "a")
	b := rootstest.NewCA(t, "b")

	ab := roots.New([]*x509.Certificate{a.Cert, b.Cert})
	ba := roots.New([]*x509.Certificate{b.Cert, a.Cert, b.Cert})
	onlyA := roots.New([]*x509.Certificate{a.Cert})

	assert.Equal(t, ab.Fingerprint(), ba.Fingerprint())
	assert.Len(t, ba.Certificates(), 2)
	assert.NotEqual(t, ab.Fingerprint(), onlyA.Fingerprint())
}

func TestLoadRejects(t *testing.T) {
	t.Run("file without a certificate", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "x.pem"), []byte("not a cert\n"), 0o600))

		_, err := roots.Load(dir)
		require.ErrorIs(t, err, roots.ErrNoCertificate)
	})

	t.Run("empty directory", func(t *testing.T) {
		_, err := roots.Load(t.TempDir())
		require.ErrorIs(t, err, roots.ErrNoRoots)
	})

	t.Run("missing directory", func(t *testing.T) {
		_, err := roots.Load(filepath.Join(t.TempDir(), "absent"))
		require.Error(t, err)
	})
}
