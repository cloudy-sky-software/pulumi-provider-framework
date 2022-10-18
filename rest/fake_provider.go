package rest

import (
	"context"
	"net/http"

	"github.com/cloudy-sky-software/pulumi-provider-framework/callback"

	"github.com/getkin/kin-openapi/openapi3"

	pbempty "github.com/golang/protobuf/ptypes/empty"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"

	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

type fakeProviderCallback struct {
}

func (p *fakeProviderCallback) OnConfigure(ctx context.Context, req *pulumirpc.ConfigureRequest) (*pulumirpc.ConfigureResponse, error) {
	return nil, nil
}

func (p *fakeProviderCallback) OnInvoke(ctx context.Context, req *pulumirpc.InvokeRequest) (*pulumirpc.InvokeResponse, error) {
	return nil, nil
}

func (p *fakeProviderCallback) OnDiff(ctx context.Context, req *pulumirpc.DiffRequest, resourceTypeToken string, diff *resource.ObjectDiff, jsonReq *openapi3.MediaType) (*pulumirpc.DiffResponse, error) {
	return nil, nil
}

func (p *fakeProviderCallback) OnPreCreate(ctx context.Context, req *pulumirpc.CreateRequest) (*pulumirpc.CreateResponse, error) {
	return nil, nil
}

func (p *fakeProviderCallback) OnPostCreate(ctx context.Context, req *pulumirpc.CreateRequest, outputs map[string]interface{}) error {
	return nil
}

func (p *fakeProviderCallback) OnPreRead(ctx context.Context, req *pulumirpc.ReadRequest, httpReq *http.Request) error {
	return nil
}

func (p *fakeProviderCallback) OnPostRead(ctx context.Context, req *pulumirpc.ReadRequest, outputs map[string]interface{}) error {
	return nil
}

func (p *fakeProviderCallback) OnPreUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, httpReq *http.Request) error {
	return nil
}

func (p *fakeProviderCallback) OnPostUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, outputs map[string]interface{}) error {
	return nil
}

func (p *fakeProviderCallback) OnPreDelete(ctx context.Context, req *pulumirpc.DeleteRequest) (*pbempty.Empty, error) {
	return nil, nil
}

func (p *fakeProviderCallback) OnPostDelete(ctx context.Context, req *pulumirpc.DeleteRequest) (*pbempty.Empty, error) {
	return nil, nil
}

var providerCallback callback.RestProviderCallback = &fakeProviderCallback{}
