// Package health runs the health server: liveness and readiness probes on their own listener.
package health

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

// newRouter routes the probes, answering every error as an RFC 9457 problem.
func newRouter(logger *slog.Logger) *echo.Echo {
	e := echo.NewWithConfig(echo.Config{
		Logger:           logger,
		HTTPErrorHandler: echo.ProblemDetailsHTTPErrorHandler(false),
	})
	e.Use(middleware.Recover())
	e.GET("/healthz", live)
	e.GET("/readyz", ready)

	return e
}

func live(c *echo.Context) error {
	return c.NoContent(http.StatusNoContent)
}

// ready answers once the app has started: the servers bind in OnStart, so a server that answers is ready.
func ready(c *echo.Context) error {
	return c.NoContent(http.StatusNoContent)
}
