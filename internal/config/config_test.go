package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/certgraft/internal/config"
	"github.com/sikhlana/certgraft/internal/health"
	"github.com/sikhlana/certgraft/internal/registry"
	"github.com/sikhlana/certgraft/internal/roots"
	"github.com/sikhlana/certgraft/internal/store"
)

func TestProvideDefaultsAndEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CERTGRAFT_REGISTRY_SERVER_PORT", "18080")
	t.Setenv("CERTGRAFT_STORE_DIR", "/data")

	var (
		registryCfg registry.Config
		healthCfg   health.Config
		rootsCfg    roots.Config
		storeCfg    store.Config
	)

	fxtest.New(t,
		config.Provide("certgraft", ""),
		fx.Populate(&registryCfg, &healthCfg, &rootsCfg, &storeCfg),
	).RequireStart().RequireStop()

	assert.Equal(t, 18080, registryCfg.Server.Port)
	assert.Equal(t, registry.DefaultWriteTimeout, registryCfg.Server.WriteTimeout)
	assert.Equal(t, health.DefaultPort, healthCfg.Server.Port)
	assert.Equal(t, roots.DefaultDir, rootsCfg.Dir)
	assert.Equal(t, "/data", storeCfg.Dir)
}
