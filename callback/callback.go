package callback

import (
	"context"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"

	pbempty "github.com/golang/protobuf/ptypes/empty"
)

type RestProviderCallback interface {
	GetAuthorizationHeader() string

	OnConfigure(ctx context.Context, req *pulumirpc.ConfigureRequest) (*pulumirpc.ConfigureResponse, error)

	OnInvoke(ctx context.Context, req *pulumirpc.InvokeRequest) (*pulumirpc.InvokeResponse, error)

	OnDiff(ctx context.Context, req *pulumirpc.DiffRequest, resourceTypeToken string, diff *resource.ObjectDiff, jsonReq *openapi3.MediaType) (*pulumirpc.DiffResponse, error)

	OnPreCreate(ctx context.Context, req *pulumirpc.CreateRequest, httpReq *http.Request) error
	OnPostCreate(ctx context.Context, req *pulumirpc.CreateRequest, outputs map[string]interface{}) (map[string]interface{}, error)

	OnPreRead(ctx context.Context, req *pulumirpc.ReadRequest, httpReq *http.Request) error
	OnPostRead(ctx context.Context, req *pulumirpc.ReadRequest, outputs map[string]interface{}) (map[string]interface{}, error)

	OnPreUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, httpReq *http.Request) error
	OnPostUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, httpReq http.Request, outputs map[string]interface{}) (map[string]interface{}, error)

	OnPreDelete(ctx context.Context, req *pulumirpc.DeleteRequest) (*pbempty.Empty, error)
	OnPostDelete(ctx context.Context, req *pulumirpc.DeleteRequest) (*pbempty.Empty, error)
}
