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
			"buildCommand":               "make render-build",
			"publishPath":                "public",
			"pullRequestPreviewsEnabled": "yes",
			"url":                        "https://someurl.onrender.com",
		},
	})

	doc := GetOpenAPISpec([]byte(renderOpenAPIEmbed))

	FilterReadOnlyProperties(ctx, *doc.Paths["/services"].Post.RequestBody.Value.Content.Get("application/json").Schema.Value, inputs)

	assert.NotNil(t, inputs)

	inputsMap := inputs.Mappable()
	assert.Contains(t, inputsMap, "name")
	assert.Equal(t, "Test service", inputsMap["name"])

	assert.Contains(t, inputsMap, "autoDeploy")
	assert.Equal(t, "yes", inputsMap["autoDeploy"])

	assert.NotContains(t, inputsMap, "id")
	assert.NotContains(t, inputsMap, "createdAt")
	assert.NotContains(t, inputsMap, "updatedAt")
	assert.Contains(t, inputsMap, "serviceDetails")

	assert.True(t, inputs["serviceDetails"].IsObject())
	serviceDetails := inputs["serviceDetails"].ObjectValue().Mappable()

	assert.NotContains(t, serviceDetails, "url")

	assert.Contains(t, serviceDetails, "buildCommand")
	assert.Equal(t, "make render-build", serviceDetails["buildCommand"])

	assert.Contains(t, serviceDetails, "publishPath")
	assert.Equal(t, "public", serviceDetails["publishPath"])

	assert.Contains(t, serviceDetails, "pullRequestPreviewsEnabled")
	assert.Equal(t, "yes", serviceDetails["pullRequestPreviewsEnabled"])
}
