package tf

import (
	"fmt"

	"github.com/dave/jennifer/jen"

	runtimetf "github.com/activatedio/tfinfra/pkg/tf"
)

const (
	pkgResourceSchema   = "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	pkgPlanmodifier     = "github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	pkgSchemaValidator  = "github.com/hashicorp/terraform-plugin-framework/schema/validator"
	pkgStringValidators = "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	pkgTypes            = "github.com/hashicorp/terraform-plugin-framework/types"
	pkgDiag             = "github.com/hashicorp/terraform-plugin-framework/diag"
	pkgPath             = "github.com/hashicorp/terraform-plugin-framework/path"
)

// attrShape maps a FieldKind to the framework attribute type and the
// per-type plan modifier package.
type attrShape struct {
	attribute       string
	planModifier    string
	planModifierPkg string
	elementType     bool
	jsonCustomType  bool
}

// attrShapes is the attribute shape of every supported kind.
var attrShapes = func() map[FieldKind]attrShape {
	base := "github.com/hashicorp/terraform-plugin-framework/resource/schema/"
	str := attrShape{"StringAttribute", "String", base + "stringplanmodifier", false, false}
	json := attrShape{"StringAttribute", "String", base + "stringplanmodifier", false, true}
	return map[FieldKind]attrShape{
		FieldString:          str,
		FieldEnum:            str,
		FieldTimestamp:       str,
		FieldDuration:        str,
		FieldBool:            {"BoolAttribute", "Bool", base + "boolplanmodifier", false, false},
		FieldInt64:           {"Int64Attribute", "Int64", base + "int64planmodifier", false, false},
		FieldFloat64:         {"Float64Attribute", "Float64", base + "float64planmodifier", false, false},
		FieldStringList:      {"ListAttribute", "List", base + "listplanmodifier", true, false},
		FieldStringMap:       {"MapAttribute", "Map", base + "mapplanmodifier", true, false},
		FieldAny:             json,
		FieldStruct:          json,
		FieldJSONMessage:     json,
		FieldJSONList:        json,
		FieldNestedMessage:   {"SingleNestedAttribute", "Object", base + "objectplanmodifier", false, false},
		FieldRepeatedMessage: {"ListNestedAttribute", "List", base + "listplanmodifier", false, false},
	}
}()

func shapeFor(kind FieldKind) attrShape {
	shape, ok := attrShapes[kind]
	if !ok {
		panic(fmt.Sprintf("unhandled field kind %d", kind))
	}
	return shape
}

// writeResourceSchema emits func <Entity>ResourceSchema() schema.Schema.
func writeResourceSchema(f *jen.File, e Entry, res Resource, n entityNames, fields []Field) {

	t := entityType(e)

	attrs := jen.Dict{}

	switch {
	case n.IDField:
		// The id is a proto field: its own attribute, built below.
	case n.CallerNamed:
		// The caller-assigned id: required, replacement on change — the
		// server composes "name" from the parent and this id.
		attrs[jen.Lit(n.IDAttribute)] = jen.Qual(pkgResourceSchema, "StringAttribute").Values(jen.Dict{
			jen.Id("Required"):            jen.True(),
			jen.Id("MarkdownDescription"): jen.Lit("Caller-assigned resource id — the last segment of `name`, which the server composes from the parent and this id. Changing it replaces the resource."),
			jen.Id("PlanModifiers"): jen.Index().Qual(pkgPlanmodifier, "String").Values(
				jen.Qual(shapeFor(FieldString).planModifierPkg, "RequiresReplace").Call(),
			),
		})
	case n.IDAttribute != "":
		// The server-assigned id, read from "name". It never changes in
		// place, so every plan after the create keeps it.
		attrs[jen.Lit(n.IDAttribute)] = jen.Qual(pkgResourceSchema, "StringAttribute").Values(jen.Dict{
			jen.Id("Computed"):            jen.True(),
			jen.Id("MarkdownDescription"): jen.Lit(serverIDDescription),
			jen.Id("PlanModifiers"): jen.Index().Qual(pkgPlanmodifier, "String").Values(
				jen.Qual(shapeFor(FieldString).planModifierPkg, "UseStateForUnknown").Call(),
			),
		})
	}

	// Scope identifier attributes: optional, replacement on change. One the
	// entity carries as a field is built with the fields.
	carried := parentIDFields(fields)
	for _, attr := range res.Scope.IdentifierAttributes() {
		if carried[attr] {
			continue
		}
		d := jen.Dict{
			jen.Id("Optional"):            jen.True(),
			jen.Id("MarkdownDescription"): jen.Lit(fmt.Sprintf("Parent identifier `%s`; overrides the provider default. Changing it replaces the resource.", attr)),
			jen.Id("PlanModifiers"): jen.Index().Qual(pkgPlanmodifier, "String").Values(
				jen.Qual(shapeFor(FieldString).planModifierPkg, "RequiresReplace").Call(),
			),
		}
		if ref, ok := n.ScopeRefs[attr]; ok {
			d[jen.Id("Validators")] = referenceValidators(ref, n.Examples)
		}
		attrs[jen.Lit(attr)] = jen.Qual(pkgResourceSchema, "StringAttribute").Values(d)
	}

	for _, fd := range fields {
		if fd.ParentID {
			attrs[jen.Lit(fd.TfName())] = parentIDAttributeFor(fd, n)
			continue
		}
		attrs[jen.Lit(fd.TfName())] = attributeFor(fd, n.Examples)
	}

	if res.Mint != nil {
		writeMintAttributes(attrs, res.Mint)
	}

	f.Commentf("%sResourceSchema returns the Terraform schema for the %s resource.", t.Name(), t.Name())
	f.Func().Id(t.Name()+"ResourceSchema").Params().Qual(pkgResourceSchema, "Schema").Block(
		jen.Return(jen.Qual(pkgResourceSchema, "Schema").Values(jen.Dict{
			jen.Id("MarkdownDescription"): jen.Lit(resourceDescription(t.Name(), res)),
			jen.Id("Attributes"): jen.Map(jen.String()).Qual(pkgResourceSchema, "Attribute").Values(
				attrs,
			),
		})),
	)
}

const pkgDatasourceSchema = "github.com/hashicorp/terraform-plugin-framework/datasource/schema"

// writeDataSourceSchema emits func <Entity>DataSourceSchema() for the
// singular data source: name required, everything else computed.
// resourceDescription is the entry's Description, or the generated
// placeholder when it has none.
func resourceDescription(typeName string, res Resource) string {
	desc := res.Description
	if desc == "" {
		desc = fmt.Sprintf("%s resource.", typeName)
	}
	if res.DeleteForgets {
		desc += " The API cannot delete one: destroying it removes it from Terraform state, and the record stays."
	}
	return desc
}

// dataSourceDescription says what the singular data source reads, led by
// the entry's Description when it has one.
func dataSourceDescription(typeName string, res Resource) string {
	desc := fmt.Sprintf("%s data source: reads one %s by its full resource name.", typeName, typeName)
	if res.Description != "" {
		desc = res.Description + " This data source reads one by its full resource name."
	}
	return desc
}

func writeDataSourceSchema(f *jen.File, e Entry, res Resource, n entityNames, fields []Field) {

	t := entityType(e)

	attrs := jen.Dict{}

	if n.IDAttribute != "" && !n.IDField {
		attrs[jen.Lit(n.IDAttribute)] = jen.Qual(pkgDatasourceSchema, "StringAttribute").Values(jen.Dict{
			jen.Id("Computed"):            jen.True(),
			jen.Id("MarkdownDescription"): jen.Lit(dataSourceIDDescription(n)),
		})
	}

	carried := parentIDFields(fields)
	for _, attr := range res.Scope.IdentifierAttributes() {
		if carried[attr] {
			continue
		}
		attrs[jen.Lit(attr)] = jen.Qual(pkgDatasourceSchema, "StringAttribute").Values(jen.Dict{
			jen.Id("Computed"): jen.True(),
		})
	}

	for _, fd := range fields {
		attrs[jen.Lit(fd.TfName())] = dataSourceAttributeFor(fd)
	}

	f.Commentf("%sDataSourceSchema returns the Terraform schema for the singular %s data source.", t.Name(), t.Name())
	f.Func().Id(t.Name()+"DataSourceSchema").Params().Qual(pkgDatasourceSchema, "Schema").Block(
		jen.Return(jen.Qual(pkgDatasourceSchema, "Schema").Values(jen.Dict{
			jen.Id("MarkdownDescription"): jen.Lit(dataSourceDescription(t.Name(), res)),
			jen.Id("Attributes"): jen.Map(jen.String()).Qual(pkgDatasourceSchema, "Attribute").Values(
				attrs,
			),
		})),
	)
}

// applyTypeKeys adds the schema keys that come from a field's type rather
// than its behavior: a collection's element type, a JSON attribute's custom
// type, and a nested message's own attributes. pkg is the schema package the
// nested attributes are built in, and nested builds each child.
func applyTypeKeys(d jen.Dict, pkg string, fd Field, nested func(Field) jen.Code) {

	shape := shapeFor(fd.Kind)

	if shape.elementType {
		d[jen.Id("ElementType")] = jen.Qual(pkgTypes, "StringType")
	}
	if shape.jsonCustomType {
		d[jen.Id("CustomType")] = jen.Qual(pkgJsontypes, "NormalizedType").Values()
	}
	if fd.Kind == FieldNestedMessage {
		d[jen.Id("Attributes")] = nestedAttributes(pkg, fd, nested)
	}
	if fd.Kind == FieldRepeatedMessage {
		d[jen.Id("NestedObject")] = jen.Qual(pkg, "NestedAttributeObject").Values(jen.Dict{
			jen.Id("Attributes"): nestedAttributes(pkg, fd, nested),
		})
	}
}

// parentIDFields is the set of scope identifier attributes a field
// carries.
func parentIDFields(fields []Field) map[string]bool {
	out := map[string]bool{}
	for _, fd := range fields {
		if fd.ParentID {
			out[fd.TfName()] = true
		}
	}
	return out
}

// parentIDAttributeFor builds a scope identifier the entity also carries
// as a field. Unlike a plain identifier it is Computed as well: reads fill
// it from the field, so an import has it and a resource that took the
// provider default shows it. Changing it still replaces the resource.
func parentIDAttributeFor(fd Field, n entityNames) jen.Code {

	d := jen.Dict{
		jen.Id("Optional"): jen.True(),
		jen.Id("Computed"): jen.True(),
		jen.Id("MarkdownDescription"): jen.Lit(fmt.Sprintf("Parent identifier `%s`; overrides the provider default, and reads back from the entity. "+
			"Changing it replaces the resource.", fd.TfName())),
		jen.Id("PlanModifiers"): jen.Index().Qual(pkgPlanmodifier, "String").Values(
			jen.Qual(shapeFor(FieldString).planModifierPkg, "RequiresReplace").Call(),
			jen.Qual(shapeFor(FieldString).planModifierPkg, "UseStateForUnknown").Call(),
		),
	}
	if ref, ok := n.ScopeRefs[fd.TfName()]; ok {
		d[jen.Id("Validators")] = referenceValidators(ref, n.Examples)
	}

	return jen.Qual(pkgResourceSchema, "StringAttribute").Values(d)
}

func dataSourceAttributeFor(fd Field) jen.Code {

	shape := shapeFor(fd.Kind)
	d := jen.Dict{}

	if fd.ProtoName == NameField {
		d[jen.Id("Required")] = jen.True()
		d[jen.Id("MarkdownDescription")] = jen.Lit("Full resource name of the object to read.")
	} else {
		d[jen.Id("Computed")] = jen.True()
	}

	if fd.Sensitive {
		d[jen.Id("Sensitive")] = jen.True()
	}
	applyTypeKeys(d, pkgDatasourceSchema, fd, dataSourceNestedAttributeFor)
	if fd.InputOnly {
		d[jen.Id("MarkdownDescription")] = jen.Lit(fmt.Sprintf("`%s` is input only: the API consumes it and never returns it, so this data source always reads it as null.", fd.TfName()))
	}

	return jen.Qual(pkgDatasourceSchema, shape.attribute).Values(d)
}

// nestedAttributes builds the Attributes map of a SingleNestedAttribute
// over the nested message's own fields.
func nestedAttributes(pkg string, fd Field, attr func(Field) jen.Code) jen.Code {

	d := jen.Dict{}
	for _, nf := range fd.Nested {
		d[jen.Lit(nf.TfName())] = attr(nf)
	}

	return jen.Map(jen.String()).Qual(pkg, "Attribute").Values(d)
}

// nestedAttributeFor builds one child of a SingleNestedAttribute in a
// resource schema. Children are Optional+Computed like any other optional
// attribute — proto3 cannot tell zero from unset inside a nested message
// either — but carry no plan modifiers: UseStateForUnknown on a child reads
// prior state at its own path, which is null whenever the parent object was
// null, and would pin the child to null against whatever the server
// actually returns.
func nestedAttributeFor(fd Field) jen.Code {

	shape := shapeFor(fd.Kind)
	d := jen.Dict{
		jen.Id("Optional"): jen.True(),
		jen.Id("Computed"): jen.True(),
	}

	applyTypeKeys(d, pkgResourceSchema, fd, nestedAttributeFor)
	if fd.Kind == FieldEnum {
		d[jen.Id("Validators")] = enumValidators(fd)
	}

	return jen.Qual(pkgResourceSchema, shape.attribute).Values(d)
}

// dataSourceNestedAttributeFor builds one child of a SingleNestedAttribute
// in a data source schema: everything a data source reads is computed.
func dataSourceNestedAttributeFor(fd Field) jen.Code {

	shape := shapeFor(fd.Kind)
	d := jen.Dict{jen.Id("Computed"): jen.True()}

	applyTypeKeys(d, pkgDatasourceSchema, fd, dataSourceNestedAttributeFor)

	return jen.Qual(pkgDatasourceSchema, shape.attribute).Values(d)
}

// serverIDDescription documents a server-named resource's id attribute.
const serverIDDescription = "Server-assigned resource id — the last segment of `name`, and what other resources' `*_id` attributes take."

// dataSourceIDDescription documents the id attribute a data source reads.
func dataSourceIDDescription(n entityNames) string {
	if n.CallerNamed {
		return "Caller-assigned resource id — the last segment of `name`."
	}
	return serverIDDescription
}

// referenceValidators is the Validators value of an attribute holding
// another resource's id. examples is referenceExamples' map.
func referenceValidators(ref Reference, examples map[string]string) jen.Code {
	return jen.Index().Qual(pkgSchemaValidator, "String").Values(
		jen.Qual(pkgRuntimeTf, "ReferenceID").Call(jen.Lit(ref.Prefix), jen.Lit(ref.Target), jen.Lit(examples[ref.Target])),
	)
}

// referenceExamples maps the type name of every resource in the spec to the
// expression that yields its id, for reference validation messages. A
// Target with no resource here, such as a parent the provider does not
// manage, gets no example.
func referenceExamples(spec *Spec) map[string]string {

	out := map[string]string{}
	if spec.ProviderTypeName == "" {
		return out
	}

	for _, e := range spec.Entries {
		if res, ok := GetImplementation[Resource](e); ok {
			n := namesFor(e, res)
			out[n.TypeName] = fmt.Sprintf("%s_%s.<name>.%s", spec.ProviderTypeName, n.TypeName, n.IDAttribute)
		}
	}

	return out
}

func attributeFor(fd Field, examples map[string]string) jen.Code {

	shape := shapeFor(fd.Kind)
	d := jen.Dict{}

	switch {
	case fd.Computed:
		d[jen.Id("Computed")] = jen.True()
	case fd.Required:
		d[jen.Id("Required")] = jen.True()
	case fd.InputOnly:
		// Input-only attributes are Optional alone. Computed would leave
		// them unknown after an apply that never reads them back, and the
		// usual reason for Computed — the server echoing a value into an
		// unset attribute — cannot arise for a field the server never
		// returns.
		d[jen.Id("Optional")] = jen.True()
	default:
		// Optional attributes are also Computed: proto3 cannot distinguish
		// zero from unset, so reads echo server values into unset
		// attributes — plain Optional would make that an "inconsistent
		// result after apply" error for scalars.
		d[jen.Id("Optional")] = jen.True()
		d[jen.Id("Computed")] = jen.True()
	}

	if fd.Sensitive {
		d[jen.Id("Sensitive")] = jen.True()
	}

	applyTypeKeys(d, pkgResourceSchema, fd, nestedAttributeFor)

	if desc := attributeDescription(fd); desc != "" {
		d[jen.Id("MarkdownDescription")] = jen.Lit(desc)
	}

	if mods := planModifiers(fd, shape); mods != nil {
		d[jen.Id("PlanModifiers")] = mods
	}

	if fd.Kind == FieldEnum {
		d[jen.Id("Validators")] = enumValidators(fd)
	}
	if fd.Reference != nil {
		d[jen.Id("Validators")] = referenceValidators(*fd.Reference, examples)
	}

	return jen.Qual(pkgResourceSchema, shape.attribute).Values(d)
}

func attributeDescription(fd Field) string {
	if fd.ProtoName == NameField {
		return "Full resource name; serves as the Terraform ID."
	}
	if fd.ID && fd.Required {
		return "Caller-assigned resource id — the last segment of `name`, which the server composes from it. Changing it replaces the resource."
	}
	if fd.ID {
		return "Resource id — the last segment of `name`. The server assigns one when it is left unset. Changing it replaces the resource."
	}
	if desc := shapeDescription(fd); desc != "" {
		return desc
	}
	if fd.InputOnly {
		return fmt.Sprintf("`%s` is input only: the API consumes it and never returns it, so it is never refreshed from the server and an imported resource has no value for it.", fd.TfName())
	}
	return ""
}

// shapeDescription says how to write a value whose attribute type does not:
// a timestamp, a duration, or a JSON document. Resource attributes and
// config data source inputs share it.
func shapeDescription(fd Field) string {
	if fd.Kind == FieldTimestamp {
		return fmt.Sprintf("`%s` as an RFC 3339 timestamp.", fd.TfName())
	}
	if fd.Kind == FieldDuration {
		return fmt.Sprintf("`%s` as a duration: `\"5s\"`, `\"1.5s\"`, `\"500ms\"`, `\"1m30s\"`.", fd.TfName())
	}
	if fd.Kind == FieldAny {
		return fmt.Sprintf("`%s` as protojson-encoded google.protobuf.Any (JSON object with `@type`); reference a generated config data source's `any` output for the type-safe form.", fd.TfName())
	}
	if fd.Kind == FieldStruct {
		return fmt.Sprintf("`%s` as a JSON object.", fd.TfName())
	}
	if fd.Kind == FieldJSONMessage {
		return fmt.Sprintf("`%s` as the protojson encoding of %s.", fd.TfName(), fd.GoType.Elem().Name())
	}
	if fd.Kind == FieldJSONList {
		return fmt.Sprintf("`%s` as a JSON array, each element the protojson encoding of %s.", fd.TfName(), fd.GoType.Elem().Elem().Name())
	}
	return ""
}

func planModifiers(fd Field, shape attrShape) jen.Code {

	var mods []jen.Code

	if fd.Immutable {
		mods = append(mods, jen.Qual(shape.planModifierPkg, "RequiresReplace").Call())
	}
	if !fd.Required && !fd.InputOnly && !fd.Volatile {
		// Computed and optional-computed alike keep their prior value in
		// plans instead of churning to unknown. An input-only attribute is
		// never computed, so it is never unknown and has nothing to keep. A
		// volatile one changes on every write, so an update must plan it
		// unknown.
		mods = append(mods, jen.Qual(shape.planModifierPkg, "UseStateForUnknown").Call())
	}
	if len(mods) == 0 {
		return nil
	}

	return jen.Index().Qual(pkgPlanmodifier, shape.planModifier).Values(mods...)
}

func enumValidators(fd Field) jen.Code {

	values := make([]jen.Code, 0, len(fd.EnumValues))
	for _, v := range fd.EnumValues {
		values = append(values, jen.Lit(v))
	}

	return jen.Index().Qual(pkgSchemaValidator, "String").Values(
		jen.Qual(pkgStringValidators, "OneOf").Call(values...),
	)
}

// writeConfigDataSourceSchema emits the schema for a ConfigDataSource
// entry: the config message's fields as inputs plus the computed "any"
// output.
func writeConfigDataSourceSchema(f *jen.File, e Entry, cds ConfigDataSource, fields []Field, examples map[string]string) {

	t := entityType(e)

	attrs := jen.Dict{
		jen.Lit(AnyAttribute): jen.Qual(pkgDatasourceSchema, "StringAttribute").Values(jen.Dict{
			jen.Id("Computed"):            jen.True(),
			jen.Id("CustomType"):          jen.Qual(pkgJsontypes, "NormalizedType").Values(),
			jen.Id("MarkdownDescription"): jen.Lit("protojson-encoded google.protobuf.Any (includes `@type`); reference this from Any-typed resource attributes."),
		}),
	}

	for _, fd := range fields {
		if fd.Reference != nil {
			attrs[jen.Lit(fd.TfName())] = configReferenceAttributeFor(fd, examples)
			continue
		}
		attrs[jen.Lit(fd.TfName())] = configInputAttributeFor(fd)
	}

	f.Commentf("%sDataSourceSchema returns the Terraform schema for the %s config data source.", t.Name(), t.Name())
	f.Func().Id(t.Name()+"DataSourceSchema").Params().Qual(pkgDatasourceSchema, "Schema").Block(
		jen.Return(jen.Qual(pkgDatasourceSchema, "Schema").Values(jen.Dict{
			jen.Id("MarkdownDescription"): jen.Lit(configDataSourceDescription(t.Name(), cds)),
			jen.Id("Attributes"): jen.Map(jen.String()).Qual(pkgDatasourceSchema, "Attribute").Values(
				attrs,
			),
		})),
	)
}

// configDataSourceDescription is the generated sentence, led by the entry's
// own Description when it has one.
func configDataSourceDescription(typeName string, cds ConfigDataSource) string {
	desc := fmt.Sprintf("Builds a %s config and exposes its google.protobuf.Any encoding as `any`. Makes no API calls.", typeName)
	if cds.Description != "" {
		desc = cds.Description + " " + desc
	}
	return desc
}

func configInputAttributeFor(fd Field) jen.Code {

	shape := shapeFor(fd.Kind)
	d := jen.Dict{}

	if fd.Required {
		d[jen.Id("Required")] = jen.True()
	} else {
		d[jen.Id("Optional")] = jen.True()
	}
	if fd.Sensitive {
		d[jen.Id("Sensitive")] = jen.True()
	}
	applyTypeKeys(d, pkgDatasourceSchema, fd, configInputAttributeFor)
	if desc := shapeDescription(fd); desc != "" {
		d[jen.Id("MarkdownDescription")] = jen.Lit(desc)
	}
	if fd.Kind == FieldEnum {
		d[jen.Id("Validators")] = enumValidators(fd)
	}

	return jen.Qual(pkgDatasourceSchema, shape.attribute).Values(d)
}

// configReferenceAttributeFor builds a config input holding a resource's id:
// a plain string input, validated by prefix.
func configReferenceAttributeFor(fd Field, examples map[string]string) jen.Code {

	d := jen.Dict{
		jen.Id("Validators"): referenceValidators(*fd.Reference, examples),
	}
	if fd.Required {
		d[jen.Id("Required")] = jen.True()
	} else {
		d[jen.Id("Optional")] = jen.True()
	}
	if fd.Sensitive {
		d[jen.Id("Sensitive")] = jen.True()
	}

	return jen.Qual(pkgDatasourceSchema, "StringAttribute").Values(d)
}

// writeMintAttributes adds a minted resource's attributes beyond its proto
// fields: one per once-only value, and the keepers replacement trigger.
func writeMintAttributes(attrs jen.Dict, mint *Mint) {

	for _, name := range mint.Once {
		attrs[jen.Lit(name)] = jen.Qual(pkgResourceSchema, "StringAttribute").Values(jen.Dict{
			jen.Id("Computed"):  jen.True(),
			jen.Id("Sensitive"): jen.True(),
			jen.Id("MarkdownDescription"): jen.Lit(fmt.Sprintf("Returned once, by `%s`, and kept in state: no read returns it, "+
				"so a resource brought in with `terraform import` has none. Replace the resource (see `%s`) to get a new one.",
				mint.Method, runtimetf.KeepersAttribute)),
			// Every plan after the create has only state to offer: the
			// value never changes in place.
			jen.Id("PlanModifiers"): jen.Index().Qual(pkgPlanmodifier, "String").Values(
				jen.Qual(shapeFor(FieldString).planModifierPkg, "UseStateForUnknown").Call(),
			),
		})
	}

	attrs[jen.Lit(runtimetf.KeepersAttribute)] = jen.Qual(pkgResourceSchema, "MapAttribute").Values(jen.Dict{
		jen.Id("Optional"):    jen.True(),
		jen.Id("ElementType"): jen.Qual(pkgTypes, "StringType"),
		jen.Id("MarkdownDescription"): jen.Lit("Arbitrary values that replace the resource when any of them changes, never sent to the API. " +
			"Change one to rotate: with `create_before_destroy`, the successor is minted before this one is deleted."),
		jen.Id("PlanModifiers"): jen.Index().Qual(pkgPlanmodifier, "Map").Values(
			jen.Qual(shapeFor(FieldStringMap).planModifierPkg, "RequiresReplace").Call(),
		),
	})
}
