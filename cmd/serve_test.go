package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/kernelrun"
	"github.com/sikhlana/pkg/log"

	"github.com/sikhlana/certgraft/cmd"
	"github.com/sikhlana/certgraft/internal/app"
	"github.com/sikhlana/certgraft/internal/store"
)

// TestServeGraph resolves serve's whole graph without running it, so a missing provider fails here rather than as a
// crash at boot.
func TestServeGraph(t *testing.T) {
	err := fx.ValidateApp(
		log.Module,
		app.Config(""),
		app.Modules(),
		cmd.ServeRunners(),
		fx.Invoke(fx.Annotate(func([]kernelrun.Runner) {}, fx.ParamTags(kernelrun.RunnerGroup))),
	)
	require.NoError(t, err)
}

// TestPruneGraph does the same for prune's runner.
func TestPruneGraph(t *testing.T) {
	err := fx.ValidateApp(
		log.Module,
		app.Config(""),
		app.Modules(),
		store.WithPruneRequest(store.PruneRequest{}),
		store.ProvidePruneRunner(),
		fx.Invoke(fx.Annotate(func([]kernelrun.Runner) {}, fx.ParamTags(kernelrun.RunnerGroup))),
	)
	require.NoError(t, err)
}
