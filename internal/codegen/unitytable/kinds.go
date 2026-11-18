package unitytable

import (
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// kind is one field codec of the Unity table backend: what a field of that
// shape is stored as, what its descriptor says, and the straight-line C# that
// measures, writes and reads it on the form-1 wire (docs/SPEC-TABLES.md §3).
type kind interface {
	name() string
	codec() int
	priority() int
	claims(f *ir.Field) bool
	carried() bool
	storage(f *ir.Field) string
	defaultValue(f *ir.Field) string
	defaultBits(f *ir.Field) uint64
	differs(f *ir.Field, v string) string
	width(f *ir.Field) int
	emitAsserts(b *strings.Builder, f *ir.Field, v string)
	emitPayload(b *strings.Builder, f *ir.Field, v string)
	emitRead(b *strings.Builder, f *ir.Field, v string)
	runtime() string
}

var registry []kind

func register(k kind) {
	for _, have := range registry {
		if have.codec() == k.codec() || have.name() == k.name() {
			panic("unitytable: codec " + k.name() + " registered twice")
		}
	}
	registry = append(registry, k)
	sort.SliceStable(registry, func(i, j int) bool { return registry[i].priority() < registry[j].priority() })
}

func kindOf(f *ir.Field) kind {
	for _, k := range registry {
		if k.claims(f) {
			return k
		}
	}
	panic("unitytable: no codec claims field " + f.Name + " — kind_unsupported claims every field")
}

func Kinds() []string {
	out := make([]string, 0, len(registry))
	for _, k := range registry {
		out = append(out, k.name())
	}
	return out
}

func CodecOf(f *ir.Field) string { return kindOf(f).name() }

// plainScalar reports whether the field is a lone value: no container shape,
// no presence bit, no pointer, no branch. Deprecated fields keep their storage
// and descriptor (and are skipped on Form-1 write while carried on Form 3).
func plainScalar(f *ir.Field) bool {
	return !f.Type.Pointer && !f.Type.Optional && f.Array == ir.ArrayNone && !f.IsMap() &&
		f.KeyEnum == "" && f.MapEntry == nil && f.Guard == ""
}
