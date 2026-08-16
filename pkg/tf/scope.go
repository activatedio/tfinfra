package tf

import (
	"fmt"
	"strings"

	"github.com/gertd/go-pluralize"
)

var pluralizeClient = pluralize.NewClient()

// Scope describes where a resource lives in an AIP-style resource hierarchy
// as an ordered list of parent collection names. A resource with
// NewScope("tenants", "issuers") has parents of the form
// "tenants/{tenant}/issuers/{issuer}".
//
// tfinfra does not predefine any scopes; consumers declare their service's
// scope table (typically as package-level vars next to the spec table in
// gen/main.go).
type Scope struct {
	collections []string
}

// NewScope creates a Scope from ordered parent collection names, outermost
// first, e.g. NewScope("tenants", "issuers").
func NewScope(collections ...string) Scope {
	return Scope{collections: collections}
}

// ScopeNone is the scope of a top-level resource with no parent.
var ScopeNone = Scope{}

// Collections returns the ordered parent collection names.
func (s Scope) Collections() []string {
	res := make([]string, len(s.collections))
	copy(res, s.collections)
	return res
}

// IdentifierAttributes returns the Terraform attribute name for each parent
// collection, outermost first: the singular collection name suffixed with
// "_id" ("tenants" → "tenant_id").
func (s Scope) IdentifierAttributes() []string {
	res := make([]string, len(s.collections))
	for i, c := range s.collections {
		res[i] = pluralizeClient.Singular(c) + "_id"
	}
	return res
}

// ComposeParent builds the AIP parent string from identifier attribute
// values keyed by IdentifierAttributes() names. A scope with no collections
// returns "". A missing or empty identifier is an error naming the
// attribute, so callers can surface an actionable diagnostic.
func (s Scope) ComposeParent(ids map[string]string) (string, error) {

	parts := make([]string, 0, 2*len(s.collections))

	for i, c := range s.collections {
		attr := s.IdentifierAttributes()[i]
		id := ids[attr]
		if id == "" {
			return "", fmt.Errorf("missing required scope attribute %q", attr)
		}
		parts = append(parts, c, id)
	}

	return strings.Join(parts, "/"), nil
}

// ComposeName builds the full AIP resource name
// "<parent>/<collection>/<id>" for a resource in the given collection.
func (s Scope) ComposeName(collection string, ids map[string]string, id string) (string, error) {

	if collection == "" {
		return "", fmt.Errorf("collection must not be empty")
	}
	if id == "" {
		return "", fmt.Errorf("id must not be empty")
	}

	parent, err := s.ComposeParent(ids)
	if err != nil {
		return "", err
	}

	if parent == "" {
		return collection + "/" + id, nil
	}

	return parent + "/" + collection + "/" + id, nil
}

// ParseName splits a full AIP resource name back into its scope identifier
// attribute values and the resource's own id. The name must match this
// scope's collections followed by the given collection:
// "tenants/t1/issuers/i1/<collection>/<id>".
func (s Scope) ParseName(collection string, name string) (map[string]string, string, error) {

	segments := strings.Split(name, "/")
	want := 2*len(s.collections) + 2

	if len(segments) != want {
		return nil, "", fmt.Errorf("name %q does not match pattern %q", name, s.pattern(collection))
	}

	ids := map[string]string{}

	for i, c := range s.collections {
		if segments[2*i] != c || segments[2*i+1] == "" {
			return nil, "", fmt.Errorf("name %q does not match pattern %q", name, s.pattern(collection))
		}
		ids[s.IdentifierAttributes()[i]] = segments[2*i+1]
	}

	if segments[want-2] != collection || segments[want-1] == "" {
		return nil, "", fmt.Errorf("name %q does not match pattern %q", name, s.pattern(collection))
	}

	return ids, segments[want-1], nil
}

func (s Scope) pattern(collection string) string {

	parts := make([]string, 0, 2*len(s.collections)+2)

	for _, c := range s.collections {
		parts = append(parts, c, "{"+pluralizeClient.Singular(c)+"}")
	}
	parts = append(parts, collection, "{id}")

	return strings.Join(parts, "/")
}
