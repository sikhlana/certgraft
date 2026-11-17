// Package roots loads the workstation's extra CA roots: the certificates every patched image gains and the registry's
// own upstream calls trust.
package roots

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

var (
	// ErrNoRoots is a roots directory with no files in it.
	ErrNoRoots = errors.New("no extra CA roots")

	// ErrNoCertificate is a roots file without a PEM certificate.
	ErrNoCertificate = errors.New("no PEM certificate")
)

// Set is a fixed set of extra CA roots and its fingerprint.
type Set struct {
	certs       []*x509.Certificate
	fingerprint string
}

// New returns the Set of certs, dropping duplicates.
func New(certs []*x509.Certificate) *Set {
	var (
		unique []*x509.Certificate
		sums   [][]byte
	)

	for _, c := range certs {
		sum := sha256.Sum256(c.Raw)

		if slices.ContainsFunc(sums, func(s []byte) bool { return bytes.Equal(s, sum[:]) }) {
			continue
		}

		unique = append(unique, c)
		sums = append(sums, sum[:])
	}

	slices.SortFunc(sums, bytes.Compare)

	return &Set{certs: unique, fingerprint: hex.EncodeToString(sha256Of(bytes.Join(sums, nil)))}
}

// Load reads every file in dir. Each must hold at least one PEM certificate.
func Load(dir string) (*Set, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading roots directory: %w", err)
	}

	var certs []*x509.Certificate

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		path := filepath.Join(dir, e.Name())

		found, parseErr := parseFile(path)
		if parseErr != nil {
			return nil, parseErr
		}

		certs = append(certs, found...)
	}

	if len(certs) == 0 {
		return nil, fmt.Errorf("%s: %w", dir, ErrNoRoots)
	}

	return New(certs), nil
}

// Certificates returns the roots.
func (s *Set) Certificates() []*x509.Certificate {
	return s.certs
}

// PEM returns each root PEM-encoded, in the Set's order.
func (s *Set) PEM() [][]byte {
	out := make([][]byte, len(s.certs))

	for i, c := range s.certs {
		out[i] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	}

	return out
}

// Fingerprint identifies the set whatever its order: the hex SHA-256 of its roots' sorted SHA-256 digests.
func (s *Set) Fingerprint() string {
	return s.fingerprint
}

// Pool returns the system roots plus the Set.
func (s *Set) Pool() (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("loading system roots: %w", err)
	}

	for _, c := range s.certs {
		pool.AddCert(c)
	}

	return pool, nil
}

func parseFile(path string) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading root: %w", err)
	}

	var certs []*x509.Certificate

	for {
		var block *pem.Block

		block, data = pem.Decode(data)

		if block == nil {
			break
		}

		if block.Type != "CERTIFICATE" {
			continue
		}

		c, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, parseErr)
		}

		certs = append(certs, c)
	}

	if len(certs) == 0 {
		return nil, fmt.Errorf("%s: %w", path, ErrNoCertificate)
	}

	return certs, nil
}

func sha256Of(b []byte) []byte {
	sum := sha256.Sum256(b)

	return sum[:]
}
