package registry

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"

	"github.com/sikhlana/certgraft/internal/upstream"
)

// Distribution spec error codes the handler answers with.
const (
	codeNameUnknown     = "NAME_UNKNOWN"
	codeNameInvalid     = "NAME_INVALID"
	codeManifestUnknown = "MANIFEST_UNKNOWN"
	codeDigestInvalid   = "DIGEST_INVALID"
	codeUnsupported     = "UNSUPPORTED"
	codeUnknown         = "UNKNOWN"
)

type registryError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type registryErrors struct {
	Errors []registryError `json:"errors"`
}

// HandlerParams is what NewHandler takes from the graph.
type HandlerParams struct {
	fx.In

	Service *Service
	Logger  *slog.Logger
}

// Handler serves the read side of the OCI distribution API: /v2/, manifests and blobs.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(p HandlerParams) *Handler {
	return &Handler{service: p.Service, logger: p.Logger}
}

func (h *Handler) register(e *echo.Echo) {
	methods := []string{http.MethodGet, http.MethodHead}
	e.Match(methods, "/v2/", h.ping)
	e.Match(methods, "/v2/*", h.serve)
}

func (*Handler) ping(c *echo.Context) error {
	c.Response().Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	return c.Blob(http.StatusOK, echo.MIMEApplicationJSON, []byte("{}"))
}

// serve splits /v2/<name>/manifests/<ref> and /v2/<name>/blobs/<digest>. The name may hold slashes, so the split is
// on the last /manifests/ or /blobs/.
func (h *Handler) serve(c *echo.Context) error {
	c.Response().Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	rest := "/" + c.Param("*")

	for _, kind := range []string{"/manifests/", "/blobs/"} {
		i := strings.LastIndex(rest, kind)

		if i <= 0 {
			continue
		}

		repo, err := upstream.ParseRepository(rest[1:i])
		if err != nil {
			return h.fail(c, err)
		}

		if kind == "/manifests/" {
			return h.manifest(c, repo, rest[i+len(kind):])
		}

		return h.blob(c, repo, rest[i+len(kind):])
	}

	return writeError(c, http.StatusNotFound, codeUnsupported, "only manifests and blobs are served")
}

func (h *Handler) manifest(c *echo.Context, repo name.Repository, ref string) error {
	m, err := h.service.Manifest(c.Request().Context(), repo, ref)
	if err != nil {
		return h.fail(c, err)
	}

	hdr := c.Response().Header()
	hdr.Set("Docker-Content-Digest", m.Digest.String())
	hdr.Set(echo.HeaderContentLength, strconv.Itoa(len(m.Body)))

	return c.Blob(http.StatusOK, string(m.MediaType), m.Body)
}

func (h *Handler) blob(c *echo.Context, repo name.Repository, ref string) error {
	digest, err := v1.NewHash(ref)
	if err != nil {
		return writeError(c, http.StatusBadRequest, codeDigestInvalid, err.Error())
	}

	b, err := h.service.Blob(c.Request().Context(), repo, digest)
	if err != nil {
		return h.fail(c, err)
	}

	hdr := c.Response().Header()
	hdr.Set("Docker-Content-Digest", digest.String())
	hdr.Set(echo.HeaderContentLength, strconv.FormatInt(b.Size, 10))
	hdr.Set(echo.HeaderContentType, echo.MIMEOctetStream)

	if c.Request().Method == http.MethodHead {
		return c.NoContent(http.StatusOK)
	}

	rc, err := b.Open()
	if err != nil {
		return h.fail(c, err)
	}
	defer rc.Close()

	return c.Stream(http.StatusOK, echo.MIMEOctetStream, rc)
}

// fail answers err as a distribution spec error. Upstream's own errors pass through with their status, so a client
// sees what it would have seen without the registry.
func (h *Handler) fail(c *echo.Context, err error) error {
	if errors.Is(err, upstream.ErrNotAllowed) {
		return writeError(c, http.StatusNotFound, codeNameUnknown, err.Error())
	}

	if errors.Is(err, ErrManifestUnknown) {
		return writeError(c, http.StatusNotFound, codeManifestUnknown, err.Error())
	}

	if _, ok := errors.AsType[*name.ErrBadName](err); ok {
		return writeError(c, http.StatusBadRequest, codeNameInvalid, err.Error())
	}

	if te, ok := errors.AsType[*transport.Error](err); ok && te.StatusCode < http.StatusInternalServerError {
		body := registryErrors{}

		for _, d := range te.Errors {
			body.Errors = append(body.Errors, registryError{Code: string(d.Code), Message: d.Message})
		}

		if len(body.Errors) == 0 {
			body.Errors = []registryError{{Code: codeUnknown, Message: err.Error()}}
		}

		return c.JSON(te.StatusCode, body)
	}

	h.logger.ErrorContext(c.Request().Context(), "registry request failed",
		slog.String("path", c.Request().URL.Path),
		slog.Any("error", err),
	)

	return writeError(c, http.StatusBadGateway, codeUnknown, err.Error())
}

func writeError(c *echo.Context, status int, code, message string) error {
	return c.JSON(status, registryErrors{Errors: []registryError{{Code: code, Message: message}}})
}
