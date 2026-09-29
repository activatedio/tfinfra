package tf_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"

	"github.com/activatedio/tfinfra/pkg/tf"
)

func TestEnumValue(t *testing.T) {

	type s struct {
		prior  types.String
		number int32
		name   string
		want   types.String
	}

	cases := map[string]s{
		"non-zero reads as its name": {
			prior: types.StringNull(), number: 1, name: "GRPC", want: types.StringValue("GRPC"),
		},
		"non-zero replaces a different prior": {
			prior: types.StringValue("REST"), number: 1, name: "GRPC", want: types.StringValue("GRPC"),
		},
		"zero written explicitly is kept": {
			prior: types.StringValue("REST"), number: 0, name: "REST", want: types.StringValue("REST"),
		},
		"zero with no prior reads as null": {
			prior: types.StringNull(), number: 0, name: "REST", want: types.StringNull(),
		},
		"zero with an unknown prior reads as null": {
			prior: types.StringUnknown(), number: 0, name: "REST", want: types.StringNull(),
		},
		"zero after a different prior reads as null": {
			prior: types.StringValue("GRPC"), number: 0, name: "REST", want: types.StringNull(),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, v.want, tf.EnumValue(v.prior, v.number, v.name))
		})
	}
}
