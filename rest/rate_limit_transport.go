package rest

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/logging"
)

// rateLimitTransport is an http.RoundTripper that handles HTTP 429 Too Many
// Requests responses by respecting the Retry-After header. If the header value
// is a non-negative decimal integer, the request is retried after that many
// seconds. If the header is absent or not a valid non-negative integer, the
// 429 response is returned to the caller as-is.
type rateLimitTransport struct {
	wrapped http.RoundTripper
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for {
		resp, err := t.wrapped.RoundTrip(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		// Read the 429 response body so we can restore it if we cannot retry.
		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		retryAfterStr := strings.TrimSpace(resp.Header.Get("Retry-After"))
		delay, parseErr := strconv.Atoi(retryAfterStr)
		if parseErr != nil || delay < 0 {
			// Retry-After header is absent, not a valid integer, or negative.
			// Restore the body and return the 429 response to the caller.
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
			if readErr != nil {
				return nil, errors.Wrap(readErr, "reading 429 response body")
			}
			return resp, nil
		}

		logging.V(3).Infof("Received 429 response. Retrying after %d second(s)...", delay)

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(time.Duration(delay) * time.Second):
		}

		// Reset the request body for the retry if possible.
		if req.GetBody != nil {
			req.Body, err = req.GetBody()
			if err != nil {
				return nil, errors.Wrap(err, "resetting request body for retry")
			}
		}
	}
}
