package petstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	"github.com/activatedio/tfinfra/examples/petstore/generated"
)

func TestPetResourceSchema(t *testing.T) {

	s := generated.PetResourceSchema()

	diags := s.ValidateImplementation(context.Background())
	require.False(t, diags.HasError(), diags)

	name, ok := s.Attributes["name"]
	require.True(t, ok)
	assert.True(t, name.IsComputed())

	storeID, ok := s.Attributes["store_id"]
	require.True(t, ok)
	assert.True(t, storeID.IsOptional())

	displayName, ok := s.Attributes["display_name"]
	require.True(t, ok)
	assert.True(t, displayName.IsRequired())

	createTime, ok := s.Attributes["create_time"]
	require.True(t, ok)
	assert.True(t, createTime.IsComputed())

	for _, attr := range []string{"type", "age", "vaccinated", "weight", "tags", "labels"} {
		a, ok := s.Attributes[attr]
		require.True(t, ok, attr)
		assert.True(t, a.IsOptional(), attr)
	}

}

func TestPetModel_RoundTrip(t *testing.T) {

	type s struct {
		arrange func() *petstorev1.Pet
		assert  func(t *testing.T, m *generated.PetModel, back *petstorev1.Pet)
	}

	createTime := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	cases := map[string]s{
		"fully populated round trips": {
			arrange: func() *petstorev1.Pet {
				return &petstorev1.Pet{
					Name:        "stores/s1/pets/p1",
					DisplayName: "Rex",
					Type:        petstorev1.PetType_PET_TYPE_DOG,
					Age:         3,
					Vaccinated:  true,
					Weight:      12.5,
					Tags:        []string{"good", "boy"},
					Labels:      map[string]string{"team": "platform"},
					CreateTime:  timestamppb.New(createTime),
				}
			},
			assert: func(t *testing.T, m *generated.PetModel, _ *petstorev1.Pet) {
				assert.Equal(t, "stores/s1/pets/p1", m.Name.ValueString())
				assert.Equal(t, "Rex", m.DisplayName.ValueString())
				assert.Equal(t, "PET_TYPE_DOG", m.Type.ValueString())
				assert.Equal(t, int64(3), m.Age.ValueInt64())
				assert.True(t, m.Vaccinated.ValueBool())
				assert.InDelta(t, 12.5, m.Weight.ValueFloat64(), 0.0001)
				assert.Equal(t, "2026-08-15T12:00:00Z", m.CreateTime.ValueString())
				assert.True(t, m.StoreId.IsNull())
			},
		},
		"zero values read as null where proto3 allows": {
			arrange: func() *petstorev1.Pet {
				return &petstorev1.Pet{Name: "stores/s1/pets/p2", DisplayName: "Mimi"}
			},
			assert: func(t *testing.T, m *generated.PetModel, _ *petstorev1.Pet) {
				assert.True(t, m.Type.IsNull())
				assert.True(t, m.Tags.IsNull())
				assert.True(t, m.Labels.IsNull())
				assert.True(t, m.CreateTime.IsNull())
				assert.Equal(t, int64(0), m.Age.ValueInt64())
				assert.False(t, m.Vaccinated.ValueBool())
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			ctx := context.Background()
			in := v.arrange()

			m := &generated.PetModel{}
			diags := m.FromProto(ctx, in)
			require.False(t, diags.HasError(), diags)

			back, diags := m.ToProto(ctx)
			require.False(t, diags.HasError(), diags)
			assert.True(t, proto.Equal(in, back), "expected %v, got %v", in, back)

			v.assert(t, m, back)

		})
	}

}
