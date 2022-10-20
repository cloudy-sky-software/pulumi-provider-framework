package rest

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cloudy-sky-software/pulumi-provider-framework/callback"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"

	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

type fakeProviderCallback struct {
}

func (p *fakeProviderCallback) GetAuthorizationHeader() string {
	return fmt.Sprintf("%s fake-token", authSchemePrefix)
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

func (p *fakeProviderCallback) OnPreCreate(ctx context.Context, req *pulumirpc.CreateRequest, http *http.Request) error {
	return nil
}

func (p *fakeProviderCallback) OnPostCreate(ctx context.Context, req *pulumirpc.CreateRequest, outputs map[string]interface{}) (map[string]interface{}, error) {
	return outputs, nil
}

func (p *fakeProviderCallback) OnPreRead(ctx context.Context, req *pulumirpc.ReadRequest, httpReq *http.Request) error {
	return nil
}

func (p *fakeProviderCallback) OnPostRead(ctx context.Context, req *pulumirpc.ReadRequest, outputs map[string]interface{}) (map[string]interface{}, error) {
	return outputs, nil
}

func (p *fakeProviderCallback) OnPreUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, httpReq *http.Request) error {
	return nil
}

func (p *fakeProviderCallback) OnPostUpdate(ctx context.Context, req *pulumirpc.UpdateRequest, httpReq http.Request, outputs map[string]interface{}) (map[string]interface{}, error) {
	return outputs, nil
}

func (p *fakeProviderCallback) OnPreDelete(ctx context.Context, req *pulumirpc.DeleteRequest, httpReq *http.Request) error {
	return nil
}

func (p *fakeProviderCallback) OnPostDelete(ctx context.Context, req *pulumirpc.DeleteRequest) error {
	return nil
}

var providerCallback callback.RestProviderCallback = &fakeProviderCallback{}
