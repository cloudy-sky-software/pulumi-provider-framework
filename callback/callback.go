package callback

import (
	"context"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

type ProviderCallback interface {
	// GetAuthorizationHeader returns the value of the Authorization header
	// used in all calls to the REST API.
	GetAuthorizationHeader() string

	// OnConfigure is a hook for configuring the provider.
	// Return a non-nil response to override the default behavior.
	OnConfigure(ctx context.Context, req *pulumirpc.ConfigureRequest) (*pulumirpc.ConfigureResponse, error)

	// GetGlobalPathParams returns a map of path parameters used across all (or most) requests
	// This method is called during the Configure phase, but after the OnConfigure hook.
	GetGlobalPathParams(ctx context.Context, req *pulumirpc.ConfigureRequest) (map[string]string, error)

	OnPreInvoke(ctx context.Context, req *pulumirpc.InvokeRequest, httpReq *http.Request) error
	OnPostInvoke(ctx context.Context, req *pulumirpc.InvokeRequest, outputs interface{}) (map[string]interface{}, error)

	// OnDiff is a hook for calculating diffs on old vs. new inputs.
	// Return a non-nil response to override the default behavior.
	OnDiff(ctx context.Context, req *pulumirpc.DiffRequest, resourceTypeToken string, diff *resource.ObjectDiff, jsonReq *openapi3.MediaType) (*pulumirpc.DiffResponse, error)

	// OnPreCreate is a hook for modifying the HTTP request
	// to be made for the create request.
	// Return a non-nil error to fail the request.
	OnPreCreate(ctx context.Context, req *pulumirpc.CreateRequest, httpReq *http.Request) error

	// OnPostCreate is a hook for modifying the outputs.
	// Implementations must return an outputs map,
	// which can either be the same as the one that
	// was provided to it or modified in some way.
	OnPostCreate(ctx context.Context, req *pulumirpc.CreateRequest, outputs interface{}) (map[string]interface{}, error)

	// OnPreRead is a hook for modifying the HTTP request
	// to be made for the read request.
	// Return a non-nil error to fail the request.
	OnPreRead(ctx context.Context, req *pulumirpc.ReadRequest, httpReq *http.Request) error
	// OnPostRead is a hook for modifying the outputs.
	// Implementations must return an outputs map,
	// which can either be the same as the one that
	// was provided to it or modified in some way.
	OnPostRead(ctx context.Context, req *pulumirpc.ReadRequest, outputs interface{}) (map[string]interface{}, error)

	// OnPreUpdate is a hook for modifying the HTTP request
	// to be made for the update request.
	// Return a non-nil error to fail the request.
	OnPreUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, httpReq *http.Request) error
	// OnPostUpdate is a hook for modifying the outputs.
	// Implementations must return an outputs map,
	// which can either be the same as the one that
	// was provided to it or modified in some way.
	OnPostUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, httpReq http.Request, outputs interface{}) (map[string]interface{}, error)

	// OnPreDelete is a hook for modifying the HTTP request
	// to be made for the delete request.
	// Return a non-nil error to fail the request.
	OnPreDelete(ctx context.Context, req *pulumirpc.DeleteRequest, httpReq *http.Request) error
	// OnPostDelete is a hook for modifying the outputs.
	OnPostDelete(ctx context.Context, req *pulumirpc.DeleteRequest) error
}

type UnimplementedProviderCallback struct{}

func (UnimplementedProviderCallback) OnConfigure(context.Context, *pulumirpc.ConfigureRequest) (*pulumirpc.ConfigureResponse, error) {
	return &pulumirpc.ConfigureResponse{
		AcceptSecrets: true,
	}, nil
}

func (UnimplementedProviderCallback) GetGlobalPathParams(context.Context, *pulumirpc.ConfigureRequest) (map[string]string, error) {
	return nil, nil
}

func (UnimplementedProviderCallback) OnPreInvoke(context.Context, *pulumirpc.InvokeRequest, *http.Request) error {
	return nil
}

func (UnimplementedProviderCallback) OnPostInvoke(_ context.Context, _ *pulumirpc.InvokeRequest, outputs interface{}) (map[string]interface{}, error) {
	return outputs.(map[string]interface{}), nil
}

func (UnimplementedProviderCallback) OnDiff(context.Context, *pulumirpc.DiffRequest, string, *resource.ObjectDiff, *openapi3.MediaType) (*pulumirpc.DiffResponse, error) {
	return nil, nil
}

func (UnimplementedProviderCallback) OnPreCreate(context.Context, *pulumirpc.CreateRequest, *http.Request) error {
	return nil
}

func (UnimplementedProviderCallback) OnPostCreate(_ context.Context, _ *pulumirpc.CreateRequest, outputs interface{}) (map[string]interface{}, error) {
	return outputs.(map[string]interface{}), nil
}

func (UnimplementedProviderCallback) OnPreRead(context.Context, *pulumirpc.ReadRequest, *http.Request) error {
	return nil
}

func (UnimplementedProviderCallback) OnPostRead(_ context.Context, _ *pulumirpc.ReadRequest, outputs interface{}) (map[string]interface{}, error) {
	return outputs.(map[string]interface{}), nil
}

func (UnimplementedProviderCallback) OnPreUpdate(context.Context, *pulumirpc.UpdateRequest, *http.Request) error {
	return nil
}

func (UnimplementedProviderCallback) OnPostUpdate(_ context.Context, _ *pulumirpc.UpdateRequest, _ http.Request, outputs interface{}) (map[string]interface{}, error) {
	return outputs.(map[string]interface{}), nil
}

func (UnimplementedProviderCallback) OnPreDelete(context.Context, *pulumirpc.DeleteRequest, *http.Request) error {
	return nil
}

func (UnimplementedProviderCallback) OnPostDelete(context.Context, *pulumirpc.DeleteRequest) error {
	return nil
}
