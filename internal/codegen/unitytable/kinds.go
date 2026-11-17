package unitytable

import (
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// kind is one field codec of the Unity table backend: what a field of that
// shape is stored as, what its descriptor says, and the straight-line C# that
// measures, writes and reads it on the form-1 wire (docs/SPEC-TABLES.md §3).
//
// THE PER-KIND LAYOUT. Each codec is one file, kind_<name>.go, that registers
// itself in init(); the table emitter asks the registry and never names a
// codec. A card that carries a new construct (a float, a string, a nested
// table, an array, a pointer) adds its own kind_<name>.go with a priority
// below kind_unsupported's and edits nothing shared: the field it claims stops
// falling through to `unsupported`, and the tables that hold it start carrying
// Load and Save.
type kind interface {
	// name is the codec's descriptor name, and codec its TableFieldInfo.Codec
	// number: each kind owns its own, unique across the registry.
	name() string
	codec() int
	// priority orders the registry: the lowest that claims a field wins.
	priority() int
	// claims reports whether this codec carries the field.
	claims(f *ir.Field) bool
	// carried is false for the one codec that stores nothing and reads and
	// writes nothing: a table holding such a field has no Load or Save yet.
	carried() bool
	// storage is the C# type of the struct member, "" for no member.
	storage(f *ir.Field) string
	// defaultValue is the C# expression of the member's declared default.
	defaultValue(f *ir.Field) string
	// defaultBits is the descriptor's raw default, two's complement in a ulong.
	defaultBits(f *ir.Field) uint64
	// differs is the C# test that the member `v` is not at its declared
	// default: the field rides (docs/SPEC-TABLES.md §3, the elision rule).
	differs(f *ir.Field, v string) string
	// width is the payload's byte count after the header.
	width(f *ir.Field) int
	// emitAsserts writes the write contracts of `v` as Debug.Assert lines:
	// Unity is an ASSERT leg (docs/PORTING.md), so a release build checks
	// nothing a caller promised.
	emitAsserts(b *strings.Builder, f *ir.Field, v string)
	// emitPayload writes the payload of `v` at `buffer[off]`, advancing off.
	emitPayload(b *strings.Builder, f *ir.Field, v string)
	// emitRead reads the payload whose wire kind is the local `kind`, at `off`
	// of `body`, into `v`; a refusal sets `reason` and jumps to `done`.
	emitRead(b *strings.Builder, f *ir.Field, v string)
	// runtime is the C# this codec adds to TableWire, once per unit.
	runtime() string
}

var registry []kind

// register adds a codec. Two codecs never share a number or a name.
func register(k kind) {
	for _, have := range registry {
		if have.codec() == k.codec() || have.name() == k.name() {
			panic("unitytable: codec " + k.name() + " registered twice")
		}
	}
	registry = append(registry, k)
	sort.SliceStable(registry, func(i, j int) bool { return registry[i].priority() < registry[j].priority() })
}

// kindOf is the codec that carries f: the lowest-priority claim.
func kindOf(f *ir.Field) kind {
	for _, k := range registry {
		if k.claims(f) {
			return k
		}
	}
	panic("unitytable: no codec claims field " + f.Name + " — kind_unsupported claims every field")
}

// Kinds lists the registered codecs by name, lowest priority first: the
// per-kind layout as data, for the tests that hold it.
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for _, k := range registry {
		out = append(out, k.name())
	}
	return out
}

// CodecOf is the codec name a field is carried by.
func CodecOf(f *ir.Field) string { return kindOf(f).name() }

// plainScalar reports whether the field is a lone value: no container shape,
// no presence bit, no pointer, no branch and no deprecation. The scalar codecs
// claim only these; every container and marker is a later card's.
func plainScalar(f *ir.Field) bool {
	return !f.Type.Pointer && !f.Type.Optional && f.Array == ir.ArrayNone && !f.IsMap() &&
		f.KeyEnum == "" && f.MapEntry == nil && f.Guard == "" && !f.Deprecated
}
