package tf

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// KeepEmpty reports whether v is a known list or map with no elements:
// what a practitioner wrote as [] or {}. A read that finds no elements
// keeps such a value rather than nulling it, which Terraform would reject
// as an inconsistent result after apply. Anything else, null and unknown
// included, reports false.
func KeepEmpty(v attr.Value) bool {

	if v == nil || v.IsNull() || v.IsUnknown() {
		return false
	}

	switch c := v.(type) {
	case basetypes.ListValue:
		return len(c.Elements()) == 0
	case basetypes.MapValue:
		return len(c.Elements()) == 0
	default:
		return false
	}
}

// JSONListToProto parses a JSON array whose elements are each the protojson
// encoding of a T, made by newT. A value that is not an array, or an element
// that is not a T, is an error naming its position.
func JSONListToProto[T proto.Message](value string, newT func() T) ([]T, error) {

	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return nil, fmt.Errorf("not a JSON array: %w", err)
	}

	out := make([]T, 0, len(raw))
	for i, r := range raw {
		item := newT()
		if err := protojson.Unmarshal(r, item); err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		out = append(out, item)
	}

	return out, nil
}

// JSONListValue encodes items as a JSON array of their protojson encodings.
// No items read as null, unless prior is already an empty array, which is
// kept as written for the same reason KeepEmpty keeps one.
func JSONListValue[T proto.Message](prior jsontypes.Normalized, items []T) (jsontypes.Normalized, error) {

	if len(items) == 0 {
		if !prior.IsNull() && !prior.IsUnknown() && isEmptyJSONArray(prior.ValueString()) {
			return prior, nil
		}
		return jsontypes.NewNormalizedNull(), nil
	}

	var b bytes.Buffer
	b.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		e, err := protojson.Marshal(item)
		if err != nil {
			return jsontypes.NewNormalizedNull(), fmt.Errorf("element %d: %w", i, err)
		}
		b.Write(e)
	}
	b.WriteByte(']')

	return jsontypes.NewNormalizedValue(b.String()), nil
}

func isEmptyJSONArray(value string) bool {
	var raw []json.RawMessage
	return json.Unmarshal([]byte(value), &raw) == nil && raw != nil && len(raw) == 0
}
