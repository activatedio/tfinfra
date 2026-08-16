package tf

import (
	"fmt"
	"reflect"

	"github.com/dave/jennifer/jen"
)

const (
	pkgTimestamppb = "google.golang.org/protobuf/types/known/timestamppb"
	pkgJsontypes   = "github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	pkgProtojson   = "google.golang.org/protobuf/encoding/protojson"
	pkgAnypb       = "google.golang.org/protobuf/types/known/anypb"
	pkgStructpb    = "google.golang.org/protobuf/types/known/structpb"
)

// tfsdkTag is the struct tag key the framework binds attributes with.
const tfsdkTag = "tfsdk"

// modelFieldType returns the model struct field type for a kind.
func modelFieldType(kind FieldKind) *jen.Statement {
	switch kind {
	case FieldString, FieldEnum, FieldTimestamp:
		return jen.Qual(pkgTypes, "String")
	case FieldBool:
		return jen.Qual(pkgTypes, "Bool")
	case FieldInt64:
		return jen.Qual(pkgTypes, "Int64")
	case FieldFloat64:
		return jen.Qual(pkgTypes, "Float64")
	case FieldStringList:
		return jen.Qual(pkgTypes, "List")
	case FieldStringMap:
		return jen.Qual(pkgTypes, "Map")
	case FieldAny, FieldStruct, FieldJSONMessage:
		return jen.Qual(pkgJsontypes, "Normalized")
	default:
		panic(fmt.Sprintf("unhandled field kind %d", kind))
	}
}

// writeModel emits the plan/state model struct plus ToProto / FromProto.
//
// Read-side null convention (first pass, refined with proto3 optional
// support in the CRUD runtime task): strings, enums, lists, maps, and
// timestamps read a proto zero value as Terraform null (except "name" and
// Required fields, which always carry a value); bools and numbers always
// carry a value because proto3 cannot distinguish zero from unset.
func writeModel(f *jen.File, e Entry, res Resource, fields []Field) {

	t := entityType(e)
	modelName := t.Name() + "Model"
	entityQual := func() *jen.Statement { return jen.Qual(t.PkgPath(), t.Name()) }

	// Struct: name first, then scope identifiers, then remaining proto
	// fields in declaration order.
	var structFields []jen.Code

	structFields = append(structFields,
		jen.Id("Name").Qual(pkgTypes, "String").Tag(map[string]string{tfsdkTag: NameField}))

	for _, attr := range res.Scope.IdentifierAttributes() {
		structFields = append(structFields,
			jen.Id(snakeToCamel(attr)).Qual(pkgTypes, "String").Tag(map[string]string{tfsdkTag: attr}))
	}

	for _, fd := range fields {
		if fd.ProtoName == NameField {
			continue
		}
		structFields = append(structFields,
			jen.Id(fd.GoName).Add(modelFieldType(fd.Kind)).Tag(map[string]string{tfsdkTag: fd.TfName()}))
	}

	f.Commentf("%s is the Terraform plan/state model for %s.", modelName, t.Name())
	f.Type().Id(modelName).Struct(structFields...)

	writeModelConstructor(f, res, fields, modelName)

	// ToProto
	to := make([]jen.Code, 0, len(fields)+3)
	to = append(to,
		jen.Var().Id("diags").Qual(pkgDiag, "Diagnostics"),
		jen.Id("out").Op(":=").Op("&").Add(entityQual()).Values(),
	)
	for _, fd := range fields {
		to = append(to, toProtoStatement(fd))
	}
	to = append(to, jen.Return(jen.Id("out"), jen.Id("diags")))

	f.Commentf("ToProto converts the model to its proto message. Null and unknown attributes map to proto zero values.")
	f.Func().Params(jen.Id("m").Op("*").Id(modelName)).Id("ToProto").
		Params(jen.Id("ctx").Qual("context", "Context")).
		Params(jen.Op("*").Add(entityQual()), jen.Qual(pkgDiag, "Diagnostics")).
		Block(to...)

	// FromProto
	from := make([]jen.Code, 0, len(fields)+2)
	from = append(from, jen.Var().Id("diags").Qual(pkgDiag, "Diagnostics"))
	for _, fd := range fields {
		from = append(from, fromProtoStatement(fd))
	}
	from = append(from, jen.Return(jen.Id("diags")))

	f.Commentf("FromProto populates the model from its proto message. Scope identifier attributes are left untouched.")
	f.Func().Params(jen.Id("m").Op("*").Id(modelName)).Id("FromProto").
		Params(jen.Id("ctx").Qual("context", "Context"), jen.Id("e").Op("*").Add(entityQual())).
		Qual(pkgDiag, "Diagnostics").
		Block(from...)

	writeModelAccessors(f, res, fields, modelName)
}

// typedNull returns the typed null expression for a kind. The zero value of
// collection types carries no element type, so models must be constructed
// with typed nulls.
func typedNull(kind FieldKind) *jen.Statement {
	switch kind {
	case FieldString, FieldEnum, FieldTimestamp:
		return jen.Qual(pkgTypes, "StringNull").Call()
	case FieldBool:
		return jen.Qual(pkgTypes, "BoolNull").Call()
	case FieldInt64:
		return jen.Qual(pkgTypes, "Int64Null").Call()
	case FieldFloat64:
		return jen.Qual(pkgTypes, "Float64Null").Call()
	case FieldStringList:
		return jen.Qual(pkgTypes, "ListNull").Call(jen.Qual(pkgTypes, "StringType"))
	case FieldStringMap:
		return jen.Qual(pkgTypes, "MapNull").Call(jen.Qual(pkgTypes, "StringType"))
	case FieldAny, FieldStruct, FieldJSONMessage:
		return jen.Qual(pkgJsontypes, "NewNormalizedNull").Call()
	default:
		panic(fmt.Sprintf("unhandled field kind %d", kind))
	}
}

// writeModelConstructor emits New<Entity>Model with every attribute as a
// typed null.
func writeModelConstructor(f *jen.File, res Resource, fields []Field, modelName string) {

	d := jen.Dict{
		jen.Id("Name"): typedNull(FieldString),
	}

	for _, attr := range res.Scope.IdentifierAttributes() {
		d[jen.Id(snakeToCamel(attr))] = typedNull(FieldString)
	}

	for _, fd := range fields {
		if fd.ProtoName == NameField {
			continue
		}
		d[jen.Id(fd.GoName)] = typedNull(fd.Kind)
	}

	f.Commentf("New%s returns a model with every attribute set to its typed null; collection types cannot be zero-valued.", modelName)
	f.Func().Id("New" + modelName).Params().Op("*").Id(modelName).Block(
		jen.Return(jen.Op("&").Id(modelName).Values(d)),
	)
}

// writeModelAccessors emits the tf.Model interface methods beyond the proto
// conversions: GetName, ScopeIdentifiers, and UpdateMask.
func writeModelAccessors(f *jen.File, res Resource, fields []Field, modelName string) {

	f.Commentf("GetName implements tf.Model.")
	f.Func().Params(jen.Id("m").Op("*").Id(modelName)).Id("GetName").Params().Qual(pkgTypes, "String").Block(
		jen.Return(jen.Id("m").Dot("Name")),
	)

	scope := jen.Dict{}
	for _, attr := range res.Scope.IdentifierAttributes() {
		scope[jen.Lit(attr)] = jen.Id("m").Dot(snakeToCamel(attr)).Dot("ValueString").Call()
	}

	f.Commentf("ScopeIdentifiers implements tf.Model: per-resource scope attribute values, null as \"\".")
	f.Func().Params(jen.Id("m").Op("*").Id(modelName)).Id("ScopeIdentifiers").Params().Map(jen.String()).String().Block(
		jen.Return(jen.Map(jen.String()).String().Values(scope)),
	)

	var mask []jen.Code
	mask = append(mask, jen.Var().Id("paths").Index().String())
	for _, fd := range fields {
		if fd.ProtoName == NameField || fd.Computed {
			continue
		}
		if fd.Kind == FieldAny || fd.Kind == FieldStruct || fd.Kind == FieldJSONMessage {
			// JSON attributes compare semantically so formatting-only
			// differences never land in the mask. Value equality short-
			// circuits first: semantic comparison cannot handle nulls.
			mask = append(mask,
				jen.If(jen.Op("!").Id("m").Dot(fd.GoName).Dot("Equal").Call(jen.Id("prior").Dot(fd.GoName))).Block(
					jen.If(
						jen.List(jen.Id("eq"), jen.Id("_")).Op(":=").Id("m").Dot(fd.GoName).Dot("StringSemanticEquals").Call(jen.Id("ctx"), jen.Id("prior").Dot(fd.GoName)),
						jen.Op("!").Id("eq"),
					).Block(
						jen.Id("paths").Op("=").Append(jen.Id("paths"), jen.Lit(fd.ProtoName)),
					),
				))
			continue
		}
		mask = append(mask, jen.If(
			jen.Op("!").Id("m").Dot(fd.GoName).Dot("Equal").Call(jen.Id("prior").Dot(fd.GoName)),
		).Block(
			jen.Id("paths").Op("=").Append(jen.Id("paths"), jen.Lit(fd.ProtoName)),
		))
	}
	mask = append(mask, jen.Return(jen.Id("paths")))

	f.Commentf("UpdateMask implements tf.Model: proto field paths whose values differ from prior, skipping name and computed fields.")
	f.Func().Params(jen.Id("m").Op("*").Id(modelName)).Id("UpdateMask").
		Params(jen.Id("ctx").Qual("context", "Context"), jen.Id("prior").Op("*").Id(modelName)).Index().String().
		Block(mask...)
}

func notNullNotUnknown(goName string) *jen.Statement {
	return jen.Op("!").Id("m").Dot(goName).Dot("IsNull").Call().
		Op("&&").Op("!").Id("m").Dot(goName).Dot("IsUnknown").Call()
}

func toProtoStatement(fd Field) jen.Code {

	out := jen.Id("out").Dot(fd.GoName)
	val := jen.Id("m").Dot(fd.GoName)

	switch fd.Kind {
	case FieldString:
		return out.Op("=").Add(val).Dot("ValueString").Call()
	case FieldBool:
		return out.Op("=").Add(val).Dot("ValueBool").Call()
	case FieldInt64:
		return out.Op("=").Add(toProtoNumeric(fd, "ValueInt64", reflect.Int64))
	case FieldFloat64:
		return out.Op("=").Add(toProtoNumeric(fd, "ValueFloat64", reflect.Float64))
	case FieldEnum:
		return jen.If(notNullNotUnknown(fd.GoName)).Block(
			out.Op("=").Qual(fd.GoType.PkgPath(), fd.GoType.Name()).Call(
				jen.Qual(fd.GoType.PkgPath(), fd.GoType.Name()+"_value").Index(
					jen.Id("m").Dot(fd.GoName).Dot("ValueString").Call(),
				),
			),
		)
	case FieldStringList, FieldStringMap:
		return jen.If(notNullNotUnknown(fd.GoName)).Block(
			jen.Id("diags").Dot("Append").Call(
				jen.Id("m").Dot(fd.GoName).Dot("ElementsAs").Call(
					jen.Id("ctx"), jen.Op("&").Add(out), jen.False(),
				).Op("..."),
			),
		)
	case FieldAny, FieldStruct, FieldJSONMessage:
		return toProtoJSON(fd, jsonSummary(fd))
	case FieldTimestamp:
		return toProtoTimestamp(fd, out)
	default:
		panic(fmt.Sprintf("unhandled field kind %d", fd.Kind))
	}
}

// toProtoNumeric emits m.<Field>.Value<Wide>(), narrowed to the pb struct
// field's kind when necessary.
func toProtoNumeric(fd Field, valueMethod string, wideKind reflect.Kind) *jen.Statement {
	expr := jen.Id("m").Dot(fd.GoName).Dot(valueMethod).Call()
	if fd.GoType.Kind() != wideKind {
		return jen.Id(fd.GoType.Kind().String()).Call(expr)
	}
	return expr
}

func toProtoTimestamp(fd Field, out *jen.Statement) jen.Code {
	return jen.If(notNullNotUnknown(fd.GoName)).Block(
		jen.List(jen.Id("t"), jen.Id("err")).Op(":=").Qual("time", "Parse").Call(
			jen.Qual("time", "RFC3339"), jen.Id("m").Dot(fd.GoName).Dot("ValueString").Call(),
		),
		jen.If(jen.Id("err").Op("!=").Nil()).Block(
			jen.Id("diags").Dot("AddAttributeError").Call(
				jen.Qual(pkgPath, "Root").Call(jen.Lit(fd.TfName())),
				jen.Lit("invalid RFC 3339 timestamp"),
				jen.Id("err").Dot("Error").Call(),
			),
		).Else().Block(
			out.Op("=").Qual(pkgTimestamppb, "New").Call(jen.Id("t")),
		),
	)
}

func fromProtoStatement(fd Field) jen.Code {

	m := func() *jen.Statement { return jen.Id("m").Dot(fd.GoName) }
	e := func() *jen.Statement { return jen.Id("e").Dot(fd.GoName) }

	switch fd.Kind {
	case FieldString:
		return fromProtoString(fd)
	case FieldBool:
		return m().Op("=").Qual(pkgTypes, "BoolValue").Call(e())
	case FieldInt64:
		return m().Op("=").Qual(pkgTypes, "Int64Value").Call(numericCast(fd, reflect.Int64, "int64"))
	case FieldFloat64:
		return m().Op("=").Qual(pkgTypes, "Float64Value").Call(numericCast(fd, reflect.Float64, "float64"))
	case FieldEnum:
		return jen.If(e().Op("==").Lit(0)).Block(
			m().Op("=").Qual(pkgTypes, "StringNull").Call(),
		).Else().Block(
			m().Op("=").Qual(pkgTypes, "StringValue").Call(e().Dot("String").Call()),
		)
	case FieldStringList:
		return fromProtoCollection(fd, "ListNull", "ListValueFrom")
	case FieldStringMap:
		return fromProtoCollection(fd, "MapNull", "MapValueFrom")
	case FieldAny, FieldStruct, FieldJSONMessage:
		return fromProtoJSON(fd)
	case FieldTimestamp:
		return jen.If(e().Op("==").Nil()).Block(
			m().Op("=").Qual(pkgTypes, "StringNull").Call(),
		).Else().Block(
			m().Op("=").Qual(pkgTypes, "StringValue").Call(
				e().Dot("AsTime").Call().Dot("Format").Call(jen.Qual("time", "RFC3339")),
			),
		)
	default:
		panic(fmt.Sprintf("unhandled field kind %d", fd.Kind))
	}
}

// jsonSummary is the diagnostic summary for a bad JSON attribute value.
func jsonSummary(fd Field) string {
	switch fd.Kind {
	case FieldAny:
		return "invalid google.protobuf.Any JSON"
	case FieldStruct:
		return "invalid JSON object"
	default:
		return fmt.Sprintf("invalid %s JSON", fd.GoType.Elem().Name())
	}
}

// toProtoJSON emits: parse the jsontypes value via protojson into the
// field's message type, with an attribute-anchored diagnostic on bad input.
func toProtoJSON(fd Field, summary string) jen.Code {
	t := fd.GoType.Elem()
	return jen.If(notNullNotUnknown(fd.GoName)).Block(
		jen.Id("v").Op(":=").Op("&").Qual(t.PkgPath(), t.Name()).Values(),
		jen.If(
			jen.Id("err").Op(":=").Qual(pkgProtojson, "Unmarshal").Call(
				jen.Id("[]byte").Call(jen.Id("m").Dot(fd.GoName).Dot("ValueString").Call()),
				jen.Id("v"),
			),
			jen.Id("err").Op("!=").Nil(),
		).Block(
			jen.Id("diags").Dot("AddAttributeError").Call(
				jen.Qual(pkgPath, "Root").Call(jen.Lit(fd.TfName())),
				jen.Lit(summary),
				jen.Id("err").Dot("Error").Call(),
			),
		).Else().Block(
			jen.Id("out").Dot(fd.GoName).Op("=").Id("v"),
		),
	)
}

// fromProtoJSON emits: protojson-encode the well-known type into the
// jsontypes attribute; nil reads as null.
func fromProtoJSON(fd Field) jen.Code {
	return jen.If(jen.Id("e").Dot(fd.GoName).Op("==").Nil()).Block(
		jen.Id("m").Dot(fd.GoName).Op("=").Qual(pkgJsontypes, "NewNormalizedNull").Call(),
	).Else().Block(
		jen.List(jen.Id("b"), jen.Id("err")).Op(":=").Qual(pkgProtojson, "Marshal").Call(jen.Id("e").Dot(fd.GoName)),
		jen.If(jen.Id("err").Op("!=").Nil()).Block(
			jen.Id("diags").Dot("AddError").Call(
				jen.Lit(fmt.Sprintf("cannot encode %s", fd.TfName())),
				jen.Id("err").Dot("Error").Call(),
			),
		).Else().Block(
			jen.Id("m").Dot(fd.GoName).Op("=").Qual(pkgJsontypes, "NewNormalizedValue").Call(jen.Id("string").Call(jen.Id("b"))),
		),
	)
}

func fromProtoString(fd Field) jen.Code {

	value := jen.Id("m").Dot(fd.GoName).Op("=").Qual(pkgTypes, "StringValue").Call(jen.Id("e").Dot(fd.GoName))

	if fd.ProtoName == NameField || fd.Required {
		return value
	}

	return jen.If(jen.Id("e").Dot(fd.GoName).Op("==").Lit("")).Block(
		jen.Id("m").Dot(fd.GoName).Op("=").Qual(pkgTypes, "StringNull").Call(),
	).Else().Block(value)
}

// numericCast returns e.<Field>, wrapped in a conversion to wide when the
// pb struct field is a narrower kind.
func numericCast(fd Field, wideKind reflect.Kind, wide string) jen.Code {

	expr := jen.Id("e").Dot(fd.GoName)

	if fd.GoType.Kind() != wideKind {
		return jen.Id(wide).Call(expr)
	}

	return expr
}

func fromProtoCollection(fd Field, nullFunc, valueFunc string) jen.Code {

	return jen.If(jen.Len(jen.Id("e").Dot(fd.GoName)).Op("==").Lit(0)).Block(
		jen.Id("m").Dot(fd.GoName).Op("=").Qual(pkgTypes, nullFunc).Call(jen.Qual(pkgTypes, "StringType")),
	).Else().Block(
		jen.List(jen.Id("v"), jen.Id("d")).Op(":=").Qual(pkgTypes, valueFunc).Call(
			jen.Id("ctx"), jen.Qual(pkgTypes, "StringType"), jen.Id("e").Dot(fd.GoName),
		),
		jen.Id("diags").Dot("Append").Call(jen.Id("d").Op("...")),
		jen.Id("m").Dot(fd.GoName).Op("=").Id("v"),
	)
}

// AnyAttribute is the computed output attribute on config data sources:
// the protojson-encoded google.protobuf.Any.
const AnyAttribute = "any"

// writeConfigModel emits the config data source model: typed input fields
// plus the computed "any" output, a typed-null constructor, and ToProto.
func writeConfigModel(f *jen.File, e Entry, fields []Field, modelName string) {

	t := entityType(e)
	entityQual := func() *jen.Statement { return jen.Qual(t.PkgPath(), t.Name()) }

	structFields := make([]jen.Code, 0, len(fields)+1)
	for _, fd := range fields {
		structFields = append(structFields,
			jen.Id(fd.GoName).Add(modelFieldType(fd.Kind)).Tag(map[string]string{tfsdkTag: fd.TfName()}))
	}
	structFields = append(structFields,
		jen.Id("Any").Qual(pkgJsontypes, "Normalized").Tag(map[string]string{tfsdkTag: AnyAttribute}))

	f.Commentf("%s is the Terraform model for the %s config data source.", modelName, t.Name())
	f.Type().Id(modelName).Struct(structFields...)

	d := jen.Dict{
		jen.Id("Any"): typedNull(FieldAny),
	}
	for _, fd := range fields {
		d[jen.Id(fd.GoName)] = typedNull(fd.Kind)
	}

	f.Commentf("New%s returns a model with every attribute set to its typed null.", modelName)
	f.Func().Id("New" + modelName).Params().Op("*").Id(modelName).Block(
		jen.Return(jen.Op("&").Id(modelName).Values(d)),
	)

	to := make([]jen.Code, 0, len(fields)+3)
	to = append(to,
		jen.Var().Id("diags").Qual(pkgDiag, "Diagnostics"),
		jen.Id("out").Op(":=").Op("&").Add(entityQual()).Values(),
	)
	for _, fd := range fields {
		to = append(to, toProtoStatement(fd))
	}
	to = append(to, jen.Return(jen.Id("out"), jen.Id("diags")))

	f.Commentf("ToProto converts the model to its proto message. Null and unknown attributes map to proto zero values.")
	f.Func().Params(jen.Id("m").Op("*").Id(modelName)).Id("ToProto").
		Params(jen.Id("ctx").Qual("context", "Context")).
		Params(jen.Op("*").Add(entityQual()), jen.Qual(pkgDiag, "Diagnostics")).
		Block(to...)
}
