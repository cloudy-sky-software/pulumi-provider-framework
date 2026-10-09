package rest

import (
	"net/url"
	"strconv"

	"github.com/pkg/errors"

	"github.com/cloudy-sky-software/pulumi-provider-framework/state"

	pulschemaPkg "github.com/cloudy-sky-software/pulschema/pkg"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/logging"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

var queryParamsPropKey = resource.PropertyKey(pulschemaPkg.QueryParamsPropName)

// unwrapPropertyValue returns the underlying value of a secret,
// computed or output property value.
func unwrapPropertyValue(v resource.PropertyValue) resource.PropertyValue {
	for {
		switch {
		case v.IsSecret():
			v = v.SecretValue().Element
		case v.IsComputed():
			v = v.Input().Element
		case v.IsOutput():
			v = v.OutputValue().Element
		default:
			return v
		}
	}
}

// getQueryParamsProp returns the value of the queryParams
// property from the first property map that has it.
func getQueryParamsProp(propMaps ...resource.PropertyMap) (resource.PropertyMap, bool) {
	for _, m := range propMaps {
		v, ok := m[queryParamsPropKey]
		if !ok {
			continue
		}

		v = unwrapPropertyValue(v)
		if v.IsObject() {
			return v.ObjectValue(), true
		}
	}

	return nil, false
}

// getResourceQueryParams returns the query params for a CRUD operation of
// a resource. op is the name of the property in the resource's queryParams
// type, for example "read". The query params are taken from the first
// property map that has the queryParams property.
func (p *Provider) getResourceQueryParams(op string, propMaps ...resource.PropertyMap) (url.Values, error) {
	queryParams, ok := getQueryParamsProp(propMaps...)
	if !ok {
		return nil, nil
	}

	opParams, ok := queryParams[resource.PropertyKey(op)]
	if !ok {
		return nil, nil
	}

	opParams = unwrapPropertyValue(opParams)
	if !opParams.IsObject() {
		return nil, nil
	}

	return p.toQueryValues(opParams.ObjectValue())
}

// getFunctionQueryParams returns the query params for a function (invoke).
func (p *Provider) getFunctionQueryParams(args resource.PropertyMap) (url.Values, error) {
	queryParams, ok := getQueryParamsProp(args)
	if !ok {
		return nil, nil
	}

	return p.toQueryValues(queryParams)
}

// toQueryValues converts the properties of a query params type to
// query values, using the API names of the query params.
func (p *Provider) toQueryValues(params resource.PropertyMap) (url.Values, error) {
	values := url.Values{}

	for k, v := range params {
		v = unwrapPropertyValue(v)
		if v.IsNull() {
			continue
		}

		if k == pulschemaPkg.AdditionalQueryParamsPropName {
			if !v.IsObject() {
				return nil, errors.Errorf("expected %s to be an object", pulschemaPkg.AdditionalQueryParamsPropName)
			}

			// Arbitrary query params are sent as-is.
			for ak, av := range v.ObjectValue() {
				s, ok := queryValueString(av)
				if !ok {
					return nil, errors.Errorf("unsupported value type for additional query param %s", ak)
				}
				values.Add(string(ak), s)
			}
			continue
		}

		name := getOrKey(p.metadata.SDKToAPINameMap, string(k))

		if v.IsArray() {
			// Repeat the query param for each item, which is the
			// default serialization (style: form, explode: true)
			// in OpenAPI.
			for _, item := range v.ArrayValue() {
				s, ok := queryValueString(item)
				if !ok {
					return nil, errors.Errorf("unsupported array item type for query param %s", name)
				}
				values.Add(name, s)
			}
			continue
		}

		s, ok := queryValueString(v)
		if !ok {
			return nil, errors.Errorf("unsupported value type for query param %s", name)
		}
		values.Set(name, s)
	}

	logging.V(3).Infof("Query params: %v", values)
	return values, nil
}

// queryValueString returns the string form of a scalar property value.
func queryValueString(v resource.PropertyValue) (string, bool) {
	v = unwrapPropertyValue(v)

	switch {
	case v.IsString():
		return v.StringValue(), true
	case v.IsBool():
		return strconv.FormatBool(v.BoolValue()), true
	case v.IsNumber():
		return strconv.FormatFloat(v.NumberValue(), 'f', -1, 64), true
	}

	return "", false
}

// withoutQueryParams returns a copy of m without the queryParams property.
func withoutQueryParams(m resource.PropertyMap) resource.PropertyMap {
	if _, ok := m[queryParamsPropKey]; !ok {
		return m
	}

	c := m.Copy()
	delete(c, queryParamsPropKey)
	return c
}

// copyQueryParamsToOutputs copies the queryParams input to the outputs,
// since API responses won't contain it but the provider needs it in
// the state to send the query params for Read and Delete requests.
func copyQueryParamsToOutputs(outputsMap map[string]interface{}, inputs resource.PropertyMap) {
	if outputsMap == nil {
		return
	}

	if v, ok := inputs[queryParamsPropKey]; ok {
		outputsMap[pulschemaPkg.QueryParamsPropName] = v.Mappable()
	} else {
		delete(outputsMap, pulschemaPkg.QueryParamsPropName)
	}
}

// noDiffOrQueryParamsDiff returns a diff response with no changes,
// unless the query params changed.
func noDiffOrQueryParamsDiff(queryParamsChanged bool) *pulumirpc.DiffResponse {
	if !queryParamsChanged {
		return &pulumirpc.DiffResponse{Changes: pulumirpc.DiffResponse_DIFF_NONE}
	}

	return &pulumirpc.DiffResponse{
		Changes: pulumirpc.DiffResponse_DIFF_SOME,
		Diffs:   []string{pulschemaPkg.QueryParamsPropName},
	}
}

// updateQueryParamsInState returns an update response for when only
// the queryParams input of a resource changed. Query params are only
// sent with requests, so there is nothing to update in the cloud
// provider. Only the resource's state is updated.
func (p *Provider) updateQueryParamsInState(req *pulumirpc.UpdateRequest, inputs resource.PropertyMap) (*pulumirpc.UpdateResponse, error) {
	// Unmarshal the state again with the default options
	// so that secrets in the state are preserved.
	currentState, err := plugin.UnmarshalProperties(req.GetOlds(), state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal olds as propertymap")
	}

	if v, ok := inputs[queryParamsPropKey]; ok {
		currentState[queryParamsPropKey] = v
	} else {
		delete(currentState, queryParamsPropKey)
	}

	if !p.engineSendsOldInputs {
		state.SetOldInputs(currentState, inputs)
	}

	outputProperties, err := plugin.MarshalProperties(currentState, state.DefaultMarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the output properties map")
	}

	return &pulumirpc.UpdateResponse{Properties: outputProperties}, nil
}
