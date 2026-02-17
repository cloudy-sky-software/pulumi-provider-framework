# package `rest`

This package contains the client implementation of an OpenAPI spec. Its purpose is to offload the various
tasks of a provider plugin gRPC server that the Pulumi engine communicates with. Authors of providers
get the benefit of a simple callback-style mechanism for customizing the output of various operations
if they choose to.

## Important files

### `provider.go`

This file contains an implementation of Pulumi's [`UnimplementedResourceProviderServer`](https://github.com/pulumi/pulumi/blob/master/sdk/proto/go/provider_grpc.pb.go#L675) interface.
The implementation is registered as a gRPC server that the Pulumi engine can communicate with. These include operations like `Diff`, `Create`, `Read`, `Update` and `Delete`.

### `request.go`

This file contains methods relevant to creation of an HTTP request that will be executed against
the OpenAPI server. The request bodies of HTTP requests are validated per the OpenAPI spec before
submitting it to the provider's API, which can help catch client-side validation errors quickly.
Validations include concerns such as authentication headers, required params in the path and
the request body.

### `response.go` and `transform.go`

These files contain methods for handling response transformation before delivering the response
to the Pulumi engine which subsequently end up in the Pulumi checkpoint file.

## Adding Tests

The `testdata` folder contains test OpenAPI specs.
For convenience, there is a `generic` provider that is initialized
from the corresponding OpenAPI spec of the same name. The generic
OpenAPI spec contains endpoints for specific scenarios to be tested.
Look at the tests in `provider_test.go` and `request_test.go` files.
