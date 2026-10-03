package rest

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"time"

	"github.com/pkg/errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/cloudy-sky-software/pulumi-provider-framework/state"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/logging"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/rpcutil/rpcerror"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

// marshalCreateOutputs converts the outputs of a create operation into
// the properties that are saved in the resource's state.
func (p *Provider) marshalCreateOutputs(outputsMap map[string]interface{}, inputs resource.PropertyMap) (*structpb.Struct, error) {
	if !p.engineSendsOldInputs {
		return plugin.MarshalProperties(state.GetResourceState(outputsMap, inputs), state.DefaultMarshalOpts)
	}

	return plugin.MarshalProperties(resource.NewPropertyMapFromMap(outputsMap), state.DefaultMarshalOpts)
}

// waitForAcceptedCreate polls the GET endpoint of a resource whose creation
// was accepted (202) but not yet complete, until the resource is ready (200)
// or the timeout is exceeded. The response of the GET endpoint is merged into
// createOutputs, which is the response body of the create request.
//
// If polling fails, the returned error carries an ErrorResourceInitFailed
// detail with the outputs of the create request, so that the engine records
// the resource in the state instead of losing track of it.
func (p *Provider) waitForAcceptedCreate(ctx context.Context, req *pulumirpc.CreateRequest, getEndpointPath *string, resourceTypeToken string, createOutputs map[string]interface{}, inputs resource.PropertyMap) error {
	if getEndpointPath == nil {
		err := errors.Errorf("resource accepted (202) but no read endpoint is available for %s", resourceTypeToken)
		return p.newResourceInitFailedError(ctx, createOutputs, inputs, err)
	}

	// Merge the 202 response body into a copy of the inputs so path params
	// (e.g. the resource id) can be resolved when constructing the GET request.
	// A copy is used so that the response props are not saved as inputs in the state.
	pollInputs := inputs.Copy()
	for k, v := range createOutputs {
		pollInputs[resource.PropertyKey(k)] = resource.NewPropertyValue(v)
	}

	var pollTimeout time.Duration
	if req.GetTimeout() > 0 {
		pollTimeout = time.Duration(req.GetTimeout()) * time.Second
	}

	pollOutputs, err := p.pollResourceUntilReady(ctx, *getEndpointPath, pollInputs, pollTimeout)
	if err != nil {
		err = errors.Wrap(err, "polling resource after 202 response")
		return p.newResourceInitFailedError(ctx, createOutputs, inputs, err)
	}

	maps.Copy(createOutputs, pollOutputs)
	return nil
}

// newResourceInitFailedError returns an error that tells the engine the
// resource was created but did not finish initializing, using the partial
// outputs that are known so far. If the id of the resource cannot be found
// in outputsMap, cause is returned as-is.
func (p *Provider) newResourceInitFailedError(ctx context.Context, outputsMap map[string]interface{}, inputs resource.PropertyMap, cause error) error {
	p.TransformBody(ctx, outputsMap, p.metadata.APIToSDKNameMap)

	id, ok := getResourceID(outputsMap)
	if !ok {
		return cause
	}

	outputProperties, err := p.marshalCreateOutputs(outputsMap, inputs)
	if err != nil {
		logging.V(3).Infof("Failed to marshal the partial outputs of resource %v: %v", id, err)
		return cause
	}

	return rpcerror.WithDetails(
		rpcerror.New(codes.Unknown, cause.Error()),
		&pulumirpc.ErrorResourceInitFailed{
			Id:         convertNumericIDToString(id),
			Properties: outputProperties,
			Reasons:    []string{cause.Error()},
			// The saved state is incomplete, so refresh it before the next update.
			RefreshBeforeUpdate: true,
		})
}

// pollResourceUntilReady polls the GET endpoint for the resource until it returns 200 OK
// or the context times out. Polling continues while the GET endpoint returns 404 (resource
// not yet created) and stops when 200 is returned (resource exists). Uses exponential
// backoff between poll attempts.
func (p *Provider) pollResourceUntilReady(ctx context.Context, getEndpointPath string, inputs resource.PropertyMap, timeout time.Duration) (map[string]interface{}, error) {
	if timeout <= 0 {
		timeout = defaultPollingTimeout
	}

	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	interval := initialPollingInterval

	for {
		httpReq, err := p.CreateGetRequest(pollCtx, getEndpointPath, inputs, nil)
		if err != nil {
			return nil, errors.Wrap(err, "creating get request during polling")
		}

		// nolint: gosec
		httpResp, err := p.httpClient.Do(httpReq)
		if err != nil {
			if pollCtx.Err() != nil {
				return nil, errors.Wrap(pollCtx.Err(), "polling timed out")
			}
			return nil, errors.Wrap(err, "executing http request during polling")
		}

		switch httpResp.StatusCode {
		case http.StatusOK:
			// Resource is ready — read the body and return.
			body, readErr := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			if readErr != nil {
				return nil, errors.Wrap(readErr, "reading response body during polling")
			}
			var outputs map[string]interface{}
			if err := json.Unmarshal(body, &outputs); err != nil {
				return nil, errors.Wrap(err, "unmarshaling response during polling")
			}
			return outputs, nil

		case http.StatusNotFound:
			// Resource not yet created — continue polling.
			httpResp.Body.Close()
			logging.V(3).Infof("pollResourceUntilReady: resource not yet ready (404), will retry in %s", interval)

		default:
			// Unexpected status code — return an error.
			body, readErr := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			if readErr != nil {
				return nil, errors.Errorf("polling returned unexpected status %s and body could not be read", httpResp.Status)
			}
			return nil, errors.Errorf("polling returned unexpected status %s: %s", httpResp.Status, string(body))
		}

		// Wait with exponential backoff before the next poll attempt.
		select {
		case <-pollCtx.Done():
			return nil, errors.Wrap(pollCtx.Err(), "polling timed out waiting for resource to become ready")
		case <-time.After(interval):
		}

		// Double the interval, capped at maxPollingInterval.
		interval *= 2
		if interval > maxPollingInterval {
			interval = maxPollingInterval
		}
	}
}

func (p *Provider) postCreate(ctx context.Context, req *pulumirpc.CreateRequest, inputs resource.PropertyMap, outputs any) (*pulumirpc.CreateResponse, error) {
	outputsMap, postCreateErr := p.providerCallback.OnPostCreate(ctx, req, outputs)
	if postCreateErr != nil {
		// TODO: returning a nil CreateResponse will mean that Pulumi will consider
		// this resource to not have been created. We should use the outputs we
		// already have to create the response.
		return nil, postCreateErr
	}

	p.TransformBody(ctx, outputsMap, p.metadata.APIToSDKNameMap)

	outputProperties, err := p.marshalCreateOutputs(outputsMap, inputs)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling the output properties map")
	}

	id, ok := getResourceID(outputsMap)
	if !ok {
		// TODO: should we return the CreateResponse without the Id property here?
		return nil, errors.New("resource may have been created successfully but the id was not present in the response")
	}

	return &pulumirpc.CreateResponse{
		Id:                  convertNumericIDToString(id),
		Properties:          outputProperties,
		RefreshBeforeUpdate: false,
	}, nil
}
