package rest

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudy-sky-software/pulumi-provider-framework/callback"
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

const fakeResourceBaseURLPath = "/v2/fakeresource"
const fakeResourceID = "fake-id"

func makeTestTailscaleProvider(ctx context.Context, t *testing.T, testServer *httptest.Server, providerCallback callback.ProviderCallback) pulumirpc.ResourceProviderServer {
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

	var testProviderCallback callback.ProviderCallback
	if providerCallback == nil {
		testProviderCallback = &fakeProviderCallback{}
	} else {
		testProviderCallback = providerCallback
	}

	p, err := MakeProvider(nil, "", "", []byte(tailscalePulSchemaEmbed), openapiBytes, []byte(tailscaleMetadataEmbed), testProviderCallback)

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

func getMarshaledProps(t *testing.T, jsonStr string) (*structpb.Struct, resource.PropertyMap) {
	t.Helper()

	var inputs map[string]any
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

	p := makeTestTailscaleProvider(ctx, t, testServer, nil)

	var inputs map[string]any
	if err := json.Unmarshal([]byte(inputsJSON), &inputs); err != nil {
		t.Fatalf("Failed to unmarshal test payload: %v", err)
	}

	inputsPropertyMap := resource.NewPropertyMapFromMap(inputs)
	inputsRecord, err := plugin.MarshalProperties(inputsPropertyMap, state.DefaultMarshalOpts)
	if err != nil {
		t.Fatalf("Failed to marshal input map: %v", err)
	}

	var outputsMap map[string]any
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
		if r.URL.Path == fakeResourceByIDURLPath {
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

	p := makeTestGenericProvider(ctx, t, testServer, nil)

	readResp, err := p.Read(ctx, &pulumirpc.ReadRequest{
		Id:         fmt.Sprintf("/%s", fakeResourceID),
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
		"objectProp": {
			"anotherProp": "a value"
		}
	}`

	newInputsJSON := `{
		"objectProp": {
			"anotherProp": "a value"
		},
		"simpleProp": "new value"
	}`
	outputsJSON := `{"anotherProp":"output value"}`

	p := makeTestGenericProvider(ctx, t, nil, nil)

	newInputs, _ := getMarshaledProps(t, newInputsJSON)
	oldInputs, oldInputsPropertyMap := getMarshaledProps(t, oldInputsJSON)

	var outputsMap map[string]any
	if err := json.Unmarshal([]byte(outputsJSON), &outputsMap); err != nil {
		t.Fatalf("Failed to unmarshal test payload: %v", err)
	}

	expectedOutputState := state.GetResourceState(outputsMap, oldInputsPropertyMap)
	serializedOutputState, err := plugin.MarshalProperties(expectedOutputState, state.DefaultMarshalOpts)
	if err != nil {
		t.Fatalf("Marshaling the output properties map: %v", err)
	}

	diffResp, err := p.Diff(ctx, &pulumirpc.DiffRequest{
		Id:        fakeResourceID,
		Olds:      serializedOutputState,
		News:      newInputs,
		OldInputs: oldInputs,
		Type:      "generic:fakeresource/v2:FakeResource",
		Name:      "myResource",
		Urn:       "urn:pulumi:some-stack::some-project::generic:fakeresource/v2:FakeResource::myResource",
	})
	assert.Nil(t, err)
	assert.NotNil(t, diffResp)
	assert.Contains(t, diffResp.Diffs, "simpleProp")
}

func TestUpdateForUpdateableResource(t *testing.T) {
	ctx := context.Background()

	outputsJSON := fmt.Sprintf(`{"id":"%s", "another_prop":"output value"}`, fakeResourceID)

	validateDiscriminatedRequest := false

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			b, _ := io.ReadAll(r.Body)
			var reqBody map[string]any
			err := json.Unmarshal(b, &reqBody)
			if err != nil {
				t.Errorf("Error unmarshaling JSON request body to map: %v", err)
				return
			}

			val := reqBody["simple_prop"]
			assert.IsType(t, "string", val, "Expected string value for simple_prop")
			assert.Equal(t, "new value", val.(string))

			if validateDiscriminatedRequest {
				discriminatorVal := reqBody["type"]
				assert.IsType(t, "string", discriminatorVal, "Expected string value for discriminator value")
				assert.Equal(t, "background_worker", discriminatorVal.(string))
			}

			_, ok := reqBody["object_prop"]
			assert.False(t, ok, "object_prop should not be in the request body since it was not updated")

			_, err = io.WriteString(w, outputsJSON)
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

	p := makeTestGenericProvider(ctx, t, testServer, nil)

	tests := []struct {
		oldInputsJSON        string
		newInputsJSON        string
		resourceTypeToken    string
		discriminatedRequest bool
	}{
		{
			oldInputsJSON: `{
				"object_prop": {
					"another_prop": "a value"
				},
				"type": "background_worker"
			}`,
			newInputsJSON: `{
				"object_prop": {
					"another_prop": "a value"
				},
				"simple_prop": "new value"
			}`,
			resourceTypeToken:    "generic:discriminatedresource/v2:BackgroundWorker",
			discriminatedRequest: true,
		},
		{
			oldInputsJSON: `{
				"object_prop": {
					"another_prop": "a value"
				}
			}`,
			newInputsJSON: `{
				"object_prop": {
					"another_prop": "a value"
				},
				"simple_prop": "new value"
			}`,
			resourceTypeToken: "generic:fakeresource/v2:FakeResource",
		},
	}

	for _, test := range tests {
		validateDiscriminatedRequest = test.discriminatedRequest
		oldInputsJSON := test.oldInputsJSON

		newInputsJSON := test.newInputsJSON
		newInputs, _ := getMarshaledProps(t, newInputsJSON)
		oldInputs, oldInputsPropertyMap := getMarshaledProps(t, oldInputsJSON)

		var outputsMap map[string]any
		if err := json.Unmarshal([]byte(outputsJSON), &outputsMap); err != nil {
			t.Fatalf("Failed to unmarshal test payload: %v", err)
		}

		expectedOutputState := state.GetResourceState(outputsMap, oldInputsPropertyMap)
		serializedOutputState, err := plugin.MarshalProperties(expectedOutputState, state.DefaultMarshalOpts)
		if err != nil {
			t.Fatalf("Marshaling the output properties map: %v", err)
		}

		updateResp, err := p.Update(ctx, &pulumirpc.UpdateRequest{
			Id:        fakeResourceID,
			Olds:      serializedOutputState,
			News:      newInputs,
			OldInputs: oldInputs,
			Type:      test.resourceTypeToken,
			Name:      "myResource",
			Urn:       fmt.Sprintf("urn:pulumi:some-stack::some-project::%s::myResource", test.resourceTypeToken),
		})
		assert.Nil(t, err)
		assert.NotNil(t, updateResp)
	}
}

func TestCreateWithSecretInput(t *testing.T) {
	ctx := context.Background()

	secretValue := "secretValue"

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fakeResourceBaseURLPath && r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			var reqBody map[string]any
			err := json.Unmarshal(b, &reqBody)
			if err != nil {
				t.Errorf("Error unmarshaling JSON request body to map: %v", err)
				return
			}

			val := reqBody["simple_prop"]
			assert.IsType(t, "string", val, "Expected string value for simple_prop")
			assert.Equal(t, secretValue, val.(string))

			_, err = io.WriteString(w, `{"id":"fakeId", "another_prop":"somevalue"}`)
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

	p := makeTestGenericProvider(ctx, t, testServer, nil)

	propMap := resource.NewPropertyMapFromMap(map[string]any{
		"simpleProp": resource.NewSecretProperty(&resource.Secret{Element: resource.NewStringProperty(secretValue)}),
		"objectProp": resource.NewPropertyMapFromMap(map[string]any{
			"anotherProp": resource.NewStringProperty("plainValue"),
		}),
	})
	props, err := plugin.MarshalProperties(propMap, state.DefaultMarshalOpts)
	assert.Nil(t, err)

	createResp, err := p.Create(ctx, &pulumirpc.CreateRequest{
		Name:       "myResource",
		Properties: props,
		Type:       "fakeresource/v2:FakeResource",
		Urn:        "urn:pulumi:some-stack::some-project::generic:fakeresource/v2:FakeResource::myResource",
	})
	assert.Nil(t, err)
	assert.NotNil(t, createResp)
	assert.Contains(t, createResp.GetProperties().AsMap(), "anotherProp")
}

func TestApiHostOverride(t *testing.T) {
	ctx := context.Background()

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	testServer.EnableHTTP2 = true
	testServer.Start()

	defer testServer.Close()
	p := makeTestGenericProvider(ctx, t, testServer, nil)

	const expectedHost = "10.1.1.1"
	_, err := p.Configure(ctx, &pulumirpc.ConfigureRequest{
		Variables: map[string]string{"generic:config:apiHost": expectedHost},
	})
	assert.Nil(t, err)
	assert.Equal(t, fmt.Sprintf("http://%s", expectedHost), p.(*Provider).GetBaseURL())

	// verify requests are still matched when using an overridden api host name
	request, err := p.(*Provider).CreateGetRequest(ctx, "/v2/fakeresource/{resourceId}", resource.NewPropertyMapFromMap(map[string]any{"resourceId": "12345"}))
	assert.Nil(t, err)
	assert.Equal(t, request.URL.String(), fmt.Sprintf("http://%s/v2/fakeresource/12345", expectedHost), "Expected request to be matched when using an overridden api host name")
}

func TestApiHostOverrideViaEnvVar(t *testing.T) {
	ctx := context.Background()

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	testServer.EnableHTTP2 = true
	testServer.Start()

	defer testServer.Close()
	p := makeTestGenericProvider(ctx, t, testServer, nil)

	const expectedHost = "10.1.1.1"
	t.Setenv("GENERIC_API_HOST", expectedHost)
	_, err := p.Configure(ctx, &pulumirpc.ConfigureRequest{})
	assert.Nil(t, err)
	assert.Equal(t, fmt.Sprintf("http://%s", expectedHost), p.(*Provider).GetBaseURL())

	// verify requests are still matched when using an overridden api host name
	request, err := p.(*Provider).CreateGetRequest(ctx, "/v2/fakeresource/{resourceId}", resource.NewPropertyMapFromMap(map[string]any{"resourceId": "12345"}))
	assert.Nil(t, err)
	assert.Equal(t, request.URL.String(), fmt.Sprintf("http://%s/v2/fakeresource/12345", expectedHost), "Expected request to be matched when using an overridden api host name")
}

func TestNoApiHostOverride(t *testing.T) {
	ctx := context.Background()

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	testServer.EnableHTTP2 = true
	testServer.Start()

	defer testServer.Close()
	p := makeTestGenericProvider(ctx, t, testServer, nil)

	expectedBaseURL := p.(*Provider).GetBaseURL()
	_, err := p.Configure(ctx, &pulumirpc.ConfigureRequest{})

	assert.Nil(t, err)
	assert.Equal(t, expectedBaseURL, p.(*Provider).GetBaseURL())
}

// TestCreateWith202PollsUntilReady verifies that when a Create returns 202,
// the provider polls the GET endpoint. Polling continues while GET returns 404
// (resource not yet created) and stops when GET returns 200 (resource ready).
func TestCreateWith202PollsUntilReady(t *testing.T) {
	ctx := context.Background()

	// Use short polling intervals so the test runs quickly.
	origInitial := initialPollingInterval
	origMax := maxPollingInterval
	initialPollingInterval = 10 * time.Millisecond
	maxPollingInterval = 50 * time.Millisecond
	t.Cleanup(func() {
		initialPollingInterval = origInitial
		maxPollingInterval = origMax
	})

	getCallCount := 0
	expectedResourceID := fakeResourceID

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fakeResourceBaseURLPath && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"%s","another_prop":"somevalue"}`, expectedResourceID))
			return
		}

		if r.URL.Path == fakeResourceByIDURLPath && r.Method == http.MethodGet {
			getCallCount++
			if getCallCount < 3 {
				// Return 404 for the first two calls.
				w.WriteHeader(http.StatusNotFound)
				return
			}
			// Return 200 on the third call.
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"%s","another_prop":"somevalue"}`, expectedResourceID))
			return
		}

		t.Errorf("Unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	testServer.EnableHTTP2 = true
	testServer.Start()
	defer testServer.Close()

	p := makeTestGenericProvider(ctx, t, testServer, nil)

	propMap := resource.NewPropertyMapFromMap(map[string]any{
		"simpleProp": "some-value",
	})
	props, err := plugin.MarshalProperties(propMap, state.DefaultMarshalOpts)
	assert.Nil(t, err)

	createResp, err := p.Create(ctx, &pulumirpc.CreateRequest{
		Name:       "myResource",
		Properties: props,
		Type:       "fakeresource/v2:FakeResource",
		Urn:        "urn:pulumi:some-stack::some-project::generic:fakeresource/v2:FakeResource::myResource",
	})
	assert.Nil(t, err)
	assert.NotNil(t, createResp)
	assert.Equal(t, expectedResourceID, createResp.GetId())
	assert.Equal(t, 3, getCallCount, "Expected exactly 3 GET calls (2 with 404, 1 with 200)")
}

// TestCreateWith202TimesOut verifies that when polling never transitions from 404 to 200,
// the provider returns an error once the timeout is exceeded.
func TestCreateWith202TimesOut(t *testing.T) {
	ctx := context.Background()

	// Use short polling intervals so the test runs quickly.
	origInitial := initialPollingInterval
	origMax := maxPollingInterval
	initialPollingInterval = 10 * time.Millisecond
	maxPollingInterval = 20 * time.Millisecond
	t.Cleanup(func() {
		initialPollingInterval = origInitial
		maxPollingInterval = origMax
	})

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fakeResourceBaseURLPath && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"%s"}`, fakeResourceID))
			return
		}

		if r.URL.Path == fakeResourceByIDURLPath && r.Method == http.MethodGet {
			// Always return 404 — resource never becomes ready.
			w.WriteHeader(http.StatusNotFound)
			return
		}

		t.Errorf("Unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	testServer.EnableHTTP2 = true
	testServer.Start()
	defer testServer.Close()

	p := makeTestGenericProvider(ctx, t, testServer, nil)

	propMap := resource.NewPropertyMapFromMap(map[string]any{
		"simpleProp": "some-value",
	})
	props, err := plugin.MarshalProperties(propMap, state.DefaultMarshalOpts)
	assert.Nil(t, err)

	// Use a 100ms timeout so the test finishes quickly.
	// The CreateRequest.Timeout field is in seconds, so we use a small fractional
	// value; however since time.Duration truncates floats to int, we set the
	// defaultPollingTimeout directly for this test instead.
	origDefault := defaultPollingTimeout
	defaultPollingTimeout = 100 * time.Millisecond
	t.Cleanup(func() { defaultPollingTimeout = origDefault })

	_, err = p.Create(ctx, &pulumirpc.CreateRequest{
		Name:       "myResource",
		Properties: props,
		Type:       "fakeresource/v2:FakeResource",
		Urn:        "urn:pulumi:some-stack::some-project::generic:fakeresource/v2:FakeResource::myResource",
	})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "polling timed out")
}

// TestUpdateWith202PollsUntilReady verifies that when an Update returns 202,
// the provider polls the GET endpoint using the old state for path param resolution.
func TestUpdateWith202PollsUntilReady(t *testing.T) {
	ctx := context.Background()

	// Use short polling intervals so the test runs quickly.
	origInitial := initialPollingInterval
	origMax := maxPollingInterval
	initialPollingInterval = 10 * time.Millisecond
	maxPollingInterval = 50 * time.Millisecond
	t.Cleanup(func() {
		initialPollingInterval = origInitial
		maxPollingInterval = origMax
	})

	getCallCount := 0

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The PATCH path param in the generic OpenAPI spec is named "id" but the URL
		// template uses "{resourceId}", so the URL arrives unreplaced. Match by method only.
		if r.Method == "PATCH" {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		if r.URL.Path == fmt.Sprintf("/v2/fakeresource/%s", fakeResourceID) && r.Method == http.MethodGet {
			getCallCount++
			if getCallCount < 2 {
				// Return 404 for the first call.
				w.WriteHeader(http.StatusNotFound)
				return
			}
			// Return 200 on the second call.
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"%s","another_prop":"updated-value"}`, fakeResourceID))
			return
		}

		t.Errorf("Unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	testServer.EnableHTTP2 = true
	testServer.Start()
	defer testServer.Close()

	p := makeTestGenericProvider(ctx, t, testServer, nil)

	oldInputsJSON := `{"object_prop":{"another_prop":"old-value"}}`
	outputsJSON := fmt.Sprintf(`{"id":"%s","another_prop":"old-value"}`, fakeResourceID)
	newInputsJSON := `{"object_prop":{"another_prop":"old-value"},"simple_prop":"updated-value"}`

	newInputs, _ := getMarshaledProps(t, newInputsJSON)
	oldInputs, oldInputsPropertyMap := getMarshaledProps(t, oldInputsJSON)

	var outputsMap map[string]any
	if err := json.Unmarshal([]byte(outputsJSON), &outputsMap); err != nil {
		t.Fatalf("Failed to unmarshal test payload: %v", err)
	}

	expectedOutputState := state.GetResourceState(outputsMap, oldInputsPropertyMap)
	serializedOutputState, err := plugin.MarshalProperties(expectedOutputState, state.DefaultMarshalOpts)
	if err != nil {
		t.Fatalf("Marshaling the output properties map: %v", err)
	}

	updateResp, err := p.Update(ctx, &pulumirpc.UpdateRequest{
		Id:        fakeResourceID,
		Olds:      serializedOutputState,
		News:      newInputs,
		OldInputs: oldInputs,
		Type:      "generic:fakeresource/v2:FakeResource",
		Name:      "myResource",
		Urn:       "urn:pulumi:some-stack::some-project::generic:fakeresource/v2:FakeResource::myResource",
	})
	assert.Nil(t, err)
	assert.NotNil(t, updateResp)
	assert.Equal(t, 2, getCallCount, "Expected exactly 2 GET calls (1 with 404, 1 with 200)")
}
