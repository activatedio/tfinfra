package aip_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/activatedio/tfinfra/pkg/aip"
)

func TestScope_IdentifierAttributes(t *testing.T) {

	type s struct {
		arrange func() aip.Scope
		assert  func(t *testing.T, got []string)
	}

	cases := map[string]s{
		"none": {
			arrange: func() aip.Scope { return aip.ScopeNone },
			assert: func(t *testing.T, got []string) {
				assert.Empty(t, got)
			},
		},
		"single": {
			arrange: func() aip.Scope { return aip.NewScope("tenants") },
			assert: func(t *testing.T, got []string) {
				assert.Equal(t, []string{"tenant_id"}, got)
			},
		},
		"nested": {
			arrange: func() aip.Scope { return aip.NewScope("tenants", "issuers", "audiences") },
			assert: func(t *testing.T, got []string) {
				assert.Equal(t, []string{"tenant_id", "issuer_id", "audience_id"}, got)
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			v.assert(t, v.arrange().IdentifierAttributes())
		})
	}

}

func TestScope_ComposeParent(t *testing.T) {

	type s struct {
		arrange func() (aip.Scope, map[string]string)
		assert  func(t *testing.T, got string, err error)
	}

	cases := map[string]s{
		"none composes empty parent": {
			arrange: func() (aip.Scope, map[string]string) {
				return aip.ScopeNone, nil
			},
			assert: func(t *testing.T, got string, err error) {
				require.NoError(t, err)
				assert.Empty(t, got)
			},
		},
		"single": {
			arrange: func() (aip.Scope, map[string]string) {
				return aip.NewScope("tenants"), map[string]string{"tenant_id": "t1"}
			},
			assert: func(t *testing.T, got string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "tenants/t1", got)
			},
		},
		"nested": {
			arrange: func() (aip.Scope, map[string]string) {
				return aip.NewScope("tenants", "issuers"), map[string]string{"tenant_id": "t1", "issuer_id": "i1"}
			},
			assert: func(t *testing.T, got string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "tenants/t1/issuers/i1", got)
			},
		},
		"missing identifier names the attribute": {
			arrange: func() (aip.Scope, map[string]string) {
				return aip.NewScope("tenants", "issuers"), map[string]string{"tenant_id": "t1"}
			},
			assert: func(t *testing.T, got string, err error) {
				require.EqualError(t, err, `missing required scope attribute "issuer_id"`)
				assert.Empty(t, got)
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			scope, ids := v.arrange()
			got, err := scope.ComposeParent(ids)
			v.assert(t, got, err)
		})
	}

}

func TestScope_ComposeName(t *testing.T) {

	type s struct {
		arrange func() (aip.Scope, map[string]string, string)
		assert  func(t *testing.T, got string, err error)
	}

	cases := map[string]s{
		"none": {
			arrange: func() (aip.Scope, map[string]string, string) {
				return aip.ScopeNone, nil, "p1"
			},
			assert: func(t *testing.T, got string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "pets/p1", got)
			},
		},
		"nested": {
			arrange: func() (aip.Scope, map[string]string, string) {
				return aip.NewScope("stores"), map[string]string{"store_id": "s1"}, "p1"
			},
			assert: func(t *testing.T, got string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "stores/s1/pets/p1", got)
			},
		},
		"empty id errors": {
			arrange: func() (aip.Scope, map[string]string, string) {
				return aip.ScopeNone, nil, ""
			},
			assert: func(t *testing.T, got string, err error) {
				require.EqualError(t, err, "id must not be empty")
				assert.Empty(t, got)
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			scope, ids, id := v.arrange()
			got, err := scope.ComposeName("pets", ids, id)
			v.assert(t, got, err)
		})
	}

}

func TestScope_ParseName(t *testing.T) {

	type s struct {
		arrange func() (aip.Scope, string)
		assert  func(t *testing.T, ids map[string]string, id string, err error)
	}

	cases := map[string]s{
		"none": {
			arrange: func() (aip.Scope, string) {
				return aip.ScopeNone, "pets/p1"
			},
			assert: func(t *testing.T, ids map[string]string, id string, err error) {
				require.NoError(t, err)
				assert.Empty(t, ids)
				assert.Equal(t, "p1", id)
			},
		},
		"nested round trip": {
			arrange: func() (aip.Scope, string) {
				return aip.NewScope("tenants", "issuers"), "tenants/t1/issuers/i1/pets/p1"
			},
			assert: func(t *testing.T, ids map[string]string, id string, err error) {
				require.NoError(t, err)
				assert.Equal(t, map[string]string{"tenant_id": "t1", "issuer_id": "i1"}, ids)
				assert.Equal(t, "p1", id)
			},
		},
		"wrong collection": {
			arrange: func() (aip.Scope, string) {
				return aip.ScopeNone, "cats/p1"
			},
			assert: func(t *testing.T, _ map[string]string, _ string, err error) {
				require.EqualError(t, err, `name "cats/p1" does not match pattern "pets/{id}"`)
			},
		},
		"wrong parent collection": {
			arrange: func() (aip.Scope, string) {
				return aip.NewScope("stores"), "houses/s1/pets/p1"
			},
			assert: func(t *testing.T, _ map[string]string, _ string, err error) {
				require.EqualError(t, err, `name "houses/s1/pets/p1" does not match pattern "stores/{store}/pets/{id}"`)
			},
		},
		"wrong segment count": {
			arrange: func() (aip.Scope, string) {
				return aip.NewScope("stores"), "pets/p1"
			},
			assert: func(t *testing.T, _ map[string]string, _ string, err error) {
				require.EqualError(t, err, `name "pets/p1" does not match pattern "stores/{store}/pets/{id}"`)
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			scope, name := v.arrange()
			ids, id, err := scope.ParseName("pets", name)
			v.assert(t, ids, id, err)
		})
	}

}
