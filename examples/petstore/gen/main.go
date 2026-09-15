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
						// intake_code covers input-only plus immutable (a
						// create-only parameter), intake_age_days covers
						// input-only on its own.
						Immutable: []string{"type", "intake_code"},
						Computed:  []string{"create_time"},
						InputOnly: []string{"intake_code", "intake_age_days"},
						JSON:      []string{"config", "metadata"},
					},
					gentf.DataSource{},
					gentf.Associate{Target: reflect.TypeFor[petstorev1.Toy]()},
				},
			},
			{
				// Toy is caller-named: the practitioner supplies "toy_id"
				// and the server composes the name from it.
				Type: reflect.TypeFor[petstorev1.Toy](),
				Implementations: []any{
					gentf.Resource{
						Scope:       scopeStore,
						ClientType:  reflect.TypeFor[petstorev1.PetStoreServiceClient](),
						Client:      "petstore",
						CallerNamed: true,
						Required:    []string{"display_name"},
					},
					gentf.DataSource{},
				},
			},
			{
				Type: reflect.TypeFor[petstorev1.CollarConfig](),
				Implementations: []any{
					gentf.ConfigDataSource{Required: []string{"color"}, JSON: []string{"buckle"}},
				},
			},
		},
	})
}
