package patch_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"testing"
	"time"

	keystore "github.com/pavlo-v-chernykh/keystore-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pkcs12 "software.sslmate.com/src/go-pkcs12"

	"github.com/sikhlana/certgraft/internal/patch"
)

const cacerts = "opt/java/openjdk/lib/security/cacerts"

func javaImage(t *testing.T, f fixture, store []byte) patch.Result {
	t.Helper()

	return f.patch(t, image(t, []string{"JAVA_HOME=/opt/java/openjdk"}, layer(t,
		reg("bin/sh", "x"), reg("etc/ssl/certs/ca-certificates.crt", string(f.upstream.PEM())),
		reg(cacerts, string(store)),
	)))
}

func TestPKCS12TrustStore(t *testing.T) {
	for name, tc := range map[string]struct {
		enc      *pkcs12.Encoder
		password string
	}{
		"changeit":      {enc: pkcs12.Modern2023, password: "changeit"},
		"password-less": {enc: pkcs12.Passwordless, password: ""},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)

			store, err := tc.enc.EncodeTrustStoreEntries([]pkcs12.TrustStoreEntry{
				{Cert: f.upstream.Cert, FriendlyName: "upstream [jdk]"},
			}, tc.password)
			require.NoError(t, err)

			res := javaImage(t, f, store)
			require.Equal(t, patch.Patched, res.Outcome)

			certs, err := pkcs12.DecodeTrustStore([]byte(files(t, res.Image)[cacerts].body), tc.password)
			require.NoError(t, err, "same format and password as upstream")
			assert.ElementsMatch(t, digests(f.upstream.Cert, f.extra.Cert), digests(certs...))
		})
	}
}

func TestJKSTrustStore(t *testing.T) {
	f := newFixture(t)

	ks := keystore.New()
	require.NoError(t, ks.SetTrustedCertificateEntry("upstream", keystore.TrustedCertificateEntry{
		CreationTime: time.Now(),
		Certificate:  keystore.Certificate{Type: "X509", Content: f.upstream.Cert.Raw},
	}))

	var store bytes.Buffer
	require.NoError(t, ks.Store(&store, []byte("changeit")))

	res := javaImage(t, f, store.Bytes())
	require.Equal(t, patch.Patched, res.Outcome)

	out := keystore.New()
	require.NoError(t, out.Load(bytes.NewReader([]byte(files(t, res.Image)[cacerts].body)), []byte("changeit")))

	var got [][32]byte

	for _, a := range out.Aliases() {
		e, err := out.GetTrustedCertificateEntry(a)
		require.NoError(t, err)

		got = append(got, sha256.Sum256(e.Certificate.Content))
	}

	assert.Contains(t, out.Aliases(), "upstream", "existing entries keep their alias")
	assert.ElementsMatch(t, digests(f.upstream.Cert, f.extra.Cert), got)
}

func TestTrustStoreAlreadyHoldingRootsIsUnchanged(t *testing.T) {
	f := newFixture(t)

	store, err := pkcs12.Modern2023.WithRand(rand.Reader).EncodeTrustStore(
		[]*x509.Certificate{f.upstream.Cert, f.extra.Cert}, "changeit")
	require.NoError(t, err)

	res := javaImage(t, f, store)
	require.Equal(t, patch.Patched, res.Outcome)

	assert.Equal(t, string(store), files(t, res.Image)[cacerts].body)
}

func digests(certs ...*x509.Certificate) [][32]byte {
	out := make([][32]byte, len(certs))

	for i, c := range certs {
		out[i] = sha256.Sum256(c.Raw)
	}

	return out
}
