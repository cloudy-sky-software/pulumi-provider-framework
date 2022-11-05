package openapi

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"

	pschema "github.com/pulumi/pulumi/pkg/v3/codegen/schema"
)

// Handler is an interface implemented by OpenAPI-based resource providers.
type Handler interface {
	// GetOpenAPIDoc returns the parsed OpenAPI3 doc.
	GetOpenAPIDoc() openapi3.T
	// GetSchemaSpec returns the unmarshaled Pulumi schema spec.
	GetSchemaSpec() pschema.PackageSpec
	// GetBaseURL returns the base URL for the provider's API.
	GetBaseURL() string
	// GetHTTPClient returns an authenticated HTTP client used to execute
	// the provider's API operations.
	GetHTTPClient() *http.Client
}
