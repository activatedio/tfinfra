package tf

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// RequiresReplaceIfConfigured replaces the resource when a parent
// identifier is configured and differs from state. A parent the
// configuration omits inherits the provider default and reads back from the
// name, so it never forces replacement. An unknown configured value does:
// it may name another parent once known.
//
// The framework's RequiresReplace cannot serve an Optional+Computed parent.
// It compares the planned value, which the framework marks unknown for an
// omitted computed attribute whenever anything else in the resource changes,
// and an unknown plan value differs from any state.
func RequiresReplaceIfConfigured() planmodifier.String {
	return requiresReplaceIfConfigured{}
}

type requiresReplaceIfConfigured struct{}

func (requiresReplaceIfConfigured) Description(_ context.Context) string {
	return "Changing a configured value replaces the resource; an omitted one keeps the value in state."
}

func (m requiresReplaceIfConfigured) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (requiresReplaceIfConfigured) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {

	// Create and destroy replace nothing.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	if req.ConfigValue.IsNull() {
		return
	}

	if req.ConfigValue.IsUnknown() || !req.ConfigValue.Equal(req.StateValue) {
		resp.RequiresReplace = true
	}
}
