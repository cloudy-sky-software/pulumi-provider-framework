package rest

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cloudy-sky-software/pulumi-provider-framework/openapi"
	"github.com/stretchr/testify/assert"

	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

//go:embed testdata/generic/openapi.yml
var genericOpenAPIEmbed string

func makeTestGenericProvider(ctx context.Context, t *testing.T, testServer *httptest.Server) pulumirpc.ResourceProviderServer {
	t.Helper()

	openAPIBytes := []byte(genericOpenAPIEmbed)
	openAPIDoc := openapi.GetOpenAPISpec(openAPIBytes)
	if testServer != nil {
		t.Logf("Setting URL in OpenAPI doc to test server URL: %s", testServer.URL)
		openAPIDoc.Servers[0].URL = testServer.URL
	}

	fakeProvider := &fakeProviderCallback{}

	schemaSpec, metadata, updatedOpenAPIDoc := genericPulumiSchema(openAPIDoc)
	schemaSpec.Version = ""
	schemaJSON, _ := json.MarshalIndent(schemaSpec, "", "    ")

	updatedOpenAPIDocBytes, _ := yaml.Marshal(updatedOpenAPIDoc)
	metadataBytes, _ := json.Marshal(metadata)

	p, err := MakeProvider(nil, "", "", schemaJSON, updatedOpenAPIDocBytes, metadataBytes, fakeProvider)

	if err != nil {
		t.Fatalf("Could not create a provider instance: %v", err)
	}

	_, err = p.Configure(ctx, &pulumirpc.ConfigureRequest{
		Variables: map[string]string{"generic:config:apiKey": "fakeapikey"},
	})

	if err != nil {
		t.Fatalf("Error configuring the provider: %v", err)
	}

	return p
}

func TestTransformSDKNamestoAPINames(t *testing.T) {
	ctx := context.Background()

	p := makeTestGenericProvider(ctx, t, nil)

	t.Run("SDKToAPINames", func(t *testing.T) {
		bodyMap := make(map[string]interface{})
		bodyMap["simpleProp"] = "test"
		bodyMap["objectProp"] = map[string]interface{}{"anotherProp": "a value"}

		provider := p.(*Provider)

		provider.TransformBody(ctx, bodyMap, provider.metadata.SDKToAPINameMap)

		assert.Contains(t, bodyMap, "simple_prop")
		assert.NotContains(t, bodyMap, "simpleProp")

		assert.Contains(t, bodyMap, "object_prop")
		objectProp := bodyMap["object_prop"]
		assert.Contains(t, objectProp, "another_prop")
	})

	t.Run("APIToSDKNames", func(t *testing.T) {
		bodyMap := make(map[string]interface{})
		bodyMap["simple_prop"] = "test"
		bodyMap["object_prop"] = map[string]interface{}{"another_prop": "a value"}

		provider := p.(*Provider)

		provider.TransformBody(ctx, bodyMap, provider.metadata.APIToSDKNameMap)

		assert.Contains(t, bodyMap, "simpleProp")
		assert.NotContains(t, bodyMap, "simple_prop")

		assert.Contains(t, bodyMap, "objectProp")
		objectProp := bodyMap["objectProp"]
		assert.Contains(t, objectProp, "anotherProp")
	})
}
