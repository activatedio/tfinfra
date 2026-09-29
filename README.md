# tfinfra

Code generation for Terraform providers over AIP-shaped gRPC APIs, in the
[datainfra](https://github.com/activatedio/datainfra) mold.

Declare your API surface as a Go table over the **published protobuf Go
types**; tfinfra reads field metadata via **protoreflect at generation
time** — no protoc plugin, no proto files, no buf — and emits Terraform
Plugin Framework schemas, plan/state models, and proto conversions, backed
by a thin runtime layer for AIP name composition.

```go
package main

//go:generate go run .

import (
	"reflect"

	gentf "github.com/activatedio/tfinfra/genlib/tf"
	tf "github.com/activatedio/tfinfra/pkg/tf"

	petstorev1 "example.com/petstore/gen/petstore/v1"
)

var scopeStore = tf.NewScope("stores")

func main() {
	gentf.NewRegistry().RunDirectoryPathHandler("../generated", &gentf.Spec{
		Package: "generated",
		Entries: []gentf.Entry{
			{
				Type: reflect.TypeFor[petstorev1.Pet](),
				Implementations: []any{
					gentf.Resource{
						Scope:      scopeStore,
						ClientType: reflect.TypeFor[petstorev1.PetStoreServiceClient](),
						Client:     "petstore",
						Required:   []string{"display_name"},
						Immutable:  []string{"type"},
						Computed:   []string{"create_time"},
					},
					gentf.DataSource{},
				},
			},
		},
	})
}
```

The behavioral layer (required / immutable / computed / sensitive /
input-only) lives in the spec because AIP protos without
`google.api.field_behavior` cannot express it; everything structural comes
from the descriptors.

A singular message field becomes a typed nested attribute unless you list it
in `JSON`, in which case it stays a protojson blob:

```hcl
resource "petstore_pet" "rex" {
  display_name = "Rex"

  feeding = {
    schedule = "twice daily"
    portions = 2
    foods    = ["kibble", "chicken"]
  }
}
```

`InputOnly` marks fields the API consumes but never returns — a
certificate's mint parameters, say. They stay Optional rather than
Optional+Computed, and reads leave them alone, so the value you configured
does not vanish on the next refresh.

Every resource carries a `<type_name>_id` attribute, its own id: the last
segment of `name`. The server assigns it, so it is computed, and it is what
another resource references rather than splitting `name`. The singular data
source and each item of the plural one carry it too.

A field or parent attribute that holds another resource's id declares a
`Reference` (on `Resource.References`, `ConfigDataSource.References`, or, for
the parent attributes, `Spec.ScopeReferences`), naming the target and the
prefix its ids start with. A full name or another type's id then fails in
plan, with a message naming the attribute to reference:

```go
gentf.Resource{
	// ...
	References: map[string]gentf.Reference{"buddy_id": {Target: "pet", Prefix: "p"}},
}
```

```hcl
resource "petstore_pet" "tom" {
  display_name = "Tom"
  buddy_id     = petstore_pet.rex.pet_id   # not petstore_pet.rex.name
}
```

Resources whose id the caller chooses rather than the server — the API takes
it from the entity's `name` field on create — declare `CallerNamed: true`,
which makes `<type_name>_id` a required, replace-on-change input:

```hcl
resource "petstore_toy" "bone" {
  toy_id       = "squeaky-bone"     # name = stores/s1/toys/squeaky-bone
  display_name = "Squeaky bone"
}
```

A credential the API mints rather than creates — the secret comes back once,
in the mint response, and no read returns it — declares
`Mint: &tf.Mint{Method: "MintAccessKey", Once: []string{"key"}}`. The key
is a sensitive attribute kept in state across refreshes, and `keepers`
replaces the resource when any value in it changes:

```hcl
resource "petstore_access_key" "ci" {
  display_name = "ci"
  keepers      = { rotation = "2026-09" }

  lifecycle {
    create_before_destroy = true
  }
}
```

See `examples/petstore/` for the end-to-end example (its `generated/`
directory is the golden output contract) and `CLAUDE.md` for architecture,
supported field shapes, and status.
