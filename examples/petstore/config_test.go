package petstore_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

const collarAnyJSON = `{"@type":"type.googleapis.com/petstore.v1.CollarConfig","color":"red","size":3}`

const collarWithBuckleAnyJSON = `{"@type":"type.googleapis.com/petstore.v1.CollarConfig","color":"red","size":3,"buckle":{"material":"steel"}}`

func TestCollarConfigDataSource_Read(t *testing.T) {

	ctx := context.Background()
	ds := generated.NewCollarConfigDataSource()

	s := generated.CollarConfigDataSourceSchema()
	seed := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	in := generated.NewCollarConfigModel()
	in.Color = types.StringValue("red")
	in.Size = types.Int64Value(3)
	in.Buckle = jsontypes.NewNormalizedValue(`{"material":"steel"}`)
	require.False(t, seed.Set(ctx, in).HasError())

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	ds.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: seed.Raw}}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	out := generated.NewCollarConfigModel()
	require.False(t, resp.State.Get(ctx, out).HasError())

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(out.Any.ValueString()), &decoded))
	assert.Equal(t, "type.googleapis.com/petstore.v1.CollarConfig", decoded["@type"])
	assert.Equal(t, "red", decoded["color"])
	assert.Equal(t, map[string]any{"material": "steel"}, decoded["buckle"])

	eq, diags := out.Any.StringSemanticEquals(ctx, jsontypes.NewNormalizedValue(collarWithBuckleAnyJSON))
	require.False(t, diags.HasError(), diags)
	assert.True(t, eq, "any output must be semantically the packed config: %s", out.Any.ValueString())

}

func TestPetResource_AnyAndStructConfig(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	const metadataJSON = `{"nested":{"a":1},"note":"hi"}`

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	m.Config = jsontypes.NewNormalizedValue(collarAnyJSON)
	m.Metadata = jsontypes.NewNormalizedValue(metadataJSON)
	created := h.create(t, m)

	// Server side: the Any unpacks to the concrete config type.
	stored := h.fake.pets[created.Name.ValueString()]
	cc := &petstorev1.CollarConfig{}
	require.NoError(t, stored.GetConfig().UnmarshalTo(cc))
	assert.Equal(t, "red", cc.GetColor())
	assert.Equal(t, int32(3), cc.GetSize())

	// Read-back is semantically equal despite protojson formatting.
	eq, diags := created.Config.StringSemanticEquals(ctx, jsontypes.NewNormalizedValue(collarAnyJSON))
	require.False(t, diags.HasError(), diags)
	assert.True(t, eq, "config read-back: %s", created.Config.ValueString())

	eq, diags = created.Metadata.StringSemanticEquals(ctx, jsontypes.NewNormalizedValue(metadataJSON))
	require.False(t, diags.HasError(), diags)
	assert.True(t, eq, "metadata read-back: %s", created.Metadata.ValueString())

	// A formatting-only change must not issue a patch (empty update mask).
	plan := *created
	plan.Config = jsontypes.NewNormalizedValue(`{ "@type": "type.googleapis.com/petstore.v1.CollarConfig", "color": "red", "size": 3 }`)
	updResp := &resource.UpdateResponse{State: h.emptyState()}
	h.res.Update(ctx, resource.UpdateRequest{
		Plan:  h.plan(t, &plan),
		State: h.state(t, created),
	}, updResp)
	require.False(t, updResp.Diagnostics.HasError(), updResp.Diagnostics)
	assert.Nil(t, h.fake.lastPatchPaths, "formatting-only diff must not patch")

	// A real config change patches exactly the config path.
	plan2 := *created
	plan2.Config = jsontypes.NewNormalizedValue(`{"@type":"type.googleapis.com/petstore.v1.CollarConfig","color":"blue","size":3}`)
	updResp2 := &resource.UpdateResponse{State: h.emptyState()}
	h.res.Update(ctx, resource.UpdateRequest{
		Plan:  h.plan(t, &plan2),
		State: h.state(t, created),
	}, updResp2)
	require.False(t, updResp2.Diagnostics.HasError(), updResp2.Diagnostics)
	assert.Equal(t, []string{"config"}, h.fake.lastPatchPaths)

}

func TestPetResource_InvalidAnyJSON(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	m.Config = jsontypes.NewNormalizedValue(`{"@type":"type.googleapis.com/petstore.v1.CollarConfig","nope":true}`)

	resp := &resource.CreateResponse{State: h.emptyState()}
	h.res.Create(ctx, resource.CreateRequest{Plan: h.plan(t, m)}, resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "invalid google.protobuf.Any JSON")

}
