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
		Variables: map[string]string{"tailscale:config:apiKey": "fakeapikey"},
	})

	if err != nil {
		t.Fatalf("Error configuring the provider: %v", err)
	}

	return p
}

func TestTransformSDKNamestoAPINames(t *testing.T) {
	ctx := context.Background()

	p := makeTestGenericProvider(ctx, t, nil)

	bodyMap := make(map[string]interface{})
	bodyMap["simpleProp"] = "test"

	provider := p.(*Provider)

	provider.TransformSDKNamestoAPINames(ctx, bodyMap)

	assert.Contains(t, bodyMap, "simple_prop")
	assert.NotContains(t, bodyMap, "simpleProp")
}
