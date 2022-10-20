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

	OnInvoke(ctx context.Context, req *pulumirpc.InvokeRequest) (*pulumirpc.InvokeResponse, error)

	// OnDiff is a hook for calculating diffs on old vs. new inputs.
	// Return a non-nil response to override the default behavior.
	OnDiff(ctx context.Context, req *pulumirpc.DiffRequest, resourceTypeToken string, diff *resource.ObjectDiff, jsonReq *openapi3.MediaType) (*pulumirpc.DiffResponse, error)

	// OnePreCreate is a hook for modifying the HTTP request
	// to be made for the create request.
	// Return a non-nil error to fail the request.
	OnPreCreate(ctx context.Context, req *pulumirpc.CreateRequest, httpReq *http.Request) error
	// OnePostCreate is a hook for modifying the outputs.
	// Implementations must return an outputs map,
	// which can either be the same as the one that
	// was provided to it or modified in some way.
	OnPostCreate(ctx context.Context, req *pulumirpc.CreateRequest, outputs map[string]interface{}) (map[string]interface{}, error)

	// OnPreRead is a hook for modifying the HTTP request
	// to be made for the read request.
	// Return a non-nil error to fail the request.
	OnPreRead(ctx context.Context, req *pulumirpc.ReadRequest, httpReq *http.Request) error
	// OnPostRead is a hook for modifying the outputs.
	// Implementations must return an outputs map,
	// which can either be the same as the one that
	// was provided to it or modified in some way.
	OnPostRead(ctx context.Context, req *pulumirpc.ReadRequest, outputs map[string]interface{}) (map[string]interface{}, error)

	// OnPreUpdate is a hook for modifying the HTTP request
	// to be made for the update request.
	// Return a non-nil error to fail the request.
	OnPreUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, httpReq *http.Request) error
	// OnPostUpdate is a hook for modifying the outputs.
	// Implementations must return an outputs map,
	// which can either be the same as the one that
	// was provided to it or modified in some way.
	OnPostUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, httpReq http.Request, outputs map[string]interface{}) (map[string]interface{}, error)

	// OnPreDelete is a hook for modifying the HTTP request
	// to be made for the delete request.
	// Return a non-nil error to fail the request.
	OnPreDelete(ctx context.Context, req *pulumirpc.DeleteRequest, httpReq *http.Request) error
	// OnPostDelete is a hook for modifying the outputs.
	OnPostDelete(ctx context.Context, req *pulumirpc.DeleteRequest) error
}
