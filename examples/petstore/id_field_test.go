package petstore_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/activatedio/tfinfra/examples/petstore/generated"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
)

// rig wires one generated resource and its singular data source to a fake
// client, for models of any entity.
type rig[M any] struct {
	fake     *fakePetStoreClient
	res      resource.Resource
	ds       datasource.DataSource
	schema   schema.Schema
	dsSchema dsschema.Schema
}

func newRig[M any](t *testing.T, defaults map[string]string, res resource.Resource, ds datasource.DataSource, s schema.Schema, dss dsschema.Schema) *rig[M] {

	t.Helper()
	ctx := context.Background()
	fake := newFakePetStoreClient()
	pd := &tfruntime.ProviderData{Clients: map[string]any{"petstore": fake}, Defaults: defaults}

	cResp := &resource.ConfigureResponse{}
	res.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: pd}, cResp)
	require.False(t, cResp.Diagnostics.HasError(), cResp.Diagnostics)

	dResp := &datasource.ConfigureResponse{}
	ds.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: pd}, dResp)
	require.False(t, dResp.Diagnostics.HasError(), dResp.Diagnostics)

	return &rig[M]{fake: fake, res: res, ds: ds, schema: s, dsSchema: dss}
}

func (r *rig[M]) empty() tfsdk.State {
	return tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(context.Background()), nil)}
}

func (r *rig[M]) plan(t *testing.T, m *M) tfsdk.Plan {
	t.Helper()
	p := tfsdk.Plan{Schema: r.schema, Raw: r.empty().Raw}
	diags := p.Set(context.Background(), m)
	require.False(t, diags.HasError(), diags)
	return p
}

func (r *rig[M]) state(t *testing.T, m *M) tfsdk.State {
	t.Helper()
	st := r.empty()
	diags := st.Set(context.Background(), m)
	require.False(t, diags.HasError(), diags)
	return st
}

func (r *rig[M]) model(t *testing.T, st tfsdk.State) *M {
	t.Helper()
	out := new(M)
	diags := st.Get(context.Background(), out)
	require.False(t, diags.HasError(), diags)
	return out
}

func (r *rig[M]) create(t *testing.T, m *M) (*M, diag.Diagnostics) {
	t.Helper()
	resp := &resource.CreateResponse{State: r.empty()}
	r.res.Create(context.Background(), resource.CreateRequest{Plan: r.plan(t, m)}, resp)
	if resp.Diagnostics.HasError() {
		return nil, resp.Diagnostics
	}
	return r.model(t, resp.State), resp.Diagnostics
}

func (r *rig[M]) mustCreate(t *testing.T, m *M) *M {
	t.Helper()
	out, diags := r.create(t, m)
	require.False(t, diags.HasError(), diags)
	return out
}

func (r *rig[M]) read(t *testing.T, m *M) *M {
	t.Helper()
	resp := &resource.ReadResponse{State: r.state(t, m)}
	r.res.Read(context.Background(), resource.ReadRequest{State: r.state(t, m)}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	return r.model(t, resp.State)
}

func (r *rig[M]) update(t *testing.T, prior, planned *M) *M {
	t.Helper()
	resp := &resource.UpdateResponse{State: r.empty()}
	r.res.Update(context.Background(), resource.UpdateRequest{Plan: r.plan(t, planned), State: r.state(t, prior)}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	return r.model(t, resp.State)
}

func (r *rig[M]) importName(t *testing.T, name string) *M {
	t.Helper()
	resp := &resource.ImportStateResponse{State: r.empty()}
	r.res.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: name}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	return r.model(t, resp.State)
}

func (r *rig[M]) readDataSource(t *testing.T, lookup *M) *M {
	t.Helper()
	ctx := context.Background()
	seed := tfsdk.State{Schema: r.dsSchema, Raw: tftypes.NewValue(r.dsSchema.Type().TerraformType(ctx), nil)}
	require.False(t, seed.Set(ctx, lookup).HasError())
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: r.dsSchema, Raw: seed.Raw}}
	r.ds.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: r.dsSchema, Raw: seed.Raw}}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	out := new(M)
	require.False(t, resp.State.Get(ctx, out).HasError())
	return out
}

func newShelterRig(t *testing.T) *rig[generated.ShelterModel] {
	return newRig[generated.ShelterModel](t, nil, generated.NewShelterResource(), generated.NewShelterDataSource(),
		generated.ShelterResourceSchema(), generated.ShelterDataSourceSchema())
}

func newRunRig(t *testing.T, defaults map[string]string) *rig[generated.RunModel] {
	return newRig[generated.RunModel](t, defaults, generated.NewRunResource(), generated.NewRunDataSource(),
		generated.RunResourceSchema(), generated.RunDataSourceSchema())
}

func shelter(id, displayName string) *generated.ShelterModel {
	m := generated.NewShelterModel()
	m.ShelterId = types.StringValue(id)
	m.DisplayName = types.StringValue(displayName)
	m.Name = types.StringUnknown()
	return m
}

// A resource whose id is one of its own fields: the field is the id
// attribute, it reaches the API in the entity, and it never collides with
// an attribute tfinfra adds.
func TestShelterResource_IDFieldLifecycle(t *testing.T) {

	r := newShelterRig(t)

	created := r.mustCreate(t, shelter("north", "North shelter"))
	assert.Empty(t, r.fake.lastCreateParent, "a top-level resource has no parent")
	assert.Equal(t, "north", r.fake.lastCreateShelter.GetShelterId(), "the id travels in its own field")
	assert.Empty(t, r.fake.lastCreateShelter.GetName(), "and not in name")
	assert.Equal(t, "shelters/north", created.Name.ValueString())
	assert.Equal(t, "north", created.ShelterId.ValueString())

	readBack := r.read(t, created)
	assert.Equal(t, "north", readBack.ShelterId.ValueString())
	assert.Equal(t, "North shelter", readBack.DisplayName.ValueString())

	planned := *readBack
	planned.DisplayName = types.StringValue("North shelter (annex)")
	updated := r.update(t, readBack, &planned)
	assert.Equal(t, "North shelter (annex)", r.fake.shelters["shelters/north"].GetDisplayName(), "UseUpdate replaces the entity")
	assert.Equal(t, "north", updated.ShelterId.ValueString())

	delResp := &resource.DeleteResponse{}
	r.res.Delete(context.Background(), resource.DeleteRequest{State: r.state(t, updated)}, delResp)
	require.False(t, delResp.Diagnostics.HasError(), delResp.Diagnostics)
	assert.Empty(t, r.fake.shelters)
}

func TestShelterResource_IDFieldSchema(t *testing.T) {

	s := generated.ShelterResourceSchema()
	require.False(t, s.ValidateImplementation(context.Background()).HasError())

	_, synthetic := s.Attributes["shelter_shelter_id"]
	assert.False(t, synthetic)

	a, ok := s.Attributes["shelter_id"].(schema.StringAttribute)
	require.True(t, ok)
	assert.True(t, a.Required)
	assert.False(t, a.Computed)
	require.Len(t, a.PlanModifiers, 1)
	assert.Contains(t, a.MarkdownDescription, "Changing it replaces the resource")
}

// Import and the data source fill the id field from the name, before any
// read has returned it: a required attribute missing from imported state
// would force replacement on the next plan.
func TestShelterResource_ImportAndDataSourceFillTheIDField(t *testing.T) {

	type s struct {
		arrange func(t *testing.T, r *rig[generated.ShelterModel]) *generated.ShelterModel
		assert  func(t *testing.T, got *generated.ShelterModel)
	}

	cases := map[string]s{
		"import": {
			arrange: func(t *testing.T, r *rig[generated.ShelterModel]) *generated.ShelterModel {
				return r.importName(t, "shelters/north")
			},
			assert: func(t *testing.T, got *generated.ShelterModel) {
				assert.Equal(t, "shelters/north", got.Name.ValueString())
				assert.Equal(t, "north", got.ShelterId.ValueString())
			},
		},
		"singular data source": {
			arrange: func(t *testing.T, r *rig[generated.ShelterModel]) *generated.ShelterModel {
				r.mustCreate(t, shelter("north", "North shelter"))
				lookup := generated.NewShelterModel()
				lookup.Name = types.StringValue("shelters/north")
				return r.readDataSource(t, lookup)
			},
			assert: func(t *testing.T, got *generated.ShelterModel) {
				assert.Equal(t, "north", got.ShelterId.ValueString())
				assert.Equal(t, "North shelter", got.DisplayName.ValueString())
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newShelterRig(t)
			c.assert(t, c.arrange(t, r))
		})
	}
}

func run(shelterID types.String, runID types.String, length int64) *generated.RunModel {
	m := generated.NewRunModel()
	m.Name = types.StringUnknown()
	m.ShelterId = shelterID
	m.RunId = runID
	m.LengthM = types.Int64Value(length)
	return m
}

// Run's id is optional (the API mints one when it is left empty), and it
// carries its parent's id as a field: one attribute with the scope
// identifier, sent in the parent and read from the entity.
func TestRunResource_IDFieldAndParentID(t *testing.T) {

	type s struct {
		defaults map[string]string
		arrange  func(r *rig[generated.RunModel])
		model    *generated.RunModel
		assert   func(t *testing.T, r *rig[generated.RunModel], got *generated.RunModel)
	}

	cases := map[string]s{
		"the caller's id and parent": {
			model: run(types.StringValue("north"), types.StringValue("long"), 40),
			assert: func(t *testing.T, r *rig[generated.RunModel], got *generated.RunModel) {
				assert.Equal(t, "shelters/north", r.fake.lastCreateParent)
				assert.Equal(t, "long", r.fake.lastCreateRun.GetRunId(), "a set id is sent")
				assert.Empty(t, r.fake.lastCreateRun.GetShelterId(), "the parent's id travels in the parent, not the entity")
				assert.Equal(t, "shelters/north/runs/long", got.Name.ValueString())
				assert.Equal(t, "long", got.RunId.ValueString())
				assert.Equal(t, "north", got.ShelterId.ValueString())
			},
		},
		"an id left unset is minted and read back": {
			model: run(types.StringValue("north"), types.StringUnknown(), 40),
			assert: func(t *testing.T, r *rig[generated.RunModel], got *generated.RunModel) {
				assert.Empty(t, r.fake.lastCreateRun.GetRunId())
				assert.Equal(t, "run1", got.RunId.ValueString())
				assert.Equal(t, "shelters/north/runs/run1", got.Name.ValueString())
			},
		},
		"the parent from the provider default reads back": {
			defaults: map[string]string{"shelter_id": "south"},
			model:    run(types.StringUnknown(), types.StringValue("short"), 10),
			assert: func(t *testing.T, r *rig[generated.RunModel], got *generated.RunModel) {
				assert.Equal(t, "shelters/south", r.fake.lastCreateParent)
				assert.Equal(t, "south", got.ShelterId.ValueString())
			},
		},
		"a server that does not echo the parent's id still fills it, from the name": {
			arrange: func(r *rig[generated.RunModel]) { r.fake.runsOmitShelterID = true },
			model:   run(types.StringValue("north"), types.StringValue("long"), 40),
			assert: func(t *testing.T, r *rig[generated.RunModel], got *generated.RunModel) {
				assert.Equal(t, "north", got.ShelterId.ValueString())
				assert.Equal(t, "north", r.read(t, got).ShelterId.ValueString())
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRunRig(t, c.defaults)
			if c.arrange != nil {
				c.arrange(r)
			}
			c.assert(t, r, r.mustCreate(t, c.model))
		})
	}
}

func TestRunResource_UpdateKeepsTheIDs(t *testing.T) {

	r := newRunRig(t, nil)
	created := r.mustCreate(t, run(types.StringValue("north"), types.StringValue("long"), 40))

	planned := *created
	planned.LengthM = types.Int64Value(45)
	updated := r.update(t, created, &planned)

	assert.Equal(t, int32(45), r.fake.runs["shelters/north/runs/long"].GetLengthM())
	assert.Equal(t, "long", updated.RunId.ValueString())
	assert.Equal(t, "north", updated.ShelterId.ValueString())
}

// An imported run has both ids before the first read, so a configured
// shelter_id does not force replacement.
func TestRunResource_ImportFillsBothIDs(t *testing.T) {

	r := newRunRig(t, nil)
	got := r.importName(t, "shelters/north/runs/long")

	assert.Equal(t, "long", got.RunId.ValueString())
	assert.Equal(t, "north", got.ShelterId.ValueString())
}

func TestRunResource_Schema(t *testing.T) {

	type s struct {
		attribute string
		assert    func(t *testing.T, a schema.StringAttribute)
	}

	cases := map[string]s{
		"the minted id is optional and computed, and replaces on change": {
			attribute: "run_id",
			assert: func(t *testing.T, a schema.StringAttribute) {
				assert.True(t, a.Optional)
				assert.True(t, a.Computed)
				assert.Len(t, a.PlanModifiers, 2)
			},
		},
		"the carried parent id is one attribute, validated as a reference": {
			attribute: "shelter_id",
			assert: func(t *testing.T, a schema.StringAttribute) {
				assert.True(t, a.Optional)
				assert.True(t, a.Computed)
				assert.Len(t, a.PlanModifiers, 2)
				require.Len(t, a.Validators, 1)
				assert.Equal(t, "the shelter's id", a.Validators[0].Description(context.Background()))
			},
		},
	}

	s0 := generated.RunResourceSchema()
	require.False(t, s0.ValidateImplementation(context.Background()).HasError())

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			a, ok := s0.Attributes[c.attribute].(schema.StringAttribute)
			require.True(t, ok)
			c.assert(t, a)
		})
	}
}

func TestRunsDataSource_ItemsCarryBothIDs(t *testing.T) {

	ctx := context.Background()
	r := newRunRig(t, nil)
	r.mustCreate(t, run(types.StringValue("north"), types.StringValue("long"), 40))
	r.mustCreate(t, run(types.StringValue("south"), types.StringValue("short"), 10))

	ds := generated.NewRunsDataSource()
	cResp := &datasource.ConfigureResponse{}
	ds.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{
		ProviderData: &tfruntime.ProviderData{Clients: map[string]any{"petstore": r.fake}},
	}, cResp)
	require.False(t, cResp.Diagnostics.HasError(), cResp.Diagnostics)

	s := generated.RunListDataSourceSchema()
	cfg := generated.RunsModel{
		ShelterId: types.StringValue("north"),
		Items:     types.ListNull(types.ObjectType{AttrTypes: generated.RunItemAttrTypes()}),
	}
	seed := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	require.False(t, seed.Set(ctx, &cfg).HasError())

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: seed.Raw}}
	ds.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: seed.Raw}}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	var out generated.RunsModel
	require.False(t, resp.State.Get(ctx, &out).HasError())
	var items []generated.RunItemModel
	require.False(t, out.Items.ElementsAs(ctx, &items, false).HasError())
	require.Len(t, items, 1)
	assert.Equal(t, "shelters/north", r.fake.lastListParent)
	assert.Equal(t, "long", items[0].RunId.ValueString())
	assert.Equal(t, "north", items[0].ShelterId.ValueString())
}
