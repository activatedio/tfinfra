package petstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

func newPetRig(t *testing.T) *rig[generated.PetModel] {
	return newRig[generated.PetModel](t, map[string]string{"store_id": "s1"}, generated.NewPetResource(), generated.NewPetDataSource(),
		generated.PetResourceSchema(), generated.PetDataSourceSchema())
}

// vaccination builds one "vaccinations" element the way a configuration
// would arrive: an attribute left out is unknown in the plan.
func vaccination(vaccine, givenTime, validFor, route string, batches []string) generated.PetVaccinationsModel {

	str := func(s string) types.String {
		if s == "" {
			return types.StringUnknown()
		}
		return types.StringValue(s)
	}

	v := generated.PetVaccinationsModel{
		Vaccine:   str(vaccine),
		GivenTime: str(givenTime),
		ValidFor:  str(validFor),
		Route:     str(route),
		Batches:   types.ListUnknown(types.StringType),
	}
	if batches != nil {
		list, _ := types.ListValueFrom(context.Background(), types.StringType, batches)
		v.Batches = list
	}

	return v
}

func vaccinations(t *testing.T, items ...generated.PetVaccinationsModel) types.List {
	t.Helper()
	if items == nil {
		items = []generated.PetVaccinationsModel{}
	}
	list, diags := types.ListValueFrom(context.Background(), types.ObjectType{AttrTypes: generated.PetVaccinationsAttrTypes()}, items)
	require.False(t, diags.HasError(), diags)
	return list
}

func vaccinationsOf(t *testing.T, m *generated.PetModel) []generated.PetVaccinationsModel {
	t.Helper()
	var out []generated.PetVaccinationsModel
	require.False(t, m.Vaccinations.ElementsAs(context.Background(), &out, false).HasError())
	return out
}

func newPet(displayName string) *generated.PetModel {
	m := generated.NewPetModel()
	m.Name = types.StringUnknown()
	m.DisplayName = types.StringValue(displayName)
	return m
}

// A repeated message left out of the JSON list is a list-nested attribute:
// one object per element, reaching the API as messages and coming back
// through state as written.
func TestPetResource_RepeatedMessage(t *testing.T) {

	r := newPetRig(t)

	m := newPet("Rex")
	m.Vaccinations = vaccinations(t,
		vaccination("rabies", "2026-03-01T09:00:00Z", "8760h", "ROUTE_INJECTION", []string{"b1", "b2"}),
		vaccination("kennel cough", "", "1m30s", "ROUTE_ORAL", nil),
	)
	created := r.mustCreate(t, m)

	stored := r.fake.pets[created.GetName().ValueString()].GetVaccinations()
	require.Len(t, stored, 2)
	assert.Equal(t, "rabies", stored[0].GetVaccine())
	assert.Equal(t, time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), stored[0].GetGivenTime().AsTime())
	assert.Equal(t, 8760*time.Hour, stored[0].GetValidFor().AsDuration())
	assert.Equal(t, petstorev1.Route_ROUTE_INJECTION, stored[0].GetRoute())
	assert.Equal(t, []string{"b1", "b2"}, stored[0].GetBatches())
	assert.Equal(t, petstorev1.Route_ROUTE_ORAL, stored[1].GetRoute())

	got := vaccinationsOf(t, created)
	require.Len(t, got, 2)
	assert.Equal(t, "8760h", got[0].ValidFor.ValueString(), "a duration keeps its spelling per element")
	assert.Equal(t, "ROUTE_INJECTION", got[0].Route.ValueString(), "an explicit zero enum reads back as written")
	assert.Equal(t, "1m30s", got[1].ValidFor.ValueString())
	assert.True(t, got[1].GivenTime.IsNull(), "an unset child reads null, not unknown")
	assert.True(t, got[1].Batches.IsNull())

	// A refresh keeps it all, and an import, with nothing to compare
	// against, reads the protojson forms.
	assert.Equal(t, "8760h", vaccinationsOf(t, r.read(t, created))[0].ValidFor.ValueString())

	imported := generated.NewPetModel()
	imported.Name = created.Name
	fresh := vaccinationsOf(t, r.read(t, imported))
	assert.Equal(t, "31536000s", fresh[0].ValidFor.ValueString())
	assert.True(t, fresh[0].Route.IsNull(), "an import has no written zero to keep")

	// The whole list is one update-mask path.
	planned := *created
	planned.Vaccinations = vaccinations(t, vaccination("rabies", "2026-03-01T09:00:00Z", "8760h", "ROUTE_INJECTION", []string{"b1", "b2"}))
	updated := r.update(t, created, &planned)
	assert.Equal(t, []string{"vaccinations"}, r.fake.lastPatchPaths)
	assert.Len(t, vaccinationsOf(t, updated), 1)
	assert.Len(t, r.fake.pets[created.GetName().ValueString()].GetVaccinations(), 1)
}

// An element's diagnostic anchors to its position in the list.
func TestPetResource_RepeatedMessageDiagnosticPath(t *testing.T) {

	r := newPetRig(t)

	m := newPet("Rex")
	m.Vaccinations = vaccinations(t,
		vaccination("rabies", "2026-03-01T09:00:00Z", "", "", nil),
		vaccination("kennel cough", "yesterday", "", "", nil),
	)
	_, diags := r.create(t, m)

	require.True(t, diags.HasError())
	d, ok := diags.Errors()[0].(interface{ Path() path.Path })
	require.True(t, ok)
	assert.Equal(t, path.Root("vaccinations").AtListIndex(1).AtName("given_time"), d.Path())
	assert.Empty(t, r.fake.pets)
}

// No elements read as null, except where the practitioner wrote an empty
// list (or map, or JSON array): that reads back empty, which is what
// Terraform requires of an apply.
func TestPetResource_EmptyCollections(t *testing.T) {

	type s struct {
		arrange func(m *generated.PetModel)
		assert  func(t *testing.T, got *generated.PetModel)
	}

	cases := map[string]s{
		"written empty, read back empty": {
			arrange: func(m *generated.PetModel) {
				m.Vaccinations = vaccinations(t)
				m.Tags = types.ListValueMust(types.StringType, []attr.Value{})
				m.Labels = types.MapValueMust(types.StringType, map[string]attr.Value{})
				m.Notes = jsontypes.NewNormalizedValue("[ ]")
			},
			assert: func(t *testing.T, got *generated.PetModel) {
				assert.False(t, got.Vaccinations.IsNull())
				assert.Empty(t, got.Vaccinations.Elements())
				assert.False(t, got.Tags.IsNull())
				assert.Empty(t, got.Tags.Elements())
				assert.False(t, got.Labels.IsNull())
				assert.Equal(t, "[ ]", got.Notes.ValueString())
			},
		},
		"left unset, read back null": {
			arrange: func(m *generated.PetModel) {
				m.Vaccinations = types.ListUnknown(types.ObjectType{AttrTypes: generated.PetVaccinationsAttrTypes()})
				m.Tags = types.ListUnknown(types.StringType)
				m.Notes = jsontypes.NewNormalizedUnknown()
			},
			assert: func(t *testing.T, got *generated.PetModel) {
				assert.True(t, got.Vaccinations.IsNull())
				assert.True(t, got.Tags.IsNull())
				assert.True(t, got.Notes.IsNull())
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newPetRig(t)
			m := newPet("Rex")
			c.arrange(m)
			created := r.mustCreate(t, m)
			c.assert(t, created)
			c.assert(t, r.read(t, created))
		})
	}
}

// A repeated message in the JSON list is a JSON array of protojson
// elements: the lane for a message too deep for the typed one.
func TestPetResource_RepeatedMessageJSON(t *testing.T) {

	type s struct {
		notes  string
		assert func(t *testing.T, r *rig[generated.PetModel], got *generated.PetModel, detail string)
	}

	cases := map[string]s{
		"elements reach the API as messages and read back equal": {
			notes: `[{"text": "calm", "author": {"name": "Ana"}}, {"text": "eats fast"}]`,
			assert: func(t *testing.T, r *rig[generated.PetModel], got *generated.PetModel, detail string) {
				require.Empty(t, detail)
				stored := r.fake.pets[got.GetName().ValueString()].GetNotes()
				require.Len(t, stored, 2)
				assert.Equal(t, "Ana", stored[0].GetAuthor().GetName())
				assert.Equal(t, "eats fast", stored[1].GetText())
				eq, diags := got.Notes.StringSemanticEquals(context.Background(),
					jsontypes.NewNormalizedValue(`[{"text":"calm","author":{"name":"Ana"}},{"text":"eats fast"}]`))
				require.False(t, diags.HasError())
				assert.True(t, eq)
			},
		},
		"not an array": {
			notes: `{"text": "calm"}`,
			assert: func(t *testing.T, _ *rig[generated.PetModel], _ *generated.PetModel, detail string) {
				assert.Contains(t, detail, "not a JSON array")
			},
		},
		"an element that is not a Note": {
			notes: `[{"text": "calm"}, {"colour": "red"}]`,
			assert: func(t *testing.T, _ *rig[generated.PetModel], _ *generated.PetModel, detail string) {
				assert.Contains(t, detail, "element 1")
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newPetRig(t)
			m := newPet("Rex")
			m.Notes = jsontypes.NewNormalizedValue(c.notes)
			got, diags := r.create(t, m)
			detail := ""
			if diags.HasError() {
				d := diags.Errors()[0]
				p, ok := d.(interface{ Path() path.Path })
				require.True(t, ok)
				assert.Equal(t, path.Root("notes"), p.Path())
				detail = d.Detail()
			}
			c.assert(t, r, got, detail)
		})
	}
}

func TestPetResource_RepeatedMessageSchema(t *testing.T) {

	s := generated.PetResourceSchema()
	require.False(t, s.ValidateImplementation(context.Background()).HasError())

	a, ok := s.Attributes["vaccinations"].(schema.ListNestedAttribute)
	require.True(t, ok)
	assert.True(t, a.Optional)
	assert.True(t, a.Computed)
	route, ok := a.NestedObject.Attributes["route"].(schema.StringAttribute)
	require.True(t, ok)
	assert.True(t, route.Optional && route.Computed)
	assert.Empty(t, route.PlanModifiers, "children carry no plan modifiers")
	assert.Len(t, route.Validators, 1)

	notes, ok := s.Attributes["notes"].(schema.StringAttribute)
	require.True(t, ok)
	assert.Equal(t, jsontypes.NormalizedType{}, notes.CustomType)
	assert.Contains(t, notes.MarkdownDescription, "JSON array")
}
