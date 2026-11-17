package patch

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"maps"
	"slices"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const newDirMode = 0o755

// newLayer writes files into one layer, plus a directory entry for each parent the image doesn't have yet (existing
// directories are left out, so their ownership and mode stay). Entries are sorted and timestamps fixed, so the same
// files always make the same layer.
func newLayer(files map[string]file, fsys *fileSystem, mediaType types.MediaType) (v1.Layer, error) {
	dirs := map[string]bool{}

	for p := range files {
		for d := parent(p); d != "" && !fsys.isDir(d); d = parent(d) {
			dirs[d] = true
		}
	}

	entries := append(slices.Collect(maps.Keys(dirs)), slices.Collect(maps.Keys(files))...)
	slices.Sort(entries)

	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)
	epoch := time.Unix(0, 0)

	for _, p := range entries {
		hdr := &tar.Header{Name: p, ModTime: epoch, Format: tar.FormatPAX}

		if f, ok := files[p]; ok {
			hdr.Typeflag, hdr.Mode, hdr.Uid, hdr.Gid, hdr.Size = tar.TypeReg, f.mode, f.uid, f.gid, int64(len(f.data))
		} else {
			hdr.Typeflag, hdr.Name, hdr.Mode = tar.TypeDir, p+"/", newDirMode
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("writing layer entry %s: %w", p, err)
		}

		if f, ok := files[p]; ok {
			if _, err := tw.Write(f.data); err != nil {
				return nil, fmt.Errorf("writing layer file %s: %w", p, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("closing layer: %w", err)
	}

	data := buf.Bytes()

	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}, tarball.WithMediaType(mediaType))
	if err != nil {
		return nil, fmt.Errorf("building layer: %w", err)
	}

	return l, nil
}

// layerMediaType matches the new layer to the manifest it joins.
func layerMediaType(manifest types.MediaType) types.MediaType {
	if manifest == types.OCIManifestSchema1 {
		return types.OCILayer
	}

	return types.DockerLayer
}
