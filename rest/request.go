package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
)

// Request interface is implemented by REST-based providers that perform
// CRUD operations using RESTful APIs.
type Request interface {
	CreateGetRequest(
		ctx context.Context,
		httpEndpointPath string,
		inputs resource.PropertyMap) (*http.Request, error)

	CreatePostRequest(ctx context.Context, httpEndpointPath string, reqBody []byte, inputs resource.PropertyMap) (*http.Request, error)
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

func (p *Provider) getAuthScheme() string {
	var scheme string

	for _, securitySchemeRef := range p.openAPIDoc.Components.SecuritySchemes {
		switch {
		case securitySchemeRef.Value.Scheme == "bearer":
			// Some APIs are specific about the case-sensitiveness
			// of the scheme. Pretty much all APIs will accept the
			// title-case of the scheme.
			scheme = bearerAuthSchemePrefix
		default:
			scheme = securitySchemeRef.Value.Scheme
		}
		break
	}

	return scheme
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

	p.replacePathParams(httpReq, pathParams)

	return httpReq, nil
}

func (p *Provider) createHTTPRequestWithBody(ctx context.Context, httpEndpointPath string, httpMethod string, reqBody []byte, inputs resource.PropertyMap) (*http.Request, error) {
	logging.V(3).Infof("REQUEST BODY: %s", string(reqBody))

	buf := bytes.NewBuffer(reqBody)
	httpReq, err := http.NewRequestWithContext(ctx, httpMethod, p.baseURL+httpEndpointPath, buf)
	if err != nil {
		return nil, errors.Wrap(err, "initializing request")
	}

	logging.V(3).Infof("URL: %s", httpReq.URL.String())

	httpReq.Header.Add(p.getAuthHeaderName(), p.providerCallback.GetAuthorizationHeader())
	httpReq.Header.Add("Accept", jsonMimeType)
	httpReq.Header.Add("Content-Type", jsonMimeType)

	hasPathParams := strings.Contains(httpEndpointPath, "{")
	var pathParams map[string]string
	// If the endpoint has path params, peek into the OpenAPI doc
	// for the param names.
	if hasPathParams {
		var err error
		pathParams, err = p.getPathParamsMap(httpEndpointPath, httpMethod, inputs)
		if err != nil {
			return nil, errors.Wrap(err, "getting path params")
		}
	}

	if err := p.validateRequest(ctx, httpReq, pathParams); err != nil {
		return nil, errors.Wrap(err, "validate http request")
	}

	p.replacePathParams(httpReq, pathParams)

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
			AuthenticationFunc: func(ctx context.Context, ai *openapi3filter.AuthenticationInput) error {
				authHeaderName := p.getAuthHeaderName()
				authHeader := ai.RequestValidationInput.Request.Header.Get(authHeaderName)
				if authHeader == "" {
					return errors.Errorf("authorization header value %s is required", authHeaderName)
				}

				authSchemePrefix := p.getAuthScheme()
				if authSchemePrefix != "" && !strings.HasPrefix(authHeader, authSchemePrefix) {
					return errors.Errorf("unexpected auth scheme (expected %q)", authSchemePrefix)
				}

				var token string
				if authSchemePrefix == "" {
					token = authHeader
				} else {
					token = strings.TrimPrefix(authHeader, fmt.Sprintf("%s ", bearerAuthSchemePrefix))
				}

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

	var parameters openapi3.Parameters

	switch requestMethod {
	case http.MethodGet:
		parameters = p.openAPIDoc.Paths[apiPath].Get.Parameters
	case http.MethodPost:
		parameters = p.openAPIDoc.Paths[apiPath].Post.Parameters
	case http.MethodPatch:
		parameters = p.openAPIDoc.Paths[apiPath].Patch.Parameters
	case http.MethodPut:
		parameters = p.openAPIDoc.Paths[apiPath].Put.Parameters
	case http.MethodDelete:
		parameters = p.openAPIDoc.Paths[apiPath].Delete.Parameters
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
		logging.V(3).Infof("Looking for path param %q in resource inputs %v", paramName, properties)
		property, ok := properties[resource.PropertyKey(paramName)]
		// If the path param is not in the properties, check if
		// we have the old inputs, if we are dealing with the state
		// of an existing resource.
		if !ok {
			if oldInputs == nil {
				return nil, errors.Errorf("did not find value for path param %s in output props (old inputs was nil)", paramName)
			}

			property, ok = oldInputs[resource.PropertyKey(paramName)]
			if !ok {
				return nil, errors.Errorf("did not find value for path param %s in output props and old inputs", paramName)
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
		return nil, errors.New("did not find any path parameters in the openapi doc for")
	}

	if numPathParams != count {
		return nil, errors.Errorf("not all path params were found in the properties (expected: %d, found: %d)", count, numPathParams)
	}

	return pathParams, nil
}

func (p *Provider) replacePathParams(httpReq *http.Request, pathParams map[string]string) error {
	path := httpReq.URL.Path
	var bodyMap map[string]interface{}

	if httpReq.Body != nil {
		body, _ := io.ReadAll(httpReq.Body)
		if err := json.Unmarshal(body, &bodyMap); err != nil {
			return errors.Wrap(err, "unmarshaling body while replacing path params")
		}
	}

	for k, v := range pathParams {
		path = strings.ReplaceAll(path, fmt.Sprintf("{%s}", k), v)
		// Delete the path param from the request body since it was added
		// as a way to take path params as inputs to the resource.
		if bodyMap != nil {
			delete(bodyMap, k)
		}
	}

	if bodyMap != nil {
		updatedBody, _ := json.Marshal(bodyMap)
		newContentLength := int64(len(updatedBody))
		httpReq.Body = io.NopCloser(bytes.NewBuffer(updatedBody))
		httpReq.ContentLength = newContentLength
		logging.V(3).Infof("replacePathParams: UPDATED HTTP REQUEST BODY: %s", string(updatedBody))
	}

	httpReq.URL.Path = path
	logging.V(3).Infof("replacePathParams: UPDATED HTTP REQUEST URL: %s", httpReq.URL.String())

	return nil
}

func (p *Provider) determineDiffsAndReplacements(d *resource.ObjectDiff, properties openapi3.Schemas) ([]string, []string) {
	replaces := make([]string, 0)
	diffs := make([]string, 0)

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
