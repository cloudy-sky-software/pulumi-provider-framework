package openapi

import (
	"context"
	_ "embed"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

//go:embed testdata/render_openapi.yml
var renderOpenAPIEmbed string

func TestFilterReadOnlyProperties(t *testing.T) {
	ctx := context.Background()

	inputs := resource.NewPropertyMapFromMap(map[string]interface{}{
		"id":           "test-id",
		"autoDeploy":   "yes",
		"branch":       "main",
		"notifyOnFail": "default",
		"name":         "Test service",
		"repo":         "https://github.com/cloudy-sky-software/test-static-site",
		"type":         "static_site",
		"createdAt":    "2012-11-06T00:00:00",
		"updatedAt":    "2012-11-06T00:00:00",
		"serviceDetails": map[string]interface{}{
			"publishPath": "public",
			"url":         "https://someurl.onrender.com",
		},
	})

	doc := GetOpenAPISpec([]byte(renderOpenAPIEmbed))

	inputs = FilterReadOnlyProperties(ctx, *doc.Paths["/services"].Post.RequestBody.Value.Content.Get("application/json").Schema.Value, inputs)

	assert.NotNil(t, inputs)
	assert.NotContains(t, inputs, "id")
	assert.NotContains(t, inputs, "createdAt")
	assert.NotContains(t, inputs, "updatedAt")
	assert.NotContains(t, inputs, "serviceDetails")
	assert.True(t, inputs["serviceDetails"].IsObject())
	assert.NotContains(t, inputs["serviceDetails"].ObjectValue(), "url")
}
