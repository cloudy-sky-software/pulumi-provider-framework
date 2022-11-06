package openapi

import (
	"context"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// FilterReadOnlyProperties recursively removes properties from the inputs map
// that are marked as read-only in the OpenAPI doc.
func FilterReadOnlyProperties(ctx context.Context, doc openapi3.Schema, inputs resource.PropertyMap) resource.PropertyMap {
	filtered := resource.NewPropertyMapFromMap(map[string]interface{}{})

	for k, v := range inputs {
		var propSchema *openapi3.SchemaRef
		propKey := string(k)

		// Don't add the id property to the inputs since it is always provider-assigned
		// and is considered read-only always.
		if propKey == "id" {
			continue
		}

		switch {
		case doc.Properties != nil:
			propSchema = doc.Properties[propKey]
		case doc.Discriminator != nil:
			discriminatorValue := inputs[resource.PropertyKey(doc.Discriminator.PropertyName)]
			mappingRefName := doc.Discriminator.Mapping[discriminatorValue.StringValue()]
			for _, schema := range doc.OneOf {
				if schema.Ref != mappingRefName {
					continue
				}

				switch {
				case schema.Value.Properties != nil:
					propSchema = schema.Value.Properties[propKey]
				case schema.Value.AllOf != nil:
					for _, schemaRef := range schema.Value.AllOf {
						if schemaRef.Value.Properties == nil {
							continue
						}

						var found bool
						propSchema, found = schemaRef.Value.Properties[propKey]
						if found {
							if v.IsObject() {
								filtered[k] = resource.NewPropertyValue(FilterReadOnlyProperties(ctx, *propSchema.Value, v.ObjectValue()))
							}
							break
						}
					}
				}
			}
		default:
			propSchema = doc.NewRef()
		}

		if !v.IsObject() && !propSchema.Value.ReadOnly {
			filtered[k] = v
		}
	}

	return filtered
}
