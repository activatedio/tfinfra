package tf_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	gentf "github.com/activatedio/tfinfra/genlib/tf"
	tf "github.com/activatedio/tfinfra/pkg/tf"
)

func fieldNamed(t *testing.T, fields []gentf.Field, name string) gentf.Field {
	t.Helper()
	for _, f := range fields {
		if f.ProtoName == name {
			return f
		}
	}
	require.Failf(t, "no such field", "%q", name)
	return gentf.Field{}
}

func TestReferences_Resolve(t *testing.T) {

	ref := gentf.Reference{Target: "pet", Prefix: "p"}

	fields := gentf.NormalizeFields(petEntry(), gentf.Resource{
		JSON:       []string{"config", "metadata"},
		References: map[string]gentf.Reference{"buddy_id": ref},
	})
	assert.Equal(t, &ref, fieldNamed(t, fields, "buddy_id").Reference)
	assert.Nil(t, fieldNamed(t, fields, "display_name").Reference)

	config := gentf.NormalizeConfigFields(
		gentf.Entry{Type: reflect.TypeFor[petstorev1.CollarConfig]()},
		gentf.ConfigDataSource{JSON: []string{"buckle"}, References: map[string]gentf.Reference{"color": ref}},
	)
	assert.Equal(t, &ref, fieldNamed(t, config, "color").Reference, "a config data source takes references too")
}

func petSpec(res gentf.Resource, scopeRefs map[string]gentf.Reference) *gentf.Spec {
	res.Scope = tf.NewScope("stores")
	res.ClientType = reflect.TypeFor[petstorev1.PetStoreServiceClient]()
	res.JSON = []string{"config", "metadata"}
	return &gentf.Spec{
		Package:         "generated",
		ScopeReferences: scopeRefs,
		Entries:         []gentf.Entry{{Type: reflect.TypeFor[petstorev1.Pet](), Implementations: []any{res}}},
	}
}

// A resource's own id attribute must not shadow another attribute of the
// same name; generation refuses it rather than emit a schema with one.
func TestIDAttribute_Collision(t *testing.T) {

	assert.PanicsWithValue(t,
		`Pet: its id attribute "buddy_id" collides with a field of the same name; set TypeName to rename the resource`,
		func() {
			gentf.NewRegistry().RunDirectoryPathHandler(t.TempDir(), petSpec(gentf.Resource{TypeName: "buddy"}, nil))
		})

	assert.PanicsWithValue(t,
		`Pet: its id attribute "store_id" collides with a parent identifier of the same name; set TypeName to rename the resource`,
		func() {
			gentf.NewRegistry().RunDirectoryPathHandler(t.TempDir(), petSpec(gentf.Resource{TypeName: "store"}, nil))
		})

	assert.NotPanics(t, func() {
		gentf.NewRegistry().RunDirectoryPathHandler(t.TempDir(), petSpec(gentf.Resource{}, nil))
	})
}

func TestScopeReferences_Validated(t *testing.T) {
	assert.PanicsWithValue(t, `ScopeReferences.store_id: a Reference needs both Target and Prefix`, func() {
		gentf.NewRegistry().RunDirectoryPathHandler(t.TempDir(),
			petSpec(gentf.Resource{}, map[string]gentf.Reference{"store_id": {Prefix: "s"}}))
	})
}
