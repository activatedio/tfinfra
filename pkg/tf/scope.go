package tf

import (
	"github.com/activatedio/tfinfra/pkg/aip"
)

// Scope is an alias of aip.Scope: the AIP hierarchy vocabulary lives in the
// framework-free pkg/aip so non-Terraform consumers (cmdinfra) can share
// the semantics without linking the Terraform plugin framework.
type Scope = aip.Scope

// NewScope creates a Scope from ordered parent collection names, outermost
// first, e.g. NewScope("tenants", "issuers").
func NewScope(collections ...string) Scope {
	return aip.NewScope(collections...)
}

// ScopeNone is the scope of a top-level resource with no parent.
var ScopeNone = aip.ScopeNone

// IsNotFound reports whether the error is a gRPC NotFound status. The Crud
// runtime uses it to translate out-of-band deletion into state removal on
// Read and into success on Delete.
func IsNotFound(err error) bool {
	return aip.IsNotFound(err)
}
