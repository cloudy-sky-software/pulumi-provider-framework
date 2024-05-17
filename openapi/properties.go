package openapi

import (
	"context"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/logging"
)

// FilterReadOnlyProperties recursively removes properties from the inputs map
// that are marked as read-only in the OpenAPI doc.
func FilterReadOnlyProperties(ctx context.Context, doc openapi3.Schema, inputs resource.PropertyMap, discriminatedValue *string) {
	for k, v := range inputs {
		var propSchema *openapi3.SchemaRef
		propKey := string(k)

		logging.V(3).Infof("FilterReadOnlyProperties: checking if %s is a read-only property", k)

		// Don't add the id property to the inputs since it is always provider-assigned
		// and is considered read-only always.
		if propKey == "id" {
			logging.V(3).Info("FilterReadOnlyProperties: deleting id property")
			delete(inputs, k)
			continue
		}

		switch {
		case doc.Properties != nil:
			propSchema = doc.Properties[propKey]
			if propSchema != nil && propSchema.Value.Discriminator != nil {
				dv := inputs[resource.PropertyKey(propSchema.Value.Discriminator.PropertyName)].StringValue()
				FilterReadOnlyProperties(ctx, *propSchema.Value, v.ObjectValue(), &dv)
			}
		case doc.Discriminator != nil:
			mappingRefName := doc.Discriminator.Mapping[*discriminatedValue]
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
								FilterReadOnlyProperties(ctx, *propSchema.Value, v.ObjectValue(), nil)
							}
							break
						}
					}
				}
			}
		default:
			propSchema = doc.NewRef()
		}

		if !v.IsObject() && (propSchema == nil || propSchema.Value.ReadOnly) {
			logging.V(3).Infof("FilterReadOnlyProperties: deleting %s", k)
			delete(inputs, k)
		}
	}
}
