package tf_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	gentf "github.com/activatedio/tfinfra/genlib/tf"
	tf "github.com/activatedio/tfinfra/pkg/tf"
)

func byProtoName(fields []gentf.Field) map[string]gentf.Field {
	out := map[string]gentf.Field{}
	for _, f := range fields {
		out[f.ProtoName] = f
	}
	return out
}

var (
	shelterEntry = gentf.Entry{Type: reflect.TypeFor[petstorev1.Shelter]()}
	runEntry     = gentf.Entry{Type: reflect.TypeFor[petstorev1.Run]()}
	scopeShelter = tf.NewScope("shelters")
)

func TestNormalizeFields_IDFieldAndParentID(t *testing.T) {

	type s struct {
		arrange func() (gentf.Entry, gentf.Resource)
		assert  func(t *testing.T, got map[string]gentf.Field)
	}

	cases := map[string]s{
		"an id field is a required, replace-on-change input": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return shelterEntry, gentf.Resource{IDField: "shelter_id"}
			},
			assert: func(t *testing.T, got map[string]gentf.Field) {
				f := got["shelter_id"]
				assert.True(t, f.ID)
				assert.True(t, f.Required)
				assert.True(t, f.Immutable)
				assert.False(t, f.Computed)
			},
		},
		"an id field listed in Computed is an optional one the server may mint": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return runEntry, gentf.Resource{Scope: scopeShelter, IDField: "run_id", Computed: []string{"run_id"}}
			},
			assert: func(t *testing.T, got map[string]gentf.Field) {
				f := got["run_id"]
				assert.True(t, f.ID)
				assert.False(t, f.Required)
				assert.False(t, f.Computed, "Optional+Computed is the default shape, not the computed-only one")
				assert.True(t, f.Immutable)
			},
		},
		"a field named like a scope identifier carries it": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return runEntry, gentf.Resource{Scope: scopeShelter, IDField: "run_id", Computed: []string{"shelter_id"}}
			},
			assert: func(t *testing.T, got map[string]gentf.Field) {
				assert.True(t, got["shelter_id"].ParentID)
				assert.False(t, got["shelter_id"].Computed)
				assert.False(t, got["run_id"].ParentID)
			},
		},
		"without the scope, it is an ordinary field": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return runEntry, gentf.Resource{IDField: "run_id"}
			},
			assert: func(t *testing.T, got map[string]gentf.Field) {
				assert.False(t, got["shelter_id"].ParentID)
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			e, r := v.arrange()
			v.assert(t, byProtoName(gentf.NormalizeFields(e, r)))
		})
	}
}

func TestNormalizeFields_IDFieldAndParentIDPanics(t *testing.T) {

	type s struct {
		arrange func() (gentf.Entry, gentf.Resource)
		assert  func(t *testing.T, f func())
	}

	cases := map[string]s{
		"an unknown id field": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return shelterEntry, gentf.Resource{IDField: "nope"}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Shelter: IDField references unknown field "nope"`, f)
			},
		},
		"IDField with CallerNamed": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return shelterEntry, gentf.Resource{IDField: "shelter_id", CallerNamed: true}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Shelter: IDField and CallerNamed are exclusive; IDField already makes the id the caller's`, f)
			},
		},
		"name as the id field": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return shelterEntry, gentf.Resource{IDField: "name"}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Shelter: IDField cannot be "name"; that is CallerNamed`, f)
			},
		},
		"a non-string id field": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return runEntry, gentf.Resource{IDField: "length_m"}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Run.length_m: IDField must be a string field`, f)
			},
		},
		"an input-only id field": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return shelterEntry, gentf.Resource{IDField: "shelter_id", InputOnly: []string{"shelter_id"}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Shelter.shelter_id: IDField cannot be input-only or sensitive; it is read back from every name`, f)
			},
		},
		"the id field as a parent identifier": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return runEntry, gentf.Resource{Scope: scopeShelter, IDField: "shelter_id"}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Run.shelter_id: the id field cannot also be a parent identifier`, f)
			},
		},
		"a carried parent identifier with behavior": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return runEntry, gentf.Resource{Scope: scopeShelter, IDField: "run_id", Required: []string{"shelter_id"}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Run.shelter_id: carries the parent identifier, so it takes no behavior but Computed; the parent path sets it`, f)
			},
		},
		"a carried parent identifier with a field reference": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return runEntry, gentf.Resource{Scope: scopeShelter, IDField: "run_id",
					References: map[string]gentf.Reference{"shelter_id": {Target: "shelter"}}}
			},
			assert: func(t *testing.T, f func()) {
				assert.PanicsWithValue(t, `Run.shelter_id: carries the parent identifier; validate it through Spec.ScopeReferences instead`, f)
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

func TestNormalizeFields_RepeatedMessages(t *testing.T) {

	type s struct {
		arrange func() (gentf.Entry, gentf.Resource)
		assert  func(t *testing.T, f func() []gentf.Field)
	}

	cases := map[string]s{
		"a repeated message left out of JSON is list-nested over its fields": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{JSON: []string{"config", "metadata", "notes"}}
			},
			assert: func(t *testing.T, f func() []gentf.Field) {
				got := byProtoName(f())
				v := got["vaccinations"]
				assert.Equal(t, gentf.FieldRepeatedMessage, v.Kind)
				require.Len(t, v.Nested, 5)
				nested := byProtoName(v.Nested)
				assert.Equal(t, gentf.FieldTimestamp, nested["given_time"].Kind)
				assert.Equal(t, gentf.FieldDuration, nested["valid_for"].Kind)
				assert.Equal(t, gentf.FieldEnum, nested["route"].Kind)
				assert.Equal(t, gentf.FieldStringList, nested["batches"].Kind)
				assert.Equal(t, gentf.FieldJSONList, got["notes"].Kind)
			},
		},
		"a repeated message holding a message must take the JSON lane": {
			arrange: func() (gentf.Entry, gentf.Resource) {
				return petEntry(), gentf.Resource{JSON: []string{"config", "metadata"}}
			},
			assert: func(t *testing.T, f func() []gentf.Field) {
				assert.PanicsWithValue(t, `Note.author: message-typed field petstore.v1.Author sits more than one level deep; typed nested attributes nest one level, so declare the outer field in the JSON list instead`,
					func() { f() })
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			e, r := v.arrange()
			v.assert(t, func() []gentf.Field { return gentf.NormalizeFields(e, r) })
		})
	}
}
