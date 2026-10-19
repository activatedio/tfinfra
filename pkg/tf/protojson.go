package tf

import (
	"bytes"
	"encoding/json"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ProtoJSON is m's protojson encoding with the insignificant whitespace
// removed, so the same message always encodes as the same string.
//
// protojson varies its whitespace on purpose, seeded from a hash of the
// running binary, so each build of a provider can spell the same value
// differently. Terraform compares a configured string with state as a
// string, and a jsontypes.Normalized attribute's semantic equality does not
// apply at plan, so a config data source's output that changes spelling
// between builds shows as a whitespace-only diff on every plan after an
// upgrade. Compacting keeps protojson's field order.
func ProtoJSON(m proto.Message) ([]byte, error) {

	b, err := protojson.Marshal(m)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	if err := json.Compact(&out, b); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}
