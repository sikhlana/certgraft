// Package store is the registry's cache on disk: the layers, configs and manifests it made, the result of each patch
// keyed by what it was made from, and the upstream digest each tag last resolved to. Upstream blobs are never stored.
//
// A file's modification time records when it was last requested; prune reads it.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// tempPrefix names files still being written; they're renamed into place when complete.
const tempPrefix = ".tmp-"

// ErrNotFound is something the cache doesn't hold.
var ErrNotFound = errors.New("not in the cache")

// Outcome is what a patch produced.
type Outcome string

const (
	// Patched is a patched manifest the store holds, with its configs and layers.
	Patched Outcome = "patched"

	// PassedThrough is an image with nothing to patch: it's served from upstream unchanged.
	PassedThrough Outcome = "passthrough"
)

// Key identifies one patch: an upstream manifest, patched with one root set by one patcher version.
type Key struct {
	Upstream    v1.Hash
	Fingerprint string
	Patcher     string
}

func (k Key) file() string {
	sum := sha256.Sum256([]byte(k.Upstream.String() + "\n" + k.Fingerprint + "\n" + k.Patcher))

	return hex.EncodeToString(sum[:]) + ".json"
}

// Entry is the result of one patch.
type Entry struct {
	Outcome Outcome `json:"outcome"`

	// Manifest is the digest of the manifest to serve when Outcome is Patched.
	Manifest string `json:"manifest,omitempty"`

	// MediaType is Manifest's media type.
	MediaType string `json:"media_type,omitempty"`

	// Blobs lists every blob the patch stored, Manifest included.
	Blobs []string `json:"blobs,omitempty"`
}

type tagRecord struct {
	Upstream string `json:"upstream"`
}

// Store is the cache in one directory.
type Store struct {
	dir string
}

// Open returns the Store in dir, creating its layout.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir}

	for _, d := range []string{s.blobDir(), s.entryDir(), s.tagDir()} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return nil, fmt.Errorf("creating cache directory: %w", err)
		}
	}

	return s, nil
}

// PutBlob stores data under its digest. Storing a blob again marks it as recently written, so a concurrent prune
// leaves it alone.
func (s *Store) PutBlob(data []byte) (v1.Hash, error) {
	sum := sha256.Sum256(data)
	h := v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(sum[:])}
	path := s.blobPath(h)

	if _, err := os.Stat(path); err == nil {
		return h, touch(path)
	}

	return h, writeAtomic(path, data)
}

// Blob opens the blob h and returns its size.
func (s *Store) Blob(h v1.Hash) (*os.File, int64, error) {
	f, err := os.Open(s.blobPath(h))

	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, ErrNotFound
	}

	if err != nil {
		return nil, 0, fmt.Errorf("opening blob: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return nil, 0, fmt.Errorf("reading blob size: %w", err)
	}

	return f, info.Size(), nil
}

// BlobBytes returns the blob h.
func (s *Store) BlobBytes(h v1.Hash) ([]byte, error) {
	data, err := os.ReadFile(s.blobPath(h))

	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("reading blob: %w", err)
	}

	return data, nil
}

// PutEntry records the result of the patch k.
func (s *Store) PutEntry(k Key, e Entry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encoding entry: %w", err)
	}

	return writeAtomic(filepath.Join(s.entryDir(), k.file()), data)
}

// Entry returns the result of the patch k and marks it as requested now.
func (s *Store) Entry(k Key) (Entry, error) {
	path := filepath.Join(s.entryDir(), k.file())

	var e Entry

	if err := readJSON(path, &e); err != nil {
		return Entry{}, err
	}

	return e, touch(path)
}

// PutTag records the upstream digest repo:tag resolved to.
func (s *Store) PutTag(repo, tag string, upstream v1.Hash) error {
	data, err := json.Marshal(tagRecord{Upstream: upstream.String()})
	if err != nil {
		return fmt.Errorf("encoding tag: %w", err)
	}

	return writeAtomic(s.tagPath(repo, tag), data)
}

// Tag returns the upstream digest repo:tag last resolved to.
func (s *Store) Tag(repo, tag string) (v1.Hash, error) {
	var r tagRecord

	if err := readJSON(s.tagPath(repo, tag), &r); err != nil {
		return v1.Hash{}, err
	}

	h, err := v1.NewHash(r.Upstream)
	if err != nil {
		return v1.Hash{}, fmt.Errorf("parsing cached tag: %w", err)
	}

	return h, nil
}

func (s *Store) blobDir() string  { return filepath.Join(s.dir, "blobs", "sha256") }
func (s *Store) entryDir() string { return filepath.Join(s.dir, "entries") }
func (s *Store) tagDir() string   { return filepath.Join(s.dir, "tags") }

func (s *Store) blobPath(h v1.Hash) string {
	return filepath.Join(s.blobDir(), h.Hex)
}

func (s *Store) tagPath(repo, tag string) string {
	sum := sha256.Sum256([]byte(repo + ":" + tag))

	return filepath.Join(s.tagDir(), hex.EncodeToString(sum[:])+".json")
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)

	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}

	if err != nil {
		return fmt.Errorf("reading cache record: %w", err)
	}

	if err = json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decoding cache record %s: %w", filepath.Base(path), err)
	}

	return nil
}

func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), tempPrefix+"*")
	if err != nil {
		return fmt.Errorf("creating cache file: %w", err)
	}

	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())

		return fmt.Errorf("writing cache file: %w", err)
	}

	if err = f.Close(); err != nil {
		_ = os.Remove(f.Name())

		return fmt.Errorf("closing cache file: %w", err)
	}

	if err = os.Rename(f.Name(), path); err != nil {
		_ = os.Remove(f.Name())

		return fmt.Errorf("moving cache file into place: %w", err)
	}

	return nil
}

func touch(path string) error {
	now := time.Now()

	if err := os.Chtimes(path, now, now); err != nil {
		return fmt.Errorf("marking cache file as requested: %w", err)
	}

	return nil
}
