package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/pkg/errors"

	pschema "github.com/pulumi/pulumi/pkg/v3/codegen/schema"
	"github.com/pulumi/pulumi/pkg/v3/resource/provider"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/logging"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"

	"github.com/cloudy-sky-software/pulumi-provider-framework/callback"
	"github.com/cloudy-sky-software/pulumi-provider-framework/state"

	providerGen "github.com/cloudy-sky-software/pulschema/pkg/gen"
	providerOpenAPI "github.com/cloudy-sky-software/pulschema/pkg/openapi"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbempty "github.com/golang/protobuf/ptypes/empty"
)

type restProvider struct {
	host    *provider.HostClient
	name    string
	version string

	HttpClient *http.Client
	OpenAPIDoc openapi3.T
	Schema     pschema.PackageSpec

	baseURL  string
	metadata providerGen.ProviderMetadata
	router   routers.Router

	apiKey string

	providerCallback callback.RestProviderCallback
}

func defaultTransportDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return dialer.DialContext
}

func MakeProvider(host *provider.HostClient, name, version string, pulumiSchemaBytes, openapiDocBytes, metadataBytes []byte, callback callback.RestProviderCallback) (pulumirpc.ResourceProviderServer, error) {
	openapiDoc := providerOpenAPI.GetOpenAPISpec(openapiDocBytes)

	router, err := gorillamux.NewRouter(openapiDoc)
	if err != nil {
		return nil, errors.Wrap(err, "creating api router mux")
	}

	var metadata providerGen.ProviderMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return nil, errors.Wrap(err, "unmarshaling the metadata bytes to json")
	}

	httpClient := &http.Client{
		// The transport is mostly a copy of the http.DefaultTransport
		// with the exception of ForceAttemptHTTP2 set to false.
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: defaultTransportDialContext(&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}),
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errors.New("unable to handle redirects")
		},
	}

	var pulumiSchema pschema.PackageSpec
	if err := json.Unmarshal(pulumiSchemaBytes, &pulumiSchema); err != nil {
		return nil, errors.Wrap(err, "unmarshaling pulumi schema into its package spec form")
	}

	// Return the new provider
	return &restProvider{
		host:       host,
		name:       name,
		version:    version,
		Schema:     pulumiSchema,
		baseURL:    openapiDoc.Servers[0].URL,
		OpenAPIDoc: *openapiDoc,
		metadata:   metadata,
		router:     router,
		HttpClient: httpClient,

		providerCallback: callback,
	}, nil
}

// GetResourceTypeToken returns the type token from a resource URN string.
func GetResourceTypeToken(u string) string {
	urn := resource.URN(u)
	return urn.Type().String()
}

// Attach sends the engine address to an already running plugin.
func (p *restProvider) Attach(context context.Context, req *pulumirpc.PluginAttach) (*pbempty.Empty, error) {
	host, err := provider.NewHostClient(req.GetAddress())
	if err != nil {
		return nil, err
	}
	p.host = host
	return &pbempty.Empty{}, nil
}

// Call dynamically executes a method in the provider associated with a component resource.
func (p *restProvider) Call(ctx context.Context, req *pulumirpc.CallRequest) (*pulumirpc.CallResponse, error) {
	return nil, status.Error(codes.Unimplemented, "call is not yet implemented")
}

// Construct creates a new component resource.
func (p *restProvider) Construct(ctx context.Context, req *pulumirpc.ConstructRequest) (*pulumirpc.ConstructResponse, error) {
	return nil, status.Error(codes.Unimplemented, "construct is not yet implemented")
}

// CheckConfig validates the configuration for this provider.
func (p *restProvider) CheckConfig(ctx context.Context, req *pulumirpc.CheckRequest) (*pulumirpc.CheckResponse, error) {
	return &pulumirpc.CheckResponse{Inputs: req.GetNews()}, nil
}

// DiffConfig diffs the configuration for this provider.
func (p *restProvider) DiffConfig(ctx context.Context, req *pulumirpc.DiffRequest) (*pulumirpc.DiffResponse, error) {
	return &pulumirpc.DiffResponse{}, nil
}

// Configure configures the resource provider with "globals" that control its behavior.
func (p *restProvider) Configure(ctx context.Context, req *pulumirpc.ConfigureRequest) (*pulumirpc.ConfigureResponse, error) {
	resp, err := p.providerCallback.OnConfigure(ctx, req)
	if err != nil || resp != nil {
		return nil, err
	}

	return &pulumirpc.ConfigureResponse{
		AcceptSecrets: true,
	}, nil
}

// Invoke dynamically executes a built-in function in the provider.
func (p *restProvider) Invoke(ctx context.Context, req *pulumirpc.InvokeRequest) (*pulumirpc.InvokeResponse, error) {
	args, err := plugin.UnmarshalProperties(req.Args, state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal input properties as propertymap")
	}

	invokeTypeToken := req.GetTok()
	crudMap, ok := p.metadata.ResourceCRUDMap[invokeTypeToken]
	if !ok {
		return nil, errors.Errorf("unknown resource type %s", invokeTypeToken)
	}
	if crudMap.R == nil {
		return nil, errors.Errorf("resource read endpoint is unknown for %s", invokeTypeToken)
	}

	httpEndpointPath := *crudMap.R

	httpReq, err := p.CreateGetRequest(ctx, httpEndpointPath, args)
	if err != nil {
		return nil, errors.Wrapf(err, "creating get request (type token: %s)", invokeTypeToken)
	}

	// Read the resource.
	httpResp, err := p.HttpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(err, "executing http request")
	}

	if httpResp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, errors.Wrap(err, "http request failed and the error response could not be read")
		}

		httpResp.Body.Close()
		return nil, errors.Errorf("http request failed (status: %s): %s", httpResp.Status, string(body))
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "reading response body")
	}

	httpResp.Body.Close()

	var obj resource.PropertyMap
	// TODO: Is this too specific for this lib?
	// Should this be pushed downstream to the actual provider
	// implementation?
	if strings.Contains(invokeTypeToken, ":list") {
		var outputs []interface{}
		if err := json.Unmarshal(body, &outputs); err != nil {
			return nil, errors.Wrap(err, "unmarshaling the response")
		}

		m := make(map[string]interface{})
		m["items"] = outputs
		obj = resource.NewPropertyMapFromMap(m)
	} else {
		var outputs map[string]interface{}
		if err := json.Unmarshal(body, &outputs); err != nil {
			return nil, errors.Wrap(err, "unmarshaling the response")
		}

		obj = resource.NewPropertyMapFromMap(outputs)
	}

	outputProperties, err := plugin.MarshalProperties(obj, state.DefaultMarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the output properties map")
	}

	return &pulumirpc.InvokeResponse{
		Return: outputProperties,
	}, nil
}

// StreamInvoke dynamically executes a built-in function in the provider. The result is streamed
// back as a series of messages.
func (p *restProvider) StreamInvoke(req *pulumirpc.InvokeRequest, server pulumirpc.ResourceProvider_StreamInvokeServer) error {
	tok := req.GetTok()
	return fmt.Errorf("unknown StreamInvoke token '%s'", tok)
}

// Check validates that the given property bag is valid for a resource of the given type and returns
// the inputs that should be passed to successive calls to Diff, Create, or Update for this
// resource. As a rule, the provider inputs returned by a call to Check should preserve the original
// representation of the properties as present in the program inputs. Though this rule is not
// required for correctness, violations thereof can negatively impact the end-user experience, as
// the provider inputs are used for detecting and rendering diffs.
func (p *restProvider) Check(ctx context.Context, req *pulumirpc.CheckRequest) (*pulumirpc.CheckResponse, error) {
	return &pulumirpc.CheckResponse{Inputs: req.News, Failures: nil}, nil
}

// Diff checks what impacts a hypothetical update will have on the resource's properties.
func (p *restProvider) Diff(ctx context.Context, req *pulumirpc.DiffRequest) (*pulumirpc.DiffResponse, error) {
	oldState, err := plugin.UnmarshalProperties(req.GetOlds(), state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, err
	}

	olds := state.GetOldInputs(oldState)
	if olds == nil {
		return nil, errors.New("fetching old inputs from the state")
	}

	news, err := plugin.UnmarshalProperties(req.GetNews(), state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, err
	}

	logging.V(3).Infof("Calculating diff: olds: %v; news: %v", olds, news)
	diff := olds.Diff(news)
	if diff == nil || !diff.AnyChanges() {
		logging.V(3).Infof("Diff: no changes for %s", req.GetUrn())
		return &pulumirpc.DiffResponse{Changes: pulumirpc.DiffResponse_DIFF_NONE}, nil
	}

	logging.V(3).Info("Detected some changes...")
	logging.V(4).Infof("ADDS: %v", diff.Adds)
	logging.V(4).Infof("DELETES: %v", diff.Deletes)
	logging.V(4).Infof("UPDATES: %v", diff.Updates)

	resourceTypeToken := GetResourceTypeToken(req.GetUrn())
	crudMap, ok := p.metadata.ResourceCRUDMap[resourceTypeToken]
	if !ok {
		return nil, errors.Errorf("unknown resource type %s", resourceTypeToken)
	}
	if crudMap.U == nil {
		return nil, errors.Errorf("resource update endpoint is unknown for %s", resourceTypeToken)
	}

	patchOp := p.OpenAPIDoc.Paths[*crudMap.U].Patch
	if patchOp == nil {
		return nil, errors.Errorf("openapi doc does not have patch endpoint definition for the path %s", *crudMap.U)
	}

	var replaces []string
	var diffs []string
	changes := pulumirpc.DiffResponse_DIFF_SOME
	jsonReq := patchOp.RequestBody.Value.Content[jsonMimeType]

	diffResp, callbackErr := p.providerCallback.OnDiff(ctx, req, resourceTypeToken, diff, jsonReq)
	if callbackErr != nil || diffResp != nil {
		return diffResp, callbackErr
	}

	if len(jsonReq.Schema.Value.Properties) != 0 {
		replaces, diffs = p.determineDiffsAndReplacements(diff, jsonReq.Schema.Value.Properties)
	} else {
		changes = pulumirpc.DiffResponse_DIFF_UNKNOWN
	}

	logging.V(3).Infof("Diff response: replaces: %v; diffs: %v", replaces, diffs)

	return &pulumirpc.DiffResponse{
		Changes:  changes,
		Replaces: replaces,
		Diffs:    diffs,
	}, nil
}

// Create allocates a new instance of the provided resource and returns its unique ID afterwards.
func (p *restProvider) Create(ctx context.Context, req *pulumirpc.CreateRequest) (*pulumirpc.CreateResponse, error) {
	logging.V(3).Infof("Create: %s", req.GetUrn())

	inputs, err := plugin.UnmarshalProperties(req.GetProperties(), state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal input properties as propertymap")
	}

	resourceTypeToken := GetResourceTypeToken(req.GetUrn())
	crudMap, ok := p.metadata.ResourceCRUDMap[resourceTypeToken]
	if !ok {
		return nil, errors.Errorf("unknown resource type %s", resourceTypeToken)
	}
	if crudMap.C == nil {
		return nil, errors.Errorf("resource construction endpoint is unknown for %s", resourceTypeToken)
	}

	httpEndpointPath := *crudMap.C

	b, err := json.Marshal(inputs.Mappable())
	if err != nil {
		return nil, errors.Wrap(err, "marshaling inputs")
	}

	httpReq, err := p.CreatePostRequest(ctx, httpEndpointPath, b, inputs)
	if err != nil {
		return nil, errors.Wrapf(err, "creating post request (type token: %s)", resourceTypeToken)
	}

	preCreateErr := p.providerCallback.OnPreCreate(ctx, req, httpReq)
	if preCreateErr != nil {
		return nil, preCreateErr
	}

	// Create the resource.
	httpResp, err := p.HttpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(err, "executing http request")
	}

	if httpResp.StatusCode != http.StatusOK && httpResp.StatusCode != http.StatusCreated {
		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, errors.Wrap(err, "http request failed and the error response could not be read")
		}

		httpResp.Body.Close()
		return nil, errors.Errorf("http request failed (status: %s): %s", httpResp.Status, string(body))
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "reading response body")
	}

	httpResp.Body.Close()

	var outputs map[string]interface{}
	if err := json.Unmarshal(body, &outputs); err != nil {
		return nil, errors.Wrap(err, "unmarshaling the response")
	}

	logging.V(3).Infof("RESPONSE BODY: %v", outputs)

	postCreateErr := p.providerCallback.OnPostCreate(ctx, req, outputs)
	if postCreateErr != nil {
		// TODO: returning a nil CreateResponse will mean that Pulumi will consider
		// this resource to not have been created. We should use the outputs we
		// already have to create the response.
		return nil, postCreateErr
	}

	outputProperties, err := plugin.MarshalProperties(state.GetResourceState(outputs, inputs), state.DefaultMarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the output properties map")
	}

	id, ok := outputs["id"]
	if !ok {
		return nil, errors.New("resource may have been created successfully but the id was not present in the response")
	}

	return &pulumirpc.CreateResponse{
		Id:         id.(string), // ID's in Render are always strings.
		Properties: outputProperties,
	}, nil
}

// Read the current live state associated with a resource.
func (p *restProvider) Read(ctx context.Context, req *pulumirpc.ReadRequest) (*pulumirpc.ReadResponse, error) {
	oldState, err := plugin.UnmarshalProperties(req.GetProperties(), state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal input properties as propertymap")
	}

	resourceTypeToken := GetResourceTypeToken(req.GetUrn())
	crudMap, ok := p.metadata.ResourceCRUDMap[resourceTypeToken]
	if !ok {
		return nil, errors.Errorf("unknown resource type %s", resourceTypeToken)
	}
	if crudMap.R == nil {
		return nil, errors.Errorf("resource read endpoint is unknown for %s", resourceTypeToken)
	}

	httpEndpointPath := *crudMap.R

	httpReq, err := p.CreateGetRequest(ctx, httpEndpointPath, oldState)
	if err != nil {
		return nil, errors.Wrapf(err, "creating get request (type token: %s)", resourceTypeToken)
	}

	preReadErr := p.providerCallback.OnPreRead(ctx, req, httpReq)
	if preReadErr != nil {
		return nil, preReadErr
	}

	// Read the resource.
	httpResp, err := p.HttpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(err, "executing http request")
	}

	if httpResp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, errors.Wrap(err, "http request failed and the error response could not be read")
		}

		httpResp.Body.Close()
		return nil, errors.Errorf("http request failed (status: %s): %s", httpResp.Status, string(body))
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "reading response body")
	}

	httpResp.Body.Close()

	var outputs map[string]interface{}
	if err := json.Unmarshal(body, &outputs); err != nil {
		return nil, errors.Wrap(err, "unmarshaling the response")
	}

	inputs := state.GetOldInputs(oldState)
	// If there is no old state, then persist the current outputs as the
	// "old" inputs going forward for this resource.
	if inputs == nil {
		inputs = resource.NewPropertyMapFromMap(outputs)
	} else {
		// Take the values from outputs and apply them to the inputs
		// so that the checkpoint is in-sync with the state in the
		// cloud provider.
		newState := resource.NewPropertyMapFromMap(outputs)
		inputs = state.ApplyDiffFromCloudProvider(newState, inputs)
	}

	postReadErr := p.providerCallback.OnPostRead(ctx, req, outputs)
	if postReadErr != nil {
		return nil, postReadErr
	}

	outputProperties, err := plugin.MarshalProperties(state.GetResourceState(outputs, inputs), state.DefaultMarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the output properties map")
	}

	id, ok := outputs["id"]
	if !ok {
		return nil, errors.New("looking up id property from the response")
	}

	// Serialize and return the calculated inputs.
	inputsRecord, err := plugin.MarshalProperties(inputs, state.DefaultMarshalOpts)
	if err != nil {
		return nil, err
	}

	return &pulumirpc.ReadResponse{
		Id:         id.(string),
		Inputs:     inputsRecord,
		Properties: outputProperties,
	}, nil
}

// Update updates an existing resource with new values.
func (p *restProvider) Update(ctx context.Context, req *pulumirpc.UpdateRequest) (*pulumirpc.UpdateResponse, error) {
	oldState, err := plugin.UnmarshalProperties(req.Olds, state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal olds as propertymap")
	}

	inputs, err := plugin.UnmarshalProperties(req.News, state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal news as propertymap")
	}

	resourceTypeToken := GetResourceTypeToken(req.GetUrn())
	crudMap, ok := p.metadata.ResourceCRUDMap[resourceTypeToken]
	if !ok {
		return nil, errors.Errorf("unknown resource type %s", resourceTypeToken)
	}
	if crudMap.U == nil && crudMap.P == nil {
		return nil, errors.Errorf("neither resource update endpoints (update and put) are available for %s", resourceTypeToken)
	}

	var httpEndpointPath string
	var method string
	if crudMap.U != nil {
		httpEndpointPath = *crudMap.U
		method = http.MethodPatch
	} else {
		httpEndpointPath = *crudMap.P
		method = http.MethodPut
	}

	b, err := json.Marshal(inputs.Mappable())
	if err != nil {
		return nil, errors.Wrap(err, "marshaling inputs")
	}

	logging.V(3).Infof("REQUEST BODY: %s", string(b))
	buf := bytes.NewBuffer(b)
	httpReq, err := http.NewRequestWithContext(ctx, method, p.baseURL+httpEndpointPath, buf)
	if err != nil {
		return nil, errors.Wrap(err, "initializing request")
	}

	// Set the API key in the auth header.
	httpReq.Header.Add("Authorization", fmt.Sprintf("%s %s", authSchemePrefix, p.apiKey))
	httpReq.Header.Add("Accept", jsonMimeType)
	httpReq.Header.Add("Content-Type", jsonMimeType)

	hasPathParams := strings.Contains(httpEndpointPath, "{")
	var pathParams map[string]string
	// If the endpoint has path params, peek into the OpenAPI doc
	// for the param names.
	if hasPathParams {
		var err error

		pathParams, err = p.getPathParamsMap(httpEndpointPath, method, oldState)
		if err != nil {
			return nil, errors.Wrapf(err, "getting path params (type token: %s)", resourceTypeToken)
		}
	}

	if err := p.validateRequest(ctx, httpReq, pathParams); err != nil {
		return nil, errors.Wrap(err, "validate http request")
	}

	httpReq.URL.Path = p.replacePathParams(httpReq.URL.Path, pathParams)

	preUpdateErr := p.providerCallback.OnPreUpdate(ctx, req, httpReq)
	if preUpdateErr != nil {
		return nil, preUpdateErr
	}

	// Update the resource.
	httpResp, err := p.HttpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(err, "executing http request")
	}

	if httpResp.StatusCode != http.StatusOK && httpResp.StatusCode != http.StatusNoContent {
		return nil, errors.Errorf("http request failed: %v. expected 200 or 204 but got %d", err, httpResp.StatusCode)
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "reading response body")
	}

	defer httpResp.Body.Close()

	if httpResp.StatusCode == http.StatusNoContent {
		return &pulumirpc.UpdateResponse{}, nil
	}

	var outputs map[string]interface{}
	if err := json.Unmarshal(body, &outputs); err != nil {
		return nil, errors.Wrap(err, "unmarshaling the response")
	}

	logging.V(3).Infof("RESPONSE BODY: %v", outputs)

	outputProperties, err := plugin.MarshalProperties(state.GetResourceState(outputs, inputs), state.DefaultMarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the output properties map")
	}

	postUpdateErr := p.providerCallback.OnPostUpdate(ctx, req, *httpReq, outputs)
	if postUpdateErr != nil {
		return nil, postUpdateErr
	}

	return &pulumirpc.UpdateResponse{
		Properties: outputProperties,
	}, nil
}

// Delete tears down an existing resource with the given ID. If it fails, the resource is assumed
// to still exist.
func (p *restProvider) Delete(ctx context.Context, req *pulumirpc.DeleteRequest) (*pbempty.Empty, error) {
	inputs, err := plugin.UnmarshalProperties(req.GetProperties(), state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal input properties as propertymap")
	}

	resourceTypeToken := GetResourceTypeToken(req.GetUrn())

	preResponse, preErr := p.providerCallback.OnPreDelete(ctx, req)
	if preErr != nil || preResponse != nil {
		return preResponse, preErr
	}

	crudMap, ok := p.metadata.ResourceCRUDMap[resourceTypeToken]
	if !ok {
		return nil, errors.Errorf("unknown resource type %s", resourceTypeToken)
	}
	if crudMap.D == nil {
		// Nothing to do to delete this resource,
		// simply drop it from the state.
		return &pbempty.Empty{}, nil
	}

	httpEndpointPath := *crudMap.D
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.baseURL+httpEndpointPath, nil)
	if err != nil {
		return nil, errors.Wrap(err, "initializing request")
	}

	// Set the API key in the auth header.
	httpReq.Header.Add("Authorization", fmt.Sprintf("%s %s", authSchemePrefix, p.apiKey))
	httpReq.Header.Add("Accept", jsonMimeType)
	httpReq.Header.Add("Content-Type", jsonMimeType)

	hasPathParams := strings.Contains(httpEndpointPath, "{")
	var pathParams map[string]string
	// If the endpoint has path params, peek into the OpenAPI doc
	// for the param names.
	if hasPathParams {
		var err error

		pathParams, err = p.getPathParamsMap(httpEndpointPath, http.MethodDelete, inputs)
		if err != nil {
			return nil, errors.Wrapf(err, "getting path params (type token: %s)", resourceTypeToken)
		}
	}

	if err := p.validateRequest(ctx, httpReq, pathParams); err != nil {
		return nil, errors.Wrap(err, "validate http request")
	}

	httpReq.URL.Path = p.replacePathParams(httpReq.URL.Path, pathParams)

	// Delete the resource.
	httpResp, err := p.HttpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(err, "executing http request")
	}

	if httpResp.StatusCode != http.StatusOK && httpResp.StatusCode != http.StatusNoContent {
		return nil, errors.Errorf("http request failed: %v. expected 200 or 204 but got %d", err, httpResp.StatusCode)
	}

	httpResp.Body.Close()

	postDeleteResp, postDeleteErr := p.providerCallback.OnPostDelete(ctx, req)
	if postDeleteErr != nil || postDeleteResp != nil {
		return postDeleteResp, postDeleteErr
	}

	return &pbempty.Empty{}, nil
}

// GetPluginInfo returns generic information about this plugin, like its version.
func (p *restProvider) GetPluginInfo(context.Context, *pbempty.Empty) (*pulumirpc.PluginInfo, error) {
	return &pulumirpc.PluginInfo{
		Version: p.version,
	}, nil
}

// GetSchema returns the JSON-serialized schema for the provider.
func (p *restProvider) GetSchema(ctx context.Context, req *pulumirpc.GetSchemaRequest) (*pulumirpc.GetSchemaResponse, error) {
	b, err := json.Marshal(p.Schema)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the schema")
	}

	return &pulumirpc.GetSchemaResponse{
		Schema: string(b),
	}, nil
}

// Cancel signals the provider to gracefully shut down and abort any ongoing resource operations.
// Operations aborted in this way will return an error (e.g., `Update` and `Create` will either
// reutrn a creation error or an initialization error). Since Cancel is advisory and non-blocking,
// it is up to the host to decide how long to wait after Cancel is called before (e.g.)
// hard-closing any gRPC connection.
func (p *restProvider) Cancel(context.Context, *pbempty.Empty) (*pbempty.Empty, error) {
	return &pbempty.Empty{}, nil
}
