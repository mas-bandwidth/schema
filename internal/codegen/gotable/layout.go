package gotable

// tableLayoutRuntime is the part of the unit's table runtime that answers
// layout facts about the unit's own generated types. Every emitted size,
// alignment and offset of a generated type goes through it.
const tableLayoutRuntime = `// THE LAYOUT FACTS of a generated type, from this build's own compiler. A
// constant unsafe.Sizeof or unsafe.Offsetof costs the type checker time
// exponential in the depth of a chain of structs that each end in a struct, so
// none is spelled over a generated type: behind a type parameter each type is
// sized once, and the type answers fold to constants when inlined.
func tableSizeof[T any]() uintptr  { return unsafe.Sizeof(*(*T)(nil)) }
func tableAlignof[T any]() uintptr { return unsafe.Alignof(*(*T)(nil)) }
func tableOffsetof[T any](name string) uintptr { return tableLayoutField[T](name).Offset }
func tableMemberSizeof[T any](name string) uintptr { return tableLayoutField[T](name).Type.Size() }
func tableElemSizeof[T any](name string) uintptr { return tableLayoutField[T](name).Type.Elem().Size() }
func tableLayoutField[T any](name string) reflect.StructField {
	f, ok := reflect.TypeFor[T]().FieldByName(name)
	if !ok {
		panic("schema layout: " + reflect.TypeFor[T]().Name() + " has no field " + name)
	}
	return f
}

`
