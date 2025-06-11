package rest

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"

	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/stretchr/testify/assert"
)

func TestRemovePathParamsFromRequestBody(t *testing.T) {
	ctx := context.Background()
	testCreateJSONPayload := `{
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
	}
	`
	var inputs map[string]interface{}
	if err := json.Unmarshal([]byte(testCreateJSONPayload), &inputs); err != nil {
		t.Fatalf("Failed to unmarshal test payload: %v", err)
	}

	p := makeTestTailscaleProvider(ctx, t, nil)
	httpReq, err := p.(Request).CreatePostRequest(ctx, "/tailnet/{tailnet}/keys", []byte(testCreateJSONPayload), resource.NewPropertyMapFromMap(inputs))
	assert.Nil(t, err)
	assert.NotNil(t, httpReq)

	var bodyMap map[string]interface{}

	body, _ := io.ReadAll(httpReq.Body)
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		t.Fatalf("unmarshaling body: %v", err)
	}

	_, ok := bodyMap["tailnet"]
	assert.False(t, ok, "Expected tailnet to be removed from the HTTP request body since it is a path param.")
}

func TestProviderGlobalPathParams(t *testing.T) {
	ctx := context.Background()
	testCreateJSONPayload := `{}`

	expectedBaseId := "fake-base-id"

	var inputs map[string]interface{}
	if err := json.Unmarshal([]byte(testCreateJSONPayload), &inputs); err != nil {
		t.Fatalf("Failed to unmarshal test payload: %v", err)
	}

	p := makeTestGenericProvider(ctx, t, nil)
	p.(*Provider).GetGlobalPathParams()["baseId"] = expectedBaseId

	httpReq, err := p.(Request).CreatePostRequest(ctx, "/v2/{baseId}/fakeresource", []byte(testCreateJSONPayload), resource.NewPropertyMapFromMap(inputs))
	assert.Nil(t, err)
	assert.NotNil(t, httpReq)
	assert.Equal(t, httpReq.URL.Path, "/v2/"+expectedBaseId+"/fakeresource")
}

func TestLastPathParamIsResourceId(t *testing.T) {
	ctx := context.Background()

	p := makeTestGenericProvider(ctx, t, nil)

	properties := map[string]interface{}{
		"id": "fake-id",
	}

	httpReq, err := p.(Request).CreateGetRequest(ctx, "/v2/anotherfakeresource/{some_id}", resource.NewPropertyMapFromMap(properties))
	assert.Nil(t, err)
	assert.NotNil(t, httpReq)

	// The request's URL should have the correct ID since `{some_id}`
	// is just points to the resource's `id` from its property
	// map.
	assert.Contains(t, httpReq.URL.Path, "fake-id")
}
