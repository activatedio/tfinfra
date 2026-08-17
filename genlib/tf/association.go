package tf

import (
	"fmt"
	"reflect"

	"github.com/dave/jennifer/jen"
)

// AssociationModel is the reflect-derived shape of one association edge:
// the kit Associate{Targets}To{Entity} / List{Targets}By{Entity} RPC pair
// plus the derived Terraform names. Exported for sibling generators
// (cmdinfra) that bind the same RPC family.
type AssociationModel struct {
	// Attribute is the member-set attribute name, e.g. "toys".
	Attribute string
	// TypeName is the Terraform type suffix, e.g. "pet_toys".
	TypeName string
	// Combined is the Go identifier stem, e.g. "PetToys".
	Combined string
	// Target is the associated entity's pb message type.
	Target reflect.Type
	// TargetNameField is the Go field bound to the target's "name" proto
	// field.
	TargetNameField string

	AssocMethod     string
	AssocRequest    reflect.Type
	AssocNameField  string
	AssocEdgeField  string
	EdgeType        reflect.Type
	EdgeSetField    string
	EdgeRemoveField string

	ListMethod         string
	ListRequest        reflect.Type
	ListNameField      string
	ListPageTokenField string
	ListResponse       reflect.Type
	ListItemsField     string
	ListNextField      string
}

// Associations collects every Associate marker on the entry.
func Associations(e Entry) []Associate {
	var out []Associate
	for _, impl := range e.Implementations {
		if a, ok := impl.(Associate); ok {
			out = append(out, a)
		}
	}
	return out
}

// AnalyzeAssociation validates the edge's RPC pair on the marker's client
// interface and derives the request shapes and Terraform names. It panics
// on anything unexpected — generation failures must be loud.
func AnalyzeAssociation(e Entry, res Resource, a Associate) AssociationModel {

	entity := entityType(e).Name()

	if a.Target == nil {
		panic(fmt.Sprintf("%s: Associate.Target is required", entity))
	}
	target := a.Target
	if target.Kind() == reflect.Pointer {
		target = target.Elem()
	}

	targetPlural := pluralizeClient.Plural(target.Name())

	attribute := a.Attribute
	if attribute == "" {
		attribute = toSnake(targetPlural)
	}

	n := namesFor(e, res)
	typeName := a.TypeName
	if typeName == "" {
		typeName = n.TypeName + "_" + attribute
	}

	m := associationMethod(entity, res, "Associate"+targetPlural+"To"+entity)
	assocReq := m.Type.In(1).Elem()
	edgeField, edgeType := associationEdge(entity, assocReq)

	lm := associationMethod(entity, res, "List"+targetPlural+"By"+entity)
	listReq := lm.Type.In(1).Elem()
	listRes := lm.Type.Out(0).Elem()

	model := AssociationModel{
		Attribute:       attribute,
		TypeName:        typeName,
		Combined:        n.Entity + snakeToCamel(attribute),
		Target:          target,
		TargetNameField: goFieldName(target, "name"),

		AssocMethod:     m.Name,
		AssocRequest:    assocReq,
		AssocNameField:  goFieldName(assocReq, "name"),
		AssocEdgeField:  edgeField,
		EdgeType:        edgeType,
		EdgeSetField:    goFieldName(edgeType, "set"),
		EdgeRemoveField: goFieldName(edgeType, "remove"),

		ListMethod:         lm.Name,
		ListRequest:        listReq,
		ListNameField:      goFieldName(listReq, "name"),
		ListPageTokenField: goFieldName(listReq, "page_token"),
		ListResponse:       listRes,
		ListNextField:      goFieldName(listRes, "next_page_token"),
	}

	model.ListItemsField = fieldOfType(listRes, reflect.SliceOf(reflect.PointerTo(target)))
	if model.ListItemsField == "" {
		panic(fmt.Sprintf("%s: %s response has no []*%s items field", entity, lm.Name, target.Name()))
	}

	return model
}

func associationMethod(entity string, res Resource, name string) reflect.Method {

	m, ok := res.ClientType.MethodByName(name)
	if !ok {
		panic(fmt.Sprintf("%s: client %s has no method %s", entity, res.ClientType, name))
	}
	if m.Type.NumIn() < 2 || m.Type.In(1).Kind() != reflect.Pointer || m.Type.In(1).Elem().Kind() != reflect.Struct {
		panic(fmt.Sprintf("%s.%s: expected an AIP-shaped signature", entity, name))
	}
	if m.Type.NumOut() < 1 || m.Type.Out(0).Kind() != reflect.Pointer {
		panic(fmt.Sprintf("%s.%s: expected a response pointer return", entity, name))
	}
	return m
}

// associationEdge finds the request field holding the AssociationRequest
// payload: the message-typed field carrying set/remove.
func associationEdge(entity string, req reflect.Type) (string, reflect.Type) {

	for i := 0; i < req.NumField(); i++ {
		f := req.Field(i)
		if f.Type.Kind() != reflect.Pointer || f.Type.Elem().Kind() != reflect.Struct {
			continue
		}
		edge := f.Type.Elem()
		if protoFieldGoName(edge, "set") != "" && protoFieldGoName(edge, "remove") != "" {
			return f.Name, edge
		}
	}
	panic(fmt.Sprintf("%s: association request %s has no AssociationRequest{set, remove} field", entity, req.Name()))
}

// writeAssociationFactory emits new<Combined>Association(providerData any),
// the Configure body of the association resource.
func writeAssociationFactory(f *jen.File, res Resource, n entityNames, am AssociationModel) {

	clientKey := res.Client
	if clientKey == "" {
		clientKey = "default"
	}

	clientQual := jen.Qual(res.ClientType.PkgPath(), res.ClientType.Name())

	scopeArgs := make([]jen.Code, 0, len(res.Scope.Collections()))
	for _, c := range res.Scope.Collections() {
		scopeArgs = append(scopeArgs, jen.Lit(c))
	}

	params := jen.Dict{
		jen.Id("TypeName"):        jen.Lit(am.TypeName),
		jen.Id("EntityAttribute"): jen.Lit(n.TypeName),
		jen.Id("Attribute"):       jen.Lit(am.Attribute),
		jen.Id("Scope"):           jen.Qual(pkgRuntimeTf, "NewScope").Call(scopeArgs...),
		jen.Id("Collection"):      jen.Lit(n.Collection),
		jen.Id("Client"): jen.Qual(pkgRuntimeTf, "AssociationClient").Values(jen.Dict{
			jen.Id("Associate"): jen.Func().
				Params(jen.Id("ctx").Qual("context", "Context"), jen.Id("name").String(), jen.List(jen.Id("set"), jen.Id("remove")).Index().String()).
				Error().
				Block(
					jen.List(jen.Id("_"), jen.Id("err")).Op(":=").Id("client").Dot(am.AssocMethod).Call(
						jen.Id("ctx"),
						jen.Op("&").Qual(am.AssocRequest.PkgPath(), am.AssocRequest.Name()).Values(jen.Dict{
							jen.Id(am.AssocNameField): jen.Id("name"),
							jen.Id(am.AssocEdgeField): jen.Op("&").Qual(am.EdgeType.PkgPath(), am.EdgeType.Name()).Values(jen.Dict{
								jen.Id(am.EdgeSetField):    jen.Id("set"),
								jen.Id(am.EdgeRemoveField): jen.Id("remove"),
							}),
						}),
					),
					jen.Return(jen.Id("err")),
				),
			jen.Id("ListBy"): jen.Func().
				Params(jen.Id("ctx").Qual("context", "Context"), jen.List(jen.Id("name"), jen.Id("pageToken")).String()).
				Params(jen.Index().String(), jen.String(), jen.Error()).
				Block(
					jen.List(jen.Id("out"), jen.Id("err")).Op(":=").Id("client").Dot(am.ListMethod).Call(
						jen.Id("ctx"),
						jen.Op("&").Qual(am.ListRequest.PkgPath(), am.ListRequest.Name()).Values(jen.Dict{
							jen.Id(am.ListNameField):      jen.Id("name"),
							jen.Id(am.ListPageTokenField): jen.Id("pageToken"),
						}),
					),
					jen.If(jen.Id("err").Op("!=").Nil()).Block(
						jen.Return(jen.Nil(), jen.Lit(""), jen.Id("err")),
					),
					jen.Id("names").Op(":=").Make(jen.Index().String(), jen.Lit(0), jen.Len(jen.Id("out").Dot(am.ListItemsField))),
					jen.For(jen.List(jen.Id("_"), jen.Id("item")).Op(":=").Range().Id("out").Dot(am.ListItemsField)).Block(
						jen.Id("names").Op("=").Append(jen.Id("names"), jen.Id("item").Dot("Get"+am.TargetNameField).Call()),
					),
					jen.Return(jen.Id("names"), jen.Id("out").Dot(am.ListNextField), jen.Nil()),
				),
		}),
	}

	f.Commentf("new%sAssociation builds the %s runtime from provider data; it returns nil (no error) before the provider is configured.", am.Combined, am.TypeName)
	f.Func().Id("new"+am.Combined+"Association").Params(jen.Id("providerData").Any()).
		Params(jen.Op("*").Qual(pkgRuntimeTf, "Association"), jen.Qual(pkgDiag, "Diagnostics")).
		Block(
			jen.Var().Id("diags").Qual(pkgDiag, "Diagnostics"),
			jen.If(jen.Id("providerData").Op("==").Nil()).Block(
				jen.Return(jen.Nil(), jen.Id("diags")),
			),
			jen.List(jen.Id("pd"), jen.Id("ok")).Op(":=").Id("providerData").Assert(jen.Op("*").Qual(pkgRuntimeTf, "ProviderData")),
			jen.If(jen.Op("!").Id("ok")).Block(
				jen.Id("diags").Dot("AddError").Call(
					jen.Lit("unexpected provider data"),
					jen.Qual("fmt", "Sprintf").Call(jen.Lit("expected *tf.ProviderData, got %T"), jen.Id("providerData")),
				),
				jen.Return(jen.Nil(), jen.Id("diags")),
			),
			jen.List(jen.Id("client"), jen.Id("ok")).Op(":=").Id("pd").Dot("Clients").Index(jen.Lit(clientKey)).Assert(clientQual),
			jen.If(jen.Op("!").Id("ok")).Block(
				jen.Id("diags").Dot("AddError").Call(
					jen.Lit("missing client"),
					jen.Lit(fmt.Sprintf("provider data key %q is not a %s.%s", clientKey, res.ClientType.PkgPath(), res.ClientType.Name())),
				),
				jen.Return(jen.Nil(), jen.Id("diags")),
			),
			jen.Return(
				jen.Qual(pkgRuntimeTf, "NewAssociation").Call(
					jen.Qual(pkgRuntimeTf, "AssociationParams").Values(params),
				),
				jen.Id("diags"),
			),
		)
}

// writeAssociationResource emits the association resource type: thin
// guarded delegates over the Association runtime.
func writeAssociationResource(f *jen.File, n entityNames, am AssociationModel) {

	recvName := lowerFirst(am.Combined) + "Resource"

	f.Commentf("%s is the generated authoritative association resource for a %s's %s.", recvName, n.TypeName, am.Attribute)
	f.Type().Id(recvName).Struct(
		jen.Id("assoc").Op("*").Qual(pkgRuntimeTf, "Association"),
	)

	f.Commentf("New%sResource returns the generated %s resource; its client arrives via Configure from tf.ProviderData.", am.Combined, am.TypeName)
	f.Func().Id("New"+am.Combined+"Resource").Params().Qual(pkgResource, "Resource").Block(
		jen.Return(jen.Op("&").Id(recvName).Values()),
	)

	recv := func() *jen.Statement {
		return f.Func().Params(jen.Id("r").Op("*").Id(recvName))
	}

	recv().Id("Metadata").Params(
		jen.Id("_").Qual("context", "Context"),
		jen.Id("req").Qual(pkgResource, "MetadataRequest"),
		jen.Id("resp").Op("*").Qual(pkgResource, "MetadataResponse"),
	).Block(
		jen.Id("resp").Dot("TypeName").Op("=").Id("req").Dot("ProviderTypeName").Op("+").Lit("_" + am.TypeName),
	)

	recv().Id("Schema").Params(
		jen.Id("_").Qual("context", "Context"),
		jen.Id("_").Qual(pkgResource, "SchemaRequest"),
		jen.Id("resp").Op("*").Qual(pkgResource, "SchemaResponse"),
	).Block(
		jen.Id("resp").Dot("Schema").Op("=").Qual(pkgRuntimeTf, "AssociationSchema").Call(jen.Lit(n.TypeName), jen.Lit(am.Attribute)),
	)

	recv().Id("Configure").Params(
		jen.Id("_").Qual("context", "Context"),
		jen.Id("req").Qual(pkgResource, "ConfigureRequest"),
		jen.Id("resp").Op("*").Qual(pkgResource, "ConfigureResponse"),
	).Block(
		jen.List(jen.Id("assoc"), jen.Id("diags")).Op(":=").Id("new"+am.Combined+"Association").Call(jen.Id("req").Dot("ProviderData")),
		jen.Id("resp").Dot("Diagnostics").Dot("Append").Call(jen.Id("diags").Op("...")),
		jen.Id("r").Dot("assoc").Op("=").Id("assoc"),
	)

	guard := func() jen.Code {
		return jen.If(jen.Id("r").Dot("assoc").Op("==").Nil()).Block(
			jen.Id("resp").Dot("Diagnostics").Dot("AddError").Call(
				jen.Lit(fmt.Sprintf("%s resource not configured", am.TypeName)),
				jen.Lit("Configure was not called with tf.ProviderData"),
			),
			jen.Return(),
		)
	}

	for _, verb := range []string{"Create", "Read", "Update", "Delete"} {
		recv().Id(verb).Params(
			jen.Id("ctx").Qual("context", "Context"),
			jen.Id("req").Qual(pkgResource, verb+"Request"),
			jen.Id("resp").Op("*").Qual(pkgResource, verb+"Response"),
		).Block(
			guard(),
			jen.Id("r").Dot("assoc").Dot(verb).Call(jen.Id("ctx"), jen.Id("req"), jen.Id("resp")),
		)
	}

	recv().Id("ImportState").Params(
		jen.Id("ctx").Qual("context", "Context"),
		jen.Id("req").Qual(pkgResource, "ImportStateRequest"),
		jen.Id("resp").Op("*").Qual(pkgResource, "ImportStateResponse"),
	).Block(
		guard(),
		jen.Id("r").Dot("assoc").Dot("ImportState").Call(jen.Id("ctx"), jen.Id("req"), jen.Id("resp")),
	)
}
