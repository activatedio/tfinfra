package petstore_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

// feedingObject builds the "feeding" attribute value the way a practitioner's
// configuration would arrive.
func feedingObject(t *testing.T, schedule string, portions int64, foods []string, notes map[string]string) types.Object {

	t.Helper()
	ctx := context.Background()

	list, diags := types.ListValueFrom(ctx, types.StringType, foods)
	require.False(t, diags.HasError(), diags)
	m, diags := types.MapValueFrom(ctx, types.StringType, notes)
	require.False(t, diags.HasError(), diags)

	obj, diags := types.ObjectValueFrom(ctx, generated.PetFeedingAttrTypes(), generated.PetFeedingModel{
		Schedule: types.StringValue(schedule),
		Portions: types.Int64Value(portions),
		Foods:    list,
		Notes:    m,
	})
	require.False(t, diags.HasError(), diags)

	return obj
}

// A singular message left out of the JSON list becomes a typed nested
// attribute: real attributes with real types, not a protojson blob.
func TestPetResource_NestedAttribute(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	m.Feeding = feedingObject(t, "twice daily", 2, []string{"kibble", "chicken"}, map[string]string{"vet": "no grains"})

	created := h.create(t, m)

	// It reached the server as a nested message.
	stored := h.fake.pets[created.GetName().ValueString()].GetFeeding()
	require.NotNil(t, stored)
	assert.Equal(t, "twice daily", stored.GetSchedule())
	assert.Equal(t, int32(2), stored.GetPortions())
	assert.Equal(t, []string{"kibble", "chicken"}, stored.GetFoods())
	assert.Equal(t, map[string]string{"vet": "no grains"}, stored.GetNotes())

	// And came back through state as an object.
	require.False(t, created.Feeding.IsNull())
	var back generated.PetFeedingModel
	require.False(t, created.Feeding.As(ctx, &back, basetypes.ObjectAsOptions{}).HasError())
	assert.Equal(t, "twice daily", back.Schedule.ValueString())
	assert.Equal(t, int64(2), back.Portions.ValueInt64())
	assert.False(t, back.Foods.IsNull())
	assert.False(t, back.Notes.IsNull())
}

func TestPetResource_NestedAttributeSchema(t *testing.T) {

	s := generated.PetResourceSchema()

	diags := s.ValidateImplementation(context.Background())
	require.False(t, diags.HasError(), diags)

	a, ok := s.Attributes["feeding"]
	require.True(t, ok)

	nested, ok := a.(schema.SingleNestedAttribute)
	require.True(t, ok, "feeding should be a SingleNestedAttribute, got %T", a)
	assert.True(t, nested.IsOptional())
	assert.True(t, nested.IsComputed())

	// The nested object carries the message's own fields as real typed
	// attributes, each Optional+Computed and none carrying a plan modifier.
	for _, name := range []string{"schedule", "portions", "foods", "notes"} {
		child, ok := nested.Attributes[name]
		require.True(t, ok, name)
		assert.True(t, child.IsOptional(), name)
		assert.True(t, child.IsComputed(), name)
	}
}

// A nil message reads as a typed object null — not an empty object, which
// would show as a diff against an unset configuration.
func TestPetModel_NestedNullRoundTrip(t *testing.T) {

	ctx := context.Background()

	m := &generated.PetModel{}
	diags := m.FromProto(ctx, &petstorev1.Pet{Name: "stores/s1/pets/p1", DisplayName: "Mimi"})
	require.False(t, diags.HasError(), diags)

	require.True(t, m.Feeding.IsNull())
	assert.Equal(t, generated.PetFeedingAttrTypes(), m.Feeding.AttributeTypes(ctx))

	back, diags := m.ToProto(ctx)
	require.False(t, diags.HasError(), diags)
	assert.Nil(t, back.GetFeeding())
}

// A changed nested attribute lands in the patch mask as one path: the
// message is replaced whole, which is what the API's update mask means.
func TestPetModel_NestedUpdateMask(t *testing.T) {

	ctx := context.Background()

	prior := generated.NewPetModel()
	prior.DisplayName = types.StringValue("Rex")
	prior.Feeding = feedingObject(t, "twice daily", 2, []string{"kibble"}, nil)

	next := generated.NewPetModel()
	next.DisplayName = types.StringValue("Rex")
	next.Feeding = feedingObject(t, "once daily", 1, []string{"kibble"}, nil)

	assert.Equal(t, []string{"feeding"}, next.UpdateMask(ctx, prior))

	same := generated.NewPetModel()
	same.DisplayName = types.StringValue("Rex")
	same.Feeding = feedingObject(t, "twice daily", 2, []string{"kibble"}, nil)

	assert.Empty(t, same.UpdateMask(ctx, prior))
}

// The config data source lane gets nested attributes too: CollarConfig
// carries buckle on the protojson lane and engraving as a typed nested
// attribute, which is how one message can use both.
func TestCollarConfig_NestedAttribute(t *testing.T) {

	ctx := context.Background()

	lines, diags := types.ListValueFrom(ctx, types.StringType, []string{"Rex", "call 555-0100"})
	require.False(t, diags.HasError(), diags)

	engraving, diags := types.ObjectValueFrom(ctx, generated.CollarConfigEngravingAttrTypes(), generated.CollarConfigEngravingModel{
		Text:  types.StringValue("Rex"),
		Font:  types.StringValue("serif"),
		Lines: lines,
	})
	require.False(t, diags.HasError(), diags)

	m := generated.NewCollarConfigModel()
	m.Color = types.StringValue("red")
	m.Engraving = engraving

	out, diags := m.ToProto(ctx)
	require.False(t, diags.HasError(), diags)

	require.NotNil(t, out.GetEngraving())
	assert.Equal(t, "Rex", out.GetEngraving().GetText())
	assert.Equal(t, "serif", out.GetEngraving().GetFont())
	assert.Equal(t, []string{"Rex", "call 555-0100"}, out.GetEngraving().GetLines())
}
