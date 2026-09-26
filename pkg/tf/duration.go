package tf

import (
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/types/known/durationpb"
)

// ParseDuration reads a google.protobuf.Duration attribute. It takes Go's
// duration syntax, which covers the protojson form ("5s", "1.5s") and adds
// the unit suffixes a practitioner reaches for first ("500ms", "1m30s").
func ParseDuration(s string) (*durationpb.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, err
	}
	return durationpb.New(d), nil
}

// FormatDuration renders a Duration in its protojson form — whole and
// fractional seconds with an "s" suffix — with trailing fractional zeros
// trimmed: "5s", "1.5s", "0.1s", "90s". Every output parses back through
// ParseDuration to the same value.
func FormatDuration(d *durationpb.Duration) string {

	secs, nanos := d.GetSeconds(), d.GetNanos()

	var b strings.Builder
	if secs < 0 || nanos < 0 {
		b.WriteByte('-')
		secs, nanos = -secs, -nanos
	}
	b.WriteString(strconv.FormatInt(secs, 10))
	if nanos != 0 {
		frac := strings.TrimRight(strconv.FormatInt(int64(nanos)+1e9, 10)[1:], "0")
		b.WriteByte('.')
		b.WriteString(frac)
	}
	b.WriteByte('s')

	return b.String()
}

// DurationValue is the value a Duration attribute reads back as. A nil
// Duration reads as null. When prior already denotes the same length it is
// kept as written, so "1m30s" in configuration does not come back as "90s"
// — which Terraform would reject as an inconsistent result after apply, and
// report as drift on every refresh. Anything else reads in FormatDuration's
// canonical form.
func DurationValue(prior types.String, d *durationpb.Duration) types.String {

	if d == nil {
		return types.StringNull()
	}
	if !prior.IsNull() && !prior.IsUnknown() {
		if p, err := time.ParseDuration(prior.ValueString()); err == nil && p == d.AsDuration() {
			return prior
		}
	}

	return types.StringValue(FormatDuration(d))
}
