// Package patch adds the extra CA roots to an image without running it: it reads the merged filesystem, appends one
// layer that rewrites each trust point, and points language runtimes at the system bundle through the config's env.
package patch

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"go.uber.org/fx"

	"github.com/sikhlana/certgraft/internal/roots"
)

const (
	// Version changes whenever what a patch produces changes; the cache keys on it.
	Version = "1"

	// LabelPatched carries the root-set fingerprint, or LabelFailed.
	LabelPatched = "io.github.sikhlana.certgraft.patched"

	// LabelPythonStrict is "skipped" when the image already had a sitecustomize.py.
	LabelPythonStrict = "io.github.sikhlana.certgraft.python-strict"

	// LabelFailed is LabelPatched's value on an image a trust point couldn't be patched in.
	LabelFailed = "failed"

	historyCreatedBy = "certgraft: add extra CA roots"
)

// Outcome is what Patch did with an image.
type Outcome int

const (
	// PassedThrough is an image with nothing to patch: serve it unchanged.
	PassedThrough Outcome = iota

	// Patched is an image with the roots added.
	Patched

	// Failed is an image a trust point couldn't be patched in: served unpatched, labelled LabelFailed.
	Failed
)

// Result is a patched image and what was done to it.
type Result struct {
	Outcome Outcome

	// Image is the image to serve; nil when Outcome is PassedThrough.
	Image v1.Image

	// Layer is the layer Patch added; nil unless Outcome is Patched.
	Layer v1.Layer

	// TrustPoint and Err say what failed when Outcome is Failed.
	TrustPoint string
	Err        error
}

// trustPointError is a trust point that couldn't be patched, as opposed to an image that couldn't be read.
type trustPointError struct {
	point string
	err   error
}

func (e *trustPointError) Error() string { return e.point + ": " + e.err.Error() }

func (e *trustPointError) Unwrap() error { return e.err }

// Params is what New takes from the graph.
type Params struct {
	fx.In

	Roots *roots.Set
}

// Patcher patches images with one root set.
type Patcher struct {
	roots *roots.Set
}

// New returns a Patcher for the roots.
func New(p Params) *Patcher {
	return &Patcher{roots: p.Roots}
}

// Patch adds the roots to img. A trust point that can't be patched gives a Failed result; an error means img itself
// couldn't be read (an upstream layer failed to download, say).
func (p *Patcher) Patch(img v1.Image) (Result, error) {
	cf, err := img.ConfigFile()
	if err != nil {
		return Result{}, fmt.Errorf("reading config: %w", err)
	}

	mf, err := img.Manifest()
	if err != nil {
		return Result{}, fmt.Errorf("reading manifest: %w", err)
	}

	env := parseEnv(cf.Config.Env)

	fsys, err := flatten(img, keepFor(env))
	if err != nil {
		return Result{}, err
	}

	pl := &plan{fsys: fsys, env: env, roots: p.roots, files: map[string]file{}, labels: map[string]string{}}

	if err = pl.build(); err != nil {
		if tpe, ok := errors.AsType[*trustPointError](err); ok {
			return p.failed(img, cf.Config, tpe)
		}

		return Result{}, err
	}

	if pl.empty() {
		return Result{Outcome: PassedThrough}, nil
	}

	return p.apply(img, mf, pl)
}

func (p *Patcher) apply(img v1.Image, mf *v1.Manifest, pl *plan) (Result, error) {
	layer, err := newLayer(pl.files, pl.fsys, layerMediaType(mf.MediaType))
	if err != nil {
		return Result{}, err
	}

	out, err := mutate.Append(img, mutate.Addendum{
		Layer:   layer,
		History: v1.History{CreatedBy: historyCreatedBy, Comment: p.roots.Fingerprint()},
	})
	if err != nil {
		return Result{}, fmt.Errorf("appending layer: %w", err)
	}

	cf, err := out.ConfigFile()
	if err != nil {
		return Result{}, fmt.Errorf("reading patched config: %w", err)
	}

	cfg := cf.Config
	cfg.Env = append(slices.Clone(cfg.Env), pl.addEnv...)
	cfg.Labels = withLabels(cfg.Labels, pl.labels, p.roots.Fingerprint())

	out, err = mutate.Config(out, cfg)
	if err != nil {
		return Result{}, fmt.Errorf("writing patched config: %w", err)
	}

	return Result{Outcome: Patched, Image: out, Layer: layer}, nil
}

func (*Patcher) failed(img v1.Image, cfg v1.Config, tpe *trustPointError) (Result, error) {
	cfg.Labels = withLabels(cfg.Labels, nil, LabelFailed)

	out, err := mutate.Config(img, cfg)
	if err != nil {
		return Result{}, fmt.Errorf("labelling failed patch: %w", err)
	}

	return Result{Outcome: Failed, Image: out, TrustPoint: tpe.point, Err: tpe.err}, nil
}

func withLabels(base, extra map[string]string, patched string) map[string]string {
	out := maps.Clone(base)

	if out == nil {
		out = map[string]string{}
	}

	maps.Copy(out, extra)
	out[LabelPatched] = patched

	return out
}

// parseEnv maps a config's KEY=VALUE list; a later duplicate wins, as it does for the runtime.
func parseEnv(list []string) map[string]string {
	env := make(map[string]string, len(list))

	for _, kv := range list {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}

	return env
}
