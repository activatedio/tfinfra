package petstore_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

// noDataSource stands in for the data source Breed does not declare, so the
// rig can configure the resource alone.
type noDataSource struct{}

func (noDataSource) Metadata(context.Context, datasource.MetadataRequest, *datasource.MetadataResponse) {
}
func (noDataSource) Schema(context.Context, datasource.SchemaRequest, *datasource.SchemaResponse) {}
func (noDataSource) Read(context.Context, datasource.ReadRequest, *datasource.ReadResponse)       {}
func (noDataSource) Configure(context.Context, datasource.ConfigureRequest, *datasource.ConfigureResponse) {
}

// Destroying a resource the API cannot delete forgets it: no error, a
// warning that names what stays, and the record left where it is.
func TestBreedResource_DeleteForgets(t *testing.T) {

	r := newRig[generated.BreedModel](t, nil, generated.NewBreedResource(), noDataSource{},
		generated.BreedResourceSchema(), dsschema.Schema{})

	m := generated.NewBreedModel()
	m.Name = types.StringUnknown()
	m.BreedId = types.StringValue("beagle")
	m.DisplayName = types.StringValue("Beagle")
	created := r.mustCreate(t, m)

	resp := &resource.DeleteResponse{State: r.state(t, created)}
	r.res.Delete(context.Background(), resource.DeleteRequest{State: r.state(t, created)}, resp)

	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	require.Len(t, resp.Diagnostics.Warnings(), 1)
	assert.Equal(t, "breed not deleted", resp.Diagnostics.Warnings()[0].Summary())
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Detail(), "the record stays")
	assert.Contains(t, r.fake.breeds, "breeds/beagle")
}

func TestBreedResource_DescriptionSaysItStays(t *testing.T) {
	assert.Contains(t, generated.BreedResourceSchema().MarkdownDescription, "destroying it removes it from Terraform state")
}
