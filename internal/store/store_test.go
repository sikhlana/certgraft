package store_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/certgraft/internal/store"
)

func TestBlobRoundTrip(t *testing.T) {
	s, err := store.Open(t.TempDir())
	require.NoError(t, err)

	h, err := s.PutBlob([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", h.String())

	f, size, err := s.Blob(h)
	require.NoError(t, err)

	defer f.Close()

	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, int64(5), size)
	assert.Equal(t, "hello", string(data))

	_, _, err = s.Blob(v1.Hash{Algorithm: "sha256", Hex: "00"})
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestEntriesAndTags(t *testing.T) {
	s, err := store.Open(t.TempDir())
	require.NoError(t, err)

	k := store.Key{Upstream: hash(t, "a"), Fingerprint: "fp", Patcher: "1"}

	_, err = s.Entry(k)
	require.ErrorIs(t, err, store.ErrNotFound)

	want := store.Entry{Outcome: store.Patched, Manifest: "sha256:x", MediaType: "m", Blobs: []string{"sha256:x"}}
	require.NoError(t, s.PutEntry(k, want))

	got, err := s.Entry(k)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	_, err = s.Entry(store.Key{Upstream: k.Upstream, Fingerprint: "other", Patcher: "1"})
	require.ErrorIs(t, err, store.ErrNotFound, "a different root set is a different entry")

	require.NoError(t, s.PutTag("docker.io/library/node", "22", k.Upstream))

	tag, err := s.Tag("docker.io/library/node", "22")
	require.NoError(t, err)
	assert.Equal(t, k.Upstream, tag)
}

func TestPruneRemovesOnlyStaleEntries(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(dir)
	require.NoError(t, err)

	old := bytes.Repeat([]byte("o"), 1500)
	oldBlob, err := s.PutBlob(old)
	require.NoError(t, err)
	shared, err := s.PutBlob([]byte("shared"))
	require.NoError(t, err)
	newBlob, err := s.PutBlob([]byte("new"))
	require.NoError(t, err)

	oldKey := store.Key{Upstream: hash(t, "old"), Fingerprint: "fp", Patcher: "1"}
	newKey := store.Key{Upstream: hash(t, "new"), Fingerprint: "fp", Patcher: "1"}
	require.NoError(t, s.PutEntry(oldKey, store.Entry{
		Outcome: store.Patched, Blobs: []string{oldBlob.String(), shared.String()},
	}))
	require.NoError(t, s.PutEntry(newKey, store.Entry{
		Outcome: store.Patched, Blobs: []string{newBlob.String(), shared.String()},
	}))

	// Backdate the old entry and every blob; only the new entry was requested recently.
	backdate(t, filepath.Join(dir, "entries"), 40)
	backdate(t, filepath.Join(dir, "blobs", "sha256"), 40)

	_, err = s.Entry(newKey)
	require.NoError(t, err)

	var out bytes.Buffer

	run := store.NewPruneRunner(store.PruneRunnerParams{Store: s, Request: store.PruneRequest{Days: 30, Out: &out}})
	require.NoError(t, run(t.Context()))

	assert.Equal(t, "pruned 1 image(s), 1.5 kB\n", out.String())

	_, err = s.Entry(oldKey)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.Entry(newKey)
	require.NoError(t, err)

	_, err = s.BlobBytes(oldBlob)
	require.ErrorIs(t, err, store.ErrNotFound)

	for _, h := range []v1.Hash{shared, newBlob} {
		_, err = s.BlobBytes(h)
		require.NoError(t, err, "blob %s is still listed by a live entry", h)
	}
}

func TestPruneRejectsZeroDays(t *testing.T) {
	s, err := store.Open(t.TempDir())
	require.NoError(t, err)

	run := store.NewPruneRunner(
		store.PruneRunnerParams{Store: s, Request: store.PruneRequest{Days: 0, Out: io.Discard}},
	)
	require.ErrorIs(t, run(t.Context()), store.ErrPruneDays)
}

func TestFormatSize(t *testing.T) {
	assert.Equal(t, "999 B", store.FormatSize(999))
	assert.Equal(t, "1.5 kB", store.FormatSize(1500))
	assert.Equal(t, "12.3 MB", store.FormatSize(12_300_000))
}

func hash(t *testing.T, s string) v1.Hash {
	t.Helper()

	h, _, err := v1.SHA256(bytes.NewReader([]byte(s)))
	require.NoError(t, err)

	return h
}

func backdate(t *testing.T, dir string, days int) {
	t.Helper()

	when := time.Now().AddDate(0, 0, -days)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, e := range entries {
		require.NoError(t, os.Chtimes(filepath.Join(dir, e.Name()), when, when))
	}
}
