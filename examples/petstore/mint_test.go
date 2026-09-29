package petstore_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/activatedio/tfinfra/examples/petstore/generated"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
)

// accessKeyHarness wires the minted AccessKey resource to the fake client.
type accessKeyHarness struct {
	fake *fakePetStoreClient
	res  resource.Resource
}

func newAccessKeyHarness(t *testing.T) *accessKeyHarness {

	fake := newFakePetStoreClient()
	res := generated.NewAccessKeyResource()
	resp := &resource.ConfigureResponse{}
	res.(resource.ResourceWithConfigure).Configure(context.Background(), resource.ConfigureRequest{
		ProviderData: &tfruntime.ProviderData{Clients: map[string]any{"petstore": fake}, Defaults: map[string]string{"store_id": "s1"}},
	}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	return &accessKeyHarness{fake: fake, res: res}
}

func accessKeyState(t *testing.T, m *generated.AccessKeyModel) tfsdk.State {
	ctx := context.Background()
	s := generated.AccessKeyResourceSchema()
	st := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	require.False(t, st.Set(ctx, m).HasError())
	return st
}

func accessKeyPlan(t *testing.T, m *generated.AccessKeyModel) tfsdk.Plan {
	st := accessKeyState(t, m)
	return tfsdk.Plan(st)
}

func emptyAccessKeyState() tfsdk.State {
	s := generated.AccessKeyResourceSchema()
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
}

func (h *accessKeyHarness) create(t *testing.T, m *generated.AccessKeyModel) *generated.AccessKeyModel {
	ctx := context.Background()
	resp := &resource.CreateResponse{State: emptyAccessKeyState()}
	h.res.Create(ctx, resource.CreateRequest{Plan: accessKeyPlan(t, m)}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	out := &generated.AccessKeyModel{}
	require.False(t, resp.State.Get(ctx, out).HasError())
	return out
}

// The mint is the only moment the key exists on the wire, so the whole
// feature is that state keeps it from there on: through a refresh the
// server answers without it, and through an in-place update.
func TestAccessKeyResource_MintLifecycle(t *testing.T) {

	ctx := context.Background()
	h := newAccessKeyHarness(t)

	m := generated.NewAccessKeyModel()
	m.DisplayName = types.StringValue("ci")
	m.ExpiresAt = types.StringValue("2027-01-01T00:00:00Z")
	m.Keepers = types.MapValueMust(types.StringType, map[string]attr.Value{"rotation": types.StringValue("1")})

	created := h.create(t, m)

	// The flat request carried the parent and every settable field.
	require.NotNil(t, h.fake.lastMint)
	assert.Equal(t, "stores/s1", h.fake.lastMint.GetParent())
	assert.Equal(t, "ci", h.fake.lastMint.GetDisplayName())
	assert.Equal(t, "2027-01-01T00:00:00Z", h.fake.lastMint.GetExpiresAt().AsTime().Format("2006-01-02T15:04:05Z07:00"))

	assert.Equal(t, "stores/s1/accessKeys/k1", created.Name.ValueString())
	assert.Equal(t, "k1_plaintext1", created.Key.ValueString(), "the once-only value lands in state")
	assert.Equal(t, "2026-08-15T12:00:00Z", created.CreateTime.ValueString())
	assert.Equal(t, "1", created.Keepers.Elements()["rotation"].(types.String).ValueString(), "keepers stay as configured")

	// A refresh: the server has no key to return.
	readResp := &resource.ReadResponse{State: accessKeyState(t, created)}
	h.res.Read(ctx, resource.ReadRequest{State: accessKeyState(t, created)}, readResp)
	require.False(t, readResp.Diagnostics.HasError(), readResp.Diagnostics)
	read := &generated.AccessKeyModel{}
	require.False(t, readResp.State.Get(ctx, read).HasError())
	assert.Equal(t, "k1_plaintext1", read.Key.ValueString(), "a refresh must never null the key")
	assert.False(t, read.Keepers.IsNull())

	// An in-place update: the plan carries the key from state
	// (UseStateForUnknown), and the patch never mentions it.
	planned := *read
	planned.DisplayName = types.StringValue("ci-renamed")
	upResp := &resource.UpdateResponse{State: accessKeyState(t, read)}
	h.res.Update(ctx, resource.UpdateRequest{Plan: accessKeyPlan(t, &planned), State: accessKeyState(t, read)}, upResp)
	require.False(t, upResp.Diagnostics.HasError(), upResp.Diagnostics)
	assert.Equal(t, []string{"display_name"}, h.fake.lastPatchPaths, "neither the key nor keepers reach the patch")
	updated := &generated.AccessKeyModel{}
	require.False(t, upResp.State.Get(ctx, updated).HasError())
	assert.Equal(t, "ci-renamed", updated.DisplayName.ValueString())
	assert.Equal(t, "k1_plaintext1", updated.Key.ValueString())

	delResp := &resource.DeleteResponse{}
	h.res.Delete(ctx, resource.DeleteRequest{State: accessKeyState(t, updated)}, delResp)
	require.False(t, delResp.Diagnostics.HasError(), delResp.Diagnostics)
	assert.Empty(t, h.fake.accessKeys)
}

func TestAccessKeyResource_MintSchema(t *testing.T) {

	ctx := context.Background()
	s := generated.AccessKeyResourceSchema()
	require.False(t, s.ValidateImplementation(ctx).HasError())

	key, ok := s.Attributes["key"].(schema.StringAttribute)
	require.True(t, ok)
	assert.True(t, key.Computed)
	assert.True(t, key.Sensitive)
	assert.False(t, key.Optional, "the practitioner never supplies the key")
	assert.Len(t, key.PlanModifiers, 1, "UseStateForUnknown: the key never changes in place")

	keepers, ok := s.Attributes["keepers"].(schema.MapAttribute)
	require.True(t, ok)
	assert.True(t, keepers.Optional)
	assert.False(t, keepers.Computed)
	require.Len(t, keepers.PlanModifiers, 1)
	req := planModifyMap(t, keepers, map[string]string{"rotation": "1"}, map[string]string{"rotation": "2"})
	assert.True(t, req.RequiresReplace, "changing a keeper replaces the resource")
}

// An import cannot recover the key: it is null, and a refresh keeps it
// null rather than inventing one.
func TestAccessKeyResource_ImportHasNoKey(t *testing.T) {

	ctx := context.Background()
	h := newAccessKeyHarness(t)
	minted := h.create(t, func() *generated.AccessKeyModel {
		m := generated.NewAccessKeyModel()
		m.DisplayName = types.StringValue("ci")
		return m
	}())

	impResp := &resource.ImportStateResponse{State: emptyAccessKeyState()}
	h.res.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: minted.Name.ValueString()}, impResp)
	require.False(t, impResp.Diagnostics.HasError(), impResp.Diagnostics)

	readResp := &resource.ReadResponse{State: impResp.State}
	h.res.Read(ctx, resource.ReadRequest{State: impResp.State}, readResp)
	require.False(t, readResp.Diagnostics.HasError(), readResp.Diagnostics)
	read := &generated.AccessKeyModel{}
	require.False(t, readResp.State.Get(ctx, read).HasError())
	assert.Equal(t, "ci", read.DisplayName.ValueString())
	assert.True(t, read.Key.IsNull())
}

// The plural data source lists minted rows without any once-only
// attribute: a list, like any read, never has one.
func TestAccessKeysDataSource_CarriesNoKey(t *testing.T) {

	ctx := context.Background()
	h := newAccessKeyHarness(t)
	for _, name := range []string{"a", "b"} {
		m := generated.NewAccessKeyModel()
		m.DisplayName = types.StringValue(name)
		h.create(t, m)
	}

	s := generated.AccessKeyListDataSourceSchema()
	require.False(t, s.ValidateImplementation(ctx).HasError())
	itemAttrs := s.Type().TerraformType(ctx).(tftypes.Object).AttributeTypes["access_keys"].(tftypes.List).ElementType.(tftypes.Object).AttributeTypes
	assert.NotContains(t, itemAttrs, "key")
	assert.NotContains(t, itemAttrs, "keepers")

	ds := generated.NewAccessKeysDataSource()
	cResp := &datasource.ConfigureResponse{}
	ds.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{
		ProviderData: &tfruntime.ProviderData{Clients: map[string]any{"petstore": h.fake}, Defaults: map[string]string{"store_id": "s1"}},
	}, cResp)
	require.False(t, cResp.Diagnostics.HasError(), cResp.Diagnostics)

	cfg := tfsdk.Config{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), map[string]tftypes.Value{
		"store_id":    tftypes.NewValue(tftypes.String, nil),
		"access_keys": tftypes.NewValue(s.Type().TerraformType(ctx).(tftypes.Object).AttributeTypes["access_keys"], nil),
	})}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	ds.Read(ctx, datasource.ReadRequest{Config: cfg}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	var out generated.AccessKeysModel
	require.False(t, resp.State.Get(ctx, &out).HasError())
	assert.Len(t, out.Items.Elements(), 2)
}

// planModifyMap runs a map attribute's plan modifiers over a change from
// prior to planned, the way Terraform would on an update.
func planModifyMap(t *testing.T, a schema.MapAttribute, prior, planned map[string]string) *planmodifier.MapResponse {

	ctx := context.Background()
	toMap := func(m map[string]string) types.Map {
		v, d := types.MapValueFrom(ctx, types.StringType, m)
		require.False(t, d.HasError())
		return v
	}

	req := planmodifier.MapRequest{
		StateValue:  toMap(prior),
		PlanValue:   toMap(planned),
		ConfigValue: toMap(planned),
		// Non-null prior state and plan are what make this an update,
		// not a create or a destroy.
		State: accessKeyState(t, generated.NewAccessKeyModel()),
		Plan:  accessKeyPlan(t, generated.NewAccessKeyModel()),
	}

	resp := &planmodifier.MapResponse{PlanValue: req.PlanValue}
	for _, pm := range a.PlanModifiers {
		pm.PlanModifyMap(ctx, req, resp)
	}
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	return resp
}
