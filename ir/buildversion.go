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
//
// FORM 3 (schema#435, THE ID-TABLE WIRE): ids render at SIXTY-FOUR bits, an
// enum field renders under its own kind 30, and a `flags` declaration takes a
// block of its own — the enum block with the keyword swapped (§20.2). Every
// unit's build version moves with it, which is what a form version is for: the
// text a cook's id is taken over is not the text it was taken over before.
const BuildVersionForm = 3

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
// a pointer rides as a canonical LEB128 index into the node table, and §20.2
// renders it as kind 17 with `type=` naming the pointee.
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
// by exactly one space; a nested line indented four spaces. Ids are SIXTEEN
// lowercase hex digits, the fnv1a64 of the effective name (§5); sizes, offsets
// and bounds decimal; every value the schemafmt-canonical text of the
// EVALUATED value.
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
	// SORTED BY THE NAME ON THE LINE (§20.2), which for a map's generated
	// entry is its anonymous key: the generated name is derived from the
	// field's SOURCE spelling, so ordering by it would let a `was` rename
	// move every line after it.
	sort.Slice(names, func(i, j int) bool {
		return ProjectionMemberName(u, names[i]) < ProjectionMemberName(u, names[j])
	})

	enums := map[string]*Enum{}
	flags := map[string]*Flags{}
	unions := map[string]*Union{}

	for _, name := range names {
		st := memberStruct(u, name)
		if st == nil {
			continue
		}
		ml := layoutRecord(u, st)
		fmt.Fprintf(&b, "record %s sizeof=%d alignof=%d\n", ProjectionMemberName(u, name), ml.Size, ml.Align)
		for _, fl := range ml.Fields {
			b.WriteString(cookFieldLine(u, fl, enums, flags, unions))
		}
		// Every record whose block form MOVES something is followed by its
		// PROJECTION, whose slots are the other side's contract (§19). A record
		// with no out-of-line array has a projection that is its own layout
		// behind the prologue and contributes no line — which is what §20.2's
		// worked example shows, and the worked example is the golden.
		if bl := blocks.Block(name); bl != nil && len(bl.Arrays) > 0 {
			fmt.Fprintf(&b, "block %s sizeof=%d alignof=%d\n", name, bl.Projection.Size, bl.Projection.Align)
			for _, fl := range bl.Projection.Fields {
				fmt.Fprintf(&b, "    slot %016x offset=%d size=%d", TableFieldWireId(fl.Field), fl.Offset, fl.Size)
				if a := bl.ArrayByName(fl.Field.Name); a != nil {
					fmt.Fprintf(&b, " out_of_line stride=%d", a.Stride)
				}
				b.WriteString("\n")
			}
		}
	}

	// A UNION'S ARMS REACH VOCABULARIES OF THEIR OWN (§2.6): an enum arm, a
	// flags arm, an arm that is another union. Close the two sets over the
	// arms before either section renders, so a vocabulary reached only
	// through an arm is projected exactly once and in name order.
	collectArmRefs(enums, flags, unions)

	for _, name := range sortedKeysOf(enums) {
		fmt.Fprintf(&b, "enum %s\n", name)
		for i := range enums[name].Variants {
			// the STORED VALUE, not a positional index: None = 0 is implicit
			// and never listed, so declared variants start at 1. The name is
			// the WIRE name (§5): a variant renamed under `was` keeps the id
			// every stored value carries, so the rename moves nothing.
			fmt.Fprintf(&b, "    variant %d %s\n", i+1, enums[name].VariantWireName(i))
		}
	}
	// A `flags` DECLARATION TAKES A BLOCK OF ITS OWN, and it is the enum block
	// with the keyword swapped (§20.2). The blocks sit AFTER the enums and
	// BEFORE the unions, each group sorted by name within itself. The number on
	// a variant line is its BIT POSITION, counted from 0, because the bit is
	// what a stored mask means (§20.1) — a mask rides raw, so a reorder or an
	// in-place rename remaps every cooked bit with nothing on the wire to say so.
	for _, name := range sortedKeysOf(flags) {
		fmt.Fprintf(&b, "flags %s\n", name)
		for i, v := range flags[name].Variants {
			fmt.Fprintf(&b, "    variant %d %s\n", i, v)
		}
	}
	for _, name := range sortedKeysOf(unions) {
		fmt.Fprintf(&b, "union %s\n", name)
		for i, v := range unions[name].Variants {
			b.WriteString(cookArmLine(u, unions[name], i+1, v, enums, flags, unions))
		}
	}
	return b.String()
}

// cookFieldLine renders one field's line, collecting the vocabularies it
// reaches. The optional tokens appear in §20.2's order and only where the fact
// exists.
func cookFieldLine(u *Unit, fl FieldLayout, enums map[string]*Enum, flags map[string]*Flags, unions map[string]*Union) string {
	f := fl.Field
	kind := TableWireScalarKind(f)
	if f.IsMap() {
		// A MAP FIELD IS AN ARRAY LINE (docs/SPEC-TABLES.md §20.1, §20.2):
		// `kind=14`, `array=map` and the entry's own storage size in `elem=`,
		// plus the `size=` its sixteen-byte slot produces. The KEY's kind and
		// capacity are NOT here: they ride on the entry's own `key` line,
		// which is where a key edit moves the id (§20.1).
		kind = TableKindArray
	}
	if f.IsList() {
		// AN UNBOUNDED ARRAY FIELD IS AN ARRAY LINE (docs/SPEC-TABLES.md
		// §20.2): `kind=14`, `array=unbounded` and the element's own storage
		// size in `elem=`, plus the `size=` its sixteen-byte slot produces.
		kind = TableKindArray
	}
	var b strings.Builder
	fmt.Fprintf(&b, "    field %016x kind=%d offset=%d size=%d", TableFieldWireId(f), kind, fl.Offset, fl.Size)
	b.WriteString(cookFacts(u, fl, enums, flags, unions))
	b.WriteString("\n")
	return b.String()
}

// collectArmRefs closes the enum and union sets over every union's ARMS
// (docs/SPEC-TABLES.md §2.6): an enum arm names an enum, an arm that is
// another union names a union, and each of those may name more. A `flags` arm
// names nothing — there is deliberately no `flags=` token (§20.1).
func collectArmRefs(enums map[string]*Enum, flags map[string]*Flags, unions map[string]*Union) {
	for grew := true; grew; {
		grew = false
		for _, name := range sortedKeysOf(unions) {
			for _, v := range unions[name].Variants {
				if v.F == nil || v.F.Type.Kind != TNamed {
					continue
				}
				switch ref := v.F.Type.Ref.(type) {
				case *Enum:
					if _, seen := enums[ref.Name]; !seen {
						enums[ref.Name] = ref
						grew = true
					}
				case *Union:
					if _, seen := unions[ref.Name]; !seen {
						unions[ref.Name] = ref
						grew = true
					}
				}
			}
		}
	}
}

// cookArmLine renders one union arm (docs/SPEC-TABLES.md §20.2). AN ARM IS A
// FIELD LINE (§2.6), so an arm that names a declared `type` or `table` by
// value carries `payload=<Name>` and nothing else — the spelling every arm had
// before an arm could be any field type, which is what keeps a unit that has
// not moved projecting exactly as it did — and any other arm carries the
// FIELD tokens for what it is, taken over the arm's own storage in the
// overlay.
func cookArmLine(u *Unit, un *Union, tag int, v UnionVariant, enums map[string]*Enum, flags map[string]*Flags, unions map[string]*Union) string {
	_, _, _, armOffset := UnionLayout(u, un)
	if v.Void() {
		// AN ARM WITH NO PAYLOAD carries `kind=none` (§20.2, §18.1): the kind
		// token saying there is no kind to carry, and no storage to offset
		return fmt.Sprintf("    arm %d %s kind=none\n", tag, v.WireName())
	}
	if v.Body() {
		// an arm that names a declared `type` or `table` carries `payload=`
		// and nothing else, exactly as it always did — so a unit whose arms
		// all name declared types projects exactly as it did before arms
		// could be anything else, and its build version does not move
		return fmt.Sprintf("    arm %d %s payload=%s\n", tag, v.WireName(), v.Type)
	}
	size, align := ArmLayout(u, v)
	fl := FieldLayout{Field: v.F, Offset: armOffset, Size: size, Align: align}
	return fmt.Sprintf("    arm %d %s kind=%d offset=%d size=%d%s\n",
		tag, v.WireName(), TableWireScalarKind(v.F), armOffset, size, cookFacts(u, fl, enums, flags, unions))
}

// cookFacts is the token tail a field line and an ARM line share: the scale,
// the referent, the array shape and capacity, the presence companion, the
// specified default and the effective range, in §20.2's order.
func cookFacts(u *Unit, fl FieldLayout, enums map[string]*Enum, flags map[string]*Flags, unions map[string]*Union) string {
	f := fl.Field
	var b strings.Builder
	if f.Type.Kind == TFixed {
		// a fixed field's SCALE: the slot holds units × 2^F, so F is a
		// meaning fact like a bound (§20.1 group 3) and rides beside the kind
		fmt.Fprintf(&b, " frac=%d", f.Type.FracBits)
	}

	// the REFERENT: the declaration this field, or its ARRAY ELEMENT, names.
	// A kind number says a record is nested and not WHICH one, so two
	// same-shaped records would be interchangeable to a digest that stopped at
	// the kind — and the nested body would then decode under different ids.
	// A BYTE BUFFER's referent is the blob node's own shape, `bytes` or
	// `string` (docs/SPEC-TABLES.md §2.5, §20.2), so a slot moved between
	// the two, or between a blob and a table, moves the id.
	if f.Type.Blob() {
		if f.Type.Kind == TString {
			b.WriteString(" type=string")
		} else {
			b.WriteString(" type=bytes")
		}
	}
	if f.Type.Kind == TNamed {
		switch ref := f.Type.Ref.(type) {
		case *Struct:
			// the WIRE name (§5): a table renamed under `was` keeps the name
			// every cooked node directory carries, so the rename moves nothing
			fmt.Fprintf(&b, " type=%s", ref.WireName())
		case *Enum:
			fmt.Fprintf(&b, " enum=%s", ref.Name)
			enums[ref.Name] = ref
		case *Union:
			fmt.Fprintf(&b, " union=%s", ref.Name)
			unions[ref.Name] = ref
		case *Flags:
			// there is deliberately NO flags= token (§20.1): a mask rides raw
			// and a load copies it verbatim, so swapping one flags declaration
			// for a same-width other changes no cook byte. Its WIDTH is in
			// size=. The DECLARATION still projects, because a variant's BIT
			// POSITION is what a stored mask means.
			flags[ref.Name] = ref
		}
	}

	switch {
	case f.IsMap():
		// `array=map` is the shape and `elem=` the ELEMENT'S STORAGE SIZE, as
		// on every other array line here: the generated entry's own `sizeof`,
		// which is the pitch its entries lie at inside the holder's node
		// extent (docs/SPEC-TABLES.md §20.2). There is no `bound=`: a map
		// declares no extent, and its count is a wire fact. The entry's own
		// record line carries everything else, keyed by this field's id.
		fmt.Fprintf(&b, " elem=%d array=map", layoutRecord(u, f.MapEntry).Size)
	case f.KeyEnum != "":
		fmt.Fprintf(&b, " elem=%d array=keyed bound=%d key=%s", cookElemSize(fl), f.ArrayBound, f.KeyEnum)
		if f.KeyEnumRef != nil {
			enums[f.KeyEnum] = f.KeyEnumRef
		}
	case f.Array == ArrayFixed:
		fmt.Fprintf(&b, " elem=%d array=fixed bound=%d", cookElemSize(fl), f.ArrayBound)
	case f.Array == ArrayCounted:
		fmt.Fprintf(&b, " elem=%d array=bounded bound=%d", cookElemSize(fl), f.ArrayBound)
	case f.Array == ArrayList:
		// `array=unbounded` is the shape and `elem=` the ELEMENT'S OWN storage
		// size, the pitch its elements lie at inside the holder's node extent,
		// as on every other array line here (docs/SPEC-TABLES.md §2.9, §20.2).
		// There is no `bound=`, for the map's reason: it declares no extent
		// and its count is a wire fact. The sixteen-byte slot is what `size=`
		// on this same line already says.
		fmt.Fprintf(&b, " elem=%d array=unbounded", elementPiece(u, f).size)
	case f.Type.Blob():
		// a byte buffer has no capacity: its blob node is exactly its size
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
	case f.Type.Kind == TString || f.Type.Kind == TBytes:
		return fmt.Sprintf("bytes:%x", f.DefBytes)
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
