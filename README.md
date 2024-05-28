# Pulumi Provider Framework

A framework for building native Pulumi providers from OpenAPI. This library handles the duties of a Pulumi resource provider server and exposing a simple callback-like mechanism for providers wanting to control the outcome of CRUD operations on resources.

`pulschema` is the first part to successfully convert an OpenAPI spec to a Pulumi schema. This is the second part required to fully implement a native Pulumi provider purely based on OpenAPI specs.
