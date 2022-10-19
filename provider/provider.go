package provider

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"

	pschema "github.com/pulumi/pulumi/pkg/v3/codegen/schema"
)

type ProviderHandler interface {
	GetOpenAPIDoc() openapi3.T
	GetSchemaSpec() pschema.PackageSpec
	GetBaseURL() string
	GetHttpClient() *http.Client
}
