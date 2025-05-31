package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/cloudy-sky-software/pulumi-provider-framework/state"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/logging"

	"github.com/pkg/errors"
)

const (
	bearerAuthSchemePrefix = "Bearer"
	jsonMimeType           = "application/json"
	pathSeparator          = "/"
	parameterLocationPath  = "path"
)

var titleCaser = cases.Title(language.AmericanEnglish)

// Request interface is implemented by REST-based providers that perform
// CRUD operations using RESTful APIs.
type Request interface {
	CreateDeleteRequest(ctx context.Context, httpEndpointPath string, reqBody []byte, inputs resource.PropertyMap) (*http.Request, error)
	CreateGetRequest(ctx context.Context, httpEndpointPath string, inputs resource.PropertyMap) (*http.Request, error)
	CreatePatchRequest(ctx context.Context, httpEndpointPath string, reqBody []byte, inputs resource.PropertyMap) (*http.Request, error)
	CreatePostRequest(ctx context.Context, httpEndpointPath string, reqBody []byte, inputs resource.PropertyMap) (*http.Request, error)
	CreatePutRequest(ctx context.Context, httpEndpointPath string, reqBody []byte, inputs resource.PropertyMap) (*http.Request, error)
}

func (p *Provider) getAuthHeaderName() string {
	var authHeaderName string

	// We are assuming that the API requires auth as a header attribute.
	for _, securitySchemeRef := range p.openAPIDoc.Components.SecuritySchemes {
		switch {
		case securitySchemeRef.Value.Name != "":
			authHeaderName = securitySchemeRef.Value.Name
		case securitySchemeRef.Value.Scheme == "bearer":
			fallthrough
		default:
			authHeaderName = "Authorization"
		}
		break
	}

	return authHeaderName
}

func (p *Provider) getSupportedAuthSchemes() []string {
	schemes := make([]string, 0, len(p.openAPIDoc.Components.SecuritySchemes))

	for _, securitySchemeRef := range p.openAPIDoc.Components.SecuritySchemes {
		scheme := titleCaser.String(securitySchemeRef.Value.Scheme)
		if scheme == "" && strings.ToLower(securitySchemeRef.Value.Type) == "oauth2" {
			scheme = "Bearer"
		}

		if scheme == "" {
			continue
		}

		schemes = append(schemes, scheme)
	}

	return schemes
}

// CreateGetRequest returns a validated GET HTTP request for the provided inputs map.
func (p *Provider) CreateGetRequest(
	ctx context.Context,
	httpEndpointPath string,
	inputs resource.PropertyMap) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+httpEndpointPath, nil)
	if err != nil {
		return nil, errors.Wrap(err, "initializing request")
	}

	httpReq.Header.Add(p.getAuthHeaderName(), p.providerCallback.GetAuthorizationHeader())
	httpReq.Header.Add("Accept", jsonMimeType)
	httpReq.Header.Add("Content-Type", jsonMimeType)

	hasPathParams := strings.Contains(httpEndpointPath, "{")
	var pathParams map[string]string
	// If the endpoint has path params, peek into the OpenAPI doc
	// for the param names.
	if hasPathParams {
		var err error

		pathParams, err = p.getPathParamsMap(httpEndpointPath, http.MethodGet, inputs)
		if err != nil {
			return nil, errors.Wrap(err, "getting path params")
		}
	}

	if err := p.validateRequest(ctx, httpReq, pathParams); err != nil {
		return nil, errors.Wrap(err, "validate http request")
	}

	if err := p.replacePathParams(httpReq, pathParams); err != nil {
		return nil, errors.Wrap(err, "replacing path params")
	}

	return httpReq, nil
}

func (p *Provider) createHTTPRequestWithBody(ctx context.Context, httpEndpointPath string, httpMethod string, reqBody []byte, inputs resource.PropertyMap) (*http.Request, error) {
	if reqBody == nil {
		logging.V(3).Infof("REQUEST BODY is nil for %s", httpEndpointPath)
	} else {
		logging.V(3).Infof("REQUEST BODY: %s", string(reqBody))
	}

	hasPathParams := strings.Contains(httpEndpointPath, "{")
	var pathParams map[string]string

	var bodyMap map[string]interface{}
	if reqBody != nil {
		if err := json.Unmarshal(reqBody, &bodyMap); err != nil {
			return nil, errors.Wrap(err, "unmarshaling body")
		}
	}

	// If the endpoint has path params, peek into the OpenAPI doc
	// for the param names.
	if hasPathParams {
		var err error
		pathParams, err = p.getPathParamsMap(httpEndpointPath, httpMethod, inputs)
		if err != nil {
			return nil, errors.Wrap(err, "getting path params")
		}

		if reqBody != nil {
			logging.V(3).Infoln("Removing path params from request body")
			p.removePathParamsFromRequestBody(bodyMap, pathParams)
		}
	}

	var buf io.Reader
	// Transform properties in the request body from SDK name to API name.
	if bodyMap != nil {
		p.TransformBody(ctx, bodyMap, p.metadata.SDKToAPINameMap)

		updatedBody, err := json.Marshal(bodyMap)
		if err != nil {
			return nil, errors.Wrap(err, "marshaling body")
		}

		buf = bytes.NewBuffer(updatedBody)
	}

	httpReq, err := http.NewRequestWithContext(ctx, httpMethod, p.baseURL+httpEndpointPath, buf)
	if err != nil {
		return nil, errors.Wrap(err, "initializing request")
	}

	logging.V(3).Infof("URL: %s", httpReq.URL.String())

	httpReq.Header.Add(p.getAuthHeaderName(), p.providerCallback.GetAuthorizationHeader())
	httpReq.Header.Add("Accept", jsonMimeType)
	httpReq.Header.Add("Content-Type", jsonMimeType)

	if err := p.validateRequest(ctx, httpReq, pathParams); err != nil {
		return nil, errors.Wrap(err, "validate http request")
	}

	if err := p.replacePathParams(httpReq, pathParams); err != nil {
		return nil, errors.Wrap(err, "replacing path params")
	}

	return httpReq, nil
}

// CreatePostRequest returns a validated POST HTTP request for the
// provided inputs map.
func (p *Provider) CreatePostRequest(ctx context.Context, httpEndpointPath string, reqBody []byte, inputs resource.PropertyMap) (*http.Request, error) {
	return p.createHTTPRequestWithBody(ctx, httpEndpointPath, http.MethodPost, reqBody, inputs)
}

// CreatePutRequest returns a validated PUT HTTP request for the
// provided inputs map.
func (p *Provider) CreatePutRequest(ctx context.Context, httpEndpointPath string, reqBody []byte, inputs resource.PropertyMap) (*http.Request, error) {
	return p.createHTTPRequestWithBody(ctx, httpEndpointPath, http.MethodPut, reqBody, inputs)
}

// CreatePatchRequest returns a validated PATCH HTTP request for the
// provided inputs map.
func (p *Provider) CreatePatchRequest(ctx context.Context, httpEndpointPath string, reqBody []byte, inputs resource.PropertyMap) (*http.Request, error) {
	return p.createHTTPRequestWithBody(ctx, httpEndpointPath, http.MethodPatch, reqBody, inputs)
}

// CreateDeleteRequest returns a validated DELETE HTTP request for the
// provided inputs map.
func (p *Provider) CreateDeleteRequest(ctx context.Context, httpEndpointPath string, reqBody []byte, inputs resource.PropertyMap) (*http.Request, error) {
	return p.createHTTPRequestWithBody(ctx, httpEndpointPath, http.MethodDelete, reqBody, inputs)
}

func (p *Provider) validateRequest(ctx context.Context, httpReq *http.Request, pathParams map[string]string) error {
	route, _, err := p.router.FindRoute(httpReq)
	if err != nil {
		return errors.Wrap(err, "finding route from router")
	}

	// Validate the request.
	requestValidationInput := &openapi3filter.RequestValidationInput{
		Request:    httpReq,
		PathParams: pathParams,
		Route:      route,
		Options: &openapi3filter.Options{
			AuthenticationFunc: func(_ context.Context, ai *openapi3filter.AuthenticationInput) error {
				authHeaderName := p.getAuthHeaderName()
				authHeaderValue := ai.RequestValidationInput.Request.Header.Get(authHeaderName)
				if authHeaderValue == "" {
					return errors.Errorf("authorization header %s is required", authHeaderName)
				}

				authSchemes := p.getSupportedAuthSchemes()
				if len(authSchemes) == 0 {
					return nil
				}

				matchingAuthSchemePrefix := ""
				for _, scheme := range authSchemes {
					if strings.HasPrefix(authHeaderValue, scheme) {
						matchingAuthSchemePrefix = scheme
						break
					}
				}
				if matchingAuthSchemePrefix == "" {
					return errors.Errorf("unexpected auth scheme (expected one of %v)", authSchemes)
				}

				token := strings.TrimPrefix(authHeaderValue, fmt.Sprintf("%s ", bearerAuthSchemePrefix))
				if token == "" {
					return errors.New("auth token is required")
				}

				return nil
			},
		},
	}

	if err := openapi3filter.ValidateRequest(ctx, requestValidationInput); err != nil {
		return errors.Wrap(err, "request validation failed")
	}

	if httpReq.Body == nil {
		logging.V(3).Info("Request does not have a body. Skipping ContentLength adjustment...")
		return nil
	}

	// Update the original HTTP request's ContentLength since the
	// body might have changed due to default properties getting
	// added to it.
	clonedReq := httpReq.Clone(ctx)
	clonedBody, _ := io.ReadAll(clonedReq.Body)
	newContentLength := int64(len(clonedBody))
	logging.V(3).Infof("REQUEST CONTENT LENGTH: current: %d, new: %d", httpReq.ContentLength, newContentLength)
	httpReq.ContentLength = newContentLength
	logging.V(3).Infof("UPDATED REQUEST BODY: %v", string(clonedBody))
	httpReq.Body = io.NopCloser(bytes.NewBuffer(clonedBody))

	return nil
}

func (p *Provider) getPathParamsMap(apiPath, requestMethod string, properties resource.PropertyMap) (map[string]string, error) {
	pathParams := make(map[string]string)

	parameters := p.openAPIDoc.Paths.Find(apiPath).Parameters

	switch requestMethod {
	case http.MethodGet:
		parameters = append(parameters, p.openAPIDoc.Paths.Find(apiPath).Get.Parameters...)
	case http.MethodPost:
		parameters = append(parameters, p.openAPIDoc.Paths.Find(apiPath).Post.Parameters...)
	case http.MethodPatch:
		parameters = append(parameters, p.openAPIDoc.Paths.Find(apiPath).Patch.Parameters...)
	case http.MethodPut:
		parameters = append(parameters, p.openAPIDoc.Paths.Find(apiPath).Put.Parameters...)
	case http.MethodDelete:
		parameters = append(parameters, p.openAPIDoc.Paths.Find(apiPath).Delete.Parameters...)
	default:
		return pathParams, nil
	}

	oldInputs := state.GetOldInputs(properties)

	logging.V(3).Infof("Process path parameters with %v", properties)
	count := 0
	for _, param := range parameters {
		if param.Value.In != "path" {
			continue
		}

		count++
		paramName := param.Value.Name
		sdkName := paramName

		if o, ok := p.metadata.PathParamNameMap[sdkName]; ok {
			logging.V(3).Infof("Path param %q is overridden in the schema as %q", paramName, o)
			sdkName = o
		}

		// If there is no such property in the properties map,
		// check if the param is an id-like param.
		if _, ok := properties[resource.PropertyKey(sdkName)]; !ok && sdkName != "id" {
			if _, ok := oldInputs[resource.PropertyKey(sdkName)]; !ok {
				// If this is the last path param in the URI,
				// it's likely to be the `id` of the resource
				// that the endpoint is targeting.
				if strings.HasSuffix(apiPath, fmt.Sprintf("{%s}", paramName)) && (strings.HasSuffix(sdkName, "_id") || strings.HasSuffix(sdkName, "Id")) {
					logging.V(3).Infof("Path param %q is likely the id property", paramName)
					sdkName = "id"
				}
			}
		}

		logging.V(3).Infof("Looking for path param %q in resource inputs %v", paramName, properties)
		property, ok := properties[resource.PropertyKey(sdkName)]
		// If the path param is not in the properties, check if
		// we have the old inputs, if we are dealing with the state
		// of an existing resource.
		if !ok {
			logging.V(3).Infof("Global Params Map: %v, sdkName: %s", p.globalPathParams, sdkName)
			// Try to see if a top-level property has the required prop perhaps.
			_, topLevelPropName, ok := tryPluckingProp(sdkName, properties.Mappable())
			if ok {
				topLevelProp := properties[resource.PropertyKey(topLevelPropName)]
				property = topLevelProp.ObjectValue()[resource.PropertyKey(sdkName)]
			} else if globalPathParam, ok := p.globalPathParams[sdkName]; ok {
				// Is the property a global path param set in the provider?
				// We look for this after checking the resource state, so it can be overridden at a resource level
				logging.V(3).Infof("Path param %q is a global path param with value %q", sdkName, globalPathParam)
				property = resource.PropertyValue{
					V: globalPathParam,
				}
			} else {
				if oldInputs == nil {
					return nil, errors.Errorf("did not find value for path param %s in output props (old inputs was nil)", paramName)
				}

				property, ok = oldInputs[resource.PropertyKey(sdkName)]
				if !ok {
					return nil, errors.Errorf("did not find value for path param %s in output props and old inputs", paramName)
				}
			}
		}

		if property.IsComputed() {
			pathParams[paramName] = property.Input().Element.StringValue()
		} else if property.IsSecret() {
			pathParams[paramName] = property.SecretValue().Element.StringValue()
		} else {
			pathParams[paramName] = property.StringValue()
		}
	}

	numPathParams := len(pathParams)
	if numPathParams == 0 {
		return nil, errors.New("did not find any path parameters")
	}

	if numPathParams != count {
		return nil, errors.Errorf("not all path params were found in the properties (expected: %d, found: %d)", count, numPathParams)
	}

	return pathParams, nil
}

func (p *Provider) removePathParamsFromRequestBody(bodyMap map[string]interface{}, pathParams map[string]string) {
	for paramName := range pathParams {
		if sdkName, ok := p.metadata.PathParamNameMap[paramName]; ok {
			logging.V(3).Infof("Path param %[1]q is overridden in the schema as %[2]q. Will remove %[2]q from body instead.", paramName, sdkName)
			paramName = sdkName
		}
		// Delete the path param from the request body since it was added
		// as a way to take path params as inputs to the resource.
		delete(bodyMap, paramName)
	}

	updatedBody, _ := json.Marshal(bodyMap)
	logging.V(3).Infof("removePathParamsFromRequestBody: UPDATED HTTP REQUEST BODY: %s", string(updatedBody))
}

func (p *Provider) replacePathParams(httpReq *http.Request, pathParams map[string]string) error {
	path := httpReq.URL.Path

	for k, v := range pathParams {
		path = strings.ReplaceAll(path, fmt.Sprintf("{%s}", k), v)
	}

	httpReq.URL.Path = path
	logging.V(3).Infof("replacePathParams: UPDATED HTTP REQUEST URL: %s", httpReq.URL.String())

	return nil
}

func (p *Provider) determineDiffsAndReplacements(d *resource.ObjectDiff, schemaRef openapi3.SchemaRef) ([]string, []string) {
	replaces := make([]string, 0)
	diffs := make([]string, 0)

	var properties openapi3.Schemas
	if len(schemaRef.Value.Properties) > 0 {
		properties = schemaRef.Value.Properties
	} else if len(schemaRef.Value.AllOf) > 0 {
		properties = make(openapi3.Schemas, 0)
		for _, schemaRef := range schemaRef.Value.AllOf {
			for k, v := range schemaRef.Value.Properties {
				properties[k] = v
			}
		}
	}

	for propKey := range d.Adds {
		prop := string(propKey)
		// If the added property is not part of the PATCH operation schema,
		// then suggest a replacement triggered by this property.
		if _, ok := properties[prop]; !ok {
			replaces = append(replaces, prop)
		} else {
			diffs = append(diffs, prop)
		}
	}

	for propKey := range d.Updates {
		prop := string(propKey)
		// If the updated property is not part of the PATCH operation schema,
		// then suggest a replacement triggered by this property.
		if _, ok := properties[prop]; !ok {
			replaces = append(replaces, prop)
		} else {
			diffs = append(diffs, prop)
		}
	}

	for propKey := range d.Deletes {
		prop := string(propKey)
		// If the deleted property is not part of the PATCH operation schema,
		// then suggest a replacement triggered by this property.
		if _, ok := properties[prop]; !ok {
			replaces = append(replaces, prop)
		} else {
			diffs = append(diffs, prop)
		}
	}

	return replaces, diffs
}

func (p *Provider) mapImportIDToPathParams(id, httpEndpointPath string) (map[string]interface{}, error) {
	pathParams := make([]string, 0)
	idParts := strings.Split(strings.TrimPrefix(id, "/"), "/")
	endpointParts := strings.Split(strings.TrimPrefix(httpEndpointPath, "/"), "/")

	for _, segment := range endpointParts {
		// Skip if this segment is not a path param.
		if !strings.HasPrefix(segment, "{") {
			continue
		}

		pathParamName := strings.Trim(segment, "{}")
		pathParams = append(pathParams, pathParamName)
	}

	pathParamsMap := make(map[string]interface{}, 0)
	for i, param := range pathParams {
		// If the path param has an SDK name override, use it.
		if sdkName, ok := p.metadata.PathParamNameMap[param]; ok {
			pathParamsMap[sdkName] = idParts[i]
		} else {
			pathParamsMap[param] = idParts[i]
		}
	}

	// If the last path param is not called `id` but if it's "id-like",
	// add an `id` param to the map since it's likely the primary
	// resource's id for this endpoint path.
	lastPathParam := pathParams[len(pathParams)-1]
	lastPathParamLower := strings.ToLower(lastPathParam)
	if lastPathParamLower != "id" && (strings.HasSuffix(lastPathParamLower, "id") || strings.HasPrefix(lastPathParamLower, "_id")) {
		pathParamsMap["id"] = pathParamsMap[lastPathParam]
	}
	return pathParamsMap, nil
}

// additionsArePathParams returns true in the special case where
// there are only additions and those additions are only for
// path params. This is likely due to the resource being imported
// and the fact that path params are injected as virtual input
// properties. That is, they are not really input properties
// required by the cloud provider. They are just filled into
// the HTTP endpoint path as path params but since they are
// required pulschema transposes them as required input
// properties for convenience.
func (p *Provider) additionsArePathParams(diff *resource.ObjectDiff, news resource.PropertyMap, endpoint string, method string) (bool, error) {
	// If there are only additions AND those additions
	// are only the path params, then show no diff.
	if len(diff.Deletes) > 0 || len(diff.Updates) > 0 || len(diff.Adds) == 0 {
		return false, nil
	}

	pathParams, err := p.getPathParamsMap(endpoint, method, news)
	if err != nil {
		return false, errors.Wrap(err, "getting path params to determine if additions are only path params")
	}

	addsMap := diff.Adds.Mappable()
	p.removePathParamsFromRequestBody(addsMap, pathParams)
	if len(addsMap) == 0 {
		return true, nil
	}

	return false, nil
}
