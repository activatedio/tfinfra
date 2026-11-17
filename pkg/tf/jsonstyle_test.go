package tf_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tf "github.com/activatedio/tfinfra/pkg/tf"
)

func TestSortedJSON(t *testing.T) {

	got, err := tf.SortedJSON(`{ "b": {"z": 1, "a": 0.50}, "a": ["<x>"] }`)
	require.NoError(t, err)
	// The same escapes as jsonencode: <, > and & as \u003c, \u003e and \u0026.
	assert.Equal(t, "{\"a\":[\"\\u003cx\\u003e\"],\"b\":{\"a\":0.50,\"z\":1}}", got) //nolint:testifylint // the spelling is under test
}

func jsonState(t *testing.T, config *string) tfsdk.State {

	t.Helper()
	s := schema.Schema{Attributes: map[string]schema.Attribute{
		"config": schema.StringAttribute{Optional: true, CustomType: jsontypes.NormalizedType{}},
		"name":   schema.StringAttribute{Computed: true},
	}}
	typ := s.Type().TerraformType(context.Background())
	var v tftypes.Value
	if config == nil {
		v = tftypes.NewValue(tftypes.String, nil)
	} else {
		v = tftypes.NewValue(tftypes.String, *config)
	}
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(typ, map[string]tftypes.Value{
		"config": v,
		"name":   tftypes.NewValue(tftypes.String, "x"),
	})}
}

func configOf(t *testing.T, s tfsdk.State) string {

	t.Helper()
	var v jsontypes.Normalized
	require.False(t, s.GetAttribute(context.Background(), path.Root("config"), &v).HasError())
	return v.ValueString()
}

func ptr(s string) *string { return &s }

func TestKeepJSONStyle(t *testing.T) {

	cases := map[string]struct{ prior, read, want string }{
		// The server changed it out of band: a sorted prior keeps the new value sorted.
		"sorted prior": {`{"a":1,"b":{"x":1,"y":2}}`, `{"b":{"y":3,"x":1},"a":1}`, `{"a":1,"b":{"x":1,"y":3}}`},
		// Any other spelling keeps protojson's field order.
		"field order prior": {`{"b":1,"a":1}`, `{"b":2,"a":1}`, `{"b":2,"a":1}`},
		"heredoc prior":     {"{\n  \"a\": 1\n}", `{"b":2,"a":1}`, `{"b":2,"a":1}`},
		// JSON-equal: left for the framework, which keeps the prior value.
		"equal": {`{"a":1,"b":2}`, `{"b":2,"a":1}`, `{"b":2,"a":1}`},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			state := jsonState(t, ptr(c.read))
			require.False(t, tf.KeepJSONStyle(context.Background(), jsonState(t, ptr(c.prior)), &state).HasError())
			assert.Equal(t, c.want, configOf(t, state)) // the spelling is under test, not only the JSON
		})
	}

	t.Run("imported", func(t *testing.T) {
		state := jsonState(t, ptr(`{"b":2,"a":1}`))
		require.False(t, tf.KeepJSONStyle(context.Background(), jsonState(t, nil), &state).HasError())
		assert.Equal(t, `{"b":2,"a":1}`, configOf(t, state)) // the spelling is under test
	})
}
