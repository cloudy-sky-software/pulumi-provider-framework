# package `callback`

This package contains the callback interface that providers can override.
Providers should embed the `UnimplementedProviderCallback` struct to ensure
that the default implementation of callback methods are always available
on the provider.

**Important**:

- The only callback method that providers are _required_ to implement is
`GetAuthorizationHeader`.
- While not strictly necessary, you might also want to implement
`OnConfigure`.
