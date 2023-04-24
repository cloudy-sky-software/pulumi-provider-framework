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

	p := makeTestProvider(ctx, t, nil)
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
