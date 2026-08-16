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
// generates. Shapes outside this set (nested messages, repeated messages,
// Struct, Any, bytes, real oneofs, non-string maps and lists) are not yet
// supported and fail generation loudly.
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

	Required  bool
	Computed  bool
	Immutable bool
	Sensitive bool
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
		fields = append(fields, normalizeField(t, fds.Get(i), jsonSet))
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
		fields = append(fields, normalizeField(t, fds.Get(i), jsonSet))
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
// the Go struct field along the way.
func normalizeField(t reflect.Type, fd protoreflect.FieldDescriptor, jsonSet map[string]bool) Field {

	name := string(fd.Name())

	f := Field{
		ProtoName: name,
		GoName:    goFieldName(t, name),
	}
	f.GoType = mustStructField(t, f.GoName).Type

	if oneof := fd.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
		panic(fmt.Sprintf("%s.%s: oneof fields are not yet supported", t.Name(), name))
	}

	switch {
	case fd.IsMap():
		if fd.MapKey().Kind() != protoreflect.StringKind || fd.MapValue().Kind() != protoreflect.StringKind {
			panic(fmt.Sprintf("%s.%s: only map<string, string> fields are supported", t.Name(), name))
		}
		f.Kind = FieldStringMap
	case fd.IsList():
		if fd.Kind() != protoreflect.StringKind {
			panic(fmt.Sprintf("%s.%s: only repeated string fields are supported", t.Name(), name))
		}
		f.Kind = FieldStringList
	default:
		f.Kind = scalarKind(t.Name(), fd, jsonSet[name])
	}

	if f.Kind == FieldEnum {
		values := fd.Enum().Values()
		for j := 0; j < values.Len(); j++ {
			f.EnumValues = append(f.EnumValues, string(values.Get(j).Name()))
		}
	}

	return f
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
}

func scalarKind(entity string, fd protoreflect.FieldDescriptor, jsonMarked bool) FieldKind {
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
		return messageKind(entity, fd, jsonMarked)
	default:
		panic(fmt.Sprintf("%s.%s: field kind %s is not yet supported", entity, fd.Name(), fd.Kind()))
	}
}

// messageKind classifies the supported well-known message types.
func messageKind(entity string, fd protoreflect.FieldDescriptor, jsonMarked bool) FieldKind {
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
	panic(fmt.Sprintf("%s.%s: message-typed field %s must be declared in the JSON list (typed nested attributes are not yet supported)", entity, fd.Name(), fd.Message().FullName()))
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
