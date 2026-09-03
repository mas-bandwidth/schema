// The BUILD VERSION (docs/SPEC-TABLES.md §20): the low 64 bits of SHA-256 over the
// unit's COOK PROJECTION, which is exactly how the protocol id is taken over
// the wire-shape projection (docs/SPEC.md §3.1), for exactly its reason — what an
// id depends on has to be printable, readable and diffable, and a fact missing
// from it has to be a review question rather than an implementation detail.
//
// There are TWO unit-wide version ids and no others. The PROTOCOL ID is the
// type wire's and it is the connect gate; the BUILD VERSION is everything
// cooked or blocked — the cooked header carries it (§7), the block form's
// prologue carries it (§19), and a store's tuple is keyed by it. A table edit
// moves the build version and never the protocol id; a type edit moves both.
//
// It is COMPILER-SETTLED, and that is the property the tuple rests on: tooling
// cooks before any game binary exists, so the number has to be knowable from
// the schema alone. The compiler owns every fact in it, including the layout,
// which it computes from its own C ABI model (§20.3) — the model both backends
// assert against, so a build that lays a record out differently fails to BUILD
// rather than cooking bytes nobody can read.
package ir

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// BuildVersionForm is the cook projection's own FORM VERSION, and the COOK
// FORM's too (docs/SPEC-TABLES.md §20.2). Bump it when this rendering changes, and
// bump it when the cook's own form changes — the region's pack order, the node
// directory's encoding, the header's shape — because without a version for
// those a cook's bytes could diverge with the id unmoved.
const BuildVersionForm = 1

// blockPrologueFacts renders the block projection's generated prologue as the
// digest sees it: each word in order, named, with its width in bytes.
func blockPrologueFacts() string {
	words := []string{"magic", "build_version", "byte_order"}
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, w+":8")
	}
	return strings.Join(out, ",")
}

// TableKindPointer is a `*T` reference slot's wire kind (docs/SPEC-TABLES.md §3.1):
// a pointer rides as a u32 index into the node table, and §20.2 renders it as
// kind 17 with `type=` naming the pointee.
const TableKindPointer = 17

// BuildVersion is the unit's build version: the low 64 bits of SHA-256 over
// [CookProjection], the final eight bytes interpreted big-endian.
func BuildVersion(u *Unit) uint64 {
	sum := sha256.Sum256([]byte(CookProjection(u)))
	return binary.BigEndian.Uint64(sum[24:])
}

// CookProjection renders the unit's cook projection (docs/SPEC-TABLES.md §20.2).
//
// ASCII, every line terminated by one "\n", no blank lines; tokens separated
// by exactly one space; a nested line indented four spaces. Ids are four
// lowercase hex digits; sizes, offsets and bounds decimal; every value the
// schemafmt-canonical text of the EVALUATED value.
func CookProjection(u *Unit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "schema-build-version %d\n", BuildVersionForm)
	fmt.Fprintf(&b, "protocol %016x\n", u.ProtocolId)
	// The byte order is a GENERATION input (§20.1): a cook is produced in the
	// byte order of the build it is cooked for, and two builds alike in every
	// other fact produce different cook bytes. It is `little` for every target
	// schema generates for today; a big-endian cook is §15's question.
	b.WriteString("byteorder little\n")
	// THE BLOCK FORM'S PROLOGUE, unconditionally and in the HEADER (§19.1,
	// §20.2). It is a fact of the BUILD rather than of any one declaration:
	// nothing selects the form, every fixed table has one, and a table whose
	// arrays are all inline has a projection that is PURE PROLOGUE — so a unit
	// with no out-of-line array anywhere carries the prologue's shape in no
	// `block` line and would otherwise share an id across a change to it. Two
	// builds either side of such a change write incompatible blocks, which the
	// invariant does not permit.
	//
	// The words are named and widthed rather than counted, so the line moves
	// when the shape does and there is no counter for anyone to forget.
	fmt.Fprintf(&b, "block prologue=%s\n", blockPrologueFacts())

	closure := TableClosure(u)
	if len(closure) == 0 {
		// a unit that declares no table has a projection of its header lines
		// alone, deliberately not equal to the protocol id
		return b.String()
	}
	blocks := Blocks(u)

	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)

	enums := map[string]*Enum{}
	unions := map[string]*Union{}

	for _, name := range names {
		st := memberStruct(u, name)
		if st == nil {
			continue
		}
		ml := layoutRecord(u, st)
		fmt.Fprintf(&b, "record %s sizeof=%d alignof=%d\n", name, ml.Size, ml.Align)
		for _, fl := range ml.Fields {
			b.WriteString(cookFieldLine(fl, enums, unions))
		}
		// Every record whose block form MOVES something is followed by its
		// PROJECTION, whose slots are the other side's contract (§19). A record
		// with no out-of-line array has a projection that is its own layout
		// behind the prologue and contributes no line — which is what §20.2's
		// worked example shows, and the worked example is the golden.
		if bl := blocks.Block(name); bl != nil && len(bl.Arrays) > 0 {
			fmt.Fprintf(&b, "block %s sizeof=%d alignof=%d\n", name, bl.Projection.Size, bl.Projection.Align)
			for _, fl := range bl.Projection.Fields {
				fmt.Fprintf(&b, "    slot %04x offset=%d size=%d", TableFieldId(fl.Field), fl.Offset, fl.Size)
				if a := bl.ArrayByName(fl.Field.Name); a != nil {
					fmt.Fprintf(&b, " out_of_line stride=%d", a.Stride)
				}
				b.WriteString("\n")
			}
		}
	}

	for _, name := range sortedKeysOf(enums) {
		fmt.Fprintf(&b, "enum %s\n", name)
		for i, v := range enums[name].Variants {
			// the STORED VALUE, not a positional index: None = 0 is implicit
			// and never listed, so declared variants start at 1
			fmt.Fprintf(&b, "    variant %d %s\n", i+1, v)
		}
	}
	for _, name := range sortedKeysOf(unions) {
		fmt.Fprintf(&b, "union %s\n", name)
		for i, v := range unions[name].Variants {
			fmt.Fprintf(&b, "    arm %d %s payload=%s\n", i+1, v.Name, v.Type)
		}
	}
	return b.String()
}

// cookFieldLine renders one field's line, collecting the vocabularies it
// reaches. The optional tokens appear in §20.2's order and only where the fact
// exists.
func cookFieldLine(fl FieldLayout, enums map[string]*Enum, unions map[string]*Union) string {
	f := fl.Field
	kind := TableScalarKind(f)
	var b strings.Builder
	fmt.Fprintf(&b, "    field %04x kind=%d offset=%d size=%d", TableFieldId(f), kind, fl.Offset, fl.Size)
	if f.Type.Kind == TFixed {
		// a fixed field's SCALE: the slot holds units × 2^F, so F is a
		// meaning fact like a bound (§20.1 group 3) and rides beside the kind
		fmt.Fprintf(&b, " frac=%d", f.Type.FracBits)
	}

	// the REFERENT: the declaration this field, or its ARRAY ELEMENT, names.
	// A kind number says a record is nested and not WHICH one, so two
	// same-shaped records would be interchangeable to a digest that stopped at
	// the kind — and the nested body would then decode under different ids.
	if f.Type.Kind == TNamed {
		switch ref := f.Type.Ref.(type) {
		case *Struct:
			fmt.Fprintf(&b, " type=%s", ref.Name)
		case *Enum:
			fmt.Fprintf(&b, " enum=%s", ref.Name)
			enums[ref.Name] = ref
		case *Union:
			fmt.Fprintf(&b, " union=%s", ref.Name)
			unions[ref.Name] = ref
		}
		// there is deliberately NO flags= token (§20.1): a mask rides raw and
		// a load copies it verbatim, so swapping one flags declaration for a
		// same-width other changes no cook byte. Its WIDTH is in size=.
	}

	switch {
	case f.KeyEnum != "":
		fmt.Fprintf(&b, " elem=%d array=keyed bound=%d key=%s", cookElemSize(fl), f.ArrayBound, f.KeyEnum)
		if f.KeyEnumRef != nil {
			enums[f.KeyEnum] = f.KeyEnumRef
		}
	case f.Array == ArrayFixed:
		fmt.Fprintf(&b, " elem=%d array=fixed bound=%d", cookElemSize(fl), f.ArrayBound)
	case f.Array == ArrayCounted:
		fmt.Fprintf(&b, " elem=%d array=bounded bound=%d", cookElemSize(fl), f.ArrayBound)
	case f.Type.Kind == TString, f.Type.Kind == TBytes:
		// a string's and a bytes' CAPACITY, which §20.1 files beside an
		// array's bound for the same reason: it is the extent the storage took
		fmt.Fprintf(&b, " bound=%d", f.Type.Size)
	}

	if f.Type.Optional {
		b.WriteString(" optional=true")
	}
	if f.HasDefault {
		fmt.Fprintf(&b, " default=%s", cookValue(f))
	}
	switch {
	case f.HasIntRange:
		fmt.Fprintf(&b, " min=%s max=%s", f.IntMin, f.IntMax)
	case f.HasFloatRange:
		fmt.Fprintf(&b, " min=%s max=%s step=%s",
			canonicalFloat(f.FMin), canonicalFloat(f.FMax), canonicalFloat(f.Resolution))
	case f.Type.Kind == TBits:
		// bits(N) declares [0, 2^N - 1] by its WIDTH and §4 clamps a load to
		// it, so the implied range is a meaning fact like any declared one
		fmt.Fprintf(&b, " min=0 max=%d", (uint64(1)<<uint(f.Type.Width))-1)
	}
	b.WriteString("\n")
	return b.String()
}

// cookElemSize is one array element's own size in bytes — the pitch the
// storage takes per slot, which is what `elem=` names.
func cookElemSize(fl FieldLayout) int64 {
	f := fl.Field
	if f.ArrayBound <= 0 {
		return 0
	}
	elems := fl.Size
	if f.Array == ArrayCounted {
		elems -= 4 // the int32 count companion
	}
	if f.Type.Optional {
		elems -= 1
	}
	return elems / f.ArrayBound
}

// cookValue renders a specified default as schemafmt-canonical text of the
// EVALUATED value — what a constant now produces, never how it was spelled.
func cookValue(f *Field) string {
	switch {
	case f.DefVariant != "":
		return f.DefVariant
	case f.Type.Kind == TBool:
		if f.DefBool {
			return "true"
		}
		return "false"
	case f.DefInt != nil:
		return f.DefInt.String()
	default:
		return canonicalFloat(f.DefFloat)
	}
}

func canonicalFloat(v float64) string {
	s := fmt.Sprintf("%g", v)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func sortedKeysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
