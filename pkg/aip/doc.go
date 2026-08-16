// Package aip is the framework-free AIP vocabulary shared by tfinfra's
// Terraform runtime and non-Terraform consumers (cmdinfra): resource
// hierarchy scopes (parent/name composition and parsing) and gRPC status
// translation. It must never import the Terraform plugin framework — CLI
// binaries link this package.
package aip
