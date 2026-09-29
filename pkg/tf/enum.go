package tf

import "github.com/hashicorp/terraform-plugin-framework/types"

// EnumValue is the value an enum attribute reads back as. A non-zero value
// reads as its name. The zero value reads as null — proto3 cannot tell it
// from unset — unless prior already names it: an enum whose zero is a real
// choice (a REST transport, a standard size) can be written explicitly, and
// reading it back as null would be an inconsistent result after apply.
func EnumValue(prior types.String, number int32, name string) types.String {

	if number != 0 {
		return types.StringValue(name)
	}
	if !prior.IsNull() && !prior.IsUnknown() && prior.ValueString() == name {
		return prior
	}

	return types.StringNull()
}
