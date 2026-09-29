package petstore_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

// lastSegment is what every <resource>_id must equal.
func lastSegment(name string) string {
	return name[strings.LastIndex(name, "/")+1:]
}

// A server-named resource's id attribute is the last segment of its name on
// every path that writes state: create, read, update, import, and both data
// sources.
func TestPetResource_IDIsTheLastSegmentOfName(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	created := h.create(t, m)
	require.Equal(t, "stores/s1/pets/p1", created.Name.ValueString())
	assert.Equal(t, "p1", created.PetId.ValueString(), "create")

	// Read fills it even when state has none: state written before the
	// attribute existed.
	prior := *created
	prior.PetId = types.StringNull()
	readResp := &resource.ReadResponse{State: h.state(t, &prior)}
	h.res.Read(ctx, resource.ReadRequest{State: h.state(t, &prior)}, readResp)
	require.False(t, readResp.Diagnostics.HasError(), readResp.Diagnostics)
	read := &generated.PetModel{}
	require.False(t, readResp.State.Get(ctx, read).HasError())
	assert.Equal(t, "p1", read.PetId.ValueString(), "read")

	planModel := *created
	planModel.DisplayName = types.StringValue("Rexi")
	planModel.PetId = types.StringUnknown()
	updResp := &resource.UpdateResponse{State: h.emptyState()}
	h.res.Update(ctx, resource.UpdateRequest{Plan: h.plan(t, &planModel), State: h.state(t, created)}, updResp)
	require.False(t, updResp.Diagnostics.HasError(), updResp.Diagnostics)
	updated := &generated.PetModel{}
	require.False(t, updResp.State.Get(ctx, updated).HasError())
	assert.Equal(t, "p1", updated.PetId.ValueString(), "update")

	impResp := &resource.ImportStateResponse{State: h.emptyState()}
	h.res.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: created.Name.ValueString()}, impResp)
	require.False(t, impResp.Diagnostics.HasError(), impResp.Diagnostics)
	imported := &generated.PetModel{}
	require.False(t, impResp.State.Get(ctx, imported).HasError())
	assert.Equal(t, "p1", imported.PetId.ValueString(), "import")

	s := generated.PetDataSourceSchema()
	seed := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	lookup := generated.NewPetModel()
	lookup.Name = created.Name
	require.False(t, seed.Set(ctx, lookup).HasError())
	dsResp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	h.ds.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: seed.Raw}}, dsResp)
	require.False(t, dsResp.Diagnostics.HasError(), dsResp.Diagnostics)
	fromDS := &generated.PetModel{}
	require.False(t, dsResp.State.Get(ctx, fromDS).HasError())
	assert.Equal(t, "p1", fromDS.PetId.ValueString(), "singular data source")

	ls := generated.PetListDataSourceSchema()
	list := listDataSource(t, h, generated.NewPetsDataSource(), map[string]string{"store_id": "s1"})
	cfg := tfsdk.Config{Schema: ls, Raw: tftypes.NewValue(ls.Type().TerraformType(ctx), map[string]tftypes.Value{
		"store_id": tftypes.NewValue(tftypes.String, nil),
		"pets":     tftypes.NewValue(ls.Type().TerraformType(ctx).(tftypes.Object).AttributeTypes["pets"], nil),
	})}
	listResp := &datasource.ReadResponse{State: tfsdk.State{Schema: ls, Raw: tftypes.NewValue(ls.Type().TerraformType(ctx), nil)}}
	list.Read(ctx, datasource.ReadRequest{Config: cfg}, listResp)
	require.False(t, listResp.Diagnostics.HasError(), listResp.Diagnostics)
	var out generated.PetsModel
	require.False(t, listResp.State.Get(ctx, &out).HasError())
	var items []generated.PetItemModel
	require.False(t, out.Items.ElementsAs(ctx, &items, false).HasError())
	require.Len(t, items, 1)
	assert.Equal(t, lastSegment(items[0].Name.ValueString()), items[0].PetId.ValueString(), "plural data source")
}

// A minted resource carries its id the same way.
func TestAccessKeyResource_ID(t *testing.T) {

	h := newAccessKeyHarness(t)
	m := generated.NewAccessKeyModel()
	m.DisplayName = types.StringValue("ci")
	minted := h.create(t, m)

	require.NotEmpty(t, minted.AccessKeyId.ValueString())
	assert.Equal(t, lastSegment(minted.Name.ValueString()), minted.AccessKeyId.ValueString())
}

// The id attribute is computed on a server-named resource and kept across
// plans, since it never changes in place.
func TestPetResource_IDSchema(t *testing.T) {

	a, ok := generated.PetResourceSchema().Attributes["pet_id"].(schema.StringAttribute)
	require.True(t, ok)
	assert.True(t, a.Computed)
	assert.False(t, a.Optional)
	assert.False(t, a.Required)
	require.Len(t, a.PlanModifiers, 1)
	assert.Contains(t, a.PlanModifiers[0].Description(context.Background()), "will not change")

	toy, ok := generated.ToyResourceSchema().Attributes["toy_id"].(schema.StringAttribute)
	require.True(t, ok)
	assert.True(t, toy.Required, "a caller-named resource's id stays an input")
}

// validate runs every validator of one attribute on value and returns the
// error details.
func validate(t *testing.T, attr string, validators []validator.String, value string) []string {
	t.Helper()
	resp := &validator.StringResponse{}
	for _, v := range validators {
		v.ValidateString(context.Background(), validator.StringRequest{
			Path:        path.Root(attr),
			ConfigValue: types.StringValue(value),
		}, resp)
	}
	errs := resp.Diagnostics.Errors()
	out := make([]string, 0, len(errs))
	for _, d := range errs {
		out = append(out, d.Detail())
	}
	return out
}

// A Reference field and a ScopeReferences parent validate by prefix in
// every schema a practitioner writes them in, and a full name fails with a
// message naming the attribute to use when a resource of the spec has one.
func TestReferenceValidators(t *testing.T) {

	res := generated.PetResourceSchema().Attributes
	list := generated.PetListDataSourceSchema().Attributes

	cases := map[string][]validator.String{
		"buddy_id (resource)":    res["buddy_id"].(schema.StringAttribute).Validators,
		"store_id (resource)":    res["store_id"].(schema.StringAttribute).Validators,
		"store_id (plural data)": list["store_id"].(dsschema.StringAttribute).Validators,
		"tag_key_id (config)":    generated.CollarConfigDataSourceSchema().Attributes["tag_key_id"].(dsschema.StringAttribute).Validators,
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			attr := strings.Fields(k)[0]
			prefix := map[string]string{"buddy_id": "p", "store_id": "s", "tag_key_id": "k"}[attr]
			example := map[string]string{
				"buddy_id": "petstore_pet.<name>.pet_id",
				// No store resource: nothing to reference, so no example.
				"store_id":   "",
				"tag_key_id": "petstore_access_key.<name>.access_key_id",
			}[attr]
			require.Len(t, v, 1)

			assert.Empty(t, validate(t, attr, v, prefix+"-7"))
			assert.Empty(t, validate(t, attr, v, ""), "an unset optional reference")

			errs := validate(t, attr, v, "stores/s-1/pets/p-7")
			require.Len(t, errs, 1)
			assert.Contains(t, errs[0], "is a resource name, not an id")
			if example == "" {
				assert.NotContains(t, errs[0], "Reference")
			} else {
				assert.Contains(t, errs[0], example)
			}

			assert.Len(t, validate(t, attr, v, "x-7"), 1, "another type's id")
		})
	}

	// A data source reads the reference, so nothing validates it there.
	assert.Empty(t, generated.PetDataSourceSchema().Attributes["buddy_id"].(dsschema.StringAttribute).Validators)
}
