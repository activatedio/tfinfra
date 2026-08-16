// The generator entry point for the petstore example: the declarative spec
// table over the published pb types. Regenerate with `go generate ./...` or
// `go run .` from this directory.
package main

//go:generate go run .

import (
	"reflect"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	gentf "github.com/activatedio/tfinfra/genlib/tf"
	tf "github.com/activatedio/tfinfra/pkg/tf"
)

// The example service's scope table. Consumers declare their own; tfinfra
// predefines none.
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
