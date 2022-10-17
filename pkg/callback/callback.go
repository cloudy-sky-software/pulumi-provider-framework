package callback

import (
	"context"
	"net/http"

	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"

	pbempty "github.com/golang/protobuf/ptypes/empty"
)

type RestProviderCallback interface {
	OnConfigure(ctx context.Context, req *pulumirpc.ConfigureRequest) (*pulumirpc.ConfigureResponse, error)
	OnInvoke(ctx context.Context, req *pulumirpc.InvokeRequest) (*pulumirpc.InvokeResponse, error)
	OnDiff(ctx context.Context, req *pulumirpc.DiffRequest) (*pulumirpc.DiffResponse, error)

	OnPreCreate(ctx context.Context, req *pulumirpc.CreateRequest) (*pulumirpc.CreateResponse, error)
	OnPostCreate(ctx context.Context, req *pulumirpc.CreateRequest, outputs map[string]interface{}) error

	OnPreRead(ctx context.Context, req *pulumirpc.ReadRequest, httpReq *http.Request) error
	OnPostRead(ctx context.Context, req *pulumirpc.ReadRequest, outputs map[string]interface{}) error

	OnPreUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, httpReq *http.Request) error
	OnPostUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, outputs map[string]interface{}) error

	OnPreDelete(ctx context.Context, req *pulumirpc.DeleteRequest) (*pbempty.Empty, error)
	OnPostDelete(ctx context.Context, req *pulumirpc.DeleteRequest) (*pbempty.Empty, error)
}
