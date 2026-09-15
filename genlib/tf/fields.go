package tf

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// NameField is the AIP resource name field: always present, always
// server-computed, and the Terraform ID.
const NameField = "name"

// FieldKind classifies a proto field into the Terraform attribute shape it
// generates. Shapes outside this set (repeated messages, bytes, real
// oneofs, non-string maps and lists, and messages nested more than one
// level deep) are not yet supported and fail generation loudly.
type FieldKind int

const (
	// FieldString is a proto string.
	FieldString FieldKind = iota
	// FieldBool is a proto bool.
	FieldBool
	// FieldInt64 is any proto integer kind, widened to types.Int64.
	FieldInt64
	// FieldFloat64 is a proto float or double.
	FieldFloat64
	// FieldEnum is a proto enum, surfaced as a string with a OneOf validator.
	FieldEnum
	// FieldTimestamp is a google.protobuf.Timestamp, surfaced as RFC 3339.
	FieldTimestamp
	// FieldStringList is a repeated string.
	FieldStringList
	// FieldStringMap is a map<string, string>.
	FieldStringMap
	// FieldAny is a google.protobuf.Any, surfaced as jsontypes.Normalized
	// holding the protojson encoding (with "@type").
	FieldAny
	// FieldStruct is a google.protobuf.Struct, surfaced as
	// jsontypes.Normalized holding a JSON object.
	FieldStruct
	// FieldJSONMessage is any other message field declared in the JSON
	// list, surfaced as jsontypes.Normalized holding its protojson
	// encoding.
	FieldJSONMessage
	// FieldNestedMessage is a singular message field left out of the JSON
	// list, surfaced as a SingleNestedAttribute over the message's own
	// fields. It nests one level: a message inside one of those fields
	// belongs on the JSON lane.
	FieldNestedMessage
)

// Field is the normalized view of one proto field: proto identity, Go
// binding, Terraform attribute shape, and resolved behavior.
type Field struct {
	// ProtoName is the proto field name (snake_case); it doubles as the
	// Terraform attribute name.
	ProtoName string
	// GoName is the Go struct field name on the pb type.
	GoName string
	// Kind is the Terraform attribute shape.
	Kind FieldKind
	// GoType is the Go struct field type; used to emit casts for narrow
	// integers and to qualify enum types.
	GoType reflect.Type
	// EnumValues holds the proto enum value names for FieldEnum.
	EnumValues []string
	// Nested holds the nested message's own normalized fields for
	// FieldNestedMessage, in proto field-number order.
	Nested []Field

	Required  bool
	Computed  bool
	Immutable bool
	Sensitive bool
	// InputOnly marks a field the API consumes but never echoes back: it
	// is Optional but never Computed, and reads leave it untouched.
	InputOnly bool
}

// TfName returns the Terraform attribute name for the field.
func (f Field) TfName() string {
	return f.ProtoName
}

// entityType returns the entry's message struct type, unwrapping a pointer
// if the spec declared one.
func entityType(e Entry) reflect.Type {
	t := e.Type
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("entry type %s is not a struct", e.Type))
	}
	return t
}

// NormalizeFields reads the entry's message descriptor via protoreflect and
// returns the normalized field list in proto field-number order, with the
// Resource marker's behavior lists resolved and validated. It panics on any
// shape or marker reference it cannot handle — generation failures must be
// loud.
func NormalizeFields(e Entry, res Resource) []Field {

	t := entityType(e)

	msg, ok := reflect.New(t).Interface().(proto.Message)
	if !ok {
		panic(fmt.Sprintf("entry type %s is not a proto.Message", t))
	}

	if len(res.WriteOnly) > 0 {
		panic(fmt.Sprintf("%s: WriteOnly fields are not yet supported", t.Name()))
	}

	desc := msg.ProtoReflect().Descriptor()
	fds := desc.Fields()

	byName := map[string]*Field{}
	res.validateFieldNames(t.Name(), fds)

	jsonSet := map[string]bool{}
	for _, n := range res.JSON {
		jsonSet[n] = true
	}

	fields := make([]Field, 0, fds.Len())

	for i := 0; i < fds.Len(); i++ {
		fields = append(fields, normalizeField(t, fds.Get(i), jsonSet, true))
		byName[fields[i].ProtoName] = &fields[i]
	}

	for _, n := range res.JSON {
		if k := byName[n].Kind; k != FieldAny && k != FieldStruct && k != FieldJSONMessage {
			panic(fmt.Sprintf("%s.%s: JSON marker applies only to message-typed fields", t.Name(), n))
		}
	}

	if _, ok := byName[NameField]; !ok {
		panic(fmt.Sprintf("%s: message has no %q field; tfinfra requires AIP-shaped resources", t.Name(), NameField))
	}
	byName[NameField].Computed = true

	applyBehavior(t.Name(), res, byName)

	return fields
}

// NormalizeConfigFields is the variant for ConfigDataSource entries: config
// messages carry no AIP name and no behavior beyond Required.
func NormalizeConfigFields(e Entry, cds ConfigDataSource) []Field {

	t := entityType(e)

	if _, ok := reflect.New(t).Interface().(proto.Message); !ok {
		panic(fmt.Sprintf("entry type %s is not a proto.Message", t))
	}

	msg := reflect.New(t).Interface().(proto.Message)
	fds := msg.ProtoReflect().Descriptor().Fields()

	jsonSet := map[string]bool{}
	for _, n := range cds.JSON {
		jsonSet[n] = true
	}

	valid := map[string]*Field{}
	fields := make([]Field, 0, fds.Len())

	for i := 0; i < fds.Len(); i++ {
		fields = append(fields, normalizeField(t, fds.Get(i), jsonSet, true))
		valid[fields[i].ProtoName] = &fields[i]
	}

	mark := func(list []string, label string, apply func(f *Field)) {
		for _, n := range list {
			f, ok := valid[n]
			if !ok {
				panic(fmt.Sprintf("%s: %s references unknown field %q", t.Name(), label, n))
			}
			apply(f)
		}
	}
	mark(cds.Required, "Required", func(f *Field) { f.Required = true })
	mark(cds.Sensitive, "Sensitive", func(f *Field) { f.Sensitive = true })

	return fields
}

// normalizeField maps one field descriptor to its normalized form, binding
// the Go struct field along the way. allowNested admits a singular
// message-typed field as a typed nested attribute; it is false inside a
// nested message, which is what holds nesting to one level.
func normalizeField(t reflect.Type, fd protoreflect.FieldDescriptor, jsonSet map[string]bool, allowNested bool) Field {

	name := string(fd.Name())

	f := Field{
		ProtoName: name,
		GoName:    goFieldName(t, name),
	}
	f.GoType = mustStructField(t, f.GoName).Type

	if oneof := fd.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
		panic(fmt.Sprintf("%s.%s: oneof fields are not yet supported", t.Name(), name))
	}

	f.Kind = fieldKind(t, fd, jsonSet[name], allowNested)

	if f.Kind == FieldEnum {
		f.EnumValues = enumValueNames(fd)
	}
	if f.Kind == FieldNestedMessage {
		f.Nested = normalizeNested(t, f, fd)
	}

	return f
}

// fieldKind classifies a field by cardinality first, then by type.
func fieldKind(t reflect.Type, fd protoreflect.FieldDescriptor, jsonMarked, allowNested bool) FieldKind {

	switch {
	case fd.IsMap():
		if fd.MapKey().Kind() != protoreflect.StringKind || fd.MapValue().Kind() != protoreflect.StringKind {
			panic(fmt.Sprintf("%s.%s: only map<string, string> fields are supported", t.Name(), fd.Name()))
		}
		return FieldStringMap
	case fd.IsList():
		if fd.Kind() != protoreflect.StringKind {
			panic(fmt.Sprintf("%s.%s: only repeated string fields are supported", t.Name(), fd.Name()))
		}
		return FieldStringList
	default:
		return scalarKind(t.Name(), fd, jsonMarked, allowNested)
	}
}

// enumValueNames returns the proto enum value names in declaration order.
func enumValueNames(fd protoreflect.FieldDescriptor) []string {

	values := fd.Enum().Values()

	names := make([]string, 0, values.Len())
	for i := 0; i < values.Len(); i++ {
		names = append(names, string(values.Get(i).Name()))
	}

	return names
}

// normalizeNested normalizes the fields of a nested message attribute. The
// nested message takes no JSON markers and no further nesting: one level is
// the whole of the feature, and anything deeper stays on the protojson lane.
func normalizeNested(owner reflect.Type, f Field, fd protoreflect.FieldDescriptor) []Field {

	t := f.GoType
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	fds := fd.Message().Fields()
	if fds.Len() == 0 {
		panic(fmt.Sprintf("%s.%s: nested message %s has no fields", owner.Name(), f.ProtoName, fd.Message().FullName()))
	}

	nested := make([]Field, 0, fds.Len())
	for i := 0; i < fds.Len(); i++ {
		nested = append(nested, normalizeField(t, fds.Get(i), nil, false))
	}

	return nested
}

// applyBehavior resolves the Resource marker's behavior lists onto the
// normalized fields.
func applyBehavior(entity string, res Resource, byName map[string]*Field) {

	for _, n := range res.Computed {
		byName[n].Computed = true
	}
	for _, n := range res.Required {
		if byName[n].Computed {
			panic(fmt.Sprintf("%s.%s: field cannot be both required and computed", entity, n))
		}
		byName[n].Required = true
	}
	for _, n := range res.Immutable {
		byName[n].Immutable = true
	}
	for _, n := range res.Sensitive {
		byName[n].Sensitive = true
	}
	for _, n := range res.InputOnly {
		if byName[n].Computed {
			panic(fmt.Sprintf("%s.%s: field cannot be both input-only and computed; the server never returns an input-only field", entity, n))
		}
		byName[n].InputOnly = true
	}
}

func scalarKind(entity string, fd protoreflect.FieldDescriptor, jsonMarked, allowNested bool) FieldKind {
	switch fd.Kind() {
	case protoreflect.StringKind:
		return FieldString
	case protoreflect.BoolKind:
		return FieldBool
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return FieldInt64
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return FieldFloat64
	case protoreflect.EnumKind:
		return FieldEnum
	case protoreflect.MessageKind:
		return messageKind(entity, fd, jsonMarked, allowNested)
	default:
		panic(fmt.Sprintf("%s.%s: field kind %s is not yet supported", entity, fd.Name(), fd.Kind()))
	}
}

// messageKind classifies a message-typed field: the well-known types, the
// protojson lane for anything in the JSON list, and a typed nested
// attribute for the rest.
func messageKind(entity string, fd protoreflect.FieldDescriptor, jsonMarked, allowNested bool) FieldKind {
	switch fd.Message().FullName() {
	case "google.protobuf.Timestamp":
		return FieldTimestamp
	case "google.protobuf.Any":
		if !jsonMarked {
			panic(fmt.Sprintf("%s.%s: google.protobuf.Any fields must be declared in Resource.JSON", entity, fd.Name()))
		}
		return FieldAny
	case "google.protobuf.Struct":
		if !jsonMarked {
			panic(fmt.Sprintf("%s.%s: google.protobuf.Struct fields must be declared in Resource.JSON", entity, fd.Name()))
		}
		return FieldStruct
	}
	if jsonMarked {
		return FieldJSONMessage
	}
	if allowNested {
		return FieldNestedMessage
	}
	panic(fmt.Sprintf("%s.%s: message-typed field %s sits more than one level deep; typed nested attributes nest one level, so declare the outer field in the JSON list instead", entity, fd.Name(), fd.Message().FullName()))
}

// validateFieldNames panics when a behavior list references a proto field
// that does not exist, listing the valid names.
func (r Resource) validateFieldNames(entity string, fds protoreflect.FieldDescriptors) {

	valid := map[string]bool{}
	names := make([]string, 0, fds.Len())
	for i := 0; i < fds.Len(); i++ {
		n := string(fds.Get(i).Name())
		valid[n] = true
		names = append(names, n)
	}
	sort.Strings(names)

	check := func(list []string, label string) {
		for _, n := range list {
			if !valid[n] {
				panic(fmt.Sprintf("%s: %s references unknown field %q (fields: %s)", entity, label, n, strings.Join(names, ", ")))
			}
		}
	}

	check(r.Required, "Required")
	check(r.Immutable, "Immutable")
	check(r.Computed, "Computed")
	check(r.Sensitive, "Sensitive")
	check(r.InputOnly, "InputOnly")
	check(r.WriteOnly, "WriteOnly")
	check(r.JSON, "JSON")
}

// goFieldName resolves a proto field name to the Go struct field name by
// reading the protoc-gen-go struct tags, so no naming scheme is guessed.
func goFieldName(t reflect.Type, protoName string) string {

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

	panic(fmt.Sprintf("%s: no Go struct field found for proto field %q", t.Name(), protoName))
}

func mustStructField(t reflect.Type, goName string) reflect.StructField {
	f, ok := t.FieldByName(goName)
	if !ok {
		panic(fmt.Sprintf("%s: no struct field %q", t.Name(), goName))
	}
	return f
}
