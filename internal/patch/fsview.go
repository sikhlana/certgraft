package patch

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

const (
	whiteoutPrefix = ".wh."
	whiteoutOpaque = ".wh..wh..opq"

	// maxLinks bounds symlink resolution, as the kernel's ELOOP does.
	maxLinks = 40
)

var errNotRegular = errors.New("not a regular file")

type node struct {
	typeflag byte
	link     string
	mode     int64
	uid, gid int
	layer    int
}

// fileSystem is an image's merged filesystem: for each path, the top-most layer's entry, with whiteouts applied.
// It holds metadata for every path but content only for the files keep selected; other content is re-read from its
// layer on demand.
type fileSystem struct {
	layers  []v1.Layer
	nodes   map[string]node
	dirs    map[string]bool
	content map[string][]byte
}

// flatten reads every layer of img, top-most first, keeping the content of the regular files keep selects.
func flatten(img v1.Image, keep func(string) bool) (*fileSystem, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("listing layers: %w", err)
	}

	f := &fileSystem{layers: layers, nodes: map[string]node{}, dirs: map[string]bool{}, content: map[string][]byte{}}
	w := whiteouts{deleted: map[string]bool{}, opaque: map[string]bool{}}

	for i := range slices.Backward(layers) {
		layerW, readErr := f.readLayer(i, w, keep)
		if readErr != nil {
			return nil, readErr
		}

		// A layer's whiteouts hide what's below it, not its own entries, so they apply from the next layer down.
		maps.Copy(w.deleted, layerW.deleted)
		maps.Copy(w.opaque, layerW.opaque)
	}

	return f, nil
}

// whiteouts are the paths upper layers deleted and the directories they made opaque.
type whiteouts struct {
	deleted, opaque map[string]bool
}

// readLayer adds layer i's entries that upper layers neither replaced nor whited out, and returns its own whiteouts.
func (f *fileSystem) readLayer(i int, upper whiteouts, keep func(string) bool) (whiteouts, error) {
	own := whiteouts{deleted: map[string]bool{}, opaque: map[string]bool{}}

	err := eachEntry(f.layers[i], func(hdr *tar.Header, r io.Reader) error {
		p := clean(hdr.Name)
		dir, base := parent(p), path.Base(p)

		switch {
		case base == whiteoutOpaque:
			own.opaque[dir] = true

			return nil
		case strings.HasPrefix(base, whiteoutPrefix):
			own.deleted[join(dir, strings.TrimPrefix(base, whiteoutPrefix))] = true

			return nil
		}

		if _, seen := f.nodes[p]; seen || p == "" || hidden(p, upper.deleted, upper.opaque) {
			return nil
		}

		f.add(p, node{typeflag: hdr.Typeflag, link: hdr.Linkname, mode: hdr.Mode, uid: hdr.Uid, gid: hdr.Gid, layer: i})

		if hdr.Typeflag != tar.TypeReg || !keep(p) {
			return nil
		}

		data, readErr := io.ReadAll(r)
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", p, readErr)
		}

		f.content[p] = data

		return nil
	})

	return own, err
}

func (f *fileSystem) add(p string, n node) {
	f.nodes[p] = n

	if n.typeflag == tar.TypeDir {
		f.dirs[p] = true
	}

	for a := parent(p); a != ""; a = parent(a) {
		f.dirs[a] = true
	}
}

// resolve follows every symlink in p, the last component included, and reports whether the result exists.
func (f *fileSystem) resolve(p string) (string, bool) {
	parts := split(p)
	cur := ""

	for hops := 0; len(parts) > 0; {
		next := join(cur, parts[0])
		parts = parts[1:]

		n, ok := f.nodes[next]

		if !ok || n.typeflag != tar.TypeSymlink {
			cur = next

			continue
		}

		if hops++; hops > maxLinks {
			return "", false
		}

		base := cur

		if path.IsAbs(n.link) {
			base = ""
		}

		parts = append(split(path.Join("/", base, n.link)), parts...)
		cur = ""
	}

	_, isNode := f.nodes[cur]

	return cur, isNode || f.dirs[cur]
}

// resolveParent resolves p's directory, so a new file is written where the image's links lead.
func (f *fileSystem) resolveParent(p string) string {
	dir, _ := f.resolve(parent(clean(p)))

	return join(dir, path.Base(p))
}

// file resolves p to a regular file (or a hard link to one) and returns its real path.
func (f *fileSystem) file(p string) (string, bool) {
	resolved, ok := f.resolve(p)

	if !ok {
		return "", false
	}

	switch f.nodes[resolved].typeflag {
	case tar.TypeReg, tar.TypeLink:
		return resolved, true
	default:
		return "", false
	}
}

func (f *fileSystem) isDir(p string) bool {
	resolved, ok := f.resolve(p)

	return ok && f.dirs[resolved]
}

func (f *fileSystem) exists(p string) bool {
	_, ok := f.resolve(p)

	return ok
}

// readFile returns the content of the regular file at the real path p.
func (f *fileSystem) readFile(p string) ([]byte, error) {
	n, ok := f.nodes[p]

	if ok && n.typeflag == tar.TypeLink {
		p = clean(n.link)
		n, ok = f.nodes[p]
	}

	if !ok || n.typeflag != tar.TypeReg {
		return nil, fmt.Errorf("%s: %w", p, errNotRegular)
	}

	if data, kept := f.content[p]; kept {
		return data, nil
	}

	var data []byte

	err := eachEntry(f.layers[n.layer], func(hdr *tar.Header, r io.Reader) error {
		if clean(hdr.Name) != p {
			return nil
		}

		var err error

		data, err = io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("reading %s: %w", p, err)
		}

		return errStop
	})
	if err != nil {
		return nil, err
	}

	f.content[p] = data

	return data, nil
}

// paths returns every path in the merged filesystem.
func (f *fileSystem) paths() []string {
	out := make([]string, 0, len(f.nodes))

	for p := range f.nodes {
		out = append(out, p)
	}

	return out
}

var errStop = errors.New("stop")

// eachEntry calls fn for each entry of l's uncompressed tar, until fn returns an error; errStop ends it cleanly.
func eachEntry(l v1.Layer, fn func(*tar.Header, io.Reader) error) error {
	rc, err := l.Uncompressed()
	if err != nil {
		return fmt.Errorf("opening layer: %w", err)
	}
	defer rc.Close()

	tr := tar.NewReader(rc)

	for {
		hdr, nextErr := tr.Next()

		if errors.Is(nextErr, io.EOF) {
			return nil
		}

		if nextErr != nil {
			return fmt.Errorf("reading layer: %w", nextErr)
		}

		if err = fn(hdr, tr); errors.Is(err, errStop) {
			return nil
		} else if err != nil {
			return err
		}
	}
}

// hidden reports whether an upper layer's whiteout removed p: p or a directory above it was deleted, or a directory
// above it was made opaque.
func hidden(p string, deleted, opaque map[string]bool) bool {
	for q := p; q != ""; q = parent(q) {
		if deleted[q] || opaque[parent(q)] {
			return true
		}
	}

	return false
}

// clean turns a tar or absolute path into the form the view keys on: relative, no dot segments, no trailing slash.
func clean(p string) string {
	return strings.TrimPrefix(path.Clean("/"+p), "/")
}

func parent(p string) string {
	d := path.Dir(p)

	if d == "." || d == "/" {
		return ""
	}

	return d
}

func join(dir, base string) string {
	if dir == "" {
		return base
	}

	return dir + "/" + base
}

func split(p string) []string {
	c := clean(p)

	if c == "" {
		return nil
	}

	return strings.Split(c, "/")
}
