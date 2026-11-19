package petstore_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
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

// Each clearable shape: a plain bool defaults to false, so a removed line
// plans false; a map, a JSON document and a nested message are Optional
// alone, so a removed one plans null. None keeps its prior value.
func TestPetResource_ClearableShapes(t *testing.T) {

	s := generated.PetResourceSchema()
	require.False(t, s.ValidateImplementation(context.Background()).HasError())

	v, ok := s.Attributes["vaccinated"].(schema.BoolAttribute)
	require.True(t, ok)
	assert.True(t, v.IsOptional())
	assert.True(t, v.IsComputed(), "a bool reads back false, never null")
	require.NotNil(t, v.Default, "removed, it plans its default")
	dr := &defaults.BoolResponse{}
	v.Default.DefaultBool(context.Background(), defaults.BoolRequest{}, dr)
	assert.False(t, dr.PlanValue.ValueBool())
	assert.Empty(t, v.PlanModifiers)

	for name, a := range map[string]schema.Attribute{
		"labels":   s.Attributes["labels"],
		"metadata": s.Attributes["metadata"],
		"feeding":  s.Attributes["feeding"],
	} {
		assert.True(t, a.IsOptional(), name)
		assert.False(t, a.IsComputed(), name)
	}
}

// Removing every clearable value at once sends each in the mask with its
// zero value; the server clears them and the read agrees with the plan.
func TestPetResource_ClearableShapesUpdate(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	feeding, d := types.ObjectValueFrom(ctx, generated.PetFeedingAttrTypes(), generated.PetFeedingModel{
		Schedule: types.StringValue("twice daily"),
		Portions: types.Int64Value(2),
		Foods:    types.ListNull(types.StringType),
		Notes:    types.MapNull(types.StringType),
		Bowl:     types.StringNull(),
		Interval: types.StringNull(),
	})
	require.False(t, d.HasError(), d)
	labels, d := types.MapValueFrom(ctx, types.StringType, map[string]string{"color": "brown"})
	require.False(t, d.HasError(), d)

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	m.Vaccinated = types.BoolValue(true)
	m.Labels = labels
	m.Metadata = jsontypes.NewNormalizedValue(`{"chip":"123"}`)
	m.Feeding = feeding
	created := h.create(t, m)
	require.True(t, created.Vaccinated.ValueBool())

	planModel := *created
	planModel.Vaccinated = types.BoolValue(false)
	planModel.Labels = types.MapNull(types.StringType)
	planModel.Metadata = jsontypes.NewNormalizedNull()
	planModel.Feeding = types.ObjectNull(generated.PetFeedingAttrTypes())
	updResp := &resource.UpdateResponse{State: h.emptyState()}
	h.res.Update(ctx, resource.UpdateRequest{
		Plan:  h.plan(t, &planModel),
		State: h.state(t, created),
	}, updResp)
	require.False(t, updResp.Diagnostics.HasError(), updResp.Diagnostics)

	assert.ElementsMatch(t, []string{"vaccinated", "labels", "metadata", "feeding"}, h.fake.lastPatchPaths)
	row := h.fake.pets[created.Name.ValueString()]
	assert.False(t, row.GetVaccinated())
	assert.Empty(t, row.GetLabels())
	assert.Nil(t, row.GetMetadata())
	assert.Nil(t, row.GetFeeding())

	updated := &generated.PetModel{}
	require.False(t, updResp.State.Get(ctx, updated).HasError())
	assert.False(t, updated.Vaccinated.ValueBool())
	assert.False(t, updated.Vaccinated.IsNull())
	assert.True(t, updated.Labels.IsNull())
	assert.True(t, updated.Metadata.IsNull())
	assert.True(t, updated.Feeding.IsNull())
}
