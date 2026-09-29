package tf

import (
	"fmt"

	"github.com/dave/jennifer/jen"
)

// listNames are the identifiers of one entry's plural data source.
type listNames struct {
	Plural   string // Pets (Go identifier prefix)
	TypeName string // pets (Terraform type suffix, and the list attribute)
	Item     string // PetItemModel
	Model    string // PetsModel
}

func listNamesFor(n entityNames, dsl DataSourceList) listNames {
	plural := pluralizeClient.Plural(n.Entity)
	typeName := dsl.TypeName
	if typeName == "" {
		typeName = toSnake(plural)
	}
	return listNames{
		Plural:   plural,
		TypeName: typeName,
		Item:     n.Entity + "ItemModel",
		Model:    plural + "Model",
	}
}

// itemFields are the fields a list item carries: every field but the
// input-only ones, which the API never returns.
func itemFields(fields []Field) []Field {
	out := make([]Field, 0, len(fields))
	for _, fd := range fields {
		if !fd.InputOnly {
			out = append(out, fd)
		}
	}
	return out
}

// itemAttrType is the attr.Type of an item field, nested objects included.
func itemAttrType(owner string, fd Field) *jen.Statement {
	if fd.Kind == FieldNestedMessage {
		return jen.Qual(pkgTypes, "ObjectType").Values(jen.Dict{
			jen.Id("AttrTypes"): jen.Id(nestedAttrTypesName(owner, fd)).Call(),
		})
	}
	return attrTypeFor(fd)
}

// writeListDataSource emits the plural data source: the item model and its
// attribute types, the data source model, the schema, the proto-to-item
// conversion, and the data source glue.
func writeListDataSource(f *jen.File, e Entry, res Resource, n entityNames, ln listNames, fields []Field) {

	t := entityType(e)
	items := itemFields(fields)
	scopeAttrs := res.Scope.IdentifierAttributes()

	// The item model: name, the caller-assigned id, the entity's fields.
	itemStruct := make([]jen.Code, 0, len(items)+1)
	itemTypes := jen.Dict{}
	if n.IDAttribute != "" {
		itemStruct = append(itemStruct, jen.Id(snakeToCamel(n.IDAttribute)).Qual(pkgTypes, "String").Tag(map[string]string{tfsdkTag: n.IDAttribute}))
		itemTypes[jen.Lit(n.IDAttribute)] = jen.Qual(pkgTypes, "StringType")
	}
	for _, fd := range items {
		itemStruct = append(itemStruct, jen.Id(fd.GoName).Add(modelFieldType(fd.Kind)).Tag(map[string]string{tfsdkTag: fd.TfName()}))
		itemTypes[jen.Lit(fd.TfName())] = itemAttrType(n.Entity, fd)
	}

	f.Commentf("%s is one element of the %s data source's %q list.", ln.Item, ln.TypeName, ln.TypeName)
	f.Type().Id(ln.Item).Struct(itemStruct...)

	f.Commentf("%sItemAttrTypes returns the attribute types of one %s list element.", n.Entity, ln.TypeName)
	f.Func().Id(n.Entity+"ItemAttrTypes").Params().Map(jen.String()).Qual(pkgAttr, "Type").Block(
		jen.Return(jen.Map(jen.String()).Qual(pkgAttr, "Type").Values(itemTypes)),
	)

	// The data source model: the scope identifiers and the list.
	modelStruct := make([]jen.Code, 0, len(scopeAttrs)+1)
	for _, attr := range scopeAttrs {
		modelStruct = append(modelStruct, jen.Id(snakeToCamel(attr)).Qual(pkgTypes, "String").Tag(map[string]string{tfsdkTag: attr}))
	}
	modelStruct = append(modelStruct, jen.Id("Items").Qual(pkgTypes, "List").Tag(map[string]string{tfsdkTag: ln.TypeName}))

	f.Commentf("%s is the Terraform model of the %s data source.", ln.Model, ln.TypeName)
	f.Type().Id(ln.Model).Struct(modelStruct...)

	writeListDataSourceSchema(f, t.Name(), res, n, ln, items, scopeAttrs)
	writeItemFromProto(f, e, n, ln, items)
	writeListDataSourceGlue(f, e, n, ln, scopeAttrs)
}

func writeListDataSourceSchema(f *jen.File, entity string, res Resource, n entityNames, ln listNames, items []Field, scopeAttrs []string) {

	itemAttrs := jen.Dict{}
	if n.IDAttribute != "" {
		itemAttrs[jen.Lit(n.IDAttribute)] = jen.Qual(pkgDatasourceSchema, "StringAttribute").Values(jen.Dict{
			jen.Id("Computed"):            jen.True(),
			jen.Id("MarkdownDescription"): jen.Lit(dataSourceIDDescription(n)),
		})
	}
	for _, fd := range items {
		if fd.ProtoName == NameField {
			itemAttrs[jen.Lit(fd.TfName())] = jen.Qual(pkgDatasourceSchema, "StringAttribute").Values(jen.Dict{
				jen.Id("Computed"):            jen.True(),
				jen.Id("MarkdownDescription"): jen.Lit("Full resource name."),
			})
			continue
		}
		itemAttrs[jen.Lit(fd.TfName())] = dataSourceAttributeFor(fd)
	}

	attrs := jen.Dict{
		jen.Lit(ln.TypeName): jen.Qual(pkgDatasourceSchema, "ListNestedAttribute").Values(jen.Dict{
			jen.Id("Computed"):            jen.True(),
			jen.Id("MarkdownDescription"): jen.Lit(fmt.Sprintf("Every %s under the parent, in the order the API lists them.", toSnake(entity))),
			jen.Id("NestedObject"): jen.Qual(pkgDatasourceSchema, "NestedAttributeObject").Values(jen.Dict{
				jen.Id("Attributes"): jen.Map(jen.String()).Qual(pkgDatasourceSchema, "Attribute").Values(itemAttrs),
			}),
		}),
	}
	for _, attr := range scopeAttrs {
		d := jen.Dict{
			jen.Id("Optional"):            jen.True(),
			jen.Id("MarkdownDescription"): jen.Lit(fmt.Sprintf("Parent identifier `%s`; overrides the provider default.", attr)),
		}
		if ref, ok := n.ScopeRefs[attr]; ok {
			d[jen.Id("Validators")] = referenceValidators(ref, n.Examples)
		}
		attrs[jen.Lit(attr)] = jen.Qual(pkgDatasourceSchema, "StringAttribute").Values(d)
	}

	desc := fmt.Sprintf("Lists every %s under one parent.", entity)
	if res.Description != "" {
		desc = res.Description + " This data source lists every one under a parent."
	}

	f.Commentf("%sListDataSourceSchema returns the Terraform schema for the %s data source.", entity, ln.TypeName)
	f.Func().Id(entity+"ListDataSourceSchema").Params().Qual(pkgDatasourceSchema, "Schema").Block(
		jen.Return(jen.Qual(pkgDatasourceSchema, "Schema").Values(jen.Dict{
			jen.Id("MarkdownDescription"): jen.Lit(desc),
			jen.Id("Attributes"):          jen.Map(jen.String()).Qual(pkgDatasourceSchema, "Attribute").Values(attrs),
		})),
	)
}

// writeItemFromProto emits the conversion of one listed entity into a list
// element. It reuses the resource's FromProto statements against a fresh
// item model, so an item reads exactly as the singular data source would.
func writeItemFromProto(f *jen.File, e Entry, n entityNames, ln listNames, items []Field) {

	t := entityType(e)
	// The item is "item", not "n": a nested attribute's read-back declares
	// its own n inside its block.
	c := conv{model: ident("item"), proto: ident("e")}

	body := []jen.Code{
		jen.Var().Id("diags").Qual(pkgDiag, "Diagnostics"),
		jen.Var().Id("item").Id(ln.Item),
	}
	for _, fd := range items {
		body = append(body, fromProtoStatement(fd, c, n.Entity))
	}
	if n.IDAttribute != "" {
		body = append(body,
			jen.List(jen.Id("id"), jen.Id("err")).Op(":=").Id("crud").Dot("IDFromName").Call(jen.Id("e").Dot("Name")),
			jen.If(jen.Id("err").Op("!=").Nil()).Block(
				jen.Id("diags").Dot("AddError").Call(jen.Lit(fmt.Sprintf("unexpected %s name", n.TypeName)), jen.Id("err").Dot("Error").Call()),
			),
			jen.Id("item").Dot(snakeToCamel(n.IDAttribute)).Op("=").Qual(pkgTypes, "StringValue").Call(jen.Id("id")),
		)
	}
	body = append(body,
		jen.List(jen.Id("obj"), jen.Id("d")).Op(":=").Qual(pkgTypes, "ObjectValueFrom").Call(
			jen.Id("ctx"), jen.Id(n.Entity+"ItemAttrTypes").Call(), jen.Id("item"),
		),
		jen.Id("diags").Dot("Append").Call(jen.Id("d").Op("...")),
		jen.Return(jen.Id("obj"), jen.Id("diags")),
	)

	f.Commentf("%sItemFromProto converts one listed %s into a %s list element.", n.LowerCamel, n.Entity, ln.TypeName)
	f.Func().Id(n.LowerCamel+"ItemFromProto").Params(
		jen.Id("ctx").Qual("context", "Context"),
		jen.Id("crud").Add(crudType(e, n)),
		jen.Id("e").Op("*").Qual(t.PkgPath(), t.Name()),
	).Params(jen.Qual(pkgTypes, "Object"), jen.Qual(pkgDiag, "Diagnostics")).Block(body...)
}

func writeListDataSourceGlue(f *jen.File, e Entry, n entityNames, ln listNames, scopeAttrs []string) {

	recvName := lowerFirst(ln.Plural) + "DataSource"

	f.Commentf("%s is the generated plural data source for %s (List under a parent).", recvName, n.Entity)
	f.Type().Id(recvName).Struct(jen.Id("crud").Add(crudType(e, n)))

	f.Commentf("New%sDataSource returns the generated %s data source.", ln.Plural, ln.TypeName)
	f.Func().Id("New"+ln.Plural+"DataSource").Params().Qual(pkgDatasource, "DataSource").Block(
		jen.Return(jen.Op("&").Id(recvName).Values()),
	)

	recv := func() *jen.Statement { return f.Func().Params(jen.Id("d").Op("*").Id(recvName)) }

	recv().Id("Metadata").Params(
		jen.Id("_").Qual("context", "Context"),
		jen.Id("req").Qual(pkgDatasource, "MetadataRequest"),
		jen.Id("resp").Op("*").Qual(pkgDatasource, "MetadataResponse"),
	).Block(
		jen.Id("resp").Dot("TypeName").Op("=").Id("req").Dot("ProviderTypeName").Op("+").Lit("_" + ln.TypeName),
	)

	recv().Id("Schema").Params(
		jen.Id("_").Qual("context", "Context"),
		jen.Id("_").Qual(pkgDatasource, "SchemaRequest"),
		jen.Id("resp").Op("*").Qual(pkgDatasource, "SchemaResponse"),
	).Block(
		jen.Id("resp").Dot("Schema").Op("=").Id(n.Entity + "ListDataSourceSchema").Call(),
	)

	recv().Id("Configure").Params(
		jen.Id("_").Qual("context", "Context"),
		jen.Id("req").Qual(pkgDatasource, "ConfigureRequest"),
		jen.Id("resp").Op("*").Qual(pkgDatasource, "ConfigureResponse"),
	).Block(
		jen.List(jen.Id("crud"), jen.Id("diags")).Op(":=").Id("new"+n.Entity+"Crud").Call(jen.Id("req").Dot("ProviderData")),
		jen.Id("resp").Dot("Diagnostics").Dot("Append").Call(jen.Id("diags").Op("...")),
		jen.Id("d").Dot("crud").Op("=").Id("crud"),
	)

	ids := jen.Dict{}
	for _, attr := range scopeAttrs {
		ids[jen.Lit(attr)] = jen.Id("m").Dot(snakeToCamel(attr)).Dot("ValueString").Call()
	}

	recv().Id("Read").Params(
		jen.Id("ctx").Qual("context", "Context"),
		jen.Id("req").Qual(pkgDatasource, "ReadRequest"),
		jen.Id("resp").Op("*").Qual(pkgDatasource, "ReadResponse"),
	).Block(
		jen.If(jen.Id("d").Dot("crud").Op("==").Nil()).Block(
			jen.Id("resp").Dot("Diagnostics").Dot("AddError").Call(
				jen.Lit(fmt.Sprintf("%s data source not configured", ln.TypeName)),
				jen.Lit("Configure was not called with tf.ProviderData"),
			),
			jen.Return(),
		),
		jen.Var().Id("m").Id(ln.Model),
		jen.Id("resp").Dot("Diagnostics").Dot("Append").Call(jen.Id("req").Dot("Config").Dot("Get").Call(jen.Id("ctx"), jen.Op("&").Id("m")).Op("...")),
		jen.If(jen.Id("resp").Dot("Diagnostics").Dot("HasError").Call()).Block(jen.Return()),
		jen.List(jen.Id("entities"), jen.Id("err")).Op(":=").Id("d").Dot("crud").Dot("ListAll").Call(
			jen.Id("ctx"), jen.Map(jen.String()).String().Values(ids),
		),
		jen.If(jen.Id("err").Op("!=").Nil()).Block(
			jen.Id("resp").Dot("Diagnostics").Dot("AddError").Call(jen.Lit(fmt.Sprintf("list %s failed", ln.TypeName)), jen.Id("err").Dot("Error").Call()),
			jen.Return(),
		),
		jen.Id("elems").Op(":=").Make(jen.Index().Qual(pkgAttr, "Value"), jen.Lit(0), jen.Len(jen.Id("entities"))),
		jen.For(jen.List(jen.Id("_"), jen.Id("e")).Op(":=").Range().Id("entities")).Block(
			jen.List(jen.Id("obj"), jen.Id("diags")).Op(":=").Id(n.LowerCamel+"ItemFromProto").Call(jen.Id("ctx"), jen.Id("d").Dot("crud"), jen.Id("e")),
			jen.Id("resp").Dot("Diagnostics").Dot("Append").Call(jen.Id("diags").Op("...")),
			jen.Id("elems").Op("=").Append(jen.Id("elems"), jen.Id("obj")),
		),
		jen.If(jen.Id("resp").Dot("Diagnostics").Dot("HasError").Call()).Block(jen.Return()),
		jen.List(jen.Id("list"), jen.Id("diags")).Op(":=").Qual(pkgTypes, "ListValue").Call(
			jen.Qual(pkgTypes, "ObjectType").Values(jen.Dict{jen.Id("AttrTypes"): jen.Id(n.Entity + "ItemAttrTypes").Call()}),
			jen.Id("elems"),
		),
		jen.Id("resp").Dot("Diagnostics").Dot("Append").Call(jen.Id("diags").Op("...")),
		jen.Id("m").Dot("Items").Op("=").Id("list"),
		jen.Id("resp").Dot("Diagnostics").Dot("Append").Call(jen.Id("resp").Dot("State").Dot("Set").Call(jen.Id("ctx"), jen.Op("&").Id("m")).Op("...")),
	)
}
