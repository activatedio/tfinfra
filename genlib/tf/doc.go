// Package tf is the generation half of tfinfra: it turns a declarative spec
// over published protobuf Go types into Terraform Plugin Framework provider
// code. Field metadata is read from the compiled pb types via protoreflect
// at generation time — no protoc plugin and no proto files are involved.
//
// Code in this package runs at build time and panics on error (via
// gen.Check or panic), matching the datainfra genlib convention.
package tf
