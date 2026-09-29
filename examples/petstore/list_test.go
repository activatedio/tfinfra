package petstore_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	"github.com/activatedio/tfinfra/examples/petstore/generated"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
)

func listDataSource(t *testing.T, h *harness, ds datasource.DataSource, defaults map[string]string) datasource.DataSource {
	t.Helper()
	resp := &datasource.ConfigureResponse{}
	ds.(datasource.DataSourceWithConfigure).Configure(context.Background(), datasource.ConfigureRequest{
		ProviderData: &tfruntime.ProviderData{Clients: map[string]any{"petstore": h.fake}, Defaults: defaults},
	}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	return ds
}

func TestPetsDataSource_ListsEveryPageUnderTheParent(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	for _, name := range []string{"Rex", "Tom", "Fido"} {
		m := generated.NewPetModel()
		m.DisplayName = types.StringValue(name)
		m.Type = types.StringValue("PET_TYPE_DOG")
		h.create(t, m)
	}
	// A pet under another store stays out of the list.
	elsewhere := generated.NewPetModel()
	elsewhere.DisplayName = types.StringValue("Elsewhere")
	elsewhere.StoreId = types.StringValue("s2")
	h.create(t, elsewhere)

	ds := listDataSource(t, h, generated.NewPetsDataSource(), map[string]string{"store_id": "s1"})
	s := generated.PetListDataSourceSchema()
	require.False(t, s.ValidateImplementation(ctx).HasError())

	cfg := tfsdk.Config{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), map[string]tftypes.Value{
		"store_id": tftypes.NewValue(tftypes.String, nil),
		"pets":     tftypes.NewValue(s.Type().TerraformType(ctx).(tftypes.Object).AttributeTypes["pets"], nil),
	})}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	ds.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	assert.Equal(t, "stores/s1", h.fake.lastListParent, "the provider default composes the parent")

	var out generated.PetsModel
	require.False(t, resp.State.Get(ctx, &out).HasError())
	var items []generated.PetItemModel
	require.False(t, out.Items.ElementsAs(ctx, &items, false).HasError())

	require.Len(t, items, 3, "three pages, one pet each, all followed")
	names := []string{items[0].DisplayName.ValueString(), items[1].DisplayName.ValueString(), items[2].DisplayName.ValueString()}
	assert.ElementsMatch(t, []string{"Rex", "Tom", "Fido"}, names)
	assert.Equal(t, "PET_TYPE_DOG", items[0].Type.ValueString())
	assert.Equal(t, "2026-08-15T12:00:00Z", items[0].CreateTime.ValueString())
	assert.True(t, items[0].Feeding.IsNull())
}

func TestToysDataSource_FillsTheCallerNamedID(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})
	h.fake.toyEntities["stores/s1/toys/squeaky"] = &petstorev1.Toy{Name: "stores/s1/toys/squeaky", DisplayName: "Squeaky duck"}

	ds := listDataSource(t, h, generated.NewToysDataSource(), map[string]string{"store_id": "s1"})
	s := generated.ToyListDataSourceSchema()

	// store_id set explicitly this time, over the same default.
	cfg := tfsdk.Config{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), map[string]tftypes.Value{
		"store_id": tftypes.NewValue(tftypes.String, "s1"),
		"toys":     tftypes.NewValue(s.Type().TerraformType(ctx).(tftypes.Object).AttributeTypes["toys"], nil),
	})}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	ds.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	var out generated.ToysModel
	require.False(t, resp.State.Get(ctx, &out).HasError())
	var items []generated.ToyItemModel
	require.False(t, out.Items.ElementsAs(ctx, &items, false).HasError())
	require.Len(t, items, 1)
	assert.Equal(t, "squeaky", items[0].ToyId.ValueString())
	assert.Equal(t, "stores/s1/toys/squeaky", items[0].Name.ValueString())
}

func TestPetsDataSource_RefusesARepeatedPageToken(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})
	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	h.create(t, m)
	h.fake.repeatPageToken = true

	ds := listDataSource(t, h, generated.NewPetsDataSource(), map[string]string{"store_id": "s1"})
	s := generated.PetListDataSourceSchema()
	cfg := tfsdk.Config{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), map[string]tftypes.Value{
		"store_id": tftypes.NewValue(tftypes.String, nil),
		"pets":     tftypes.NewValue(s.Type().TerraformType(ctx).(tftypes.Object).AttributeTypes["pets"], nil),
	})}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	ds.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "returned page token \"again\" twice")
}
