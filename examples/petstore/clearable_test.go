package petstore_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

// A clearable attribute is an optional input the server stores as sent and
// never fills itself. Under the default Optional+Computed shape, removing
// it from the configuration kept the prior value (UseStateForUnknown), so
// it could never be cleared in place. Optional alone, it plans null.
func TestPetResource_ClearableSchema(t *testing.T) {

	a, ok := generated.PetResourceSchema().Attributes["buddy_id"].(schema.StringAttribute)
	require.True(t, ok)
	assert.True(t, a.IsOptional())
	assert.False(t, a.IsComputed(), "a computed attribute keeps its state value when the configuration drops it")
	assert.Empty(t, a.PlanModifiers, "UseStateForUnknown would carry the prior value into the plan")
}

// Removing the value sends the field in the mask with its zero value, the
// server clears it, and the read gives null back.
func TestPetResource_ClearableUpdate(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	m.BuddyId = types.StringValue("p-01")
	created := h.create(t, m)
	require.Equal(t, "p-01", created.BuddyId.ValueString())

	planModel := *created
	planModel.BuddyId = types.StringNull()
	updResp := &resource.UpdateResponse{State: h.emptyState()}
	h.res.Update(ctx, resource.UpdateRequest{
		Plan:  h.plan(t, &planModel),
		State: h.state(t, created),
	}, updResp)
	require.False(t, updResp.Diagnostics.HasError(), updResp.Diagnostics)

	assert.Equal(t, []string{"buddy_id"}, h.fake.lastPatchPaths)
	assert.Empty(t, h.fake.pets[created.Name.ValueString()].GetBuddyId(), "the server cleared it")

	updated := &generated.PetModel{}
	require.False(t, updResp.State.Get(ctx, updated).HasError())
	assert.True(t, updated.BuddyId.IsNull(), "the read gives null back, as the plan said")
}
