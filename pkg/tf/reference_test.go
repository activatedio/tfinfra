package tf

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestReferenceID(t *testing.T) {

	cases := []struct {
		name   string
		value  types.String
		errors []string
	}{
		{name: "the prefix", value: types.StringValue("st-01")},
		{name: "null", value: types.StringNull()},
		{name: "unknown", value: types.StringUnknown()},
		{name: "empty, an unset optional reference", value: types.StringValue("")},
		{
			name:   "a full name",
			value:  types.StringValue("stores/st-01"),
			errors: []string{`"stores/st-01" is a resource name, not an id`, "store_id attribute", "petstore_store.<name>.store_id"},
		},
		{
			name:   "another type's id",
			value:  types.StringValue("p-01"),
			errors: []string{`"p-01" is not one`, `starts with "st-"`},
		},
		{
			name:   "a longer prefix sharing the first letters",
			value:  types.StringValue("stx-01"),
			errors: []string{`"stx-01" is not one`},
		},
		{
			name:   "the prefix alone",
			value:  types.StringValue("st-"),
			errors: []string{`"st-" is not one`},
		},
	}

	v := ReferenceID("st", "store", "petstore_store.<name>.store_id")

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			resp := &validator.StringResponse{}
			v.ValidateString(context.Background(), validator.StringRequest{
				Path:        path.Root("store_id"),
				ConfigValue: c.value,
			}, resp)

			if len(c.errors) == 0 {
				if resp.Diagnostics.HasError() {
					t.Fatalf("unexpected error: %v", resp.Diagnostics)
				}
				return
			}

			if resp.Diagnostics.ErrorsCount() != 1 {
				t.Fatalf("want one error, got %v", resp.Diagnostics)
			}
			detail := resp.Diagnostics.Errors()[0].Detail()
			if !strings.HasPrefix(detail, "store_id takes the store's id") {
				t.Errorf("detail does not name the attribute: %s", detail)
			}
			for _, want := range c.errors {
				if !strings.Contains(detail, want) {
					t.Errorf("detail %q does not contain %q", detail, want)
				}
			}
		})
	}
}

func TestReferenceIDWithoutExample(t *testing.T) {

	resp := &validator.StringResponse{}
	ReferenceID("ac", "access_condition", "").ValidateString(context.Background(), validator.StringRequest{
		Path:        path.Root("condition_id"),
		ConfigValue: types.StringValue("x"),
	}, resp)

	detail := resp.Diagnostics.Errors()[0].Detail()
	if detail != `condition_id takes the access condition's id, which starts with "ac-": "x" is not one.` {
		t.Errorf("unexpected detail: %s", detail)
	}
}
