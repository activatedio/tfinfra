package tf_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"

	"github.com/activatedio/tfinfra/pkg/tf"
)

func TestRequiresReplaceIfConfigured(t *testing.T) {

	present := tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})
	absent := tftypes.NewValue(tftypes.Object{}, nil)

	type s struct {
		state, plan tftypes.Value
		config      types.String
		prior       types.String
		want        bool
	}

	cases := map[string]s{
		"create replaces nothing": {
			state: absent, plan: present, config: types.StringValue("b"), prior: types.StringNull(),
		},
		"destroy replaces nothing": {
			state: present, plan: absent, config: types.StringNull(), prior: types.StringValue("a"),
		},
		"omitted keeps state": {
			state: present, plan: present, config: types.StringNull(), prior: types.StringValue("a"),
		},
		"configured and equal": {
			state: present, plan: present, config: types.StringValue("a"), prior: types.StringValue("a"),
		},
		"configured and different": {
			state: present, plan: present, config: types.StringValue("b"), prior: types.StringValue("a"), want: true,
		},
		"configured over a null state": {
			state: present, plan: present, config: types.StringValue("b"), prior: types.StringNull(), want: true,
		},
		"configured but unknown": {
			state: present, plan: present, config: types.StringUnknown(), prior: types.StringValue("a"), want: true,
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			resp := &planmodifier.StringResponse{PlanValue: v.config}
			tf.RequiresReplaceIfConfigured().PlanModifyString(context.Background(), planmodifier.StringRequest{
				ConfigValue: v.config,
				StateValue:  v.prior,
				PlanValue:   v.config,
				State:       tfsdk.State{Raw: v.state},
				Plan:        tfsdk.Plan{Raw: v.plan},
			}, resp)
			assert.Equal(t, v.want, resp.RequiresReplace)
		})
	}
}
