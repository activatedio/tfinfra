package petstore_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

// newPetRigWith is newPetRig over the given provider defaults.
func newPetRigWith(t *testing.T, defaults map[string]string) *rig[generated.PetModel] {
	return newRig[generated.PetModel](t, defaults, generated.NewPetResource(), generated.NewPetDataSource(),
		generated.PetResourceSchema(), generated.PetDataSourceSchema())
}

// planParent runs a parent attribute's plan modifiers the way the framework
// does for an update: an omitted computed value is planned unknown (as it is
// whenever another attribute changes), a configured one as written.
func planParent(t *testing.T, s schema.Schema, attr string, config, state types.String) (types.String, bool) {
	t.Helper()

	a, ok := s.Attributes[attr].(schema.StringAttribute)
	require.True(t, ok)

	ctx := context.Background()
	// Any non-null object stands for an existing resource and a non-destroy
	// plan; the modifiers read only the attribute values.
	obj := tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})

	planned := config
	if config.IsNull() {
		planned = types.StringUnknown()
	}

	resp := &planmodifier.StringResponse{PlanValue: planned}
	for _, m := range a.PlanModifiers {
		req := planmodifier.StringRequest{
			Path:        path.Root(attr),
			ConfigValue: config,
			StateValue:  state,
			PlanValue:   resp.PlanValue,
			State:       tfsdk.State{Raw: obj},
			Plan:        tfsdk.Plan{Raw: obj},
		}
		m.PlanModifyString(ctx, req, resp)
		require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	}

	return resp.PlanValue, resp.RequiresReplace
}

// An imported resource carries its parent identifier, whether the
// configuration sets it or inherits the provider default, and the next plan
// keeps it without replacing the resource.
func TestPetResource_ImportFillsTheParent(t *testing.T) {

	type s struct {
		defaults map[string]string
		config   types.String
	}

	cases := map[string]s{
		"configured parent": {
			config: types.StringValue("s2"),
		},
		"inherited parent": {
			defaults: map[string]string{"store_id": "s2"},
			config:   types.StringNull(),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			r := newPetRigWith(t, v.defaults)
			m := generated.NewPetModel()
			m.DisplayName = types.StringValue("Rex")
			m.StoreId = types.StringValue("s2")
			created := r.mustCreate(t, m)

			imported := r.importName(t, created.Name.ValueString())
			assert.Equal(t, "s2", imported.StoreId.ValueString())

			refreshed := r.read(t, imported)
			assert.Equal(t, "s2", refreshed.StoreId.ValueString())

			planned, replace := planParent(t, r.schema, "store_id", v.config, refreshed.StoreId)
			assert.False(t, replace)
			assert.Equal(t, "s2", planned.ValueString())
		})
	}
}

// Every parent identifier, carried by the entity or not, replaces the
// resource only when a configured value differs from state.
func TestParentIdentifiers_Replacement(t *testing.T) {

	type s struct {
		config      types.String
		wantReplace bool
		wantPlanned types.String
	}

	cases := map[string]s{
		"configured and unchanged": {
			config: types.StringValue("a"), wantPlanned: types.StringValue("a"),
		},
		"configured and changed": {
			config: types.StringValue("b"), wantReplace: true, wantPlanned: types.StringValue("b"),
		},
		"configured but unknown": {
			config: types.StringUnknown(), wantReplace: true, wantPlanned: types.StringUnknown(),
		},
		"omitted keeps the state's": {
			config: types.StringNull(), wantPlanned: types.StringValue("a"),
		},
	}

	schemas := map[string]struct {
		schema schema.Schema
		attr   string
	}{
		"pet store_id":   {generated.PetResourceSchema(), "store_id"},
		"run shelter_id": {generated.RunResourceSchema(), "shelter_id"},
	}

	for sk, sv := range schemas {
		for k, v := range cases {
			t.Run(sk+"/"+k, func(t *testing.T) {
				planned, replace := planParent(t, sv.schema, sv.attr, v.config, types.StringValue("a"))
				assert.Equal(t, v.wantReplace, replace)
				assert.Equal(t, v.wantPlanned, planned)
			})
		}
	}
}
