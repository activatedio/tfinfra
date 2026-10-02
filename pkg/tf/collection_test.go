package tf_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	tf "github.com/activatedio/tfinfra/pkg/tf"
)

func TestKeepEmpty(t *testing.T) {

	type s struct {
		arrange func() attr.Value
		assert  func(t *testing.T, got bool)
	}

	keep := func(t *testing.T, got bool) { assert.True(t, got) }
	drop := func(t *testing.T, got bool) { assert.False(t, got) }

	cases := map[string]s{
		"an empty list":     {arrange: func() attr.Value { return types.ListValueMust(types.StringType, []attr.Value{}) }, assert: keep},
		"an empty map":      {arrange: func() attr.Value { return types.MapValueMust(types.StringType, map[string]attr.Value{}) }, assert: keep},
		"a null list":       {arrange: func() attr.Value { return types.ListNull(types.StringType) }, assert: drop},
		"an unknown list":   {arrange: func() attr.Value { return types.ListUnknown(types.StringType) }, assert: drop},
		"a zero-value list": {arrange: func() attr.Value { return types.List{} }, assert: drop},
		"a non-empty list": {
			arrange: func() attr.Value { return types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a")}) },
			assert:  drop,
		},
		"a scalar": {arrange: func() attr.Value { return types.StringValue("") }, assert: drop},
		"nil":      {arrange: func() attr.Value { return nil }, assert: drop},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			v.assert(t, tf.KeepEmpty(v.arrange()))
		})
	}
}

func TestJSONList(t *testing.T) {

	type s struct {
		arrange func() (jsontypes.Normalized, []*structpb.Struct)
		assert  func(t *testing.T, got jsontypes.Normalized, err error)
	}

	cases := map[string]s{
		"elements encode as a JSON array": {
			arrange: func() (jsontypes.Normalized, []*structpb.Struct) {
				a, _ := structpb.NewStruct(map[string]any{"text": "calm"})
				b, _ := structpb.NewStruct(map[string]any{"text": "fast"})
				return jsontypes.NewNormalizedNull(), []*structpb.Struct{a, b}
			},
			assert: func(t *testing.T, got jsontypes.Normalized, err error) {
				require.NoError(t, err)
				eq, diags := got.StringSemanticEquals(t.Context(), jsontypes.NewNormalizedValue(`[{"text":"calm"},{"text":"fast"}]`))
				require.False(t, diags.HasError())
				assert.True(t, eq)
			},
		},
		"no elements read null": {
			arrange: func() (jsontypes.Normalized, []*structpb.Struct) {
				return jsontypes.NewNormalizedValue(`[{"text":"calm"}]`), nil
			},
			assert: func(t *testing.T, got jsontypes.Normalized, err error) {
				require.NoError(t, err)
				assert.True(t, got.IsNull())
			},
		},
		"no elements keep an empty array as written": {
			arrange: func() (jsontypes.Normalized, []*structpb.Struct) {
				return jsontypes.NewNormalizedValue("[ ]"), nil
			},
			assert: func(t *testing.T, got jsontypes.Normalized, err error) {
				require.NoError(t, err)
				assert.Equal(t, "[ ]", got.ValueString())
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			got, err := tf.JSONListValue(v.arrange())
			v.assert(t, got, err)
		})
	}
}

func TestJSONListToProto(t *testing.T) {

	type s struct {
		value  string
		assert func(t *testing.T, got []*structpb.Struct, err error)
	}

	cases := map[string]s{
		"an array of messages": {
			value: `[{"a": 1}, {"b": "x"}]`,
			assert: func(t *testing.T, got []*structpb.Struct, err error) {
				require.NoError(t, err)
				require.Len(t, got, 2)
				assert.Equal(t, "x", got[1].GetFields()["b"].GetStringValue())
			},
		},
		"an empty array": {
			value: `[]`,
			assert: func(t *testing.T, got []*structpb.Struct, err error) {
				require.NoError(t, err)
				assert.Empty(t, got)
			},
		},
		"not an array": {
			value: `{"a": 1}`,
			assert: func(t *testing.T, _ []*structpb.Struct, err error) {
				assert.ErrorContains(t, err, "not a JSON array")
			},
		},
		"an element that is not the message": {
			value: `[{"a": 1}, 3]`,
			assert: func(t *testing.T, _ []*structpb.Struct, err error) {
				assert.ErrorContains(t, err, "element 1")
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			got, err := tf.JSONListToProto(v.value, func() *structpb.Struct { return &structpb.Struct{} })
			v.assert(t, got, err)
		})
	}
}
