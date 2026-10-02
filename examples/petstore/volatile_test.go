package petstore_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

// A volatile field carries no UseStateForUnknown: an update plans it unknown
// rather than promising the prior value the server is about to replace.
func TestPetResource_VolatileSchema(t *testing.T) {

	s := generated.PetResourceSchema()

	type c struct {
		attribute string
		assert    func(t *testing.T, a schema.StringAttribute)
	}

	cases := map[string]c{
		"a volatile field plans unknown on update": {
			attribute: "update_time",
			assert: func(t *testing.T, a schema.StringAttribute) {
				assert.True(t, a.Computed)
				assert.False(t, a.Optional)
				assert.Empty(t, a.PlanModifiers)
			},
		},
		"an ordinary computed field keeps its state": {
			attribute: "create_time",
			assert: func(t *testing.T, a schema.StringAttribute) {
				assert.True(t, a.Computed)
				assert.Len(t, a.PlanModifiers, 1)
			},
		},
	}

	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			a, ok := s.Attributes[v.attribute].(schema.StringAttribute)
			require.True(t, ok)
			v.assert(t, a)
		})
	}
}

func TestPetResource_VolatileChangesOnEveryWrite(t *testing.T) {

	r := newPetRig(t)

	created := r.mustCreate(t, newPet("Rex"))
	require.False(t, created.UpdateTime.IsNull())

	planned := *created
	planned.DisplayName = types.StringValue("Rex II")
	planned.UpdateTime = types.StringUnknown()
	updated := r.update(t, created, &planned)

	assert.NotEqual(t, created.UpdateTime.ValueString(), updated.UpdateTime.ValueString())
	assert.Equal(t, updated.UpdateTime.ValueString(), r.read(t, updated).UpdateTime.ValueString())
}
