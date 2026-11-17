package tf

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

// SortedJSON is s with the insignificant whitespace removed and every
// object's keys sorted, numbers kept as written: the form Terraform's
// jsonencode produces.
func SortedJSON(s string) (string, error) {

	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return "", err
	}

	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// KeepJSONStyle gives each top-level jsontypes.Normalized attribute that a
// read changed the key order its prior value had.
//
// Terraform plans a configured attribute's value exactly as written, so a
// JSON string in state that is equal to the config but spelled differently
// plans a whitespace-only update, and semantic equality cannot hide it at
// plan. After an apply, state holds the config's own string. A read keeps
// it while the server's value is JSON-equal (the framework's semantic
// equality). When the value changed out of band, the read used to store the
// server's protojson spelling, in field order. A config built with
// jsonencode, or with a config data source that sorts its keys, then never
// matched it, even once the server's value equalled the config again.
//
// So when the prior value was in sorted form (SortedJSON), the new one is
// stored sorted too. Any other prior spelling keeps protojson's field
// order, which is also what a generated config data source emits. Nothing
// a practitioner wrote is rewritten: only a value the server changed is
// re-spelled, and only into the form the prior value already had.
func KeepJSONStyle(ctx context.Context, prior tfsdk.State, state *tfsdk.State) diag.Diagnostics {

	var diags diag.Diagnostics
	if prior.Raw.IsNull() || state.Raw.IsNull() {
		return diags
	}

	for name, a := range state.Schema.GetAttributes() {
		if _, ok := a.GetType().(jsontypes.NormalizedType); !ok {
			continue
		}
		p := path.Root(name)

		var before, after jsontypes.Normalized
		diags.Append(prior.GetAttribute(ctx, p, &before)...)
		diags.Append(state.GetAttribute(ctx, p, &after)...)
		if diags.HasError() {
			return diags
		}

		out, ok := restyle(ctx, before, after)
		if !ok {
			continue
		}
		diags.Append(state.SetAttribute(ctx, p, jsontypes.NewNormalizedValue(out))...)
	}

	return diags
}

// restyle is after in sorted form when before was in it and the two
// differ; ok is false when after stays as it is.
func restyle(ctx context.Context, before, after jsontypes.Normalized) (string, bool) {

	if !changed(ctx, before, after) || !isSorted(before.ValueString()) {
		return "", false
	}

	out, err := SortedJSON(after.ValueString())
	if err != nil || out == after.ValueString() {
		return "", false
	}
	return out, true
}

// changed is whether a read replaced a known prior value with one that is
// not JSON-equal to it; when it is equal, the framework keeps the prior.
func changed(ctx context.Context, before, after jsontypes.Normalized) bool {

	if before.IsNull() || before.IsUnknown() || after.IsNull() || after.IsUnknown() {
		return false
	}
	if before.ValueString() == after.ValueString() {
		return false
	}
	eq, d := before.StringSemanticEquals(ctx, after)
	return !d.HasError() && !eq
}

// isSorted is whether s is already in SortedJSON's form.
func isSorted(s string) bool {
	sorted, err := SortedJSON(s)
	return err == nil && sorted == s
}
