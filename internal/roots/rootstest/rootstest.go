// Package rootstest mints throwaway CAs and leaf certificates for tests.
package rootstest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// CA is a self-signed root and its key.
type CA struct {
	Cert *x509.Certificate
	Key  crypto.Signer
}

// NewCA returns a self-signed root named cn.
func NewCA(t *testing.T, cn string) CA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return CA{Cert: cert, Key: key}
}

// PEM returns the CA's certificate PEM-encoded.
func (ca CA) PEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Cert.Raw})
}

// WriteDir writes each CA into its own file in a new temporary directory and returns it.
func WriteDir(t *testing.T, cas ...CA) string {
	t.Helper()

	dir := t.TempDir()

	for i, ca := range cas {
		name := filepath.Join(dir, string(rune('a'+i))+".pem")
		require.NoError(t, os.WriteFile(name, ca.PEM(), 0o600))
	}

	return dir
}

// ServerCert returns a TLS certificate for hosts, signed by ca.
func (ca CA) ServerCert(t *testing.T, hosts ...string) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: serial(t),
		Subject:      pkix.Name{CommonName: hosts[0]},
		DNSNames:     hosts,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, key.Public(), ca.Key)
	require.NoError(t, err)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func serial(t *testing.T) *big.Int {
	t.Helper()

	n, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	require.NoError(t, err)

	return n
}
