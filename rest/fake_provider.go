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

var _ callback.ProviderCallback = &fakeProviderCallback{}

func (p *fakeProviderCallback) GetAuthorizationHeader() string {
	return fmt.Sprintf("%s fake-token", bearerAuthSchemePrefix)
}

func (p *fakeProviderCallback) OnConfigure(_ context.Context, _ *pulumirpc.ConfigureRequest) (*pulumirpc.ConfigureResponse, error) {
	return nil, nil
}

func (p *fakeProviderCallback) OnPreInvoke(_ context.Context, _ *pulumirpc.InvokeRequest, _ *http.Request) error {
	return nil
}

func (p *fakeProviderCallback) OnPostInvoke(_ context.Context, _ *pulumirpc.InvokeRequest, outputs interface{}) (map[string]interface{}, error) {
	return outputs.(map[string]interface{}), nil
}

func (p *fakeProviderCallback) OnDiff(_ context.Context, _ *pulumirpc.DiffRequest, _ string, _ *resource.ObjectDiff, _ *openapi3.MediaType) (*pulumirpc.DiffResponse, error) {
	return nil, nil
}

func (p *fakeProviderCallback) OnPreCreate(_ context.Context, _ *pulumirpc.CreateRequest, _ *http.Request) error {
	return nil
}

func (p *fakeProviderCallback) OnPostCreate(_ context.Context, _ *pulumirpc.CreateRequest, outputs interface{}) (map[string]interface{}, error) {
	return outputs.(map[string]interface{}), nil
}

func (p *fakeProviderCallback) OnPreRead(_ context.Context, _ *pulumirpc.ReadRequest, _ *http.Request) error {
	return nil
}

func (p *fakeProviderCallback) OnPostRead(_ context.Context, _ *pulumirpc.ReadRequest, outputs interface{}) (map[string]interface{}, error) {
	return outputs.(map[string]interface{}), nil
}

func (p *fakeProviderCallback) OnPreUpdate(_ context.Context, _ *pulumirpc.UpdateRequest, _ *http.Request) error {
	return nil
}

func (p *fakeProviderCallback) OnPostUpdate(_ context.Context, _ *pulumirpc.UpdateRequest, _ http.Request, outputs interface{}) (map[string]interface{}, error) {
	return outputs.(map[string]interface{}), nil
}

func (p *fakeProviderCallback) OnPreDelete(_ context.Context, _ *pulumirpc.DeleteRequest, _ *http.Request) error {
	return nil
}

func (p *fakeProviderCallback) OnPostDelete(_ context.Context, _ *pulumirpc.DeleteRequest) error {
	return nil
}
