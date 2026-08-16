package tf

import (
	"reflect"

	runtimetf "github.com/activatedio/tfinfra/pkg/tf"
)

// Spec is the root input to the generator: the package to emit plus one
// Entry per API resource. It is the entry value passed to
// Registry.RunDirectoryPathHandler.
type Spec struct {
	// Package is the Go package name of the generated files.
	Package string
	Entries []Entry
}

// Entry describes one API resource: the published pb message type plus
// implementation markers declaring what to generate for it.
type Entry struct {
	// Type is the pb message struct type, e.g.
	// reflect.TypeFor[petstorev1.Pet]().
	Type reflect.Type
	// Implementations are the markers (Resource, DataSource, ...) selecting
	// and configuring the handlers that fire for this entry.
	Implementations []any
}

// GetImplementation returns the first implementation of type I declared on
// the entry, if any.
func GetImplementation[I any](e Entry) (I, bool) {
	for _, impl := range e.Implementations {
		if v, ok := impl.(I); ok {
			return v, true
		}
	}
	var zero I
	return zero, false
}

// HasImplementation reports whether the entry declares an implementation of
// type I.
func HasImplementation[I any](e Entry) bool {
	_, ok := GetImplementation[I](e)
	return ok
}

// Resource declares a Terraform managed resource for the entry.
//
// The structural schema (field names, types, cardinality) comes from the pb
// type via protoreflect. Resource carries the behavioral layer that protos
// do not: which fields are required, immutable, server-computed, or
// sensitive. Field names are proto field names (snake_case); referencing an
// unknown field panics at generation time.
type Resource struct {
	// Scope is the resource's position in the AIP hierarchy; it contributes
	// one optional, RequiresReplace identifier attribute per parent
	// collection (e.g. "tenant_id").
	Scope runtimetf.Scope
	// Ops selects which operations the API exposes; the zero value means
	// all (OpAll).
	Ops Ops
	// ClientType is the gRPC client interface carrying this resource's
	// operations, e.g. reflect.TypeFor[petstorev1.PetStoreServiceClient]().
	// Required.
	ClientType reflect.Type
	// Client is the ProviderData.Clients key the generated Configure reads
	// the client from. Defaults to "default".
	Client string
	// Plural overrides the derived plural used in List method names
	// ("List" + plural).
	Plural string
	// Collection overrides the derived AIP collection name (lower-camel
	// plural of the entity name, e.g. "appearanceProfiles").
	Collection string
	// UseUpdate selects the full-replace Update operation instead of Patch
	// with an update mask.
	UseUpdate bool
	// Required lists proto fields the practitioner must set.
	Required []string
	// Immutable lists proto fields that force replacement when changed
	// (RequiresReplace plan modifier).
	Immutable []string
	// Computed lists proto fields set by the server ("name" is always
	// computed and need not be listed).
	Computed []string
	// Sensitive lists proto fields masked in CLI output and state listings.
	Sensitive []string
	// WriteOnly lists proto fields surfaced as write-only arguments
	// (Terraform >= 1.11). PENDING: not yet implemented; declaring one
	// panics at generation time.
	WriteOnly []string
	// JSON lists google.protobuf.Struct / Any fields surfaced as
	// jsontypes.Normalized. PENDING: not yet implemented; declaring one
	// panics at generation time.
	JSON []string
}

// DataSource declares a singular data source (Get by name) for the entry.
// PENDING: recognized in the spec but not yet generated.
type DataSource struct{}

// DataSourceList declares a plural data source (List under a parent) for
// the entry. PENDING: recognized in the spec but not yet generated.
type DataSourceList struct{}
