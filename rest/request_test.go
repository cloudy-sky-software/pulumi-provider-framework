package rest

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"

	"testing"

	"github.com/cloudy-sky-software/pulumi-provider-framework/openapi"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
	"github.com/stretchr/testify/assert"
)

//go:embed testdata/tailscale/openapi.yml
var tailscaleOpenAPIEmbed string

//go:embed testdata/tailscale/metadata.json
var tailscaleMetadataEmbed string

//go:embed testdata/tailscale/schema.json
var tailscalePulSchemaEmbed string

func makeTestProvider(ctx context.Context, t *testing.T) pulumirpc.ResourceProviderServer {
	t.Helper()

	openapiBytes := []byte(tailscaleOpenAPIEmbed)
	d := openapi.GetOpenAPISpec(openapiBytes)
	d.Servers[0].URL = "http://localhost:8080"

	fakeProvider := &fakeProviderCallback{}

	p, err := MakeProvider(nil, "", "", []byte(tailscalePulSchemaEmbed), openapiBytes, []byte(tailscaleMetadataEmbed), fakeProvider)

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

func TestRemovePathParamsFromRequestBody(t *testing.T) {
	ctx := context.Background()
	testCreateJSONPayload := `{
		"capabilities": {
			"devices": {
				"create": {
					"ephemeral": true,
					"preauthorized": true,
					"reusable": true,
					"tags": ["admin:authorized"]
				}
			}
		},
		"expirySeconds": 300,
		"tailnet": "mytailnet@tailscale.com"
	}
	`
	var inputs map[string]interface{}
	if err := json.Unmarshal([]byte(testCreateJSONPayload), &inputs); err != nil {
		t.Fatalf("Failed to unmarshal test payload: %v", err)
	}

	p := makeTestProvider(ctx, t)
	httpReq, err := p.(Request).CreatePostRequest(ctx, "/tailnet/{tailnet}/keys", []byte(testCreateJSONPayload), resource.NewPropertyMapFromMap(inputs))
	assert.Nil(t, err)
	assert.NotNil(t, httpReq)

	var bodyMap map[string]interface{}

	body, _ := io.ReadAll(httpReq.Body)
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		t.Fatalf("unmarshaling body: %v", err)
	}

	_, ok := bodyMap["tailnet"]
	assert.False(t, ok, "Expected tailnet to be removed from the HTTP request.")
}
