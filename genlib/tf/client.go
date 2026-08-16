package tf

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/gertd/go-pluralize"
)

var pluralizeClient = pluralize.NewClient()

// ClientOp is the reflect-derived shape of one client method: the request
// type and the Go names of the request fields the adapter must populate.
// Field names are resolved from protoc-gen-go struct tags, never guessed.
// Exported for sibling generators (cmdinfra) that bind the same AIP client
// shapes.
type ClientOp struct {
	Method      string
	RequestType reflect.Type

	NameField      string
	ParentField    string
	EntityField    string
	MaskField      string
	PageTokenField string

	// List only: response type plus its items and next-page-token fields.
	ResponseType       reflect.Type
	ResponseItemsField string
	ResponseNextField  string
}

// ClientModel is the reflect-derived view of the resource's operations on
// its gRPC client interface. Ops absent from the mask are nil.
type ClientModel struct {
	Type reflect.Type

	Get    *ClientOp
	List   *ClientOp
	Create *ClientOp
	Update *ClientOp
	Patch  *ClientOp
	Delete *ClientOp
}

// AnalyzeClient inspects the marker's ClientType and validates that every
// operation selected by Ops exists with an AIP-shaped signature. It panics
// on anything unexpected — a wrong Ops mask or client type must fail at
// generation time, not at provider runtime. Sibling generators (cmdinfra)
// call it with a synthesized Resource carrying ClientType, Ops, and
// Plural.
func AnalyzeClient(e Entry, res Resource) ClientModel {

	t := entityType(e)

	if res.ClientType == nil {
		panic(fmt.Sprintf("%s: Resource.ClientType is required to generate resources", t.Name()))
	}
	if res.ClientType.Kind() != reflect.Interface {
		panic(fmt.Sprintf("%s: Resource.ClientType must be the client interface type, got %s", t.Name(), res.ClientType))
	}

	cm := ClientModel{Type: res.ClientType}
	entity := t.Name()
	plural := res.Plural
	if plural == "" {
		plural = pluralizeClient.Plural(entity)
	}

	type opSpec struct {
		op     Ops
		method string
		out    **ClientOp
	}

	specs := []opSpec{
		{OpGet, "Get" + entity, &cm.Get},
		{OpList, "List" + plural, &cm.List},
		{OpCreate, "Create" + entity, &cm.Create},
		{OpUpdate, "Update" + entity, &cm.Update},
		{OpPatch, "Patch" + entity, &cm.Patch},
		{OpDelete, "Delete" + entity, &cm.Delete},
	}

	for _, s := range specs {
		if !res.Ops.Has(s.op) {
			continue
		}
		m, ok := res.ClientType.MethodByName(s.method)
		if !ok {
			panic(fmt.Sprintf("%s: client %s has no method %s; narrow Resource.Ops if the API does not expose it",
				entity, res.ClientType, s.method))
		}
		op := analyzeOp(entity, t, m)
		*s.out = &op
	}

	return cm
}

// analyzeOp extracts the request (and, for List, response) shape from a
// client method: func(ctx, *Req, ...grpc.CallOption) (*Out, error).
func analyzeOp(entity string, entityType reflect.Type, m reflect.Method) ClientOp {

	mt := m.Type

	if mt.NumIn() < 2 || mt.In(1).Kind() != reflect.Pointer || mt.In(1).Elem().Kind() != reflect.Struct {
		panic(fmt.Sprintf("%s.%s: expected an AIP-shaped signature func(ctx, *Request, ...) — got %s", entity, m.Name, mt))
	}

	req := mt.In(1).Elem()

	op := ClientOp{
		Method:         m.Name,
		RequestType:    req,
		NameField:      protoFieldGoName(req, "name"),
		ParentField:    protoFieldGoName(req, "parent"),
		MaskField:      protoFieldGoName(req, "update_mask"),
		PageTokenField: protoFieldGoName(req, "page_token"),
		EntityField:    fieldOfType(req, reflect.PointerTo(entityType)),
	}

	if strings.HasPrefix(m.Name, "List") {
		if mt.NumOut() < 1 || mt.Out(0).Kind() != reflect.Pointer {
			panic(fmt.Sprintf("%s.%s: expected a response pointer return", entity, m.Name))
		}
		res := mt.Out(0).Elem()
		op.ResponseType = res
		op.ResponseItemsField = fieldOfType(res, reflect.SliceOf(reflect.PointerTo(entityType)))
		op.ResponseNextField = protoFieldGoName(res, "next_page_token")
		if op.ResponseItemsField == "" {
			panic(fmt.Sprintf("%s.%s: response %s has no []*%s items field", entity, m.Name, res.Name(), entity))
		}
	}

	return op
}

// protoFieldGoName is the tolerant variant of goFieldName: it returns ""
// when the request has no field with that proto name.
func protoFieldGoName(t reflect.Type, protoName string) string {

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("protobuf")
		if tag == "" {
			continue
		}
		for _, part := range strings.Split(tag, ",") {
			if part == "name="+protoName {
				return f.Name
			}
		}
	}

	return ""
}

// fieldOfType returns the name of the first struct field with exactly the
// given type, or "".
func fieldOfType(t reflect.Type, want reflect.Type) string {

	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Type == want {
			return t.Field(i).Name
		}
	}

	return ""
}
