package tf

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// ReferenceID validates an attribute holding another resource's id: the
// last segment of its name, which starts with prefix and a hyphen. target
// is the referenced resource's Terraform type suffix ("store"), and example
// an expression that yields such an id ("petstore_store.<name>.store_id"),
// or "" when no resource of the provider's has one to reference.
//
// A full resource name fails, as does an id of another type; with an
// example, the message names the attribute to reference instead. An empty
// prefix is for ids that carry none ("m50"): only a full name fails then.
// Null, unknown and empty values pass: an empty value is an unset optional
// reference, and a required one is the API's to refuse.
func ReferenceID(prefix, target, example string) validator.String {
	return referenceID{prefix: prefix, target: target, example: example}
}

type referenceID struct {
	prefix  string
	target  string
	example string
}

func (v referenceID) noun() string {
	return strings.ReplaceAll(v.target, "_", " ")
}

func (v referenceID) Description(_ context.Context) string {
	return fmt.Sprintf("the %s's %s", v.noun(), v.shape())
}

// shape says what an id looks like: "id, which starts with "s-"", or just
// "id" when the ids carry no prefix.
func (v referenceID) shape() string {
	if v.prefix == "" {
		return "id"
	}
	return fmt.Sprintf("id, which starts with %q", v.prefix+"-")
}

// idAttribute is the attribute of the target that holds its id, read from
// the example expression's last step ("petstore_store.<name>.store_id").
func (v referenceID) idAttribute() string {
	if i := strings.LastIndex(v.example, "."); i >= 0 {
		return v.example[i+1:]
	}
	return v.target + "_id"
}

func (v referenceID) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v referenceID) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {

	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	value := req.ConfigValue.ValueString()
	if value == "" || validReferenceID(value, v.prefix) {
		return
	}

	attr := req.Path.String()

	var got string
	if strings.Contains(value, "/") {
		got = fmt.Sprintf("%q is a resource name, not an id", value)
	} else {
		got = fmt.Sprintf("%q is not one", value)
	}

	detail := fmt.Sprintf("%s takes the %s's %s: %s.", attr, v.noun(), v.shape(), got)
	if v.example != "" {
		detail += fmt.Sprintf(" Reference the %s's %s attribute rather than its name, as in %s.", v.noun(), v.idAttribute(), v.example)
	}

	resp.Diagnostics.AddAttributeError(req.Path, fmt.Sprintf("Invalid %s id", v.noun()), detail)
}

// validReferenceID reports whether value is an id of the given prefix: the
// prefix, a hyphen, and at least one more character, with no "/" (a name).
// With no prefix, any value without a "/" is one.
func validReferenceID(value, prefix string) bool {
	if prefix == "" {
		return !strings.Contains(value, "/")
	}
	rest, ok := strings.CutPrefix(value, prefix+"-")
	return ok && rest != "" && !strings.Contains(rest, "/")
}
