package tf

import (
	"fmt"

	"github.com/dave/jennifer/jen"
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
}

func shapeFor(kind FieldKind) attrShape {
	base := "github.com/hashicorp/terraform-plugin-framework/resource/schema/"
	switch kind {
	case FieldString, FieldEnum, FieldTimestamp:
		return attrShape{"StringAttribute", "String", base + "stringplanmodifier", false}
	case FieldBool:
		return attrShape{"BoolAttribute", "Bool", base + "boolplanmodifier", false}
	case FieldInt64:
		return attrShape{"Int64Attribute", "Int64", base + "int64planmodifier", false}
	case FieldFloat64:
		return attrShape{"Float64Attribute", "Float64", base + "float64planmodifier", false}
	case FieldStringList:
		return attrShape{"ListAttribute", "List", base + "listplanmodifier", true}
	case FieldStringMap:
		return attrShape{"MapAttribute", "Map", base + "mapplanmodifier", true}
	default:
		panic(fmt.Sprintf("unhandled field kind %d", kind))
	}
}

// writeResourceSchema emits func <Entity>ResourceSchema() schema.Schema.
func writeResourceSchema(f *jen.File, e Entry, res Resource, fields []Field) {

	t := entityType(e)

	attrs := jen.Dict{}

	// Scope identifier attributes: optional, replacement on change.
	for _, attr := range res.Scope.IdentifierAttributes() {
		attrs[jen.Lit(attr)] = jen.Qual(pkgResourceSchema, "StringAttribute").Values(jen.Dict{
			jen.Id("Optional"):            jen.True(),
			jen.Id("MarkdownDescription"): jen.Lit(fmt.Sprintf("Parent identifier `%s`; overrides the provider default. Changing it replaces the resource.", attr)),
			jen.Id("PlanModifiers"): jen.Index().Qual(pkgPlanmodifier, "String").Values(
				jen.Qual(shapeFor(FieldString).planModifierPkg, "RequiresReplace").Call(),
			),
		})
	}

	for _, fd := range fields {
		attrs[jen.Lit(fd.TfName())] = attributeFor(fd)
	}

	f.Commentf("%sResourceSchema returns the Terraform schema for the %s resource.", t.Name(), t.Name())
	f.Func().Id(t.Name()+"ResourceSchema").Params().Qual(pkgResourceSchema, "Schema").Block(
		jen.Return(jen.Qual(pkgResourceSchema, "Schema").Values(jen.Dict{
			jen.Id("MarkdownDescription"): jen.Lit(fmt.Sprintf("%s resource.", t.Name())),
			jen.Id("Attributes"): jen.Map(jen.String()).Qual(pkgResourceSchema, "Attribute").Values(
				attrs,
			),
		})),
	)
}

const pkgDatasourceSchema = "github.com/hashicorp/terraform-plugin-framework/datasource/schema"

// writeDataSourceSchema emits func <Entity>DataSourceSchema() for the
// singular data source: name required, everything else computed.
func writeDataSourceSchema(f *jen.File, e Entry, res Resource, fields []Field) {

	t := entityType(e)

	attrs := jen.Dict{}

	for _, attr := range res.Scope.IdentifierAttributes() {
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
			jen.Id("MarkdownDescription"): jen.Lit(fmt.Sprintf("%s data source: reads one %s by its full resource name.", t.Name(), t.Name())),
			jen.Id("Attributes"): jen.Map(jen.String()).Qual(pkgDatasourceSchema, "Attribute").Values(
				attrs,
			),
		})),
	)
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
	if shape.elementType {
		d[jen.Id("ElementType")] = jen.Qual(pkgTypes, "StringType")
	}

	return jen.Qual(pkgDatasourceSchema, shape.attribute).Values(d)
}

func attributeFor(fd Field) jen.Code {

	shape := shapeFor(fd.Kind)
	d := jen.Dict{}

	switch {
	case fd.Computed:
		d[jen.Id("Computed")] = jen.True()
	case fd.Required:
		d[jen.Id("Required")] = jen.True()
	default:
		d[jen.Id("Optional")] = jen.True()
	}

	if fd.Sensitive {
		d[jen.Id("Sensitive")] = jen.True()
	}

	if shape.elementType {
		d[jen.Id("ElementType")] = jen.Qual(pkgTypes, "StringType")
	}

	if desc := attributeDescription(fd); desc != "" {
		d[jen.Id("MarkdownDescription")] = jen.Lit(desc)
	}

	if mods := planModifiers(fd, shape); mods != nil {
		d[jen.Id("PlanModifiers")] = mods
	}

	if fd.Kind == FieldEnum {
		d[jen.Id("Validators")] = enumValidators(fd)
	}

	return jen.Qual(pkgResourceSchema, shape.attribute).Values(d)
}

func attributeDescription(fd Field) string {
	if fd.ProtoName == NameField {
		return "Full resource name; serves as the Terraform ID."
	}
	if fd.Kind == FieldTimestamp {
		return fmt.Sprintf("`%s` as an RFC 3339 timestamp.", fd.TfName())
	}
	return ""
}

func planModifiers(fd Field, shape attrShape) jen.Code {

	var mods []jen.Code

	if fd.Immutable {
		mods = append(mods, jen.Qual(shape.planModifierPkg, "RequiresReplace").Call())
	}
	if fd.Computed {
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
