package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
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
	"github.com/cloudy-sky-software/pulumi-provider-framework/openapi"
	"github.com/cloudy-sky-software/pulumi-provider-framework/state"

	providerGen "github.com/cloudy-sky-software/pulschema/pkg"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbempty "github.com/golang/protobuf/ptypes/empty"
)

// Provider implements Pulumi's `ResourceProviderServer` interface.
// The implemented methods assume that the cloud provider supports RESTful
// APIs that accept a content-type of `application/json`.
type Provider struct {
	pulumirpc.UnimplementedResourceProviderServer

	host    *provider.HostClient
	name    string
	version string

	metadata providerGen.ProviderMetadata
	router   routers.Router

	providerCallback callback.ProviderCallback

	baseURL    string
	httpClient *http.Client
	openAPIDoc openapi3.T
	schema     pschema.PackageSpec

	// Global path params for this provider - for path params that are fixed
	// for a provider. Can be configured during the OnConfigure callback func
	globalPathParams map[string]string
}

func defaultTransportDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return dialer.DialContext
}

// MakeProvider returns an instance of the REST-based resource provider handler.
func MakeProvider(host *provider.HostClient, name, version string, pulumiSchemaBytes, openapiDocBytes, metadataBytes []byte, callback callback.ProviderCallback) (pulumirpc.ResourceProviderServer, error) {
	openapiDoc := openapi.GetOpenAPISpec(openapiDocBytes)

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
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("unable to handle redirects")
		},
	}

	var pulumiSchema pschema.PackageSpec
	if err := json.Unmarshal(pulumiSchemaBytes, &pulumiSchema); err != nil {
		return nil, errors.Wrap(err, "unmarshaling pulumi schema into its package spec form")
	}

	// Return the new provider
	return &Provider{
		host:       host,
		name:       name,
		version:    version,
		schema:     pulumiSchema,
		baseURL:    openapiDoc.Servers[0].URL,
		openAPIDoc: *openapiDoc,
		metadata:   metadata,
		router:     router,
		httpClient: httpClient,

		providerCallback: callback,
		globalPathParams: make(map[string]string),
	}, nil
}

// GetResourceTypeToken returns the type token from a resource URN string.
func GetResourceTypeToken(u string) string {
	urn := resource.URN(u)
	return urn.Type().String()
}

func getResourceName(u string) string {
	return resource.URN(u).Name()
}

// Attach sends the engine address to an already running plugin.
func (p *Provider) Attach(_ context.Context, req *pulumirpc.PluginAttach) (*pbempty.Empty, error) {
	host, err := provider.NewHostClient(req.GetAddress())
	if err != nil {
		return nil, err
	}
	p.host = host
	return &pbempty.Empty{}, nil
}

// Call dynamically executes a method in the provider associated with a component resource.
func (p *Provider) Call(_ context.Context, _ *pulumirpc.CallRequest) (*pulumirpc.CallResponse, error) {
	return nil, status.Error(codes.Unimplemented, "call is not yet implemented")
}

// Construct creates a new component resource.
func (p *Provider) Construct(_ context.Context, _ *pulumirpc.ConstructRequest) (*pulumirpc.ConstructResponse, error) {
	return nil, status.Error(codes.Unimplemented, "construct is not yet implemented")
}

// CheckConfig validates the configuration for this provider.
func (p *Provider) CheckConfig(_ context.Context, req *pulumirpc.CheckRequest) (*pulumirpc.CheckResponse, error) {
	return &pulumirpc.CheckResponse{Inputs: req.GetNews()}, nil
}

// DiffConfig diffs the configuration for this provider.
func (p *Provider) DiffConfig(_ context.Context, _ *pulumirpc.DiffRequest) (*pulumirpc.DiffResponse, error) {
	return &pulumirpc.DiffResponse{}, nil
}

// Configure configures the resource provider with "globals" that control its behavior.
func (p *Provider) Configure(ctx context.Context, req *pulumirpc.ConfigureRequest) (*pulumirpc.ConfigureResponse, error) {
	resp, err := p.providerCallback.OnConfigure(ctx, req)
	if err != nil || resp != nil {
		return resp, err
	}

	globalPathParams, err := p.providerCallback.GetGlobalPathParams(ctx, req)
	if err != nil {
		return nil, errors.Wrap(err, "getting global path params")
	} else if globalPathParams != nil {
		p.globalPathParams = globalPathParams
	}

	// Override the API host, if required. Intended for providers where the server names in the
	// openapi spec will not match the API host that the provider needs to interact with during a deployment.
	// To set via pulumi config, this will be "providername:apiHost"
	// Otherwise, this can be set via the PROVIDERNAME_API_HOST env var
	apiHost, ok := req.GetVariables()[fmt.Sprintf("%s:config:apiHost", p.name)]
	if !ok {
		// Check if it's set in the {p.name}_API_HOST env var.
		apiHostEnvVar := strings.ToUpper(strings.ReplaceAll(fmt.Sprintf("%s_API_HOST", p.name), "-", "_"))
		v := os.Getenv(apiHostEnvVar)
		if v != "" {
			apiHost = v
		}
	}

	if apiHost != "" {
		if !strings.HasPrefix(p.baseURL, "/") {
			// The provider is already relative
			return nil, errors.Errorf("cannot override ApiHost when Open API server URL is not a base path e.g. /api/v1 - currently set to %s", p.baseURL)
		} else {
			logging.V(3).Infof("ApiHost overridden to %s", apiHost)
			p.baseURL = fmt.Sprintf("https://%s%s", apiHost, p.baseURL)
			logging.V(3).Infof("Full API URL now %s", p.baseURL)
		}
	}

	return &pulumirpc.ConfigureResponse{
		AcceptSecrets: true,
	}, nil
}

func (p *Provider) convertInvokeOutput(_ context.Context, req *pulumirpc.InvokeRequest, outputs interface{}) (map[string]interface{}, error) {
	invokeTypeToken := req.GetTok()

	// Return non-list operations as-is.
	if !strings.Contains(invokeTypeToken, ":list") {
		return outputs.(map[string]interface{}), nil
	}

	schemaSpec := p.GetSchemaSpec()
	funcSpec, ok := schemaSpec.Functions[invokeTypeToken]
	if !ok {
		return nil, fmt.Errorf("function definition (type token: %q) not found in schema spec", invokeTypeToken)
	}

	// If the return type for this function has an object
	// spec, it means it is already properly wrapped in a
	// JSON object.
	if funcSpec.ReturnType.ObjectTypeSpec == nil {
		return outputs.(map[string]interface{}), nil
	}

	// Otherwise, it is a naked array response that should
	// be enveloped by an `items` property in a new object.
	m := make(map[string]interface{})
	m["items"] = outputs
	return m, nil
}

// Invoke dynamically executes a built-in function in the provider.
func (p *Provider) Invoke(ctx context.Context, req *pulumirpc.InvokeRequest) (*pulumirpc.InvokeResponse, error) {
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

	if err := p.providerCallback.OnPreInvoke(ctx, req, httpReq); err != nil {
		return nil, err
	}

	// Read the resource.
	httpResp, err := p.httpClient.Do(httpReq)
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

	var outputs interface{}
	if err := json.Unmarshal(body, &outputs); err != nil {
		return nil, errors.Wrap(err, "unmarshaling the response")
	}

	logging.V(3).Infof("RESPONSE BODY: %v", outputs)

	outputsMap, postInvokeErr := p.providerCallback.OnPostInvoke(ctx, req, outputs)
	if postInvokeErr != nil {
		return nil, postInvokeErr
	}

	if outputsMap == nil {
		logging.V(3).Infof("OnPostInvoke returned nil output map. Attemping to self-convert output of invoke (token: %s) in provider framework", invokeTypeToken)
		var convertErr error
		outputsMap, convertErr = p.convertInvokeOutput(ctx, req, outputs)
		if convertErr != nil {
			return nil, errors.Wrapf(convertErr, "converting outputs")
		}
	}

	outputProperties, err := plugin.MarshalProperties(resource.NewPropertyMapFromMap(outputsMap), state.DefaultMarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the output properties map")
	}

	return &pulumirpc.InvokeResponse{
		Return: outputProperties,
	}, nil
}

// Check validates that the given property bag is valid for a resource of the given type and returns
// the inputs that should be passed to successive calls to Diff, Create, or Update for this
// resource. As a rule, the provider inputs returned by a call to Check should preserve the original
// representation of the properties as present in the program inputs. Though this rule is not
// required for correctness, violations thereof can negatively impact the end-user experience, as
// the provider inputs are used for detecting and rendering diffs.
func (p *Provider) Check(_ context.Context, req *pulumirpc.CheckRequest) (*pulumirpc.CheckResponse, error) {
	urn := req.GetUrn()
	resourceName := getResourceName(urn)
	resourceTypeToken := GetResourceTypeToken(urn)
	autoNameProp, ok := p.metadata.AutoNameMap[resourceTypeToken]

	// If this resource type token is not in the auto-name map,
	// then return the default `CheckResponse`.
	if !ok {
		return &pulumirpc.CheckResponse{Inputs: req.GetNews(), Failures: nil}, nil
	}

	logging.V(3).Infof("Resource type %q has an auto-name property %q", resourceTypeToken, autoNameProp)

	inputs, err := plugin.UnmarshalProperties(req.GetNews(), state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshaling new inputs in check method")
	}

	olds, err := plugin.UnmarshalProperties(req.GetOlds(), state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshaling old inputs in check method")
	}

	namePropKey := resource.PropertyKey(autoNameProp)

	// If neither the new inputs nor the old inputs have the name property
	if _, ok := inputs[namePropKey]; !ok {
		logging.V(3).Infof("New inputs did not have auto-name property %q", autoNameProp)

		if oldAutoNameValue, ok := olds[namePropKey]; !ok {
			logging.V(3).Infof("Old inputs did not have auto-name property %q. Will generate a new value...", autoNameProp)

			randomName, err := resource.NewUniqueName(req.GetRandomSeed(), resourceName+"-", 8, 24, nil)
			if err != nil {
				return nil, errors.Wrapf(err, "creating unique name for %s (token: %s)", resourceName, resourceTypeToken)
			}
			inputs[namePropKey] = resource.NewStringProperty(randomName)
		} else {
			logging.V(3).Infof("Found auto-name property %q in old inputs. Will set that in new inputs...", autoNameProp)
			inputs[namePropKey] = oldAutoNameValue
		}
	}

	updatedInputs, err := plugin.MarshalProperties(inputs, state.DefaultMarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling updated inputs in check method")
	}
	return &pulumirpc.CheckResponse{Inputs: updatedInputs}, nil
}

// Diff checks what impacts a hypothetical update will have on the resource's properties.
func (p *Provider) Diff(ctx context.Context, req *pulumirpc.DiffRequest) (*pulumirpc.DiffResponse, error) {
	oldState, err := plugin.UnmarshalProperties(req.GetOlds(), state.HTTPRequestBodyUnmarshalOpts)
	if err != nil {
		return nil, err
	}

	olds := state.GetOldInputs(oldState)
	if olds == nil {
		return nil, errors.New("fetching old inputs from the state")
	}

	resourceTypeToken := GetResourceTypeToken(req.GetUrn())
	crudMap, ok := p.metadata.ResourceCRUDMap[resourceTypeToken]
	if !ok {
		return nil, errors.Errorf("unknown resource type %s", resourceTypeToken)
	}

	news, err := plugin.UnmarshalProperties(req.GetNews(), state.HTTPRequestBodyUnmarshalOpts)
	if err != nil {
		return nil, err
	}

	logging.V(3).Infof("Calculating diff: olds: %v; news: %v", olds, news)
	diff := olds.Diff(news)
	if diff == nil || !diff.AnyChanges() {
		logging.V(3).Infof("Diff: no changes for %s", req.GetUrn())
		return &pulumirpc.DiffResponse{Changes: pulumirpc.DiffResponse_DIFF_NONE}, nil
	}

	logging.V(3).Info("Detected some changes before filtering out path params...")
	logging.V(4).Infof("ADDS: %v", diff.Adds)
	logging.V(4).Infof("DELETES: %v", diff.Deletes)
	logging.V(4).Infof("UPDATES: %v", diff.Updates)

	if crudMap.U == nil && crudMap.P == nil {
		ok, err := p.additionsArePathParams(diff, news, *crudMap.C, http.MethodPost)
		if err != nil {
			return nil, errors.Wrap(err, "determining if all additions were just path params")
		}

		if ok {
			logging.V(3).Infof("Reporting no diff for %s because all new additions were path params", req.GetUrn())
			return &pulumirpc.DiffResponse{Changes: pulumirpc.DiffResponse_DIFF_NONE}, nil
		}

		// If there is no PATCH or PUT endpoint for this type token,
		// then we'll need to trigger a replacement.
		logging.V(3).Infof("Resource type %s will only support replacement as it does not have update endpoints", resourceTypeToken)

		changedKeys := diff.ChangedKeys()
		replaces := make([]string, 0, len(changedKeys))

		// Capture all keys that have changed.
		for _, prop := range changedKeys {
			replaces = append(replaces, string(prop))
		}

		logging.V(3).Infof("Diffs for properties: %v", replaces)

		return &pulumirpc.DiffResponse{
			Changes:  pulumirpc.DiffResponse_DIFF_SOME,
			Replaces: replaces,
			Diffs:    replaces,
		}, nil
	}

	var updateOp *openapi3.Operation
	var endpoint string
	var method string
	switch {
	case crudMap.U != nil:
		endpoint = *crudMap.U
		updateOp = p.openAPIDoc.Paths.Find(endpoint).Patch
		method = http.MethodPatch
		if updateOp == nil {
			return nil, errors.Errorf("openapi doc does not have PATCH endpoint definition for the path %s", *crudMap.U)
		}
	case crudMap.P != nil:
		endpoint = *crudMap.P
		updateOp = p.openAPIDoc.Paths.Find(*crudMap.P).Put
		method = http.MethodPut
		if updateOp == nil {
			return nil, errors.Errorf("openapi doc does not have PUT endpoint definition for the path %s", *crudMap.U)
		}
	}

	if id, ok := news["id"]; !ok {
		logging.V(3).Info("Adding id property to news")
		// It's essential to add an `id` property here since the update
		// endpoint path for the resource will have path params that
		// may map to the `id` property.
		news["id"] = resource.NewPropertyValue(req.GetId())
	} else {
		logging.V(3).Infof("news already has an id property (val: %s). won't override it", id.StringValue())
	}

	noChanges, err := p.additionsArePathParams(diff, news, endpoint, method)
	if err != nil {
		return nil, errors.Wrap(err, "determining if all additions were just path params")
	}

	if noChanges {
		logging.V(3).Infof("Reporting no diff for %s because all new additions were path params", req.GetUrn())
		return &pulumirpc.DiffResponse{Changes: pulumirpc.DiffResponse_DIFF_NONE}, nil
	}

	var replaces []string
	var diffs []string
	changes := pulumirpc.DiffResponse_DIFF_SOME
	patchReqSchema := updateOp.RequestBody.Value.Content[jsonMimeType]

	diffResp, callbackErr := p.providerCallback.OnDiff(ctx, req, resourceTypeToken, diff, patchReqSchema)
	if callbackErr != nil || diffResp != nil {
		return diffResp, callbackErr
	}

	if len(patchReqSchema.Schema.Value.Properties) != 0 {
		replaces, diffs = p.determineDiffsAndReplacements(diff, *patchReqSchema.Schema)
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
func (p *Provider) Create(ctx context.Context, req *pulumirpc.CreateRequest) (*pulumirpc.CreateResponse, error) {
	logging.V(3).Infof("Create: %s", req.GetUrn())

	inputs, err := plugin.UnmarshalProperties(req.GetProperties(), state.HTTPRequestBodyUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal input properties as propertymap")
	}

	resourceTypeToken := GetResourceTypeToken(req.GetUrn())
	crudMap, ok := p.metadata.ResourceCRUDMap[resourceTypeToken]
	if !ok {
		return nil, errors.Errorf("unknown resource type %s", resourceTypeToken)
	}
	if crudMap.C == nil && crudMap.P == nil {
		return nil, errors.Errorf("resource construction endpoint is unknown for %s", resourceTypeToken)
	}

	bodyBytes, err := json.Marshal(inputs.Mappable())
	if err != nil {
		return nil, errors.Wrap(err, "marshaling inputs to json")
	}

	var httpEndpointPath string
	var httpReq *http.Request
	var httpReqErr error

	switch {
	// Prefer the PUT request over POST request for resource creation.
	// That's because if a resource has a PUT request, it's likely that
	// the endpoint for resource creation in the CRUD map is just there
	// as a placeholder so that the resource construction is possible.
	// In other words, this is a dirty hack. :)
	case crudMap.P != nil:
		// BUT if the Create (POST) endpoint is different from the Put endpoint,
		// we should use the Create endpoint.
		if crudMap.C != nil && *crudMap.C != *crudMap.P {
			logging.V(3).Infof("Using POST endpoint to create resource %s", resourceTypeToken)
			httpEndpointPath = *crudMap.C
			httpReq, httpReqErr = p.CreatePostRequest(ctx, httpEndpointPath, bodyBytes, inputs)
			if httpReqErr != nil {
				return nil, errors.Wrapf(httpReqErr, "creating post request (type token: %s)", resourceTypeToken)
			}
		} else {
			logging.V(3).Infof("Using PUT endpoint to create resource %s", resourceTypeToken)
			httpEndpointPath = *crudMap.P
			httpReq, httpReqErr = p.CreatePutRequest(ctx, httpEndpointPath, bodyBytes, inputs)
			if httpReqErr != nil {
				return nil, errors.Wrapf(httpReqErr, "creating put request (type token: %s)", resourceTypeToken)
			}
		}
	case crudMap.C != nil:
		httpEndpointPath = *crudMap.C
		httpReq, httpReqErr = p.CreatePostRequest(ctx, httpEndpointPath, bodyBytes, inputs)
		if httpReqErr != nil {
			return nil, errors.Wrapf(httpReqErr, "creating post request (type token: %s)", resourceTypeToken)
		}
	}

	preCreateErr := p.providerCallback.OnPreCreate(ctx, req, httpReq)
	if preCreateErr != nil {
		return nil, preCreateErr
	}

	// Create the resource.
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(err, "executing http request")
	}

	if httpResp.StatusCode != http.StatusOK &&
		httpResp.StatusCode != http.StatusCreated &&
		httpResp.StatusCode != http.StatusAccepted {
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

	var outputs interface{}
	if err := json.Unmarshal(body, &outputs); err != nil {
		return nil, errors.Wrap(err, "unmarshaling the response")
	}

	logging.V(3).Infof("RESPONSE BODY: %v", outputs)

	outputsMap, postCreateErr := p.providerCallback.OnPostCreate(ctx, req, outputs)
	if postCreateErr != nil {
		// TODO: returning a nil CreateResponse will mean that Pulumi will consider
		// this resource to not have been created. We should use the outputs we
		// already have to create the response.
		return nil, postCreateErr
	}

	p.TransformBody(ctx, outputsMap, p.metadata.APIToSDKNameMap)

	outputProperties, err := plugin.MarshalProperties(state.GetResourceState(outputsMap, inputs), state.DefaultMarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the output properties map")
	}

	id, ok := outputsMap["id"]
	if !ok {
		logging.V(3).Infof("id prop not found in top-level response. Checking if an embedded property has it...")
		// Try plucking the id from top-level properties.
		id, _, ok = tryPluckingProp("id", outputsMap)
		if !ok {
			// TODO: should we return the CreateResponse without the Id property here?
			return nil, errors.New("resource may have been created successfully but the id was not present in the response")
		}
	}

	return &pulumirpc.CreateResponse{
		Id:         convertNumericIDToString(id),
		Properties: outputProperties,
	}, nil
}

// Read the current live state associated with a resource.
func (p *Provider) Read(ctx context.Context, req *pulumirpc.ReadRequest) (*pulumirpc.ReadResponse, error) {
	oldState, err := plugin.UnmarshalProperties(req.GetProperties(), state.DefaultUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal current state as propertymap")
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

	if len(oldState) == 0 {
		if req.GetInputs() != nil {
			logging.V(3).Infoln("Resource does not have existing state. Will use input properties as existing state instead...")
			oldState, err = plugin.UnmarshalProperties(req.GetInputs(), state.DefaultUnmarshalOpts)
			if err != nil {
				return nil, errors.Wrap(err, "unmarshal input properties as propertymap")
			}
		} else {
			// This is a request to import a resource.
			id := req.GetId()
			if strings.Contains(id, "/") {
				pathParams, err := p.mapImportIDToPathParams(id, httpEndpointPath)
				if err != nil {
					return nil, errors.Wrapf(err, "mapping import id %s to path params", id)
				}

				oldState = resource.NewPropertyMapFromMap(pathParams)
			} else {
				oldState = resource.NewPropertyMapFromMap(
					map[string]interface{}{
						"id": id,
					})
			}
		}
	}

	logging.V(3).Infof("Resource read will use state: %v", oldState)

	if !oldState.HasValue("id") {
		// Add the id property to the state map since our HTTP request creation will
		// look for it in the inputs map.
		oldState["id"] = resource.NewPropertyValue(req.GetId())
	}

	httpReq, err := p.CreateGetRequest(ctx, httpEndpointPath, oldState)
	if err != nil {
		return nil, errors.Wrapf(err, "creating get request (type token: %s)", resourceTypeToken)
	}

	// TODO: At this point the request is already validated and path params
	// have been replaced. If the user modifies the request now, there is a
	// chance for it to fail. Should we just revalidate the request after this?
	preReadErr := p.providerCallback.OnPreRead(ctx, req, httpReq)
	if preReadErr != nil {
		return nil, preReadErr
	}

	// Read the resource.
	httpResp, err := p.httpClient.Do(httpReq)
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

	var outputs interface{}
	if err := json.Unmarshal(body, &outputs); err != nil {
		return nil, errors.Wrap(err, "unmarshaling the response")
	}

	outputsMap, postReadErr := p.providerCallback.OnPostRead(ctx, req, outputs)
	if postReadErr != nil {
		return nil, postReadErr
	}

	inputs := state.GetOldInputs(oldState)
	// If there is no old state, then persist the current outputs as the
	// "old" inputs going forward for this resource.
	if inputs == nil {
		inputs = resource.NewPropertyMapFromMap(outputsMap)
		// Filter out read-only properties from the inputs.
		pathItem := p.openAPIDoc.Paths.Find(*crudMap.C)
		var operation *openapi3.Operation
		if pathItem.Post != nil {
			operation = pathItem.Post
		} else if pathItem.Put != nil {
			operation = pathItem.Put
		} else {
			return nil, errors.Errorf("cannot determine the operation to use for endpoint path %s", *crudMap.C)
		}

		requestBodySchema := *operation.RequestBody.Value.Content.Get(jsonMimeType).Schema.Value
		var dv *string
		if requestBodySchema.Discriminator != nil {
			val := inputs[resource.PropertyKey(requestBodySchema.Discriminator.PropertyName)].StringValue()
			dv = &val
		}
		openapi.FilterReadOnlyProperties(ctx, requestBodySchema, inputs, dv)

		// Transform the inputs to match the API to SDK name map, which is assumed in later operations
		var inputsMappable = inputs.Mappable()
		p.TransformBody(ctx, inputsMappable, p.metadata.APIToSDKNameMap)
		inputs = resource.NewPropertyMapFromMap(inputsMappable)
	} else {
		// Take the values from outputs and apply them to the inputs
		// so that the checkpoint is in-sync with the state in the
		// cloud provider.
		newState := resource.NewPropertyMapFromMap(outputsMap)
		// Filter out read-only properties before we apply the cloud provider
		// state to our input state.
		pathItem := p.openAPIDoc.Paths.Find(*crudMap.C)
		var operation *openapi3.Operation
		if pathItem.Post != nil {
			operation = pathItem.Post
		} else if pathItem.Put != nil {
			operation = pathItem.Put
		} else {
			return nil, errors.Errorf("cannot determine the operation to use for endpoint path %s", *crudMap.C)
		}

		requestBodySchema := *operation.RequestBody.Value.Content.Get(jsonMimeType).Schema.Value
		var dv *string
		if requestBodySchema.Discriminator != nil {
			val := inputs[resource.PropertyKey(requestBodySchema.Discriminator.PropertyName)].StringValue()
			dv = &val
		}
		openapi.FilterReadOnlyProperties(ctx, requestBodySchema, newState, dv)

		// Transform the newState to match the API to SDK name map, which the inputs being diff'd with certainly will be
		var newStateMappable = newState.Mappable()
		p.TransformBody(ctx, newStateMappable, p.metadata.APIToSDKNameMap)
		newState = resource.NewPropertyMapFromMap(newStateMappable)

		inputs = state.ApplyDiffFromCloudProvider(newState, inputs)
	}

	// Make sure that the original output properties still remain in the state.
	// For example, resources like keys, secrets would return the actual secret
	// payload on creation but on subsequent reads, they won't be returned by
	// APIs, so we should maintain those in the outputs.
	updatedOutputsMap := state.ApplyDiffFromCloudProvider(resource.NewPropertyMapFromMap(outputsMap), oldState)

	outputsMap = updatedOutputsMap.Mappable()

	p.TransformBody(ctx, outputsMap, p.metadata.APIToSDKNameMap)

	outputProperties, err := plugin.MarshalProperties(state.GetResourceState(outputsMap, inputs), state.DefaultMarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the output properties map")
	}

	id, ok := outputsMap["id"]
	if !ok {
		logging.V(3).Infof("id prop not found in top-level response. Checking if an embedded property has it...")
		// Try plucking the id from top-level properties.
		id, _, ok = tryPluckingProp("id", outputsMap)
		if !ok {
			return nil, errors.New("looking-up id property from the response")
		}
	}

	// Serialize and return the calculated inputs.
	inputsRecord, err := plugin.MarshalProperties(inputs, state.DefaultMarshalOpts)
	if err != nil {
		return nil, err
	}

	return &pulumirpc.ReadResponse{
		Id:         convertNumericIDToString(id),
		Inputs:     inputsRecord,
		Properties: outputProperties,
	}, nil
}

// Update updates an existing resource with new values.
func (p *Provider) Update(ctx context.Context, req *pulumirpc.UpdateRequest) (*pulumirpc.UpdateResponse, error) {
	oldState, err := plugin.UnmarshalProperties(req.Olds, state.HTTPRequestBodyUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal olds as propertymap")
	}

	inputs, err := plugin.UnmarshalProperties(req.News, state.HTTPRequestBodyUnmarshalOpts)
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

	bodyBytes, err := json.Marshal(inputs.Mappable())
	if err != nil {
		return nil, errors.Wrap(err, "marshaling inputs")
	}

	var httpEndpointPath string
	var httpReq *http.Request
	var httpReqErr error

	if crudMap.U != nil {
		logging.V(3).Infof("Using PATCH endpoint to update resource %s", resourceTypeToken)
		httpEndpointPath = *crudMap.U
		httpReq, httpReqErr = p.CreatePatchRequest(ctx, httpEndpointPath, bodyBytes, oldState)
		if httpReqErr != nil {
			return nil, errors.Wrapf(httpReqErr, "creating patch request (type token: %s)", resourceTypeToken)
		}
	} else {
		logging.V(3).Infof("Using PUT endpoint to update resource %s", resourceTypeToken)
		httpEndpointPath = *crudMap.P
		httpReq, httpReqErr = p.CreatePutRequest(ctx, httpEndpointPath, bodyBytes, oldState)
		if httpReqErr != nil {
			return nil, errors.Wrapf(httpReqErr, "creating put request (type token: %s)", resourceTypeToken)
		}
	}

	preUpdateErr := p.providerCallback.OnPreUpdate(ctx, req, httpReq)
	if preUpdateErr != nil {
		return nil, preUpdateErr
	}

	// Update the resource.
	httpResp, err := p.httpClient.Do(httpReq)
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

	var outputs interface{}
	if err := json.Unmarshal(body, &outputs); err != nil {
		return nil, errors.Wrap(err, "unmarshaling the response")
	}

	logging.V(3).Infof("RESPONSE BODY: %v", outputs)

	outputsMap, postUpdateErr := p.providerCallback.OnPostUpdate(ctx, req, *httpReq, outputs)
	if postUpdateErr != nil {
		return nil, postUpdateErr
	}

	p.TransformBody(ctx, outputsMap, p.metadata.APIToSDKNameMap)

	// TODO: Could this erase refreshed inputs that were previously saved in outputs state?
	outputProperties, err := plugin.MarshalProperties(state.GetResourceState(outputsMap, inputs), state.DefaultMarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the output properties map")
	}

	return &pulumirpc.UpdateResponse{
		Properties: outputProperties,
	}, nil
}

// Delete tears down an existing resource with the given ID. If it fails, the resource is assumed
// to still exist.
func (p *Provider) Delete(ctx context.Context, req *pulumirpc.DeleteRequest) (*pbempty.Empty, error) {
	inputs, err := plugin.UnmarshalProperties(req.GetProperties(), state.HTTPRequestBodyUnmarshalOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal input properties as propertymap")
	}

	resourceTypeToken := GetResourceTypeToken(req.GetUrn())

	crudMap, ok := p.metadata.ResourceCRUDMap[resourceTypeToken]
	if !ok {
		return nil, errors.Errorf("unknown resource type %s", resourceTypeToken)
	}
	if crudMap.D == nil {
		// Nothing to do to delete this resource,
		// simply drop it from the state.
		return &pbempty.Empty{}, nil
	}

	logging.V(3).Infof("Using DELETE endpoint to delete resource %s", resourceTypeToken)
	var httpEndpointPath = *crudMap.D
	var httpReq, httpReqErr = p.CreateDeleteRequest(ctx, httpEndpointPath, nil, inputs)
	if httpReqErr != nil {
		return nil, errors.Wrapf(httpReqErr, "creating delete request (type token: %s)", resourceTypeToken)
	}

	preErr := p.providerCallback.OnPreDelete(ctx, req, httpReq)
	if preErr != nil {
		return nil, preErr
	}

	// Delete the resource.
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(err, "executing http request")
	}

	if !slices.Contains(validStatusCodesForDelete, httpResp.StatusCode) {
		return nil, errors.Errorf("http request failed: %v. expected one of %v but got %d", err, validStatusCodesForDelete, httpResp.StatusCode)
	}

	httpResp.Body.Close()

	postDeleteErr := p.providerCallback.OnPostDelete(ctx, req)
	if postDeleteErr != nil {
		return nil, postDeleteErr
	}

	return &pbempty.Empty{}, nil
}

// GetPluginInfo returns generic information about this plugin, like its version.
func (p *Provider) GetPluginInfo(context.Context, *pbempty.Empty) (*pulumirpc.PluginInfo, error) {
	return &pulumirpc.PluginInfo{
		Version: p.version,
	}, nil
}

// GetSchema returns the JSON-serialized schema for the provider.
func (p *Provider) GetSchema(_ context.Context, _ *pulumirpc.GetSchemaRequest) (*pulumirpc.GetSchemaResponse, error) {
	b, err := json.Marshal(p.schema)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the schema")
	}

	return &pulumirpc.GetSchemaResponse{
		Schema: string(b),
	}, nil
}

// Cancel signals the provider to gracefully shut down and abort any ongoing resource operations.
// Operations aborted in this way will return an error (e.g., `Update` and `Create` will either
// return a creation error or an initialization error). Since Cancel is advisory and non-blocking,
// it is up to the host to decide how long to wait after Cancel is called before (e.g.)
// hard-closing any gRPC connection.
func (p *Provider) Cancel(context.Context, *pbempty.Empty) (*pbempty.Empty, error) {
	return &pbempty.Empty{}, nil
}

func (p *Provider) GetOpenAPIDoc() openapi3.T {
	return p.openAPIDoc
}

func (p *Provider) GetSchemaSpec() pschema.PackageSpec {
	return p.schema
}

func (p *Provider) GetBaseURL() string {
	return p.baseURL
}

func (p *Provider) GetHTTPClient() *http.Client {
	return p.httpClient
}

func (p *Provider) GetMapping(_ context.Context, _ *pulumirpc.GetMappingRequest) (*pulumirpc.GetMappingResponse, error) {
	return &pulumirpc.GetMappingResponse{}, nil
}
