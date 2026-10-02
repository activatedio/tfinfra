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

// petstore is the provider's type name and the ProviderData.Clients key.
const petstore = "petstore"

// The example service's scope table. Consumers declare their own; tfinfra
// predefines none.
var (
	scopeStore   = tf.NewScope("stores")
	scopeShelter = tf.NewScope("shelters")
)

func main() {

	gentf.NewRegistry().RunDirectoryPathHandler("../generated", &gentf.Spec{
		Package:          "generated",
		ProviderTypeName: petstore,
		// Every store id starts "s-", so a store_id is validated as one.
		ScopeReferences: map[string]gentf.Reference{
			"store_id": {Target: "store", Prefix: "s"},
			// Shelter ids carry no prefix: only a full name is refused.
			"shelter_id": {Target: "shelter"},
		},
		Entries: []gentf.Entry{
			{
				Type: reflect.TypeFor[petstorev1.Pet](),
				Implementations: []any{
					gentf.Resource{
						Scope:       scopeStore,
						ClientType:  reflect.TypeFor[petstorev1.PetStoreServiceClient](),
						Client:      petstore,
						Required:    []string{"display_name"},
						Description: "An animal in a store's care, from intake to adoption.",
						// intake_code covers input-only plus immutable (a
						// create-only parameter), intake_age_days covers
						// input-only on its own.
						Immutable: []string{"type", "intake_code"},
						Computed:  []string{"create_time"},
						// update_time changes on every write.
						Volatile:  []string{"update_time"},
						InputOnly: []string{"intake_code", "intake_age_days"},
						// notes is a repeated message on the JSON lane;
						// vaccinations, left out, is a list-nested attribute.
						JSON: []string{"config", "metadata", "notes"},
						// buddy_id takes another pet's pet_id.
						References: map[string]gentf.Reference{
							"buddy_id": {Target: "pet", Prefix: "p"},
						},
					},
					gentf.DataSource{},
					gentf.DataSourceList{},
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
						Client:      petstore,
						CallerNamed: true,
						Required:    []string{"display_name"},
					},
					gentf.DataSource{},
					gentf.DataSourceList{},
				},
			},
			{
				// AccessKey is minted: MintAccessKey stands in for the Create the
				// API does not have, and returns the key exactly once.
				Type: reflect.TypeFor[petstorev1.AccessKey](),
				Implementations: []any{
					gentf.Resource{
						Scope:       scopeStore,
						Ops:         gentf.OpGet | gentf.OpList | gentf.OpPatch | gentf.OpDelete,
						ClientType:  reflect.TypeFor[petstorev1.PetStoreServiceClient](),
						Client:      petstore,
						Required:    []string{"display_name"},
						Computed:    []string{"create_time"},
						Description: "A store's key for calling the API.",
						Mint:        &gentf.Mint{Method: "MintAccessKey", Once: []string{"key"}},
					},
					gentf.DataSourceList{},
				},
			},
			{
				// Shelter's id is its own shelter_id field, which the create
				// request carries in the entity; the API has no Patch.
				Type: reflect.TypeFor[petstorev1.Shelter](),
				Implementations: []any{
					gentf.Resource{
						Scope:      tf.ScopeNone,
						Ops:        gentf.OpGet | gentf.OpList | gentf.OpCreate | gentf.OpUpdate | gentf.OpDelete,
						ClientType: reflect.TypeFor[petstorev1.PetStoreServiceClient](),
						Client:     petstore,
						UseUpdate:  true,
						IDField:    "shelter_id",
						Required:   []string{"display_name"},
					},
					gentf.DataSource{},
					gentf.DataSourceList{},
				},
			},
			{
				// Run is a shelter's child. Its run_id is optional: the server
				// mints one when the create leaves it empty. Its shelter_id is
				// the parent identifier, which the entity carries too.
				Type: reflect.TypeFor[petstorev1.Run](),
				Implementations: []any{
					gentf.Resource{
						Scope:      scopeShelter,
						Ops:        gentf.OpGet | gentf.OpList | gentf.OpCreate | gentf.OpUpdate | gentf.OpDelete,
						ClientType: reflect.TypeFor[petstorev1.PetStoreServiceClient](),
						Client:     petstore,
						UseUpdate:  true,
						IDField:    "run_id",
						Computed:   []string{"run_id", "shelter_id"},
					},
					gentf.DataSource{},
					gentf.DataSourceList{},
				},
			},
			{
				Type: reflect.TypeFor[petstorev1.CollarConfig](),
				Implementations: []any{
					gentf.ConfigDataSource{
						Required: []string{"color"},
						JSON:     []string{"buckle"},
						References: map[string]gentf.Reference{
							"tag_key_id": {Target: "access_key", Prefix: "k"},
						},
						Description: "A collar is only worn by a pet with a feeding schedule.",
					},
				},
			},
		},
	})
}
