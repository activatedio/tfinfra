package tf

// Ops is a bitmask of the operations an API resource exposes, mirroring the
// apiinfra crud.Ops convention. The zero value means all operations.
type Ops uint8

const (
	// OpGet is the Get<Entity> operation.
	OpGet Ops = 1 << iota
	// OpList is the List<Entities> operation.
	OpList
	// OpCreate is the Create<Entity> operation.
	OpCreate
	// OpUpdate is the full-replace Update<Entity> operation.
	OpUpdate
	// OpPatch is the Patch<Entity> operation with an update mask.
	OpPatch
	// OpDelete is the Delete<Entity> operation.
	OpDelete
)

// OpAll is the zero value: all operations.
const OpAll Ops = 0

// Has reports whether the mask includes the given operation. The zero value
// includes everything.
func (o Ops) Has(op Ops) bool {
	if o == OpAll {
		return true
	}
	return o&op != 0
}
