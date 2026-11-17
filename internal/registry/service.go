// Package registry runs the registry server: a read-only OCI distribution API that answers every allowlisted
// upstream image with the extra roots added. Patches are made on first request, per upstream digest, and cached.
package registry

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"go.uber.org/fx"
	"golang.org/x/sync/singleflight"

	"github.com/sikhlana/certgraft/internal/patch"
	"github.com/sikhlana/certgraft/internal/roots"
	"github.com/sikhlana/certgraft/internal/store"
	"github.com/sikhlana/certgraft/internal/upstream"
)

// digestTagPrefix marks a tag the source policy made from a digest pin: sha256-<hex> means upstream digest sha256:<hex>.
const digestTagPrefix = "sha256-"

// ErrManifestUnknown is a manifest upstream doesn't have.
var ErrManifestUnknown = errors.New("manifest unknown")

// Manifest is a manifest to serve.
type Manifest struct {
	Body      []byte
	MediaType types.MediaType
	Digest    v1.Hash
}

// Blob is a blob to serve, opened only when its body is needed.
type Blob struct {
	Size int64
	Open func() (io.ReadCloser, error)
}

// ServiceParams is what NewService takes from the graph.
type ServiceParams struct {
	fx.In

	Upstream *upstream.Client
	Store    *store.Store
	Patcher  *patch.Patcher
	Roots    *roots.Set
	Logger   *slog.Logger
}

// Service resolves manifests and blobs: patched ones from the store, everything else from upstream.
type Service struct {
	upstream *upstream.Client
	store    *store.Store
	patcher  *patch.Patcher
	roots    *roots.Set
	logger   *slog.Logger
	inflight singleflight.Group
}

// NewService returns a Service.
func NewService(p ServiceParams) *Service {
	return &Service{upstream: p.Upstream, store: p.Store, patcher: p.Patcher, roots: p.Roots, logger: p.Logger}
}

// Manifest returns the manifest ref names in repo: a digest, a digest-pin tag (sha256-<hex>) or a tag.
func (s *Service) Manifest(ctx context.Context, repo name.Repository, ref string) (Manifest, error) {
	if h, err := v1.NewHash(ref); err == nil {
		return s.byDigest(ctx, repo, h)
	}

	if hexPart, ok := strings.CutPrefix(ref, digestTagPrefix); ok && isSHA256Hex(hexPart) {
		return s.pinned(ctx, repo, v1.Hash{Algorithm: "sha256", Hex: hexPart})
	}

	return s.tag(ctx, repo, ref)
}

// Blob returns the blob h: a stored patched blob, else upstream's.
func (s *Service) Blob(ctx context.Context, repo name.Repository, h v1.Hash) (Blob, error) {
	if f, size, err := s.store.Blob(h); err == nil {
		_ = f.Close()

		return Blob{Size: size, Open: func() (io.ReadCloser, error) {
			opened, _, openErr := s.store.Blob(h)

			return opened, openErr
		}}, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return Blob{}, err
	}

	l, err := s.upstream.Layer(ctx, repo.Digest(h.String()))
	if err != nil {
		return Blob{}, err
	}

	size, err := l.Size()
	if err != nil {
		return Blob{}, fmt.Errorf("reading blob size upstream: %w", err)
	}

	return Blob{Size: size, Open: l.Compressed}, nil
}

// byDigest serves a manifest by digest: one this registry made (an index's patched platform manifest), else upstream's.
func (s *Service) byDigest(ctx context.Context, repo name.Repository, h v1.Hash) (Manifest, error) {
	if body, err := s.store.BlobBytes(h); err == nil {
		return Manifest{Body: body, MediaType: sniffMediaType(body), Digest: h}, nil
	}

	d, err := s.upstream.Get(ctx, repo.Digest(h.String()))
	if err != nil {
		return Manifest{}, err
	}

	return upstreamManifest(d), nil
}

// pinned serves the patch of the upstream manifest with digest h. Content is verified against h when fetched.
func (s *Service) pinned(ctx context.Context, repo name.Repository, h v1.Hash) (Manifest, error) {
	if m, ok, err := s.cached(ctx, repo, h); ok || err != nil {
		return m, err
	}

	d, err := s.upstream.Get(ctx, repo.Digest(h.String()))

	if te, ok := errors.AsType[*transport.Error](err); ok && te.StatusCode == http.StatusNotFound {
		return Manifest{}, fmt.Errorf("%w: %s not found in %s upstream", ErrManifestUnknown, h, repo)
	}

	if err != nil {
		return Manifest{}, err
	}

	return s.resolve(ctx, repo, d)
}

// tag serves the patch of whatever the tag points at upstream now. If upstream can't answer, it falls back to the
// patch of the digest the tag last resolved to.
func (s *Service) tag(ctx context.Context, repo name.Repository, tag string) (Manifest, error) {
	ref := repo.Tag(tag)

	desc, err := s.upstream.Head(ctx, ref)
	if err != nil {
		return s.degraded(ctx, repo, tag, err)
	}

	m, ok, err := s.cached(ctx, repo, desc.Digest)
	if err != nil {
		return Manifest{}, err
	}

	if !ok {
		var d *remote.Descriptor

		if d, err = s.upstream.Get(ctx, repo.Digest(desc.Digest.String())); err != nil {
			return Manifest{}, err
		}

		if m, err = s.resolve(ctx, repo, d); err != nil {
			return Manifest{}, err
		}
	}

	if err = s.store.PutTag(repo.String(), tag, desc.Digest); err != nil {
		return Manifest{}, err
	}

	return m, nil
}

func (s *Service) degraded(ctx context.Context, repo name.Repository, tag string, upErr error) (Manifest, error) {
	if !upstream.Unreachable(upErr) {
		return Manifest{}, upErr
	}

	h, err := s.store.Tag(repo.String(), tag)
	if err != nil {
		return Manifest{}, upErr
	}

	m, ok, err := s.cached(ctx, repo, h)

	if !ok || err != nil {
		return Manifest{}, upErr
	}

	s.logger.WarnContext(ctx, "upstream unreachable; serving the cached patch",
		slog.String("image", repo.Tag(tag).String()),
		slog.String("upstream_digest", h.String()),
		slog.Any("error", upErr),
	)

	return m, nil
}

// cached returns the stored result of patching upstream digest h with the current roots, if there is one.
func (s *Service) cached(ctx context.Context, repo name.Repository, h v1.Hash) (Manifest, bool, error) {
	e, err := s.store.Entry(s.key(h))

	if errors.Is(err, store.ErrNotFound) {
		return Manifest{}, false, nil
	}

	if err != nil {
		return Manifest{}, false, err
	}

	if e.Outcome == store.PassedThrough {
		d, getErr := s.upstream.Get(ctx, repo.Digest(h.String()))
		if getErr != nil {
			return Manifest{}, false, getErr
		}

		return upstreamManifest(d), true, nil
	}

	mh, err := v1.NewHash(e.Manifest)
	if err != nil {
		return Manifest{}, false, fmt.Errorf("parsing cached manifest digest: %w", err)
	}

	body, err := s.store.BlobBytes(mh)
	if err != nil {
		return Manifest{}, false, err
	}

	return Manifest{Body: body, MediaType: types.MediaType(e.MediaType), Digest: mh}, true, nil
}

func (s *Service) key(h v1.Hash) store.Key {
	return store.Key{Upstream: h, Fingerprint: s.roots.Fingerprint(), Patcher: patch.Version}
}

// resolve patches d once, however many requests ask for it at the same time.
func (s *Service) resolve(ctx context.Context, repo name.Repository, d *remote.Descriptor) (Manifest, error) {
	v, err, _ := s.inflight.Do(d.Digest.String(), func() (any, error) {
		return s.patch(ctx, repo, d)
	})
	if err != nil {
		return Manifest{}, err
	}

	m, _ := v.(Manifest)

	return m, nil
}

func (s *Service) patch(ctx context.Context, repo name.Repository, d *remote.Descriptor) (Manifest, error) {
	var (
		m   Manifest
		out stored
		err error
	)

	switch {
	case d.MediaType.IsIndex():
		m, out, err = s.patchIndex(ctx, repo, d)
	case d.MediaType.IsImage():
		m, out, err = s.patchSingle(ctx, repo, d)
	default:
		return upstreamManifest(d), nil
	}

	if err != nil {
		return Manifest{}, err
	}

	e := store.Entry{Outcome: store.PassedThrough}

	if out.patched {
		e = store.Entry{
			Outcome:   store.Patched,
			Manifest:  m.Digest.String(),
			MediaType: string(m.MediaType),
			Blobs:     out.blobs,
		}
	}

	if err = s.store.PutEntry(s.key(d.Digest), e); err != nil {
		return Manifest{}, err
	}

	return m, nil
}

type stored struct {
	patched bool
	blobs   []string
}

func (s *Service) patchSingle(
	ctx context.Context,
	repo name.Repository,
	d *remote.Descriptor,
) (Manifest, stored, error) {
	img, err := d.Image()
	if err != nil {
		return Manifest{}, stored{}, fmt.Errorf("reading image: %w", err)
	}

	res, err := s.patchImage(ctx, repo.Digest(d.Digest.String()).String(), img)

	if err != nil || res.Outcome == patch.PassedThrough {
		return upstreamManifest(d), stored{}, err
	}

	var out stored

	m, err := s.storeImage(res, &out)

	return m, out, err
}

// patchIndex patches every platform of an index and stores an index pointing at the results. Attestations are
// dropped: they describe the upstream manifests, not the patched ones. An index none of whose platforms needed
// patching is passed through as it is.
func (s *Service) patchIndex(
	ctx context.Context,
	repo name.Repository,
	d *remote.Descriptor,
) (Manifest, stored, error) {
	idx, err := d.ImageIndex()
	if err != nil {
		return Manifest{}, stored{}, fmt.Errorf("reading index: %w", err)
	}

	im, err := idx.IndexManifest()
	if err != nil {
		return Manifest{}, stored{}, fmt.Errorf("reading index manifest: %w", err)
	}

	var (
		out       stored
		platforms []v1.Descriptor
	)

	for _, desc := range im.Manifests {
		if isAttestation(desc) {
			continue
		}

		if !desc.MediaType.IsImage() {
			platforms = append(platforms, desc)

			continue
		}

		patched, patchErr := s.patchPlatform(ctx, repo, idx, desc, &out)
		if patchErr != nil {
			return Manifest{}, stored{}, patchErr
		}

		platforms = append(platforms, patched)
	}

	if !out.patched {
		return upstreamManifest(d), stored{}, nil
	}

	next := *im
	next.Manifests = platforms

	body, err := json.Marshal(next)
	if err != nil {
		return Manifest{}, stored{}, fmt.Errorf("encoding patched index: %w", err)
	}

	h, err := s.store.PutBlob(body)
	if err != nil {
		return Manifest{}, stored{}, err
	}

	out.blobs = append(out.blobs, h.String())

	return Manifest{Body: body, MediaType: d.MediaType, Digest: h}, out, nil
}

func (s *Service) patchPlatform(
	ctx context.Context, repo name.Repository, idx v1.ImageIndex, desc v1.Descriptor, out *stored,
) (v1.Descriptor, error) {
	img, err := idx.Image(desc.Digest)
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("reading platform image: %w", err)
	}

	res, err := s.patchImage(ctx, repo.Digest(desc.Digest.String()).String(), img)

	if err != nil || res.Outcome == patch.PassedThrough {
		return desc, err
	}

	m, err := s.storeImage(res, out)
	if err != nil {
		return v1.Descriptor{}, err
	}

	desc.Digest, desc.Size, desc.MediaType = m.Digest, int64(len(m.Body)), m.MediaType

	return desc, nil
}

func (s *Service) patchImage(ctx context.Context, image string, img v1.Image) (patch.Result, error) {
	res, err := s.patcher.Patch(img)
	if err != nil {
		return patch.Result{}, fmt.Errorf("patching %s: %w", image, err)
	}

	if res.Outcome == patch.Failed {
		s.logger.WarnContext(ctx, "patching failed; serving the image unpatched",
			slog.String("image", image),
			slog.String("trust_point", res.TrustPoint),
			slog.Any("error", res.Err),
		)
	}

	return res, nil
}

// storeImage stores what a patch made: the added layer, the config and the manifest. Base layers stay upstream.
func (s *Service) storeImage(res patch.Result, out *stored) (Manifest, error) {
	var blobs [][]byte

	if res.Layer != nil {
		rc, err := res.Layer.Compressed()
		if err != nil {
			return Manifest{}, fmt.Errorf("compressing patch layer: %w", err)
		}

		data, err := io.ReadAll(rc)
		_ = rc.Close()

		if err != nil {
			return Manifest{}, fmt.Errorf("reading patch layer: %w", err)
		}

		blobs = append(blobs, data)
	}

	cfg, err := res.Image.RawConfigFile()
	if err != nil {
		return Manifest{}, fmt.Errorf("reading patched config: %w", err)
	}

	body, err := res.Image.RawManifest()
	if err != nil {
		return Manifest{}, fmt.Errorf("reading patched manifest: %w", err)
	}

	mt, err := res.Image.MediaType()
	if err != nil {
		return Manifest{}, fmt.Errorf("reading patched media type: %w", err)
	}

	var h v1.Hash

	for _, b := range append(blobs, cfg, body) {
		if h, err = s.store.PutBlob(b); err != nil {
			return Manifest{}, err
		}

		out.blobs = append(out.blobs, h.String())
	}

	out.patched = true

	return Manifest{Body: body, MediaType: mt, Digest: h}, nil
}

func upstreamManifest(d *remote.Descriptor) Manifest {
	return Manifest{Body: d.Manifest, MediaType: d.MediaType, Digest: d.Digest}
}

func isAttestation(d v1.Descriptor) bool {
	return d.Annotations["vnd.docker.reference.type"] == "attestation-manifest" ||
		d.Platform != nil && d.Platform.OS == "unknown"
}

// sniffMediaType reads a stored manifest's mediaType field; every manifest this registry writes has one.
func sniffMediaType(body []byte) types.MediaType {
	var m struct {
		MediaType types.MediaType `json:"mediaType"`
	}

	if err := json.Unmarshal(body, &m); err != nil || m.MediaType == "" {
		return types.OCIManifestSchema1
	}

	return m.MediaType
}

func isSHA256Hex(s string) bool {
	b, err := hex.DecodeString(s)

	return err == nil && len(b) == 32 && strings.ToLower(s) == s
}
