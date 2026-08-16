package tf

import (
	"fmt"
	"strings"

	"github.com/dave/jennifer/jen"
)

const (
	pkgRuntimeTf   = "github.com/activatedio/tfinfra/pkg/tf"
	pkgResource    = "github.com/hashicorp/terraform-plugin-framework/resource"
	pkgDatasource  = "github.com/hashicorp/terraform-plugin-framework/datasource"
	pkgFieldmaskpb = "google.golang.org/protobuf/types/known/fieldmaskpb"
)

// entityNames bundles the derived identifiers one entry generates around.
type entityNames struct {
	Entity     string // Pet
	TypeName   string // pet (Terraform type suffix)
	Collection string // pets (AIP collection)
	Model      string // PetModel
	LowerCamel string // pet (Go identifier prefix for unexported types)
}

func namesFor(e Entry, res Resource) entityNames {

	t := entityType(e)

	collection := res.Collection
	if collection == "" {
		collection = lowerFirst(pluralizeClient.Plural(t.Name()))
	}

	return entityNames{
		Entity:     t.Name(),
		TypeName:   toSnake(t.Name()),
		Collection: collection,
		Model:      t.Name() + "Model",
		LowerCamel: lowerFirst(t.Name()),
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// entityPtr returns *<pkg>.<Entity>.
func entityPtr(e Entry) *jen.Statement {
	t := entityType(e)
	return jen.Op("*").Qual(t.PkgPath(), t.Name())
}

// crudType returns *tf.Crud[*pb.Entity, *EntityModel].
func crudType(e Entry, n entityNames) *jen.Statement {
	return jen.Op("*").Qual(pkgRuntimeTf, "Crud").Index(jen.List(entityPtr(e), jen.Op("*").Id(n.Model)))
}

// writeCrudFactory emits new<Entity>Crud(providerData any), the shared
// Configure body for the resource and the data source.
func writeCrudFactory(f *jen.File, e Entry, res Resource, cm clientModel, n entityNames) {

	clientKey := res.Client
	if clientKey == "" {
		clientKey = "default"
	}

	clientQual := jen.Qual(cm.Type.PkgPath(), cm.Type.Name())

	scopeArgs := make([]jen.Code, 0, len(res.Scope.Collections()))
	for _, c := range res.Scope.Collections() {
		scopeArgs = append(scopeArgs, jen.Lit(c))
	}

	params := jen.Dict{
		jen.Id("TypeName"):   jen.Lit(n.TypeName),
		jen.Id("Collection"): jen.Lit(n.Collection),
		jen.Id("Scope"):      jen.Qual(pkgRuntimeTf, "NewScope").Call(scopeArgs...),
		jen.Id("Defaults"):   jen.Id("pd").Dot("Defaults"),
		jen.Id("NewModel"):   jen.Id("New" + n.Model),
		jen.Id("Client"):     clientAdapters(e, cm),
	}
	if res.UseUpdate {
		params[jen.Id("UseUpdate")] = jen.True()
	}

	f.Commentf("new%sCrud builds the %s runtime from provider data; it returns nil (no error) before the provider is configured.", n.Entity, n.TypeName)
	f.Func().Id("new"+n.Entity+"Crud").Params(jen.Id("providerData").Any()).
		Params(crudType(e, n), jen.Qual(pkgDiag, "Diagnostics")).
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
					jen.Lit(fmt.Sprintf("provider data key %q is not a %s.%s", clientKey, cm.Type.PkgPath(), cm.Type.Name())),
				),
				jen.Return(jen.Nil(), jen.Id("diags")),
			),
			jen.Return(
				jen.Qual(pkgRuntimeTf, "NewCrud").Call(
					jen.Qual(pkgRuntimeTf, "CrudParams").Index(jen.List(entityPtr(e), jen.Op("*").Id(n.Model))).Values(params),
				),
				jen.Id("diags"),
			),
		)
}

// clientAdapters builds the tf.CrudClient literal wiring each available
// operation to the stub.
func clientAdapters(e Entry, cm clientModel) jen.Code {

	d := jen.Dict{}
	ePtr := func() *jen.Statement { return entityPtr(e) }

	reqValues := func(op *clientOp, fields jen.Dict) *jen.Statement {
		return jen.Id("client").Dot(op.Method).Call(
			jen.Id("ctx"),
			jen.Op("&").Qual(op.RequestType.PkgPath(), op.RequestType.Name()).Values(fields),
		)
	}

	if op := cm.Get; op != nil {
		d[jen.Id("Get")] = jen.Func().
			Params(jen.Id("ctx").Qual("context", "Context"), jen.Id("name").String()).
			Params(ePtr(), jen.Error()).
			Block(jen.Return(reqValues(op, jen.Dict{jen.Id(op.NameField): jen.Id("name")})))
	}

	if op := cm.Create; op != nil {
		fields := jen.Dict{jen.Id(op.EntityField): jen.Id("entity")}
		if op.ParentField != "" {
			fields[jen.Id(op.ParentField)] = jen.Id("parent")
		}
		d[jen.Id("Create")] = jen.Func().
			Params(jen.Id("ctx").Qual("context", "Context"), jen.Id("parent").String(), jen.Id("entity").Add(ePtr())).
			Params(ePtr(), jen.Error()).
			Block(jen.Return(reqValues(op, fields)))
	}

	if op := cm.Update; op != nil {
		d[jen.Id("Update")] = jen.Func().
			Params(jen.Id("ctx").Qual("context", "Context"), jen.Id("name").String(), jen.Id("entity").Add(ePtr())).
			Params(ePtr(), jen.Error()).
			Block(jen.Return(reqValues(op, jen.Dict{
				jen.Id(op.NameField):   jen.Id("name"),
				jen.Id(op.EntityField): jen.Id("entity"),
			})))
	}

	if op := cm.Patch; op != nil {
		d[jen.Id("Patch")] = jen.Func().
			Params(jen.Id("ctx").Qual("context", "Context"), jen.Id("name").String(), jen.Id("entity").Add(ePtr()), jen.Id("mask").Index().String()).
			Params(ePtr(), jen.Error()).
			Block(jen.Return(reqValues(op, jen.Dict{
				jen.Id(op.NameField):   jen.Id("name"),
				jen.Id(op.EntityField): jen.Id("entity"),
				jen.Id(op.MaskField): jen.Op("&").Qual(pkgFieldmaskpb, "FieldMask").Values(jen.Dict{
					jen.Id("Paths"): jen.Id("mask"),
				}),
			})))
	}

	if op := cm.Delete; op != nil {
		d[jen.Id("Delete")] = jen.Func().
			Params(jen.Id("ctx").Qual("context", "Context"), jen.Id("name").String()).
			Error().
			Block(
				jen.List(jen.Id("_"), jen.Id("err")).Op(":=").Add(reqValues(op, jen.Dict{jen.Id(op.NameField): jen.Id("name")})),
				jen.Return(jen.Id("err")),
			)
	}

	if op := cm.List; op != nil {
		fields := jen.Dict{jen.Id(op.PageTokenField): jen.Id("pageToken")}
		if op.ParentField != "" {
			fields[jen.Id(op.ParentField)] = jen.Id("parent")
		}
		d[jen.Id("List")] = jen.Func().
			Params(jen.Id("ctx").Qual("context", "Context"), jen.Id("parent").String(), jen.Id("pageToken").String()).
			Params(jen.Index().Add(ePtr()), jen.String(), jen.Error()).
			Block(
				jen.List(jen.Id("out"), jen.Id("err")).Op(":=").Add(reqValues(op, fields)),
				jen.If(jen.Id("err").Op("!=").Nil()).Block(
					jen.Return(jen.Nil(), jen.Lit(""), jen.Id("err")),
				),
				jen.Return(jen.Id("out").Dot(op.ResponseItemsField), jen.Id("out").Dot(op.ResponseNextField), jen.Nil()),
			)
	}

	return jen.Qual(pkgRuntimeTf, "CrudClient").Index(entityPtr(e)).Values(d)
}

// writeResource emits the resource type: thin guarded delegates over the
// Crud runtime.
func writeResource(f *jen.File, e Entry, n entityNames) {

	recvName := n.LowerCamel + "Resource"

	f.Commentf("%s is the generated Terraform resource for %s.", recvName, n.Entity)
	f.Type().Id(recvName).Struct(
		jen.Id("crud").Add(crudType(e, n)),
	)

	f.Commentf("New%sResource returns the generated %s resource; its client and scope defaults arrive via Configure from tf.ProviderData.", n.Entity, n.TypeName)
	f.Func().Id("New"+n.Entity+"Resource").Params().Qual(pkgResource, "Resource").Block(
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
		jen.Id("resp").Dot("TypeName").Op("=").Id("req").Dot("ProviderTypeName").Op("+").Lit("_" + n.TypeName),
	)

	recv().Id("Schema").Params(
		jen.Id("_").Qual("context", "Context"),
		jen.Id("_").Qual(pkgResource, "SchemaRequest"),
		jen.Id("resp").Op("*").Qual(pkgResource, "SchemaResponse"),
	).Block(
		jen.Id("resp").Dot("Schema").Op("=").Id(n.Entity + "ResourceSchema").Call(),
	)

	recv().Id("Configure").Params(
		jen.Id("_").Qual("context", "Context"),
		jen.Id("req").Qual(pkgResource, "ConfigureRequest"),
		jen.Id("resp").Op("*").Qual(pkgResource, "ConfigureResponse"),
	).Block(
		jen.List(jen.Id("crud"), jen.Id("diags")).Op(":=").Id("new"+n.Entity+"Crud").Call(jen.Id("req").Dot("ProviderData")),
		jen.Id("resp").Dot("Diagnostics").Dot("Append").Call(jen.Id("diags").Op("...")),
		jen.Id("r").Dot("crud").Op("=").Id("crud"),
	)

	guard := func() jen.Code {
		return jen.If(jen.Id("r").Dot("crud").Op("==").Nil()).Block(
			jen.Id("resp").Dot("Diagnostics").Dot("AddError").Call(
				jen.Lit(fmt.Sprintf("%s resource not configured", n.TypeName)),
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
			jen.Id("r").Dot("crud").Dot(verb).Call(jen.Id("ctx"), jen.Id("req"), jen.Id("resp")),
		)
	}

	recv().Id("ImportState").Params(
		jen.Id("ctx").Qual("context", "Context"),
		jen.Id("req").Qual(pkgResource, "ImportStateRequest"),
		jen.Id("resp").Op("*").Qual(pkgResource, "ImportStateResponse"),
	).Block(
		guard(),
		jen.Id("r").Dot("crud").Dot("ImportState").Call(jen.Id("ctx"), jen.Id("req"), jen.Id("resp")),
	)
}

// writeDataSource emits the singular data source: Get by full resource name.
func writeDataSource(f *jen.File, e Entry, n entityNames) {

	recvName := n.LowerCamel + "DataSource"

	f.Commentf("%s is the generated singular data source for %s (Get by name).", recvName, n.Entity)
	f.Type().Id(recvName).Struct(
		jen.Id("crud").Add(crudType(e, n)),
	)

	f.Commentf("New%sDataSource returns the generated %s data source.", n.Entity, n.TypeName)
	f.Func().Id("New"+n.Entity+"DataSource").Params().Qual(pkgDatasource, "DataSource").Block(
		jen.Return(jen.Op("&").Id(recvName).Values()),
	)

	recv := func() *jen.Statement {
		return f.Func().Params(jen.Id("d").Op("*").Id(recvName))
	}

	recv().Id("Metadata").Params(
		jen.Id("_").Qual("context", "Context"),
		jen.Id("req").Qual(pkgDatasource, "MetadataRequest"),
		jen.Id("resp").Op("*").Qual(pkgDatasource, "MetadataResponse"),
	).Block(
		jen.Id("resp").Dot("TypeName").Op("=").Id("req").Dot("ProviderTypeName").Op("+").Lit("_" + n.TypeName),
	)

	recv().Id("Schema").Params(
		jen.Id("_").Qual("context", "Context"),
		jen.Id("_").Qual(pkgDatasource, "SchemaRequest"),
		jen.Id("resp").Op("*").Qual(pkgDatasource, "SchemaResponse"),
	).Block(
		jen.Id("resp").Dot("Schema").Op("=").Id(n.Entity + "DataSourceSchema").Call(),
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

	recv().Id("Read").Params(
		jen.Id("ctx").Qual("context", "Context"),
		jen.Id("req").Qual(pkgDatasource, "ReadRequest"),
		jen.Id("resp").Op("*").Qual(pkgDatasource, "ReadResponse"),
	).Block(
		jen.If(jen.Id("d").Dot("crud").Op("==").Nil()).Block(
			jen.Id("resp").Dot("Diagnostics").Dot("AddError").Call(
				jen.Lit(fmt.Sprintf("%s data source not configured", n.TypeName)),
				jen.Lit("Configure was not called with tf.ProviderData"),
			),
			jen.Return(),
		),
		jen.Id("d").Dot("crud").Dot("ReadDataSource").Call(jen.Id("ctx"), jen.Id("req"), jen.Id("resp")),
	)
}
