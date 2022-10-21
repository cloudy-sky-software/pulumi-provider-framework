package openapi

import (
	"context"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

// GetOpenAPISpec returns a parsed and validated openapi doc.
func GetOpenAPISpec(data []byte) *openapi3.T {
	doc, err := openapi3.NewLoader().LoadFromData(data)
	if err != nil {
		contract.Failf("Failed to load openapi.yml: %v", err)
	}

	ctx := context.Background()
	if err := doc.Validate(ctx); err != nil {
		contract.Failf("OpenAPI spec failed validation: %v", err)
	}

	return doc
}
