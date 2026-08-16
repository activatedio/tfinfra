package petstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/activatedio/tfinfra/examples/petstore/generated"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
)

var createTimeFixture = time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

// harness wires the generated resource and data source to a fake client the
// way a provider's Configure would.
type harness struct {
	fake       *fakePetStoreClient
	res        resource.Resource
	ds         datasource.DataSource
	emptyState func() tfsdk.State
}

func newHarness(t *testing.T, defaults map[string]string) *harness {

	ctx := context.Background()
	fake := newFakePetStoreClient()

	pd := &tfruntime.ProviderData{
		Clients:  map[string]any{"petstore": fake},
		Defaults: defaults,
	}

	res := generated.NewPetResource()
	cResp := &resource.ConfigureResponse{}
	res.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: pd}, cResp)
	require.False(t, cResp.Diagnostics.HasError(), cResp.Diagnostics)

	ds := generated.NewPetDataSource()
	dResp := &datasource.ConfigureResponse{}
	ds.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: pd}, dResp)
	require.False(t, dResp.Diagnostics.HasError(), dResp.Diagnostics)

	rSchema := generated.PetResourceSchema()

	return &harness{
		fake: fake,
		res:  res,
		ds:   ds,
		emptyState: func() tfsdk.State {
			return tfsdk.State{Schema: rSchema, Raw: tftypes.NewValue(rSchema.Type().TerraformType(ctx), nil)}
		},
	}
}

func (h *harness) plan(t *testing.T, m *generated.PetModel) tfsdk.Plan {
	ctx := context.Background()
	s := generated.PetResourceSchema()
	p := tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	diags := p.Set(ctx, m)
	require.False(t, diags.HasError(), diags)
	return p
}

func (h *harness) state(t *testing.T, m *generated.PetModel) tfsdk.State {
	ctx := context.Background()
	s := generated.PetResourceSchema()
	st := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	diags := st.Set(ctx, m)
	require.False(t, diags.HasError(), diags)
	return st
}

func (h *harness) create(t *testing.T, m *generated.PetModel) *generated.PetModel {
	ctx := context.Background()
	resp := &resource.CreateResponse{State: h.emptyState()}
	h.res.Create(ctx, resource.CreateRequest{Plan: h.plan(t, m)}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	out := &generated.PetModel{}
	require.False(t, resp.State.Get(ctx, out).HasError())
	return out
}

func TestPetResource_Lifecycle(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	// Create: parent composed from the provider default.
	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	m.Type = types.StringValue("PET_TYPE_DOG")
	created := h.create(t, m)
	assert.Equal(t, "stores/s1", h.fake.lastCreateParent)
	assert.Equal(t, "stores/s1/pets/p1", created.Name.ValueString())
	assert.Equal(t, "2026-08-15T12:00:00Z", created.CreateTime.ValueString())
	assert.True(t, created.StoreId.IsNull(), "unset optional scope attribute must stay null")

	// Read: round-trips state.
	readResp := &resource.ReadResponse{State: h.state(t, created)}
	h.res.Read(ctx, resource.ReadRequest{State: h.state(t, created)}, readResp)
	require.False(t, readResp.Diagnostics.HasError(), readResp.Diagnostics)
	readBack := &generated.PetModel{}
	require.False(t, readResp.State.Get(ctx, readBack).HasError())
	assert.Equal(t, "Rex", readBack.DisplayName.ValueString())

	// Update: only the changed field lands in the patch mask.
	planModel := *created
	planModel.DisplayName = types.StringValue("Rexi")
	updResp := &resource.UpdateResponse{State: h.emptyState()}
	h.res.Update(ctx, resource.UpdateRequest{
		Plan:  h.plan(t, &planModel),
		State: h.state(t, created),
	}, updResp)
	require.False(t, updResp.Diagnostics.HasError(), updResp.Diagnostics)
	assert.Equal(t, []string{"display_name"}, h.fake.lastPatchPaths)
	updated := &generated.PetModel{}
	require.False(t, updResp.State.Get(ctx, updated).HasError())
	assert.Equal(t, "Rexi", updated.DisplayName.ValueString())
	assert.Equal(t, "PET_TYPE_DOG", updated.Type.ValueString(), "untouched fields survive the patch")

	// Delete: entity gone; a second delete tolerates NotFound.
	for range 2 {
		delResp := &resource.DeleteResponse{}
		h.res.Delete(ctx, resource.DeleteRequest{State: h.state(t, updated)}, delResp)
		require.False(t, delResp.Diagnostics.HasError(), delResp.Diagnostics)
	}
	assert.Empty(t, h.fake.pets)
}

func TestPetResource_ScopeOverrideAndMissing(t *testing.T) {

	type s struct {
		arrange func() (*harness, *generated.PetModel)
		assert  func(t *testing.T, h *harness, resp *resource.CreateResponse)
	}

	cases := map[string]s{
		"resource attribute overrides provider default": {
			arrange: func() (*harness, *generated.PetModel) {
				m := generated.NewPetModel()
				m.DisplayName = types.StringValue("Mimi")
				m.StoreId = types.StringValue("s2")
				return newHarness(t, map[string]string{"store_id": "s1"}), m
			},
			assert: func(t *testing.T, h *harness, resp *resource.CreateResponse) {
				require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
				assert.Equal(t, "stores/s2", h.fake.lastCreateParent)
			},
		},
		"missing scope attribute is an actionable error": {
			arrange: func() (*harness, *generated.PetModel) {
				m := generated.NewPetModel()
				m.DisplayName = types.StringValue("Mimi")
				return newHarness(t, nil), m
			},
			assert: func(t *testing.T, _ *harness, resp *resource.CreateResponse) {
				require.True(t, resp.Diagnostics.HasError())
				detail := resp.Diagnostics.Errors()[0].Detail()
				assert.Contains(t, detail, `"store_id"`)
				assert.Contains(t, detail, "provider default")
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			ctx := context.Background()
			h, m := v.arrange()
			resp := &resource.CreateResponse{State: h.emptyState()}
			h.res.Create(ctx, resource.CreateRequest{Plan: h.plan(t, m)}, resp)
			v.assert(t, h, resp)
		})
	}
}

func TestPetResource_OutOfBandDelete(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	created := h.create(t, m)
	delete(h.fake.pets, created.Name.ValueString())

	resp := &resource.ReadResponse{State: h.state(t, created)}
	h.res.Read(ctx, resource.ReadRequest{State: h.state(t, created)}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	assert.True(t, resp.State.Raw.IsNull(), "NotFound on read must remove the resource from state")
}

func TestPetResource_ImportState(t *testing.T) {

	type s struct {
		arrange func() string
		assert  func(t *testing.T, resp *resource.ImportStateResponse)
	}

	cases := map[string]s{
		"valid full name passes through": {
			arrange: func() string { return "stores/s1/pets/p1" },
			assert: func(t *testing.T, resp *resource.ImportStateResponse) {
				require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
				out := &generated.PetModel{}
				require.False(t, resp.State.Get(context.Background(), out).HasError())
				assert.Equal(t, "stores/s1/pets/p1", out.Name.ValueString())
			},
		},
		"malformed name is rejected": {
			arrange: func() string { return "pets/p1" },
			assert: func(t *testing.T, resp *resource.ImportStateResponse) {
				require.True(t, resp.Diagnostics.HasError())
				assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "stores/{store}/pets/{id}")
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t, nil)
			resp := &resource.ImportStateResponse{State: h.emptyState()}
			h.res.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: v.arrange()}, resp)
			v.assert(t, resp)
		})
	}
}

func TestPetDataSource_Read(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})
	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	created := h.create(t, m)

	s := generated.PetDataSourceSchema()
	// The data source shares the resource model; only name is user input.
	// tfsdk.Config has no Set, so build the raw value through a State.
	seed := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	lookup := generated.NewPetModel()
	lookup.Name = created.Name
	diags := seed.Set(ctx, lookup)
	require.False(t, diags.HasError(), diags)
	cfg := tfsdk.Config{Schema: s, Raw: seed.Raw}

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	h.ds.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	out := &generated.PetModel{}
	require.False(t, resp.State.Get(ctx, out).HasError())
	assert.Equal(t, "Rex", out.DisplayName.ValueString())
}
