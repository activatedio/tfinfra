package petstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

// feedingWith builds a feeding object carrying only a bowl and an interval.
func feedingWith(t *testing.T, bowl, interval types.String) types.Object {

	t.Helper()

	obj, diags := types.ObjectValueFrom(context.Background(), generated.PetFeedingAttrTypes(), generated.PetFeedingModel{
		Schedule: types.StringValue("daily"),
		Portions: types.Int64Value(1),
		Foods:    types.ListNull(types.StringType),
		Notes:    types.MapNull(types.StringType),
		Bowl:     bowl,
		Interval: interval,
	})
	require.False(t, diags.HasError(), diags)

	return obj
}

func feedingOf(t *testing.T, m *generated.PetModel) generated.PetFeedingModel {
	t.Helper()
	var n generated.PetFeedingModel
	require.False(t, m.Feeding.As(context.Background(), &n, basetypes.ObjectAsOptions{}).HasError())
	return n
}

// An enum's zero value is indistinguishable from unset on the wire, so it
// reads back as null — unless it was written. Then it must read back as
// written: BOWL_STANDARD is a real choice, like a REST transport, and
// nulling it would be an inconsistent result after apply. Children of a
// nested attribute get the same treatment, and so does a nested duration's
// spelling.
func TestPetResource_ZeroEnumWrittenExplicitly(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	m.Type = types.StringValue("PET_TYPE_UNSPECIFIED")
	m.Feeding = feedingWith(t, types.StringValue("BOWL_STANDARD"), types.StringValue("1m30s"))

	created := h.create(t, m)

	stored := h.fake.pets[created.GetName().ValueString()]
	assert.Equal(t, petstorev1.PetType_PET_TYPE_UNSPECIFIED, stored.GetType())
	assert.Equal(t, petstorev1.Bowl_BOWL_STANDARD, stored.GetFeeding().GetBowl())
	assert.Equal(t, 90*time.Second, stored.GetFeeding().GetInterval().AsDuration())

	assert.Equal(t, "PET_TYPE_UNSPECIFIED", created.Type.ValueString())
	back := feedingOf(t, created)
	assert.Equal(t, "BOWL_STANDARD", back.Bowl.ValueString())
	assert.Equal(t, "1m30s", back.Interval.ValueString())

	// An import has nothing written to keep: the zero reads as null and the
	// duration in protojson form.
	imported := generated.NewPetModel()
	imported.Name = created.Name
	resp := &resource.ReadResponse{State: h.state(t, imported)}
	h.res.Read(ctx, resource.ReadRequest{State: h.state(t, imported)}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	var got generated.PetModel
	require.False(t, resp.State.Get(ctx, &got).HasError())
	assert.True(t, got.Type.IsNull())
	fresh := feedingOf(t, &got)
	assert.True(t, fresh.Bowl.IsNull())
	assert.Equal(t, "90s", fresh.Interval.ValueString())
}

// Left unset, the zero stays null rather than surfacing as a value nobody
// wrote.
func TestPetResource_ZeroEnumUnset(t *testing.T) {

	h := newHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	m.Feeding = feedingWith(t, types.StringUnknown(), types.StringUnknown())

	created := h.create(t, m)

	assert.True(t, created.Type.IsNull())
	back := feedingOf(t, created)
	assert.True(t, back.Bowl.IsNull())
	assert.True(t, back.Interval.IsNull())
}

// A bad value inside a nested attribute is reported against the child, not
// a top-level attribute of the same name.
func TestPetResource_NestedDiagnosticPath(t *testing.T) {

	m := generated.NewPetModel()
	m.Feeding = feedingWith(t, types.StringNull(), types.StringValue("often"))

	_, diags := m.ToProto(context.Background())

	require.True(t, diags.HasError())
	e, ok := diags.Errors()[0].(interface{ Path() path.Path })
	require.True(t, ok)
	assert.Equal(t, path.Root("feeding").AtName("interval"), e.Path())
}
