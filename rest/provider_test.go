package rest

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	"testing"

	"github.com/cloudy-sky-software/pulumi-provider-framework/openapi"
	"github.com/cloudy-sky-software/pulumi-provider-framework/state"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/stretchr/testify/assert"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"

	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

//go:embed testdata/tailscale/openapi.yml
var tailscaleOpenAPIEmbed string

//go:embed testdata/tailscale/metadata.json
var tailscaleMetadataEmbed string

//go:embed testdata/tailscale/schema.json
var tailscalePulSchemaEmbed string

func makeTestTailscaleProvider(ctx context.Context, t *testing.T, testServer *httptest.Server) pulumirpc.ResourceProviderServer {
	t.Helper()

	openapiBytes := []byte(tailscaleOpenAPIEmbed)
	d := openapi.GetOpenAPISpec(openapiBytes)
	if testServer != nil {
		t.Logf("Setting URL in OpenAPI doc to test server URL: %s", testServer.URL)
		d.Servers[0].URL = testServer.URL

		var err error
		openapiBytes, err = d.MarshalJSON()
		assert.Nil(t, err, "Failed to marshal updated OpenAPI doc: %v", err)
	}

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

func TestResourceReadResultsInNoChanges(t *testing.T) {
	ctx := context.Background()

	inputsJSON := `{
		"capabilities": {
			"devices": {
				"create": {
					"ephemeral": true,
					"preauthorized": true,
					"reusable": true,
					"tags": ["tag:sometag"]
				}
			}
		},
		"expirySeconds": 300,
		"tailnet": "mytailnet@tailscale.com"
	}`
	outputsJSON := `{"capabilities": {"devices": {"create": {"ephemeral": true,"preauthorized": true,"reusable": true,"tags": ["tag:sometag"]}}},"created": "2023-04-24T02:19:42Z","expires": "2023-07-23T02:19:42Z","id": "kTaPCj2CNTRL"}`

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tailnet/mytailnet@tailscale.com/keys/kTaPCj2CNTRL" {
			_, err := io.WriteString(w, outputsJSON)
			if err != nil {
				t.Errorf("Error writing string to the response stream: %v", err)
			}
			return
		}

		_, err := io.WriteString(w, "Unknown path")
		if err != nil {
			t.Errorf("Error writing string to the response stream: %v", err)
		}
	}))
	testServer.EnableHTTP2 = true
	testServer.Start()

	defer testServer.Close()

	p := makeTestTailscaleProvider(ctx, t, testServer)

	var inputs map[string]interface{}
	if err := json.Unmarshal([]byte(inputsJSON), &inputs); err != nil {
		t.Fatalf("Failed to unmarshal test payload: %v", err)
	}

	inputsPropertyMap := resource.NewPropertyMapFromMap(inputs)
	inputsRecord, err := plugin.MarshalProperties(inputsPropertyMap, state.DefaultMarshalOpts)
	if err != nil {
		t.Fatalf("Failed to marshal input map: %v", err)
	}

	var outputsMap map[string]interface{}
	if err := json.Unmarshal([]byte(outputsJSON), &outputsMap); err != nil {
		t.Fatalf("Failed to unmarshal test payload: %v", err)
	}

	expectedOutputState := state.GetResourceState(outputsMap, inputsPropertyMap)
	serializedOutputState, err := plugin.MarshalProperties(expectedOutputState, state.DefaultMarshalOpts)
	if err != nil {
		t.Fatalf("Marshaling the output properties map: %v", err)
	}

	readResp, err := p.Read(ctx, &pulumirpc.ReadRequest{
		Id:         "kTaPCj2CNTRL",
		Inputs:     inputsRecord,
		Properties: serializedOutputState,
		Urn:        "urn:pulumi:some-stack::some-project::tailscale-native:tailnet:Key::myAuthKey",
	})
	assert.Nil(t, err)
	assert.NotNil(t, readResp)

	actualOutputState, err := plugin.UnmarshalProperties(readResp.GetProperties(), state.DefaultUnmarshalOpts)
	assert.Nil(t, err, "Failed to unmarshal output properties struct: %v", err)
	actualStashedInputState := state.GetOldInputs(actualOutputState)

	// The read operation should not have modified the stashed inputs
	// because the read would have returned read-only properties
	// in the response which should not be serialized into
	// the stashed inputs.
	assert.Equal(t, inputsPropertyMap, actualStashedInputState, "Expected the stashed inputs to match the origin inputs")
}

func TestImports(t *testing.T) {
	ctx := context.Background()

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/fakeresource/fake-id" {
			_, err := io.WriteString(w, `{"another_prop":"somevalue"}`)
			if err != nil {
				t.Errorf("Error writing string to the response stream: %v", err)
			}
			return
		}

		_, err := io.WriteString(w, "Unknown path")
		if err != nil {
			t.Errorf("Error writing string to the response stream: %v", err)
		}
	}))
	testServer.EnableHTTP2 = true
	testServer.Start()

	defer testServer.Close()

	p := makeTestGenericProvider(ctx, t, testServer)

	readResp, err := p.Read(ctx, &pulumirpc.ReadRequest{
		Id:         "/fake-id",
		Inputs:     nil,
		Properties: nil,
		Urn:        "urn:pulumi:some-stack::some-project::generic:fakeresource/v2:FakeResource::myResource",
	})
	assert.Nil(t, err)
	assert.NotNil(t, readResp)
	// Note how we check for anotherProp instead of another_prop
	// as it is in the OpenAPI. That's because we would have
	// applied a transformation on the response body before
	// it's serialized into the state.
	assert.Contains(t, readResp.GetProperties().AsMap(), "anotherProp")
}

func TestDiffForUpdateableResource(t *testing.T) {
	ctx := context.Background()

	oldInputsJSON := `{
		"object_prop": {
			"another_prop": "a value"
		}
	}`

	newInputsJSON := `{
		"object_prop": {
			"another_prop": "a value"
		},
		"simple_prop": "new value"
	}`
	outputsJSON := `{"another_prop":"output value"}`

	p := makeTestGenericProvider(ctx, t, nil)

	getMarshaledProps := func(jsonStr string) (*structpb.Struct, resource.PropertyMap) {
		var inputs map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &inputs); err != nil {
			t.Fatalf("Failed to unmarshal test payload: %v", err)
		}

		inputsPropertyMap := resource.NewPropertyMapFromMap(inputs)
		inputsRecord, err := plugin.MarshalProperties(inputsPropertyMap, state.DefaultMarshalOpts)
		if err != nil {
			t.Fatalf("Failed to marshal input map: %v", err)
		}

		return inputsRecord, inputsPropertyMap
	}

	newInputs, _ := getMarshaledProps(newInputsJSON)
	oldInputs, oldInputsPropertyMap := getMarshaledProps(oldInputsJSON)

	var outputsMap map[string]interface{}
	if err := json.Unmarshal([]byte(outputsJSON), &outputsMap); err != nil {
		t.Fatalf("Failed to unmarshal test payload: %v", err)
	}

	expectedOutputState := state.GetResourceState(outputsMap, oldInputsPropertyMap)
	serializedOutputState, err := plugin.MarshalProperties(expectedOutputState, state.DefaultMarshalOpts)
	if err != nil {
		t.Fatalf("Marshaling the output properties map: %v", err)
	}

	diffResp, err := p.Diff(ctx, &pulumirpc.DiffRequest{
		Id:        "fake-id",
		Olds:      serializedOutputState,
		News:      newInputs,
		OldInputs: oldInputs,
		Type:      "generic:fakeresource/v2:FakeResource",
		Name:      "myResource",
		Urn:       "urn:pulumi:some-stack::some-project::generic:fakeresource/v2:FakeResource::myResource",
	})
	assert.Nil(t, err)
	assert.NotNil(t, diffResp)
	assert.Contains(t, diffResp.Diffs, "simple_prop")
}
