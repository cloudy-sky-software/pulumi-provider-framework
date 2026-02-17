[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/cloudy-sky-software/pulumi-provider-framework)

# Pulumi Provider Framework

A framework for building native Pulumi providers from OpenAPI. This library handles the duties of a Pulumi resource provider server and exposing a simple callback-like mechanism for providers wanting to control the outcome of CRUD operations on resources.

[`pulschema`](https://github.com/cloudy-sky-software/pulschema) is the first part to successfully convert an OpenAPI spec to a Pulumi schema. This library is the second part required to fully implement a native Pulumi provider purely based on OpenAPI specs.

## Development

This repo uses `make` build system.

- Run tests with `make test`.
- Tidy/restore Go deps with `make ensure`.
- Run a lint step with `make lint`.
