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
	// ProviderTypeName is the provider's type name ("petstore"), used only
	// to spell a reference example in validation messages
	// ("petstore_pet.<name>.pet_id"). A reference gets one when its Target
	// is a resource of this spec; empty leaves every example out.
	ProviderTypeName string
	// ScopeReferences validates the scope identifier attributes of every
	// resource and data source, keyed by attribute name ("store_id"). A
	// scope identifier with no entry is not validated.
	ScopeReferences map[string]Reference
	Entries         []Entry
}

// Reference declares that a string attribute holds the id of another
// resource: the last segment of its name, which the API also calls its id.
// The attribute gains a plan-time validator, so a full resource name pasted
// into it, or another type's id, fails in plan with a message naming the
// attribute to reference instead. An empty value passes: it is an unset
// optional reference.
//
// Nothing accepts both forms. An id is validated as an id, and an expression
// that extracts the last segment of a name still yields one.
type Reference struct {
	// Target is the referenced resource's Terraform type suffix ("store"). Its
	// "<Target>_id" attribute is the value to reference. Required.
	Target string
	// Prefix is what every id of Target starts with, before a hyphen: "st"
	// for "st-01". Empty means the ids carry no prefix ("m50"): the value is
	// then only checked to be an id rather than a resource name, so a full
	// name still fails but another type's id cannot be told apart.
	Prefix string
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
//
// Every resource carries an attribute holding its own id, the last segment of
// "name": "<type_name>_id", a required input on a CallerNamed resource and
// otherwise computed from "name" on create, read and import, or the proto
// field IDField names. Its singular data source and each item of its plural
// one carry it too. It is what other resources' References take.
type Resource struct {
	// Scope is the resource's position in the AIP hierarchy; it contributes
	// one optional, RequiresReplace identifier attribute per parent
	// collection (e.g. "tenant_id").
	//
	// An entity may also carry its parent's id as a proto field of the same
	// name (a lane's "site_id" under sites/{site_id}/lanes/{lane_id}). The
	// field and the identifier are then one attribute: the parent path
	// carries it to the API, never the entity, and reads fill it from the
	// field, so an imported resource has it. It is Optional+Computed to
	// allow that, and may appear in no behavior list but Computed.
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
	// TypeName overrides the derived Terraform type suffix (snake case of
	// the entity name); useful for acronym-heavy names ("ProviderGitHub"
	// derives "provider_git_hub" — override with "provider_github").
	TypeName string
	// UseUpdate selects the full-replace Update operation instead of Patch
	// with an update mask.
	UseUpdate bool
	// CallerNamed declares that the resource's own id comes from the caller
	// rather than the server: the create request carries it in the entity's
	// "name" field, and the server composes the full resource name from the
	// parent and that id.
	//
	// It makes the "<type_name>_id" attribute every resource carries a
	// required, replace-on-change input instead of a computed one; "name"
	// stays computed and keeps its role as the full resource name and the
	// Terraform ID.
	CallerNamed bool
	// IDField names the proto string field that holds the resource's own id,
	// for APIs that take the id from a field of the entity on create
	// ("facility_id" on Facility, whose name is facilities/{facility_id})
	// rather than from "name". That field is the id attribute: no
	// "<type_name>_id" is added, and the id travels to the API through the
	// ordinary proto conversion. Import and data source reads fill it from
	// the last segment of "name", like any id attribute.
	//
	// It is a required, replace-on-change input. Listed in Computed too, it
	// is an optional one instead, for an API that mints an id when the
	// create leaves the field empty: set, it is sent; unset, the minted id
	// is read back and kept. Exclusive with CallerNamed.
	IDField string
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
	// InputOnly lists proto fields the API consumes but never echoes back
	// on a read — creation parameters that describe how to make something
	// rather than what was made, and secrets accepted once and stored
	// hashed.
	//
	// They are Optional but never Computed, and reads leave them untouched:
	// the default Optional+Computed shape would null them on every refresh
	// (the server returns a zero value), which shows up as a permanent diff
	// — or, for an Immutable field, as replacement on every plan.
	//
	// The tradeoff is that an imported resource has no value for them, and
	// a data source always reads them as null.
	InputOnly []string
	// Description is what the resource is, for its schema description and
	// so for its documentation page: a sentence or two a practitioner reads
	// before the attribute list. The singular data source reuses it. Empty
	// falls back to "<Entity> resource."
	Description string
	// Mint creates the resource through a mint verb instead of
	// Create<Entity>: an API that hands over a credential exactly once,
	// beside the row it wrote. Nil means the AIP Create. See Mint.
	Mint *Mint
	// WriteOnly lists proto fields surfaced as write-only arguments
	// (Terraform >= 1.11). PENDING: not yet implemented; declaring one
	// panics at generation time.
	WriteOnly []string
	// JSON lists google.protobuf.Any / Struct fields surfaced as
	// jsontypes.Normalized: Any as its protojson encoding (with "@type"),
	// Struct as a JSON object. Any/Struct fields MUST be listed here.
	JSON []string
	// References maps string fields holding another resource's id, by proto
	// name, to what they reference. See Reference.
	References map[string]Reference
}

// Mint declares a resource the API mints rather than creates: the client
// method Method, func(ctx, *Request, ...) (*Response, error), stands in for
// Create<Entity>, and its response carries the entity beside values
// returned exactly once — a secret, a token.
//
// The request takes the parent in its "parent" field and the entity either
// whole, in a field of the entity's type, or flat: each other request field
// matched to the entity field of the same proto name and Go type. Every
// field the practitioner can set must reach the request by one of those
// routes, so a mint never silently drops one; mark the rest Computed.
//
// Each Once field names a string field of the response. It becomes a
// Sensitive, Computed attribute set from the mint and carried across every
// read, since no read returns it. `terraform import` cannot recover it: an
// imported resource holds it as null.
//
// The resource also gains "keepers", a map whose change replaces the
// resource. It is how a practitioner rotates a credential the API offers no
// rotate verb for: bump a value, and with create_before_destroy the
// successor is minted before the old one is deleted.
//
// Ops need not exclude OpCreate: a minted resource never looks up
// Create<Entity>. A singular DataSource on the same entry is refused, since
// its model would carry the once-only attributes no read has; the plural
// DataSourceList is fine.
type Mint struct {
	// Method is the client method that mints, e.g. "MintClientSecret".
	// Required.
	Method string
	// Once lists the response's string fields returned only by the mint,
	// by proto name; each names its attribute too. Required.
	Once []string
}

// DataSource declares a singular data source (Get by full resource name)
// for the entry. Requires a Resource marker on the same entry.
type DataSource struct{}

// ConfigDataSource declares a typed builder data source for a config
// message that resources receive as google.protobuf.Any. It makes no API
// calls: it exposes the message's fields as typed attributes and computes
// an "any" attribute holding the protojson-encoded Any (with "@type") to
// reference from JSON-marked resource attributes — the type-safe
// alternative to hand-written Any JSON.
//
// Mutually exclusive with Resource on the same entry.
type ConfigDataSource struct {
	// TypeName overrides the derived Terraform type suffix.
	TypeName string
	// Required lists the config fields the practitioner must set.
	Required []string
	// Sensitive lists config fields masked in output (e.g. client secrets).
	Sensitive []string
	// JSON lists message-typed config fields surfaced as
	// jsontypes.Normalized protojson blobs.
	JSON []string
	// Description leads the data source's generated description — what a
	// practitioner must know before using the config (a prerequisite, a
	// caveat) that the message's fields cannot say.
	Description string
	// References maps string config fields holding a resource's id, by
	// proto name, to what they reference. See Reference.
	References map[string]Reference
}

// DataSourceList declares a plural data source: every entity under one
// parent, read through the List RPC with its page tokens followed. The
// parent's scope identifiers are optional attributes that fall back to the
// provider defaults, and the entities arrive as a list of objects carrying
// the singular data source's attributes (input-only fields excepted: the
// API never returns them).
//
// Requires a Resource marker on the same entry, whose client must expose
// List.
type DataSourceList struct {
	// TypeName overrides the derived Terraform type suffix, the snake plural
	// of the entity ("pets", "access_permissions"). It also names the
	// attribute holding the list.
	TypeName string
}

// Associate declares an authoritative association resource for the entry —
// the Terraform surface of the kit Associate{Targets}To{Entity} /
// List{Targets}By{Entity} RPC pair. The generated resource owns the
// entity's FULL association set: members present on the server but absent
// from the configuration are removed on apply.
//
// Requires a Resource marker on the same entry (the RPC pair is validated
// against its ClientType). An entry may declare multiple Associate markers,
// one per edge.
type Associate struct {
	// Target is the associated entity's pb message type, e.g.
	// reflect.TypeFor[petstorev1.Toy](). Required.
	Target reflect.Type
	// Attribute overrides the derived member-set attribute name (the snake
	// plural of the target type, e.g. "toys").
	Attribute string
	// TypeName overrides the derived Terraform type suffix
	// ("<entity>_<attribute>", e.g. "pet_toys").
	TypeName string
}
