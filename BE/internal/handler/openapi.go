package handler

import (
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/openapi"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// OpenAPIHandler serves the public API reference of this release.
type OpenAPIHandler struct {
	document []byte
}

// NewOpenAPIHandler stamps the checked-in document with the running release.
func NewOpenAPIHandler(version string) *OpenAPIHandler {
	document, err := openapi.Document(version)
	if err != nil {
		// The embedded file is verified by the openapi package's tests.
		panic("openapi: embedded document is invalid: " + err.Error())
	}
	return &OpenAPIHandler{document: document}
}

// Spec returns the OpenAPI 3.1 document. It is public: it describes the API
// and holds no workspace data.
func (h *OpenAPIHandler) Spec(c *echo.Context) error {
	if h == nil {
		return response.NotFound(c, "API reference")
	}
	c.Response().Header().Set("Cache-Control", "public, max-age=300")
	return c.Blob(http.StatusOK, echo.MIMEApplicationJSON, h.document)
}
