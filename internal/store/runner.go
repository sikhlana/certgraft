package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"go.uber.org/fx"

	"github.com/sikhlana/pkg/kernelrun"
)

const (
	// DefaultPruneRunnerName is the runner ProvidePruneRunner registers.
	DefaultPruneRunnerName = "prune"

	// DefaultPruneDays is how long an entry may go unrequested before prune removes it.
	DefaultPruneDays = 30
)

// ErrPruneDays is a prune request for fewer than one day.
var ErrPruneDays = errors.New("--days must be at least 1")

// PruneRequest is what one prune run does, supplied by the command from its flags.
type PruneRequest struct {
	// Days is how long an entry may go unrequested.
	Days int

	// Out receives the summary line.
	Out io.Writer
}

// WithPruneRequest supplies the PruneRequest the runner serves.
func WithPruneRequest(r PruneRequest) fx.Option {
	return fx.Supply(r)
}

// PruneRunnerParams is what the prune runner takes from the graph.
type PruneRunnerParams struct {
	fx.In

	Store   *Store
	Request PruneRequest
}

// ProvidePruneRunner opts a command in to pruning the cache once. The runner returns when done, which ends the app.
func ProvidePruneRunner() fx.Option {
	return kernelrun.ProvideRunner(DefaultPruneRunnerName, newPruneRunner)
}

func newPruneRunner(p PruneRunnerParams) kernelrun.Func {
	return func(context.Context) error {
		if p.Request.Days < 1 {
			return ErrPruneDays
		}

		res, err := p.Store.Prune(time.Now().AddDate(0, 0, -p.Request.Days))
		if err != nil {
			return fmt.Errorf("pruning cache: %w", err)
		}

		if _, err = fmt.Fprintf(
			p.Request.Out,
			"pruned %d image(s), %s\n",
			res.Images,
			FormatSize(res.Bytes),
		); err != nil {
			return fmt.Errorf("writing prune summary: %w", err)
		}

		return nil
	}
}
