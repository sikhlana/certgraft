package patch

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"

	keystore "github.com/pavlo-v-chernykh/keystore-go/v4"
	pkcs12 "software.sslmate.com/src/go-pkcs12"

	"github.com/sikhlana/certgraft/internal/roots"
)

// storePassword is the JDK's default trust store password. JDK 18+ ships cacerts password-less; that's tried next.
const storePassword = "changeit"

var jksMagic = []byte{0xfe, 0xed, 0xfe, 0xed}

// addToTrustStore returns the JVM trust store data with every root it lacks added, in the same format (JKS or
// PKCS12) and with the same password. Data that already holds every root comes back unchanged.
func addToTrustStore(data []byte, set *roots.Set) ([]byte, error) {
	if bytes.HasPrefix(data, jksMagic) {
		return addToJKS(data, set)
	}

	return addToPKCS12(data, set)
}

func addToJKS(data []byte, set *roots.Set) ([]byte, error) {
	ks := keystore.New(keystore.WithOrderedAliases())

	if err := ks.Load(bytes.NewReader(data), []byte(storePassword)); err != nil {
		return nil, fmt.Errorf("decoding JKS: %w", err)
	}

	have := map[[sha256.Size]byte]bool{}

	for _, a := range ks.Aliases() {
		if e, err := ks.GetTrustedCertificateEntry(a); err == nil {
			have[sha256.Sum256(e.Certificate.Content)] = true
		}
	}

	added := false

	for _, c := range set.Certificates() {
		if have[sha256.Sum256(c.Raw)] {
			continue
		}

		err := ks.SetTrustedCertificateEntry(rootAlias(c), keystore.TrustedCertificateEntry{
			CreationTime: c.NotBefore,
			Certificate:  keystore.Certificate{Type: "X509", Content: c.Raw},
		})
		if err != nil {
			return nil, fmt.Errorf("adding root to JKS: %w", err)
		}

		added = true
	}

	if !added {
		return data, nil
	}

	var buf bytes.Buffer

	if err := ks.Store(&buf, []byte(storePassword)); err != nil {
		return nil, fmt.Errorf("encoding JKS: %w", err)
	}

	return buf.Bytes(), nil
}

// addToPKCS12 re-encodes the store with the roots added. The library can't read entry aliases back, so existing
// entries keep their certificates but get aliases derived from their subjects.
func addToPKCS12(data []byte, set *roots.Set) ([]byte, error) {
	password, enc := storePassword, pkcs12.Modern2023

	certs, err := pkcs12.DecodeTrustStore(data, password)
	if err != nil {
		var errEmpty error

		certs, errEmpty = pkcs12.DecodeTrustStore(data, "")
		if errEmpty != nil {
			return nil, fmt.Errorf("decoding PKCS12: %w", err)
		}

		password, enc = "", pkcs12.Passwordless
	}

	have := map[[sha256.Size]byte]bool{}
	entries := make([]pkcs12.TrustStoreEntry, 0, len(certs)+len(set.Certificates()))
	used := map[string]bool{}

	for _, c := range certs {
		have[sha256.Sum256(c.Raw)] = true
		entries = append(entries, pkcs12.TrustStoreEntry{Cert: c, FriendlyName: uniqueAlias(subjectAlias(c), used)})
	}

	before := len(entries)

	for _, c := range set.Certificates() {
		if !have[sha256.Sum256(c.Raw)] {
			entries = append(entries, pkcs12.TrustStoreEntry{Cert: c, FriendlyName: uniqueAlias(rootAlias(c), used)})
		}
	}

	if len(entries) == before {
		return data, nil
	}

	// Salts come from a generator seeded by the input, so the same store and roots always encode to the same bytes.
	seed := sha256.Sum256(append(bytes.Clone(data), set.Fingerprint()...))

	out, err := enc.WithRand(rand.NewChaCha8(seed)).EncodeTrustStoreEntries(entries, password)
	if err != nil {
		return nil, fmt.Errorf("encoding PKCS12: %w", err)
	}

	return out, nil
}

func rootAlias(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)

	return "certgraft-" + hex.EncodeToString(sum[:4])
}

func subjectAlias(c *x509.Certificate) string {
	if c.Subject.CommonName != "" {
		return strings.ToLower(c.Subject.CommonName)
	}

	return strings.ToLower(c.Subject.String())
}

func uniqueAlias(alias string, used map[string]bool) string {
	out := alias

	for n := 2; used[out]; n++ {
		out = alias + "-" + strconv.Itoa(n)
	}

	used[out] = true

	return out
}
