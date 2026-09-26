package tf_test

import (
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/activatedio/tfinfra/pkg/tf"
)

func TestParseDuration(t *testing.T) {

	type s struct {
		in      string
		want    time.Duration
		wantErr bool
	}

	cases := map[string]s{
		"protojson seconds":            {in: "5s", want: 5 * time.Second},
		"protojson fractional seconds": {in: "1.500s", want: 1500 * time.Millisecond},
		"unit suffix":                  {in: "500ms", want: 500 * time.Millisecond},
		"compound":                     {in: "1m30s", want: 90 * time.Second},
		"negative":                     {in: "-2s", want: -2 * time.Second},
		"no unit":                      {in: "5", wantErr: true},
		"empty":                        {in: "", wantErr: true},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			got, err := tf.ParseDuration(v.in)
			if v.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, v.want, got.AsDuration())
		})
	}
}

func TestFormatDuration(t *testing.T) {

	type s struct {
		in   time.Duration
		want string
	}

	cases := map[string]s{
		"zero":           {in: 0, want: "0s"},
		"whole seconds":  {in: 5 * time.Second, want: "5s"},
		"over a minute":  {in: 90 * time.Second, want: "90s"},
		"trimmed millis": {in: 1500 * time.Millisecond, want: "1.5s"},
		"sub-second":     {in: 100 * time.Millisecond, want: "0.1s"},
		"nanosecond":     {in: time.Nanosecond, want: "0.000000001s"},
		"negative":       {in: -1500 * time.Millisecond, want: "-1.5s"},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			got := tf.FormatDuration(durationpb.New(v.in))
			assert.Equal(t, v.want, got)

			back, err := tf.ParseDuration(got)
			require.NoError(t, err)
			assert.Equal(t, v.in, back.AsDuration(), "the canonical form parses back to the same value")
		})
	}
}

func TestDurationValue(t *testing.T) {

	type s struct {
		prior types.String
		in    *durationpb.Duration
		want  types.String
	}

	cases := map[string]s{
		"nil reads as null": {
			prior: types.StringValue("5s"),
			in:    nil,
			want:  types.StringNull(),
		},
		"a prior spelling of the same length is kept": {
			prior: types.StringValue("1m30s"),
			in:    durationpb.New(90 * time.Second),
			want:  types.StringValue("1m30s"),
		},
		"a prior of a different length gives way to the server": {
			prior: types.StringValue("1m"),
			in:    durationpb.New(90 * time.Second),
			want:  types.StringValue("90s"),
		},
		"no prior reads canonical": {
			prior: types.StringNull(),
			in:    durationpb.New(500 * time.Millisecond),
			want:  types.StringValue("0.5s"),
		},
		"an unknown prior reads canonical": {
			prior: types.StringUnknown(),
			in:    durationpb.New(5 * time.Second),
			want:  types.StringValue("5s"),
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, v.want, tf.DurationValue(v.prior, v.in))
		})
	}
}
