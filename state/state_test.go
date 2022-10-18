package state

import (
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"

	"github.com/stretchr/testify/assert"
)

func TestGetResourceState(t *testing.T) {
	val := "Testing"
	outputs := make(map[string]interface{})
	outputs["name"] = val
	outputs["id"] = "someid"

	inputs := make(map[string]interface{})
	inputs["name"] = val

	state := GetResourceState(outputs, resource.NewPropertyMapFromMap(inputs))
	assert.True(t, state.HasValue(resource.PropertyKey(stateKeyInputs)))
}
