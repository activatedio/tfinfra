package petstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

// A google.protobuf.Duration is a string attribute. The practitioner writes
// it in whichever spelling reads best; state keeps that spelling for as long
// as the server agrees on the length, and a read with nothing to compare
// against — an import — takes the protojson form.
func TestPetResource_Duration(t *testing.T) {

	ctx := context.Background()
	h := newHarness(t, map[string]string{"store_id": "s1"})

	m := generated.NewPetModel()
	m.DisplayName = types.StringValue("Rex")
	m.GroomingInterval = types.StringValue("1m30s")

	created := h.create(t, m)

	assert.Equal(t, 90*time.Second, h.fake.pets[created.GetName().ValueString()].GetGroomingInterval().AsDuration())
	assert.Equal(t, "1m30s", created.GroomingInterval.ValueString(), "the configured spelling survives the round trip")

	// A refresh against the same state keeps it too.
	readResp := &resource.ReadResponse{State: h.state(t, created)}
	h.res.Read(ctx, resource.ReadRequest{State: h.state(t, created)}, readResp)
	require.False(t, readResp.Diagnostics.HasError(), readResp.Diagnostics)
	var refreshed types.String
	require.False(t, readResp.State.GetAttribute(ctx, path.Root("grooming_interval"), &refreshed).HasError())
	assert.Equal(t, "1m30s", refreshed.ValueString())

	// An import has no spelling to keep.
	imported := generated.NewPetModel()
	imported.Name = created.Name
	importResp := &resource.ReadResponse{State: h.state(t, imported)}
	h.res.Read(ctx, resource.ReadRequest{State: h.state(t, imported)}, importResp)
	require.False(t, importResp.Diagnostics.HasError(), importResp.Diagnostics)
	var canonical types.String
	require.False(t, importResp.State.GetAttribute(ctx, path.Root("grooming_interval"), &canonical).HasError())
	assert.Equal(t, "90s", canonical.ValueString())

	// A change of length is one patch path, and reads back as written.
	planModel := *created
	planModel.GroomingInterval = types.StringValue("2m")
	updResp := &resource.UpdateResponse{State: h.emptyState()}
	h.res.Update(ctx, resource.UpdateRequest{Plan: h.plan(t, &planModel), State: h.state(t, created)}, updResp)
	require.False(t, updResp.Diagnostics.HasError(), updResp.Diagnostics)
	assert.Equal(t, []string{"grooming_interval"}, h.fake.lastPatchPaths)
	var updated types.String
	require.False(t, updResp.State.GetAttribute(ctx, path.Root("grooming_interval"), &updated).HasError())
	assert.Equal(t, "2m", updated.ValueString())
}

func TestPetResource_DurationInvalid(t *testing.T) {

	m := generated.NewPetModel()
	m.GroomingInterval = types.StringValue("fortnightly")

	_, diags := m.ToProto(context.Background())

	require.True(t, diags.HasError())
	assert.Equal(t, "invalid duration", diags.Errors()[0].Summary())
}
