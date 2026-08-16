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

The behavioral layer (required / immutable / computed / sensitive) lives in
the spec because AIP protos without `google.api.field_behavior` cannot
express it; everything structural comes from the descriptors.

See `examples/petstore/` for the end-to-end example (its `generated/`
directory is the golden output contract) and `CLAUDE.md` for architecture,
supported field shapes, and status.
