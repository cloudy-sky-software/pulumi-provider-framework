package rest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
)

const fakeResourceByIDURLPath = "/v2/fakeresource/fake-id"

// TestRateLimitTransportRetryWithRetryAfterInteger verifies that when a 429
// response is received with a valid non-negative integer Retry-After header,
// the request is retried and ultimately succeeds.
func TestRateLimitTransportRetryWithRetryAfterInteger(t *testing.T) {
	ctx := context.Background()

	outputsJSON := `{"another_prop":"output value"}`
	var requestCount atomic.Int32

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if r.URL.Path == fakeResourceByIDURLPath {
			if count == 1 {
				// Return 429 on the first request with Retry-After: 0
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"message":"rate limited"}`)
				return
			}
			// Return success on subsequent requests
			_, err := io.WriteString(w, outputsJSON)
			if err != nil {
				t.Errorf("Error writing string to the response stream: %v", err)
			}
			return
		}

		_, err := io.WriteString(w, "Unknown path")
		if err != nil {
			t.Errorf("Error writing string to the response stream: %v", err)
		}
	}))
	testServer.EnableHTTP2 = true
	testServer.Start()
	defer testServer.Close()

	p := makeTestGenericProvider(ctx, t, testServer, nil)

	readResp, err := p.Read(ctx, &pulumirpc.ReadRequest{
		Id:         "/fake-id",
		Inputs:     nil,
		Properties: nil,
		Urn:        "urn:pulumi:some-stack::some-project::generic:fakeresource/v2:FakeResource::myResource",
	})

	require.NoError(t, err)
	assert.NotNil(t, readResp)
	// The transport should have retried: total requests = 2
	assert.Equal(t, int32(2), requestCount.Load(), "Expected 2 requests: one 429 and one successful retry")
	assert.Contains(t, readResp.GetProperties().AsMap(), "anotherProp")
}

// TestRateLimitTransportNoRetryWithoutRetryAfterHeader verifies that when a
// 429 response is received without a Retry-After header, the 429 is returned
// to the caller without retrying.
func TestRateLimitTransportNoRetryWithoutRetryAfterHeader(t *testing.T) {
	ctx := context.Background()

	var requestCount atomic.Int32

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if r.URL.Path == fakeResourceByIDURLPath {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"message":"rate limited"}`)
			return
		}

		_, err := io.WriteString(w, "Unknown path")
		if err != nil {
			t.Errorf("Error writing string to the response stream: %v", err)
		}
	}))
	testServer.EnableHTTP2 = true
	testServer.Start()
	defer testServer.Close()

	p := makeTestGenericProvider(ctx, t, testServer, nil)

	readResp, err := p.Read(ctx, &pulumirpc.ReadRequest{
		Id:         "/fake-id",
		Inputs:     nil,
		Properties: nil,
		Urn:        "urn:pulumi:some-stack::some-project::generic:fakeresource/v2:FakeResource::myResource",
	})

	assert.Error(t, err, "Expected an error when 429 has no Retry-After header")
	assert.Nil(t, readResp)
	// Only one request should have been made (no retry)
	assert.Equal(t, int32(1), requestCount.Load(), "Expected exactly 1 request with no retry")
}

// TestRateLimitTransportNoRetryWithNegativeRetryAfter verifies that when a
// 429 response includes a negative Retry-After value, the 429 is returned to
// the caller without retrying.
func TestRateLimitTransportNoRetryWithNegativeRetryAfter(t *testing.T) {
	ctx := context.Background()

	var requestCount atomic.Int32

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if r.URL.Path == fakeResourceByIDURLPath {
			w.Header().Set("Retry-After", "-1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"message":"rate limited"}`)
			return
		}

		_, err := io.WriteString(w, "Unknown path")
		if err != nil {
			t.Errorf("Error writing string to the response stream: %v", err)
		}
	}))
	testServer.EnableHTTP2 = true
	testServer.Start()
	defer testServer.Close()

	p := makeTestGenericProvider(ctx, t, testServer, nil)

	readResp, err := p.Read(ctx, &pulumirpc.ReadRequest{
		Id:         "/fake-id",
		Inputs:     nil,
		Properties: nil,
		Urn:        "urn:pulumi:some-stack::some-project::generic:fakeresource/v2:FakeResource::myResource",
	})

	assert.Error(t, err, "Expected an error when Retry-After is negative")
	assert.Nil(t, readResp)
	assert.Equal(t, int32(1), requestCount.Load(), "Expected exactly 1 request with no retry")
}

// TestRateLimitTransportNoRetryWithNonIntegerRetryAfter verifies that when a
// 429 response includes a non-integer Retry-After value (e.g., an HTTP-date),
// the 429 is returned to the caller without retrying.
func TestRateLimitTransportNoRetryWithNonIntegerRetryAfter(t *testing.T) {
	ctx := context.Background()

	var requestCount atomic.Int32

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if r.URL.Path == fakeResourceByIDURLPath {
			w.Header().Set("Retry-After", "Wed, 21 Oct 2015 07:28:00 GMT")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"message":"rate limited"}`)
			return
		}

		_, err := io.WriteString(w, "Unknown path")
		if err != nil {
			t.Errorf("Error writing string to the response stream: %v", err)
		}
	}))
	testServer.EnableHTTP2 = true
	testServer.Start()
	defer testServer.Close()

	p := makeTestGenericProvider(ctx, t, testServer, nil)

	readResp, err := p.Read(ctx, &pulumirpc.ReadRequest{
		Id:         "/fake-id",
		Inputs:     nil,
		Properties: nil,
		Urn:        "urn:pulumi:some-stack::some-project::generic:fakeresource/v2:FakeResource::myResource",
	})

	assert.Error(t, err, "Expected an error when Retry-After is an HTTP-date")
	assert.Nil(t, readResp)
	assert.Equal(t, int32(1), requestCount.Load(), "Expected exactly 1 request with no retry")
}
