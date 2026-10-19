package tf_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	tf "github.com/activatedio/tfinfra/pkg/tf"
)

// TestProtoJSON pins the spelling: protojson's own whitespace varies with
// the binary, so the only stable form is the one with none.
func TestProtoJSON(t *testing.T) {

	s, err := structpb.NewStruct(map[string]any{
		"uris": []any{"https://a.example/cb", "https://b.example/cb"},
		"note": "keeps, its: spaces",
	})
	require.NoError(t, err)

	a, err := anypb.New(s)
	require.NoError(t, err)

	got, err := tf.ProtoJSON(a)
	require.NoError(t, err)

	assert.Equal(t, //nolint:testifylint // the spelling is under test; JSONEq would accept any whitespace
		`{"@type":"type.googleapis.com/google.protobuf.Struct","value":{"note":"keeps, its: spaces","uris":["https://a.example/cb","https://b.example/cb"]}}`,
		string(got))
}
