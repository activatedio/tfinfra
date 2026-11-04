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
					JSON:      []string{"config", "metadata", "notes"},
				}
			},
			assert: func(t *testing.T, got []gentf.Field) {

				byName := map[string]gentf.Field{}
				for _, f := range got {
					byName[f.ProtoName] = f
				}
				require.Len(t, got, 19)

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
				assert.Equal(t, gentf.FieldDuration, byName["grooming_interval"].Kind)
				assert.Equal(t, gentf.FieldAny, byName["config"].Kind)
				assert.Equal(t, gentf.FieldStruct, byName["metadata"].Kind)
			},
		},
		"a singular message left out of the JSON list becomes a nested attribute": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{JSON: []string{"config", "metadata", "notes"}}
			},
			assert: func(t *testing.T, got []gentf.Field) {

				byName := map[string]gentf.Field{}
				for _, f := range got {
					byName[f.ProtoName] = f
				}

				feeding := byName["feeding"]
				assert.Equal(t, gentf.FieldNestedMessage, feeding.Kind)
				require.Len(t, feeding.Nested, 6)

				// Nested fields keep proto field-number order and carry the
				// same kinds they would at the top level.
				names := make([]string, 0, len(feeding.Nested))
				for _, n := range feeding.Nested {
					names = append(names, n.ProtoName)
				}
				assert.Equal(t, []string{"schedule", "portions", "foods", "notes", "bowl", "interval"}, names)
				assert.Equal(t, gentf.FieldString, feeding.Nested[0].Kind)
				assert.Equal(t, gentf.FieldInt64, feeding.Nested[1].Kind)
				assert.Equal(t, gentf.FieldStringList, feeding.Nested[2].Kind)
				assert.Equal(t, gentf.FieldStringMap, feeding.Nested[3].Kind)
				assert.Equal(t, gentf.FieldEnum, feeding.Nested[4].Kind)
				assert.Equal(t, gentf.FieldDuration, feeding.Nested[5].Kind)
				assert.Equal(t, "Schedule", feeding.Nested[0].GoName)

				// A JSON-marked message on the same entry stays on the
				// protojson lane; the two coexist.
				assert.Equal(t, gentf.FieldAny, byName["config"].Kind)
			},
		},
		"InputOnly resolves alongside Immutable": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{
					Immutable: []string{"intake_code"},
					InputOnly: []string{"intake_code", "intake_age_days"},
					JSON:      []string{"config", "metadata", "notes"},
				}
			},
			assert: func(t *testing.T, got []gentf.Field) {

				byName := map[string]gentf.Field{}
				for _, f := range got {
					byName[f.ProtoName] = f
				}

				assert.True(t, byName["intake_code"].InputOnly)
				assert.True(t, byName["intake_code"].Immutable)
				// Never computed: that is the whole point of the marker.
				assert.False(t, byName["intake_code"].Computed)

				assert.True(t, byName["intake_age_days"].InputOnly)
				assert.False(t, byName["intake_age_days"].Immutable)

				assert.False(t, byName["display_name"].InputOnly)
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
				return petEntry(), gentf.Resource{Required: []string{"nope"}, JSON: []string{"config", "metadata", "notes"}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t,
					`Pet: Required references unknown field "nope" (fields: age, buddy_id, config, create_time, display_name, feeding, grooming_interval, intake_age_days, intake_code, labels, metadata, name, notes, tags, type, update_time, vaccinated, vaccinations, weight)`,
					f)
			},
		},
		"required and computed conflict": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{Required: []string{"create_time"}, Computed: []string{"create_time"}, JSON: []string{"config", "metadata", "notes"}}
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
				return petEntry(), gentf.Resource{JSON: []string{"labels", "config", "metadata", "notes"}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, "Pet.labels: JSON marker applies only to message-typed fields", f)
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
		"input-only and computed conflict": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{
					InputOnly: []string{"create_time"},
					Computed:  []string{"create_time"},
					JSON:      []string{"config", "metadata", "notes"},
				}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t,
					"Pet.create_time: field cannot be both input-only and computed; the server never returns an input-only field",
					f)
			},
		},
		"message nested more than one level": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				// Kennel.collar is a nested attribute; CollarConfig.buckle
				// inside it is one level too deep.
				return gentf.Entry{Type: reflect.TypeFor[petstorev1.Kennel]()}, gentf.Resource{}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t,
					"CollarConfig.buckle: message-typed field petstore.v1.Buckle sits more than one level deep; typed nested attributes nest one level, so declare the outer field in the JSON list instead",
					f)
			},
		},
		"a reference to an unknown field": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{JSON: []string{"config", "metadata", "notes"},
					References: map[string]gentf.Reference{"nope_id": {Target: "pet", Prefix: "p"}}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Pet: References references unknown field "nope_id"`, f)
			},
		},
		"a reference that is not a string": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{JSON: []string{"config", "metadata", "notes"},
					References: map[string]gentf.Reference{"age": {Target: "pet", Prefix: "p"}}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Pet.age: a reference must be a string field`, f)
			},
		},
		"a computed reference": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{JSON: []string{"config", "metadata", "notes"}, Computed: []string{"buddy_id"},
					References: map[string]gentf.Reference{"buddy_id": {Target: "pet", Prefix: "p"}}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Pet.buddy_id: a reference is an input; it cannot be computed`, f)
			},
		},
		"a reference without a target": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{JSON: []string{"config", "metadata", "notes"},
					References: map[string]gentf.Reference{"buddy_id": {Prefix: "p"}}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Pet.buddy_id: a Reference needs a Target`, f)
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

func TestNormalizeConfigFields_Panics(t *testing.T) {

	collar := gentf.Entry{Type: reflect.TypeFor[petstorev1.CollarConfig]()}

	cases := map[string]struct {
		cds  gentf.ConfigDataSource
		want string
	}{
		"values on an unknown field": {
			cds:  gentf.ConfigDataSource{Values: map[string]gentf.Values{"nope": {Allowed: []string{"a"}}}},
			want: `CollarConfig: Values references unknown field "nope"`,
		},
		"values on a non-string field": {
			cds:  gentf.ConfigDataSource{Values: map[string]gentf.Values{"size": {Allowed: []string{"1"}}}},
			want: "CollarConfig.size: Values apply only to a string field",
		},
		"values with nothing allowed": {
			cds:  gentf.ConfigDataSource{Values: map[string]gentf.Values{"color": {}}},
			want: "CollarConfig.color: Values needs at least one allowed value",
		},
		"an alias for a value not allowed": {
			cds:  gentf.ConfigDataSource{Values: map[string]gentf.Values{"color": {Allowed: []string{"red"}, Aliases: map[string]string{"scarlet": "crimson"}}}},
			want: `CollarConfig.color: alias "scarlet" stands for "crimson", which is not an allowed value`,
		},
		"an alias that is itself allowed": {
			cds:  gentf.ConfigDataSource{Values: map[string]gentf.Values{"color": {Allowed: []string{"red", "blue"}, Aliases: map[string]string{"blue": "red"}}}},
			want: `CollarConfig.color: alias "blue" is itself an allowed value`,
		},
		"values on a reference": {
			cds: gentf.ConfigDataSource{
				References: map[string]gentf.Reference{"tag_key_id": {Target: "access_key", Prefix: "k"}},
				Values:     map[string]gentf.Values{"tag_key_id": {Allowed: []string{"k-1"}}},
			},
			want: "CollarConfig.tag_key_id: a reference is validated by prefix; it takes no Values",
		},
		"omit of an unknown field": {
			cds:  gentf.ConfigDataSource{Omit: []string{"engraving.nope"}},
			want: `CollarConfig: Omit references unknown field "engraving.nope"`,
		},
		"an omitted field made required": {
			cds:  gentf.ConfigDataSource{Omit: []string{"color"}, Required: []string{"color"}},
			want: `CollarConfig: Required references unknown field "color"`,
		},
		"an omitted nested field described": {
			cds:  gentf.ConfigDataSource{Omit: []string{"engraving.font"}, Descriptions: map[string]string{"engraving.font": "x"}},
			want: `CollarConfig: Descriptions references unknown field "engraving.font"`,
		},
		"a description for an unknown nested field": {
			cds:  gentf.ConfigDataSource{Descriptions: map[string]string{"engraving.nope": "x"}},
			want: `CollarConfig: Descriptions references unknown field "engraving.nope"`,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.PanicsWithValue(t, c.want, func() { gentf.NormalizeConfigFields(collar, c.cds) })
		})
	}
}

// TestNormalizeConfigFields_Omit: an omitted field is gone from the fields
// every part of the data source is generated from, at the top level and
// inside a nested message, whose other fields stay.
func TestNormalizeConfigFields_Omit(t *testing.T) {

	collar := gentf.Entry{Type: reflect.TypeFor[petstorev1.CollarConfig]()}

	fields := gentf.NormalizeConfigFields(collar, gentf.ConfigDataSource{Omit: []string{"tag_key_id", "engraving.font"}})

	names := func(fs []gentf.Field) []string {
		out := make([]string, 0, len(fs))
		for _, f := range fs {
			out = append(out, f.ProtoName)
		}
		return out
	}

	assert.NotContains(t, names(fields), "tag_key_id")
	assert.Contains(t, names(fields), "color")

	for _, f := range fields {
		if f.ProtoName == "engraving" {
			assert.ElementsMatch(t, []string{"text", "lines"}, names(f.Nested))
			return
		}
	}
	t.Fatal("engraving was omitted with its field")
}
