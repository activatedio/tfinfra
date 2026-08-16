package tf_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	gentf "github.com/activatedio/tfinfra/genlib/tf"
)

func petEntry() gentf.Entry {
	return gentf.Entry{Type: reflect.TypeFor[petstorev1.Pet]()}
}

func TestNormalizeFields(t *testing.T) {

	type s struct {
		arrange func() (gentf.Entry, gentf.Resource)
		assert  func(t *testing.T, got []gentf.Field)
	}

	cases := map[string]s{
		"kinds and behavior resolve from descriptor plus markers": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{
					Required:  []string{"display_name"},
					Immutable: []string{"type"},
					Computed:  []string{"create_time"},
					Sensitive: []string{"labels"},
					JSON:      []string{"config", "metadata"},
				}
			},
			assert: func(t *testing.T, got []gentf.Field) {

				byName := map[string]gentf.Field{}
				for _, f := range got {
					byName[f.ProtoName] = f
				}
				require.Len(t, got, 11)

				assert.Equal(t, gentf.FieldString, byName["name"].Kind)
				assert.True(t, byName["name"].Computed)
				assert.Equal(t, "Name", byName["name"].GoName)

				assert.True(t, byName["display_name"].Required)
				assert.Equal(t, "DisplayName", byName["display_name"].GoName)

				assert.Equal(t, gentf.FieldEnum, byName["type"].Kind)
				assert.True(t, byName["type"].Immutable)
				assert.Equal(t, []string{"PET_TYPE_UNSPECIFIED", "PET_TYPE_DOG", "PET_TYPE_CAT"}, byName["type"].EnumValues)

				assert.Equal(t, gentf.FieldInt64, byName["age"].Kind)
				assert.Equal(t, reflect.Int32, byName["age"].GoType.Kind())
				assert.Equal(t, gentf.FieldBool, byName["vaccinated"].Kind)
				assert.Equal(t, gentf.FieldFloat64, byName["weight"].Kind)
				assert.Equal(t, gentf.FieldStringList, byName["tags"].Kind)
				assert.Equal(t, gentf.FieldStringMap, byName["labels"].Kind)
				assert.True(t, byName["labels"].Sensitive)
				assert.Equal(t, gentf.FieldTimestamp, byName["create_time"].Kind)
				assert.True(t, byName["create_time"].Computed)
				assert.Equal(t, gentf.FieldAny, byName["config"].Kind)
				assert.Equal(t, gentf.FieldStruct, byName["metadata"].Kind)
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			e, r := v.arrange()
			v.assert(t, gentf.NormalizeFields(e, r))
		})
	}

}

func TestNormalizeFields_Panics(t *testing.T) {

	type s struct {
		arrange func() (gentf.Entry, gentf.Resource)
		assert  func(t *testing.T, f func())
	}

	cases := map[string]s{
		"unknown field reference lists valid names": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{Required: []string{"nope"}, JSON: []string{"config", "metadata"}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t,
					`Pet: Required references unknown field "nope" (fields: age, config, create_time, display_name, labels, metadata, name, tags, type, vaccinated, weight)`,
					f)
			},
		},
		"required and computed conflict": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{Required: []string{"create_time"}, Computed: []string{"create_time"}, JSON: []string{"config", "metadata"}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, "Pet.create_time: field cannot be both required and computed", f)
			},
		},
		"pending WriteOnly fails loudly": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{WriteOnly: []string{"display_name"}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, "Pet: WriteOnly fields are not yet supported", f)
			},
		},
		"JSON marker on a non-Any field": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{JSON: []string{"labels", "config", "metadata"}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, "Pet.labels: JSON marker applies only to google.protobuf.Any and Struct fields", f)
			},
		},
		"Any field without JSON marker": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, "Pet.config: google.protobuf.Any fields must be declared in Resource.JSON", f)
			},
		},
		"non-message type": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return gentf.Entry{Type: reflect.TypeFor[struct{ Name string }]()}, gentf.Resource{}
			},
			assert: func(t *testing.T, f func()) {
				assert.Panics(t, f)
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			e, r := v.arrange()
			v.assert(t, func() { gentf.NormalizeFields(e, r) })
		})
	}

}
