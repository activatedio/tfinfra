package tf

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/gertd/go-pluralize"

	runtimetf "github.com/activatedio/tfinfra/pkg/tf"
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

	// Mint is set by AnalyzeMint for a Resource with a Mint marker; it
	// replaces Create.
	Mint *MintOp
}

// MintOp is the reflect-derived shape of a mint method: how the adapter
// fills the request from parent and entity, and where the response keeps
// the entity and the once-only values.
type MintOp struct {
	Method      string
	RequestType reflect.Type

	ParentField string
	// EntityField is the request field holding the whole entity; empty when
	// the request takes the entity's fields flat, as Flat lists them.
	EntityField string
	Flat        []MintFlatField

	// ResponseEntityField is the response field holding the entity.
	ResponseEntityField string
	// Once maps each once-only attribute to its response field's Go name,
	// in the order the spec lists them.
	Once []MintOnceField
}

// MintFlatField copies one entity field into the request field that
// carries it.
type MintFlatField struct {
	Request string
	Entity  string
}

// MintOnceField binds a once-only attribute to its response field.
type MintOnceField struct {
	Attribute string
	GoName    string
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
		// A minted resource never creates through Create<Entity>, which a
		// mint API typically does not have.
		if s.op == OpCreate && res.Mint != nil {
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

// AnalyzeMint resolves a Resource's Mint marker against its client and the
// entity's normalized fields. It panics on anything it cannot bind: a
// missing method, a request field with no entity counterpart, a settable
// field the request cannot carry, or a once-only field that is not a
// string on the response.
func AnalyzeMint(e Entry, res Resource, fields []Field) *MintOp {

	if res.Mint == nil {
		return nil
	}

	t := entityType(e)
	mint := res.Mint

	if mint.Method == "" {
		panic(fmt.Sprintf("%s: Mint.Method is required", t.Name()))
	}
	if len(mint.Once) == 0 {
		panic(fmt.Sprintf("%s: Mint.Once must name the response fields returned only by %s", t.Name(), mint.Method))
	}

	req, out := mintSignature(t, res.ClientType, mint.Method)
	where := t.Name() + "." + mint.Method

	op := &MintOp{
		Method:              mint.Method,
		RequestType:         req,
		ParentField:         protoFieldGoName(req, "parent"),
		ResponseEntityField: fieldOfType(out, reflect.PointerTo(t)),
	}
	if op.ParentField == "" {
		panic(fmt.Sprintf("%s: request %s has no parent field", where, req.Name()))
	}
	if op.ResponseEntityField == "" {
		panic(fmt.Sprintf("%s: response %s has no *%s field", where, out.Name(), t.Name()))
	}

	bindMintRequest(where, t, req, fields, op)
	op.Once = bindMintOnce(where, res, out, fields)

	return op
}

// mintSignature returns the request and response struct types of the mint
// method, which must be func(ctx, *Request, ...) (*Response, error).
func mintSignature(t, client reflect.Type, method string) (req, out reflect.Type) {

	m, ok := client.MethodByName(method)
	if !ok {
		panic(fmt.Sprintf("%s: client %s has no method %s", t.Name(), client, method))
	}

	mt := m.Type
	isStructPtr := func(x reflect.Type) bool {
		return x.Kind() == reflect.Pointer && x.Elem().Kind() == reflect.Struct
	}
	if mt.NumIn() < 2 || !isStructPtr(mt.In(1)) || mt.NumOut() < 1 || !isStructPtr(mt.Out(0)) {
		panic(fmt.Sprintf("%s.%s: expected func(ctx, *Request, ...) (*Response, error) — got %s", t.Name(), method, mt))
	}

	return mt.In(1).Elem(), mt.Out(0).Elem()
}

// bindMintRequest decides how the request carries the entity — whole, or
// flat field by field — and checks that every settable field gets there.
func bindMintRequest(where string, t, req reflect.Type, fields []Field, op *MintOp) {

	byProto := map[string]Field{}
	for _, f := range fields {
		byProto[f.ProtoName] = f
	}

	carried := map[string]bool{}
	for i := 0; i < req.NumField(); i++ {
		rf := req.Field(i)
		protoName := protoTagName(rf)
		switch {
		case protoName == "" || protoName == "parent":
			continue
		case rf.Type == reflect.PointerTo(t):
			op.EntityField = rf.Name
		default:
			ef, ok := byProto[protoName]
			if !ok || ef.GoType != rf.Type {
				panic(fmt.Sprintf("%s: request field %q has no %s field of the same name and type to fill it from",
					where, protoName, t.Name()))
			}
			op.Flat = append(op.Flat, MintFlatField{Request: rf.Name, Entity: ef.GoName})
			carried[protoName] = true
		}
	}

	if op.EntityField != "" {
		if len(op.Flat) > 0 {
			panic(fmt.Sprintf("%s: the request takes the entity both whole and flat", where))
		}
		return
	}

	checkMintCarried(where, fields, carried)
}

// checkMintCarried refuses a flat mint request that leaves out a field the
// practitioner can set: the mint would drop its value without a word.
func checkMintCarried(where string, fields []Field, carried map[string]bool) {

	for _, f := range fields {
		if f.ProtoName != NameField && !f.Computed && !carried[f.ProtoName] {
			panic(fmt.Sprintf("%s: %q can be set, but the request has no field to carry it; mark it Computed if the mint does not take it",
				where, f.ProtoName))
		}
	}
}

// bindMintOnce binds each once-only attribute to its string response
// field, refusing a name another attribute already has.
func bindMintOnce(where string, res Resource, out reflect.Type, fields []Field) []MintOnceField {

	taken := map[string]bool{runtimetf.KeepersAttribute: true}
	for _, f := range fields {
		taken[f.ProtoName] = true
	}
	for _, a := range res.Scope.IdentifierAttributes() {
		taken[a] = true
	}

	once := make([]MintOnceField, 0, len(res.Mint.Once))
	for _, name := range res.Mint.Once {
		goName := protoFieldGoName(out, name)
		if goName == "" {
			panic(fmt.Sprintf("%s: response %s has no field %q", where, out.Name(), name))
		}
		if f, _ := out.FieldByName(goName); f.Type.Kind() != reflect.String {
			panic(fmt.Sprintf("%s: once-only field %q must be a string, got %s", where, name, f.Type))
		}
		if taken[name] {
			panic(fmt.Sprintf("%s: once-only field %q collides with another attribute", where, name))
		}
		taken[name] = true
		once = append(once, MintOnceField{Attribute: name, GoName: goName})
	}

	return once
}

// protoTagName returns a struct field's proto name from its protoc-gen-go
// tag, or "" for the generated bookkeeping fields that carry none.
func protoTagName(f reflect.StructField) string {

	for _, part := range strings.Split(f.Tag.Get("protobuf"), ",") {
		if name, ok := strings.CutPrefix(part, "name="); ok {
			return name
		}
	}

	return ""
}
