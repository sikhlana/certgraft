package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PruneResult is what a prune removed.
type PruneResult struct {
	// Images is the number of patch entries removed.
	Images int

	// Bytes is the size of the blobs removed.
	Bytes int64
}

// Prune removes entries and tag records last requested before cutoff, then every blob no remaining entry lists that
// was also last written before cutoff (so blobs of a patch still in progress stay).
func (s *Store) Prune(cutoff time.Time) (PruneResult, error) {
	var res PruneResult

	live := map[string]bool{}

	err := eachFile(s.entryDir(), func(path string, info os.FileInfo) error {
		if info.ModTime().Before(cutoff) {
			res.Images++

			return remove(path)
		}

		var e Entry

		if err := readJSON(path, &e); err != nil {
			return err
		}

		for _, b := range e.Blobs {
			live[b] = true
		}

		return nil
	})
	if err != nil {
		return res, err
	}

	err = eachFile(s.tagDir(), func(path string, info os.FileInfo) error {
		if info.ModTime().Before(cutoff) {
			return remove(path)
		}

		return nil
	})
	if err != nil {
		return res, err
	}

	err = eachFile(s.blobDir(), func(path string, info os.FileInfo) error {
		if live["sha256:"+info.Name()] || !info.ModTime().Before(cutoff) {
			return nil
		}

		res.Bytes += info.Size()

		return remove(path)
	})

	return res, err
}

// eachFile calls fn for every regular file in dir, skipping temporary files.
func eachFile(dir string, fn func(path string, info os.FileInfo) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("listing cache directory: %w", err)
	}

	for _, e := range entries {
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), tempPrefix) {
			continue
		}

		var info os.FileInfo

		if info, err = e.Info(); err != nil {
			return fmt.Errorf("reading cache file: %w", err)
		}

		if err = fn(filepath.Join(dir, e.Name()), info); err != nil {
			return err
		}
	}

	return nil
}

func remove(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing cache file: %w", err)
	}

	return nil
}

// FormatSize renders n bytes in decimal units, e.g. "12.3 MB".
func FormatSize(n int64) string {
	const unit = 1000

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0

	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}
