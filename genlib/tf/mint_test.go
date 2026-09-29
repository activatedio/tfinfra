package tf_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	petstorev1 "github.com/activatedio/tfinfra/examples/petstore/gen/petstore/v1"
	gentf "github.com/activatedio/tfinfra/genlib/tf"
	tf "github.com/activatedio/tfinfra/pkg/tf"
)

func accessKeyEntry(impls ...any) gentf.Entry {
	return gentf.Entry{Type: reflect.TypeFor[petstorev1.AccessKey](), Implementations: impls}
}

func accessKeyResource(mint *gentf.Mint, computed ...string) gentf.Resource {
	return gentf.Resource{
		Scope:      tf.NewScope("stores"),
		Ops:        gentf.OpGet | gentf.OpList | gentf.OpPatch | gentf.OpDelete,
		ClientType: reflect.TypeFor[petstorev1.PetStoreServiceClient](),
		Computed:   computed,
		Mint:       mint,
	}
}

func TestAnalyzeMint(t *testing.T) {

	type s struct {
		arrange func() gentf.Resource
		assert  func(t *testing.T, f func() *gentf.MintOp)
	}

	cases := map[string]s{
		"a flat request binds by proto name and type": {
			arrange: func() gentf.Resource {
				return accessKeyResource(&gentf.Mint{Method: "MintAccessKey", Once: []string{"key"}}, "create_time")
			},
			assert: func(t *testing.T, f func() *gentf.MintOp) {
				op := f()
				require.NotNil(t, op)
				assert.Equal(t, "MintAccessKey", op.Method)
				assert.Equal(t, "Parent", op.ParentField)
				assert.Empty(t, op.EntityField)
				assert.ElementsMatch(t, []gentf.MintFlatField{
					{Request: "DisplayName", Entity: "DisplayName"},
					{Request: "ExpiresAt", Entity: "ExpiresAt"},
				}, op.Flat)
				assert.Equal(t, "AccessKey", op.ResponseEntityField)
				assert.Equal(t, []gentf.MintOnceField{{Attribute: "key", GoName: "Key"}}, op.Once)
			},
		},
		"no Mint marker": {
			arrange: func() gentf.Resource { return accessKeyResource(nil, "create_time") },
			assert: func(t *testing.T, f func() *gentf.MintOp) {
				assert.Nil(t, f())
			},
		},
		"a settable field the request cannot carry": {
			// create_time is left settable, and MintAccessKeyRequest has no
			// such field: the mint would drop it without a word.
			arrange: func() gentf.Resource {
				return accessKeyResource(&gentf.Mint{Method: "MintAccessKey", Once: []string{"key"}})
			},
			assert: func(t *testing.T, f func() *gentf.MintOp) {
				assert.PanicsWithValue(t,
					`AccessKey.MintAccessKey: "create_time" can be set, but the request has no field to carry it; mark it Computed if the mint does not take it`, func() { f() })
			},
		},
		"a missing method": {
			arrange: func() gentf.Resource {
				return accessKeyResource(&gentf.Mint{Method: "ForgeAccessKey", Once: []string{"key"}}, "create_time")
			},
			assert: func(t *testing.T, f func() *gentf.MintOp) {
				assert.PanicsWithValue(t,
					"AccessKey: client petstorev1.PetStoreServiceClient has no method ForgeAccessKey", func() { f() })
			},
		},
		"no once-only fields": {
			arrange: func() gentf.Resource {
				return accessKeyResource(&gentf.Mint{Method: "MintAccessKey"}, "create_time")
			},
			assert: func(t *testing.T, f func() *gentf.MintOp) {
				assert.PanicsWithValue(t,
					"AccessKey: Mint.Once must name the response fields returned only by MintAccessKey", func() { f() })
			},
		},
		"a once-only field the response lacks": {
			arrange: func() gentf.Resource {
				return accessKeyResource(&gentf.Mint{Method: "MintAccessKey", Once: []string{"secret"}}, "create_time")
			},
			assert: func(t *testing.T, f func() *gentf.MintOp) {
				assert.PanicsWithValue(t,
					`AccessKey.MintAccessKey: response MintAccessKeyResponse has no field "secret"`, func() { f() })
			},
		},
		"a once-only field that is not a string": {
			arrange: func() gentf.Resource {
				return accessKeyResource(&gentf.Mint{Method: "MintAccessKey", Once: []string{"access_key"}}, "create_time")
			},
			assert: func(t *testing.T, f func() *gentf.MintOp) {
				assert.PanicsWithValue(t,
					`AccessKey.MintAccessKey: once-only field "access_key" must be a string, got *petstorev1.AccessKey`, func() { f() })
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			res := v.arrange()
			e := accessKeyEntry(res)
			v.assert(t, func() *gentf.MintOp { return gentf.AnalyzeMint(e, res, gentf.NormalizeFields(e, res)) })
		})
	}
}

// A singular data source would share the resource's model, once-only
// attributes and all; generation refuses the pairing.
func TestMintRefusesSingularDataSource(t *testing.T) {

	res := accessKeyResource(&gentf.Mint{Method: "MintAccessKey", Once: []string{"key"}}, "create_time")

	assert.PanicsWithValue(t,
		"AccessKey: a minted resource takes no singular DataSource; its model carries once-only attributes no read has (DataSourceList is fine)",
		func() {
			gentf.NewRegistry().RunDirectoryPathHandler(t.TempDir(), &gentf.Spec{
				Package: "generated",
				Entries: []gentf.Entry{accessKeyEntry(res, gentf.DataSource{})},
			})
		})
}
