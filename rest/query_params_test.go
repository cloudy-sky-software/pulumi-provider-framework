package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/cloudy-sky-software/pulumi-provider-framework/rest/rest_test"
	"github.com/cloudy-sky-software/pulumi-provider-framework/state"

	pulschemaPkg "github.com/cloudy-sky-software/pulschema/pkg"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

const (
	resourceIDParam       = "resourceId"
	anotherProp           = "anotherProp"
	expandParam           = "expand"
	includeFieldsParam    = "includeFields"
	includeFieldsAPIParam = "include_fields"
	customParam           = "x-custom"
	forceParam            = "force"
	expandValue           = "owner"
)

func marshalPropertyMap(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()

	props, err := plugin.MarshalProperties(resource.NewPropertyMapFromMap(m), state.DefaultMarshalOpts)
	assert.Nil(t, err)
	return props
}

// recordingServer returns a test server that records the requests
// it receives and responds with the given status code and body.
func recordingServer(t *testing.T, statusCode int, respBody string, requests *[]*http.Request, bodies *[]map[string]any) *httptest.Server {
	t.Helper()

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r)

		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		if len(b) > 0 {
			if err := json.Unmarshal(b, &body); err != nil {
				t.Errorf("Error unmarshaling JSON request body to map: %v", err)
			}
		}
		*bodies = append(*bodies, body)

		w.WriteHeader(statusCode)
		if respBody != "" {
			_, _ = io.WriteString(w, respBody)
		}
	}))
	testServer.EnableHTTP2 = true
	testServer.Start()
	t.Cleanup(testServer.Close)

	return testServer
}

func TestToQueryValues(t *testing.T) {
	ctx := context.Background()
	p := makeTestGenericProvider(ctx, t, nil, nil).(*Provider)

	params := resource.NewPropertyMapFromMap(map[string]any{
		"dryRun":           true,
		expandParam:        expandValue,
		"pageSize":         50,
		includeFieldsParam: []any{"a", "b"},
		"secretParam":      resource.MakeSecret(resource.NewStringProperty("s3cret")),
		"nullParam":        nil,
		pulschemaPkg.AdditionalQueryParamsPropName: map[string]any{
			customParam: "1",
		},
	})

	values, err := p.toQueryValues(params)
	assert.Nil(t, err)
	assert.Equal(t, url.Values{
		// SDK names are mapped back to the API names.
		"dry_run":             {"true"},
		expandParam:           {expandValue},
		"pageSize":            {"50"},
		includeFieldsAPIParam: {"a", "b"},
		"secretParam":         {"s3cret"},
		customParam:           {"1"},
	}, values)

	_, err = p.toQueryValues(resource.NewPropertyMapFromMap(map[string]any{
		"objectParam": map[string]any{"a": "b"},
	}))
	assert.NotNil(t, err)
}

func TestCreateSendsQueryParams(t *testing.T) {
	ctx := context.Background()

	var requests []*http.Request
	var bodies []map[string]any
	testServer := recordingServer(t, http.StatusOK, `{"id":"fake-id","another_prop":"somevalue"}`, &requests, &bodies)
	p := makeTestGenericProvider(ctx, t, testServer, nil)

	queryParams := map[string]any{
		pulschemaPkg.QueryParamsOpCreate: map[string]any{"dryRun": true},
		pulschemaPkg.QueryParamsOpDelete: map[string]any{forceParam: true},
	}
	createResp, err := p.Create(ctx, &pulumirpc.CreateRequest{
		Properties: marshalPropertyMap(t, map[string]any{
			rest_test.SimpleProp:             fakeSimplePropValue,
			pulschemaPkg.QueryParamsPropName: queryParams,
		}),
		Urn: rest_test.MyResourceURN,
	})
	assert.Nil(t, err)
	assert.Len(t, requests, 1)

	assert.Equal(t, http.MethodPost, requests[0].Method)
	assert.Equal(t, "true", requests[0].URL.Query().Get("dry_run"))
	assert.Len(t, requests[0].URL.Query(), 1)
	// queryParams must not be sent in the request body.
	assert.NotContains(t, bodies[0], pulschemaPkg.QueryParamsPropName)
	assert.Equal(t, fakeSimplePropValue, bodies[0]["simple_prop"])

	// queryParams should be saved in the outputs so that
	// Read and Delete can use them.
	assert.Equal(t, queryParams, createResp.GetProperties().AsMap()[pulschemaPkg.QueryParamsPropName])
}

func TestReadSendsQueryParams(t *testing.T) {
	ctx := context.Background()

	var requests []*http.Request
	var bodies []map[string]any
	testServer := recordingServer(t, http.StatusOK, `{"id":"fake-id","another_prop":"somevalue"}`, &requests, &bodies)
	p := makeTestGenericProvider(ctx, t, testServer, nil)

	queryParams := map[string]any{
		pulschemaPkg.QueryParamsOpRead: map[string]any{
			expandParam:        expandValue,
			includeFieldsParam: []any{"a", "b"},
			pulschemaPkg.AdditionalQueryParamsPropName: map[string]any{customParam: "1"},
		},
	}
	props := marshalPropertyMap(t, map[string]any{
		"id":                             rest_test.FakeID,
		resourceIDParam:                  rest_test.FakeID,
		pulschemaPkg.QueryParamsPropName: queryParams,
	})

	readResp, err := p.Read(ctx, &pulumirpc.ReadRequest{
		Id:         rest_test.FakeID,
		Urn:        rest_test.MyResourceURN,
		Properties: props,
		Inputs:     props,
	})
	assert.Nil(t, err)
	assert.Len(t, requests, 1)

	assert.Equal(t, fakeResourceByIDURLPath, requests[0].URL.Path)
	assert.Equal(t, url.Values{
		expandParam:           {expandValue},
		includeFieldsAPIParam: {"a", "b"},
		customParam:           {"1"},
	}, requests[0].URL.Query())

	// The query params are kept in the state after a refresh.
	assert.Equal(t, queryParams, readResp.GetProperties().AsMap()[pulschemaPkg.QueryParamsPropName])
	assert.Equal(t, queryParams, readResp.GetInputs().AsMap()[pulschemaPkg.QueryParamsPropName])
}

func TestDeleteSendsQueryParams(t *testing.T) {
	ctx := context.Background()

	queryParams := map[string]any{
		pulschemaPkg.QueryParamsOpDelete: map[string]any{forceParam: true},
	}

	t.Run("EngineSendsOldInputs", func(t *testing.T) {
		var requests []*http.Request
		var bodies []map[string]any
		testServer := recordingServer(t, http.StatusNoContent, "", &requests, &bodies)
		p := makeTestGenericProviderWithOpts(ctx, t, testServer, nil, true)

		props := marshalPropertyMap(t, map[string]any{
			"id":                             rest_test.FakeID,
			resourceIDParam:                  rest_test.FakeID,
			pulschemaPkg.QueryParamsPropName: queryParams,
		})
		_, err := p.Delete(ctx, &pulumirpc.DeleteRequest{
			Id:         rest_test.FakeID,
			Urn:        rest_test.MyResourceURN,
			Properties: props,
			OldInputs:  props,
		})
		assert.Nil(t, err)
		assert.Len(t, requests, 1)
		assert.Equal(t, http.MethodDelete, requests[0].Method)
		assert.Equal(t, "true", requests[0].URL.Query().Get(forceParam))
	})

	t.Run("InputsStashedInState", func(t *testing.T) {
		var requests []*http.Request
		var bodies []map[string]any
		testServer := recordingServer(t, http.StatusNoContent, "", &requests, &bodies)
		p := makeTestGenericProviderWithOpts(ctx, t, testServer, nil, false)

		inputs := resource.NewPropertyMapFromMap(map[string]any{
			resourceIDParam:                  rest_test.FakeID,
			pulschemaPkg.QueryParamsPropName: queryParams,
		})
		props, err := plugin.MarshalProperties(state.GetResourceState(map[string]any{"id": rest_test.FakeID}, inputs), state.DefaultMarshalOpts)
		assert.Nil(t, err)

		_, err = p.Delete(ctx, &pulumirpc.DeleteRequest{
			Id:         rest_test.FakeID,
			Urn:        rest_test.MyResourceURN,
			Properties: props,
		})
		assert.Nil(t, err)
		assert.Len(t, requests, 1)
		assert.Equal(t, "true", requests[0].URL.Query().Get(forceParam))
	})

	t.Run("MissingRequiredQueryParamFailsValidation", func(t *testing.T) {
		var requests []*http.Request
		var bodies []map[string]any
		testServer := recordingServer(t, http.StatusNoContent, "", &requests, &bodies)
		p := makeTestGenericProviderWithOpts(ctx, t, testServer, nil, true)

		props := marshalPropertyMap(t, map[string]any{
			"id":            rest_test.FakeID,
			resourceIDParam: rest_test.FakeID,
		})
		_, err := p.Delete(ctx, &pulumirpc.DeleteRequest{
			Id:         rest_test.FakeID,
			Urn:        rest_test.MyResourceURN,
			Properties: props,
			OldInputs:  props,
		})
		assert.NotNil(t, err)
		assert.Empty(t, requests)
	})
}

func TestDiffOnlyQueryParamsChanged(t *testing.T) {
	ctx := context.Background()
	p := makeTestGenericProvider(ctx, t, nil, nil)

	olds := marshalPropertyMap(t, map[string]any{
		rest_test.SimpleProp: fakeSimplePropValue,
		pulschemaPkg.QueryParamsPropName: map[string]any{
			pulschemaPkg.QueryParamsOpDelete: map[string]any{forceParam: false},
		},
	})
	news := marshalPropertyMap(t, map[string]any{
		rest_test.SimpleProp: fakeSimplePropValue,
		pulschemaPkg.QueryParamsPropName: map[string]any{
			pulschemaPkg.QueryParamsOpDelete: map[string]any{forceParam: true},
		},
	})

	diffResp, err := p.Diff(ctx, &pulumirpc.DiffRequest{
		Id:        rest_test.FakeID,
		Urn:       rest_test.MyResourceURN,
		OldInputs: olds,
		News:      news,
	})
	assert.Nil(t, err)
	assert.Equal(t, pulumirpc.DiffResponse_DIFF_SOME, diffResp.GetChanges())
	assert.Equal(t, []string{pulschemaPkg.QueryParamsPropName}, diffResp.GetDiffs())
	assert.Empty(t, diffResp.GetReplaces())

	// Changes to other props are still reported along with queryParams,
	// and the queryParams change never causes a replacement.
	news = marshalPropertyMap(t, map[string]any{
		rest_test.SimpleProp: "new-value",
		pulschemaPkg.QueryParamsPropName: map[string]any{
			pulschemaPkg.QueryParamsOpDelete: map[string]any{forceParam: true},
		},
	})
	diffResp, err = p.Diff(ctx, &pulumirpc.DiffRequest{
		Id:        rest_test.FakeID,
		Urn:       rest_test.MyResourceURN,
		OldInputs: olds,
		News:      news,
	})
	assert.Nil(t, err)
	assert.Equal(t, pulumirpc.DiffResponse_DIFF_SOME, diffResp.GetChanges())
	assert.ElementsMatch(t, []string{rest_test.SimpleProp, pulschemaPkg.QueryParamsPropName}, diffResp.GetDiffs())
	assert.NotContains(t, diffResp.GetReplaces(), pulschemaPkg.QueryParamsPropName)
}

func TestUpdateOnlyQueryParamsChanged(t *testing.T) {
	ctx := context.Background()

	var requests []*http.Request
	var bodies []map[string]any
	testServer := recordingServer(t, http.StatusInternalServerError, "", &requests, &bodies)
	p := makeTestGenericProvider(ctx, t, testServer, nil)

	oldInputs := map[string]any{
		rest_test.SimpleProp: fakeSimplePropValue,
		pulschemaPkg.QueryParamsPropName: map[string]any{
			pulschemaPkg.QueryParamsOpDelete: map[string]any{forceParam: false},
		},
	}
	newQueryParams := map[string]any{
		pulschemaPkg.QueryParamsOpDelete: map[string]any{forceParam: true},
	}
	newInputs := map[string]any{
		rest_test.SimpleProp:             fakeSimplePropValue,
		pulschemaPkg.QueryParamsPropName: newQueryParams,
	}
	olds := map[string]any{
		"id":                             rest_test.FakeID,
		anotherProp:                      "somevalue",
		rest_test.SimpleProp:             fakeSimplePropValue,
		pulschemaPkg.QueryParamsPropName: oldInputs[pulschemaPkg.QueryParamsPropName],
	}

	updateResp, err := p.Update(ctx, &pulumirpc.UpdateRequest{
		Id:        rest_test.FakeID,
		Urn:       rest_test.MyResourceURN,
		Olds:      marshalPropertyMap(t, olds),
		OldInputs: marshalPropertyMap(t, oldInputs),
		News:      marshalPropertyMap(t, newInputs),
	})
	assert.Nil(t, err)
	// No request should be sent to the API.
	assert.Empty(t, requests)

	outputs := updateResp.GetProperties().AsMap()
	assert.Equal(t, newQueryParams, outputs[pulschemaPkg.QueryParamsPropName])
	assert.Equal(t, "somevalue", outputs[anotherProp])
}

func TestInvokeSendsQueryParams(t *testing.T) {
	ctx := context.Background()

	var requests []*http.Request
	var bodies []map[string]any
	testServer := recordingServer(t, http.StatusOK, `{"id":"fake-id","another_prop":"somevalue"}`, &requests, &bodies)
	p := makeTestGenericProvider(ctx, t, testServer, nil)

	_, err := p.Invoke(ctx, &pulumirpc.InvokeRequest{
		Tok: "generic:fakeresource/v2:getFakeResource",
		Args: marshalPropertyMap(t, map[string]any{
			resourceIDParam: rest_test.FakeID,
			pulschemaPkg.QueryParamsPropName: map[string]any{
				expandParam:        expandValue,
				includeFieldsParam: []any{"a"},
			},
		}),
	})
	assert.Nil(t, err)
	assert.Len(t, requests, 1)
	assert.Equal(t, fakeResourceByIDURLPath, requests[0].URL.Path)
	assert.Equal(t, url.Values{
		expandParam:           {expandValue},
		includeFieldsAPIParam: {"a"},
	}, requests[0].URL.Query())
}
