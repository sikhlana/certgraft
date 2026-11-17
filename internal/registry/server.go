package registry

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/sikhlana/pkg/http/httpserver"
)

// ServerName names the registry server; a command runs it with httpserver.ProvideRunner(ServerName).
const ServerName = "registry"

// tracingOrder puts tracing outside the server's other middleware, so a span covers the whole request.
const tracingOrder = -900

// SpecParams is what the server's spec takes from the graph.
type SpecParams struct {
	fx.In

	Config  Config
	Logger  *slog.Logger
	Handler *Handler
}

func newSpec(p SpecParams) httpserver.Spec {
	return httpserver.Spec{Config: p.Config.Server, Handler: newRouter(p.Logger, p.Handler)}
}

// newRouter routes the distribution API. Registry errors are answered in the distribution spec's own format by the
// handler; anything else (a panic, an unrouted path) becomes an RFC 9457 problem.
func newRouter(logger *slog.Logger, h *Handler) *echo.Echo {
	e := echo.NewWithConfig(echo.Config{
		Logger:           logger,
		HTTPErrorHandler: echo.ProblemDetailsHTTPErrorHandler(false),
	})
	e.Use(middleware.Recover())
	h.register(e)

	return e
}

// TracingParams is what the server's tracing middleware takes from the graph.
type TracingParams struct {
	fx.In

	TracerProvider oteltrace.TracerProvider
	Propagator     propagation.TextMapPropagator
}

type tracing struct {
	wrap func(http.Handler) http.Handler
}

func newTracing(p TracingParams) *tracing {
	return &tracing{wrap: otelhttp.NewMiddleware(ServerName,
		otelhttp.WithTracerProvider(p.TracerProvider),
		otelhttp.WithPropagators(p.Propagator),
	)}
}

func (*tracing) Order() int {
	return tracingOrder
}

func (t *tracing) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.Handler) {
	t.wrap(next).ServeHTTP(w, r)
}
