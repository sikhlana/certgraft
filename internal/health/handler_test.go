package health_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sikhlana/certgraft/internal/health"
)

func TestProbes(t *testing.T) {
	e := health.NewRouter(slog.New(slog.DiscardHandler))

	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

			assert.Equal(t, http.StatusNoContent, rec.Code)
		})
	}
}

func TestUnknownPathIsProblemJSON(t *testing.T) {
	e := health.NewRouter(slog.New(slog.DiscardHandler))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/missing", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
}
