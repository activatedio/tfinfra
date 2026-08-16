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
	if t.Kind() == reflect.Ptr {
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
	if len(res.JSON) > 0 {
		panic(fmt.Sprintf("%s: JSON fields are not yet supported", t.Name()))
	}

	desc := msg.ProtoReflect().Descriptor()
	fds := desc.Fields()

	byName := map[string]*Field{}
	res.validateFieldNames(t.Name(), fds)

	fields := make([]Field, 0, fds.Len())

	for i := 0; i < fds.Len(); i++ {
		fields = append(fields, normalizeField(t, fds.Get(i)))
		byName[fields[i].ProtoName] = &fields[i]
	}

	if _, ok := byName[NameField]; !ok {
		panic(fmt.Sprintf("%s: message has no %q field; tfinfra requires AIP-shaped resources", t.Name(), NameField))
	}
	byName[NameField].Computed = true

	applyBehavior(t.Name(), res, byName)

	return fields
}

// normalizeField maps one field descriptor to its normalized form, binding
// the Go struct field along the way.
func normalizeField(t reflect.Type, fd protoreflect.FieldDescriptor) Field {

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
		f.Kind = scalarKind(t.Name(), fd)
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

func scalarKind(entity string, fd protoreflect.FieldDescriptor) FieldKind {
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
		if fd.Message().FullName() == "google.protobuf.Timestamp" {
			return FieldTimestamp
		}
		panic(fmt.Sprintf("%s.%s: message-typed fields are not yet supported (%s)", entity, fd.Name(), fd.Message().FullName()))
	default:
		panic(fmt.Sprintf("%s.%s: field kind %s is not yet supported", entity, fd.Name(), fd.Kind()))
	}
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
