package rest

import (
	"context"
	"fmt"
	"strings"

	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

func (p *Provider) convertInvokeOutput(_ context.Context, req *pulumirpc.InvokeRequest, outputs interface{}) (map[string]interface{}, error) {
	invokeTypeToken := req.GetTok()

	// Return non-list operations as-is.
	if !strings.Contains(invokeTypeToken, ":list") {
		return outputs.(map[string]interface{}), nil
	}

	schemaSpec := p.GetSchemaSpec()
	funcSpec, ok := schemaSpec.Functions[invokeTypeToken]
	if !ok {
		return nil, fmt.Errorf("function definition (type token: %q) not found in schema spec", invokeTypeToken)
	}

	// If the return type for this function has an object
	// spec, it means it is already properly wrapped in a
	// JSON object.
	if funcSpec.ReturnType.ObjectTypeSpec == nil {
		return outputs.(map[string]interface{}), nil
	}

	// Otherwise, it is a naked array response that should
	// be enveloped by an `items` property in a new object.
	m := make(map[string]interface{})
	m["items"] = outputs
	return m, nil
}
