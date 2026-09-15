package petstore_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

// Input-only attributes are the ones the API consumes and never echoes
// back — the shelter intake parameters here, a certificate's mint
// parameters in a real provider. The hazard they close is the refresh that
// reads the server's zero value back over what the practitioner configured.
func TestPetResource_InputOnly(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	m.IntakeCode = types.StringValue("SHELTER-42")
	m.IntakeAgeDays = types.Int64Value(90)

	created := h.create(t, m)

	// They reached the server...
	assert.Equal(t, "SHELTER-42", h.fake.lastIntakeCode)
	assert.Equal(t, int32(90), h.fake.lastIntakeAgeDays)
	// ...which stored none of them, as an input-only API does not.
	stored := h.fake.pets[created.Name.ValueString()]
	assert.Empty(t, stored.GetIntakeCode())
	assert.Zero(t, stored.GetIntakeAgeDays())

	// State still holds what was configured: FromProto never touched it.
	assert.Equal(t, "SHELTER-42", created.IntakeCode.ValueString())
	assert.Equal(t, int64(90), created.IntakeAgeDays.ValueInt64())

	// The refresh is the real test. Under the default Optional+Computed
	// shape this read would null both attributes, which shows up as a
	// permanent diff — and, for immutable intake_code, as replacement on
	// every plan.
	readResp := &resource.ReadResponse{State: h.state(t, created)}
	h.res.Read(ctx, resource.ReadRequest{State: h.state(t, created)}, readResp)
	require.False(t, readResp.Diagnostics.HasError(), readResp.Diagnostics)

	readBack := &generated.PetModel{}
	require.False(t, readResp.State.Get(ctx, readBack).HasError())
	assert.Equal(t, "SHELTER-42", readBack.IntakeCode.ValueString(),
		"an input-only attribute must survive a refresh the server answers with a zero value")
	assert.Equal(t, int64(90), readBack.IntakeAgeDays.ValueInt64())
}

func TestPetResource_InputOnlySchema(t *testing.T) {

	s := generated.PetResourceSchema()

	for _, attr := range []string{"intake_code", "intake_age_days"} {
		a, ok := s.Attributes[attr]
		require.True(t, ok, attr)
		assert.True(t, a.IsOptional(), attr)
		// Never computed: an attribute the server never answers would stay
		// unknown after apply, which Terraform rejects outright.
		assert.False(t, a.IsComputed(), attr)
	}
}

// An unset input-only attribute stays null rather than picking up the
// proto zero value on the way back.
func TestPetResource_InputOnlyUnset(t *testing.T) {

	h := newHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Mimi")

	created := h.create(t, m)

	assert.True(t, created.IntakeCode.IsNull())
	assert.True(t, created.IntakeAgeDays.IsNull())
}
