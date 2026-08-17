package petstore_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	"github.com/activatedio/tfinfra/examples/petstore/generated"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
)

const assocPet = "stores/s-1/pets/p1"

// assocHarness wires the generated association resource to the fake client
// the way a provider's Configure would.
type assocHarness struct {
	fake   *fakePetStoreClient
	res    resource.Resource
	schema fwschema.Schema
}

func newAssocHarness(t *testing.T) *assocHarness {

	ctx := context.Background()
	fake := newFakePetStoreClient()
	fake.pets[assocPet] = &petstorev1.Pet{Name: assocPet, DisplayName: "Rex"}

	res := generated.NewPetToysResource()
	cResp := &resource.ConfigureResponse{}
	res.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{
		ProviderData: &tfruntime.ProviderData{Clients: map[string]any{"petstore": fake}},
	}, cResp)
	require.False(t, cResp.Diagnostics.HasError(), cResp.Diagnostics)

	sResp := &resource.SchemaResponse{}
	res.Schema(ctx, resource.SchemaRequest{}, sResp)
	require.False(t, sResp.Diagnostics.HasError(), sResp.Diagnostics)

	return &assocHarness{fake: fake, res: res, schema: sResp.Schema}
}

// object builds the resource's whole-object value.
func (h *assocHarness) object(pet string, toys []string) tftypes.Value {

	ctx := context.Background()
	typ := h.schema.Type().TerraformType(ctx).(tftypes.Object)

	elems := make([]tftypes.Value, len(toys))
	for i, s := range toys {
		elems[i] = tftypes.NewValue(tftypes.String, s)
	}

	return tftypes.NewValue(typ, map[string]tftypes.Value{
		"pet":  tftypes.NewValue(tftypes.String, pet),
		"toys": tftypes.NewValue(typ.AttributeTypes["toys"], elems),
	})
}

func (h *assocHarness) nullState(ctx context.Context) tfsdk.State {
	return tfsdk.State{Schema: h.schema, Raw: tftypes.NewValue(h.schema.Type().TerraformType(ctx), nil)}
}

func (h *assocHarness) stateToys(t *testing.T, s tfsdk.State) []string {
	var toys []string
	diags := s.GetAttribute(context.Background(), path.Root("toys"), &toys)
	require.False(t, diags.HasError(), diags)
	return toys
}

func TestPetToysResource_Lifecycle(t *testing.T) {

	ctx := context.Background()
	h := newAssocHarness(t)

	// A member associated out of band before Terraform takes ownership.
	h.fake.toys = map[string]map[string]bool{assocPet: {"stores/s-1/toys/unmanaged": true}}

	desired := []string{"stores/s-1/toys/t1", "stores/s-1/toys/t2"}
	cResp := &resource.CreateResponse{State: h.nullState(ctx)}
	h.res.Create(ctx, resource.CreateRequest{
		Plan: tfsdk.Plan{Schema: h.schema, Raw: h.object(assocPet, desired)},
	}, cResp)
	require.False(t, cResp.Diagnostics.HasError(), cResp.Diagnostics)

	// Authoritative create: the unmanaged member is removed in the same call.
	assert.Equal(t, desired, h.fake.petToys(assocPet))
	assert.Equal(t, desired, h.fake.lastAssociateSet)
	assert.Equal(t, []string{"stores/s-1/toys/unmanaged"}, h.fake.lastAssociateRemove)
	assert.Equal(t, desired, h.stateToys(t, cResp.State))

	// Read reflects out-of-band drift (and walks the one-item pages).
	h.fake.toys[assocPet]["stores/s-1/toys/t3"] = true
	delete(h.fake.toys[assocPet], "stores/s-1/toys/t2")
	rResp := &resource.ReadResponse{State: cResp.State}
	h.res.Read(ctx, resource.ReadRequest{State: cResp.State}, rResp)
	require.False(t, rResp.Diagnostics.HasError(), rResp.Diagnostics)
	assert.ElementsMatch(t, []string{"stores/s-1/toys/t1", "stores/s-1/toys/t3"}, h.stateToys(t, rResp.State))

	// Update reconciles back to the plan.
	uResp := &resource.UpdateResponse{State: h.nullState(ctx)}
	h.res.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: h.schema, Raw: h.object(assocPet, []string{"stores/s-1/toys/t1"})},
		State: rResp.State,
	}, uResp)
	require.False(t, uResp.Diagnostics.HasError(), uResp.Diagnostics)
	assert.Equal(t, []string{"stores/s-1/toys/t1"}, h.fake.petToys(assocPet))

	// Delete removes every remaining member.
	dResp := &resource.DeleteResponse{}
	h.res.Delete(ctx, resource.DeleteRequest{State: uResp.State}, dResp)
	require.False(t, dResp.Diagnostics.HasError(), dResp.Diagnostics)
	assert.Empty(t, h.fake.petToys(assocPet))
}

func TestPetToysResource_EntityGoneOnRead(t *testing.T) {

	ctx := context.Background()
	h := newAssocHarness(t)

	state := tfsdk.State{Schema: h.schema, Raw: h.object("stores/s-1/pets/gone", nil)}
	rResp := &resource.ReadResponse{State: state}
	h.res.Read(ctx, resource.ReadRequest{State: state}, rResp)

	require.False(t, rResp.Diagnostics.HasError(), rResp.Diagnostics)
	assert.True(t, rResp.State.Raw.IsNull(), "state should be removed when the entity is gone")
}

func TestPetToysResource_ImportState(t *testing.T) {

	ctx := context.Background()
	h := newAssocHarness(t)

	cases := map[string]struct {
		id      string
		wantErr bool
	}{
		"full name imports":     {id: assocPet},
		"wrong collection":      {id: "stores/s-1/toys/t1", wantErr: true},
		"short id is rejected":  {id: "p1", wantErr: true},
		"wrong hierarchy depth": {id: "pets/p1", wantErr: true},
	}

	for n, c := range cases {
		t.Run(n, func(t *testing.T) {
			resp := &resource.ImportStateResponse{State: h.nullState(ctx)}
			h.res.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: c.id}, resp)

			if c.wantErr {
				assert.True(t, resp.Diagnostics.HasError())
				return
			}
			require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
			var pet string
			diags := resp.State.GetAttribute(ctx, path.Root("pet"), &pet)
			require.False(t, diags.HasError(), diags)
			assert.Equal(t, c.id, pet)
		})
	}
}
