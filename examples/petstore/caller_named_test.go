package petstore_test

import (
	"context"
	"testing"

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

// toyHarness wires the generated caller-named resource and its data source
// to a fake client the way a provider's Configure would.
type toyHarness struct {
	fake       *fakePetStoreClient
	res        resource.Resource
	ds         datasource.DataSource
	emptyState func() tfsdk.State
}

func newToyHarness(t *testing.T, defaults map[string]string) *toyHarness {

	ctx := context.Background()
	fake := newFakePetStoreClient()

	pd := &tfruntime.ProviderData{
		Clients:  map[string]any{"petstore": fake},
		Defaults: defaults,
	}

	res := generated.NewToyResource()
	cResp := &resource.ConfigureResponse{}
	res.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: pd}, cResp)
	require.False(t, cResp.Diagnostics.HasError(), cResp.Diagnostics)

	ds := generated.NewToyDataSource()
	dResp := &datasource.ConfigureResponse{}
	ds.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: pd}, dResp)
	require.False(t, dResp.Diagnostics.HasError(), dResp.Diagnostics)

	s := generated.ToyResourceSchema()

	return &toyHarness{
		fake: fake,
		res:  res,
		ds:   ds,
		emptyState: func() tfsdk.State {
			return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
		},
	}
}

func (h *toyHarness) plan(t *testing.T, m *generated.ToyModel) tfsdk.Plan {
	ctx := context.Background()
	s := generated.ToyResourceSchema()
	p := tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	diags := p.Set(ctx, m)
	require.False(t, diags.HasError(), diags)
	return p
}

func (h *toyHarness) state(t *testing.T, m *generated.ToyModel) tfsdk.State {
	ctx := context.Background()
	s := generated.ToyResourceSchema()
	st := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	diags := st.Set(ctx, m)
	require.False(t, diags.HasError(), diags)
	return st
}

func (h *toyHarness) create(t *testing.T, m *generated.ToyModel) *generated.ToyModel {
	ctx := context.Background()
	resp := &resource.CreateResponse{State: h.emptyState()}
	h.res.Create(ctx, resource.CreateRequest{Plan: h.plan(t, m)}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	out := &generated.ToyModel{}
	require.False(t, resp.State.Get(ctx, out).HasError())
	return out
}

func TestToyResource_CallerNamedLifecycle(t *testing.T) {

	ctx := context.Background()
	h := newToyHarness(t, map[string]string{"store_id": "s1"})

	// Create: the id travels in the entity's name field, and the server
	// composes the full resource name from parent and id.
	m := generated.NewToyModel()
	m.ToyId = types.StringValue("squeaky-bone")
	m.DisplayName = types.StringValue("Squeaky bone")
	created := h.create(t, m)
	assert.Equal(t, "stores/s1", h.fake.lastCreateParent)
	assert.Equal(t, "squeaky-bone", h.fake.lastCreateToyID)
	assert.Equal(t, "stores/s1/toys/squeaky-bone", created.Name.ValueString())
	assert.Equal(t, "squeaky-bone", created.ToyId.ValueString(), "the id attribute survives create")

	// Read: refresh keeps the id attribute, which is not a proto field.
	readResp := &resource.ReadResponse{State: h.state(t, created)}
	h.res.Read(ctx, resource.ReadRequest{State: h.state(t, created)}, readResp)
	require.False(t, readResp.Diagnostics.HasError(), readResp.Diagnostics)
	readBack := &generated.ToyModel{}
	require.False(t, readResp.State.Get(ctx, readBack).HasError())
	assert.Equal(t, "squeaky-bone", readBack.ToyId.ValueString())
	assert.Equal(t, "Squeaky bone", readBack.DisplayName.ValueString())

	// Update: the id is not a proto field, so it never lands in the mask.
	planModel := *created
	planModel.DisplayName = types.StringValue("Squeaky bone II")
	updResp := &resource.UpdateResponse{State: h.emptyState()}
	h.res.Update(ctx, resource.UpdateRequest{
		Plan:  h.plan(t, &planModel),
		State: h.state(t, created),
	}, updResp)
	require.False(t, updResp.Diagnostics.HasError(), updResp.Diagnostics)
	assert.Equal(t, []string{"display_name"}, h.fake.lastPatchPaths)
	updated := &generated.ToyModel{}
	require.False(t, updResp.State.Get(ctx, updated).HasError())
	assert.Equal(t, "squeaky-bone", updated.ToyId.ValueString())

	// Delete.
	delResp := &resource.DeleteResponse{}
	h.res.Delete(ctx, resource.DeleteRequest{State: h.state(t, updated)}, delResp)
	require.False(t, delResp.Diagnostics.HasError(), delResp.Diagnostics)
	assert.Empty(t, h.fake.toyEntities)
}

func TestToyResource_MissingID(t *testing.T) {

	ctx := context.Background()
	h := newToyHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewToyModel()
	m.DisplayName = types.StringValue("Nameless")

	resp := &resource.CreateResponse{State: h.emptyState()}
	h.res.Create(ctx, resource.CreateRequest{Plan: h.plan(t, m)}, resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "must not be empty")
	assert.Empty(t, h.fake.toyEntities, "nothing is created without an id")
}

func TestToyResource_ImportFillsID(t *testing.T) {

	ctx := context.Background()
	h := newToyHarness(t, nil)

	resp := &resource.ImportStateResponse{State: h.emptyState()}
	h.res.(resource.ResourceWithImportState).ImportState(ctx,
		resource.ImportStateRequest{ID: "stores/s1/toys/squeaky-bone"}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	out := &generated.ToyModel{}
	require.False(t, resp.State.Get(ctx, out).HasError())
	assert.Equal(t, "stores/s1/toys/squeaky-bone", out.Name.ValueString())
	// Required attributes must land in state on import, or the first plan
	// after it would force replacement.
	assert.Equal(t, "squeaky-bone", out.ToyId.ValueString())
}

func TestToyDataSource_ReadFillsID(t *testing.T) {

	ctx := context.Background()
	h := newToyHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewToyModel()
	m.ToyId = types.StringValue("squeaky-bone")
	m.DisplayName = types.StringValue("Squeaky bone")
	created := h.create(t, m)

	s := generated.ToyDataSourceSchema()
	seed := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	lookup := generated.NewToyModel()
	lookup.Name = created.Name
	require.False(t, seed.Set(ctx, lookup).HasError())

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	h.ds.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: seed.Raw}}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	out := &generated.ToyModel{}
	require.False(t, resp.State.Get(ctx, out).HasError())
	assert.Equal(t, "Squeaky bone", out.DisplayName.ValueString())
	assert.Equal(t, "squeaky-bone", out.ToyId.ValueString())
}
