// Package tabletext is the compiler-side instance model and text form for the
// TABLE wire (docs/SPEC-TABLES.md §16): an IR-driven reader and writer of §16's
// JSON, over an in-memory instance built from the same IR the emitters
// consume.
//
// It is TOOLING, never a runtime. The generated C++ carries the walk a program
// runs (§16.1); this carries the walk the COMPILER runs, because `schema pack`
// (§17) is a Go command and a compiler cannot execute the code it emits. The
// two are held to one wire by goldens, not by sharing source: for every corpus
// tree the bytes this engine produces equal C++ `Save` of the same instance.
// Where this package's comments name a C++ construct they name the contract it
// mirrors — a port never invents one.
package tabletext

import (
	"math/big"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Report is the read report both forms share (docs/SPEC-TABLES.md §4, §16.2):
// silence — every counter zero and Malformed false — means the data matched
// this schema exactly.
type Report struct {
	Unknown      int
	KindMismatch int
	// Widened counts a kind that GREW since the writer (docs/SPEC-TABLES.md
	// §4): an integer kind read into a wider one of the same signedness, or
	// f32 into f64, decoded exactly. It is the one counter that names no
	// loss, and the wire's own event: a text carries no kind to widen from,
	// so the text form leaves it at zero (§16.2).
	Widened   int
	Clamped   int
	Duplicate int
	Malformed bool
	// Refused is the VERDICT, not one of §4's events (docs/SPEC-TABLES.md §3):
	// a FORM BYTE this reader does not carry. It moves no counter and reports
	// no damage, so without it a refusal and a clean read are the same answer.
	Refused bool
}

// Add folds another report into this one, which is how a pack over a tree of
// files reports once (docs/SPEC-TABLES.md §17.3).
func (r *Report) Add(o Report) {
	r.Unknown += o.Unknown
	r.KindMismatch += o.KindMismatch
	r.Widened += o.Widened
	r.Clamped += o.Clamped
	r.Duplicate += o.Duplicate
	r.Malformed = r.Malformed || o.Malformed
}

// Silent reports whether nothing at all was counted.
func (r Report) Silent() bool {
	return r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0 && r.Duplicate == 0 && !r.Malformed
}

// Cell is one storage slot: a scalar in whichever representation its kind
// uses, a nested instance, or a union's tag beside its arm. One struct with
// every representation keeps the walks free of a type switch per access; only
// the member the field's kind names is ever read.
type Cell struct {
	B bool    // KindBool
	I int64   // signed integers
	U uint64  // unsigned integers, bits, flags, an enum value, a union tag
	F float64 // f32 / f64, held at double width as the descriptors do
	// Wide is the RAW integer of a wide kind (ir.TableKindWide: the 128-bit
	// integers and the fixed-point family, docs/SPEC-TABLES.md §3) — a fixed
	// field holds units × 2^F here, exactly what its storage holds. nil is
	// zero, so a fresh cell and an elided field agree without a materialized
	// big.Int per slot.
	Wide *big.Int
	Str  []byte    // string / bytes payload, at its used extent
	// Units is a `wstring(N)` value's UTF-16 CODE UNITS, at its used extent
	// (docs/SPEC-TABLES.md §3's kind 33, SPEC.md §4.12). It is a separate
	// member from Str for the reason the two are separate KINDS: a wstring's
	// extent is counted in units and a string's in bytes, and one member
	// holding both would make the count's meaning a field's type rather than
	// the member's. Wide text takes no specified default, so nil is the whole
	// of an unset one.
	Units []uint16
	Tab  *Instance // a nested table or type, or a union arm's declared-type payload

	// Arm is a UNION ARM that names no declaration (docs/SPEC-TABLES.md
	// §2.6): an arm is a field line, so its storage is a field's storage —
	// the value, its used count, its elements — held here beside the tag in
	// U. A body arm keeps Tab instead, and exactly one of the two is set on a
	// selected arm.
	Arm *Field

	// Node is a `*T` POINTER field's referent (docs/SPEC-TABLES.md §2.1, §3.1), and
	// nil is NULL — a pointer takes no specified default, so null is the only
	// thing an absence could mean. It is deliberately not Tab: a pointee is a
	// NODE with an identity, written once however many slots name it, while Tab
	// is a by-value nesting that belongs to the field that holds it. The node
	// INDEX is never carried here; it is re-derived from the graph by the
	// depth-first pre-order walk both the wire and a region number by (§3.1),
	// which is what makes measure and save agree without passing anything
	// between them.
	Node *Instance

	// Blob is a `*bytes` or `*string` BYTE BUFFER field's referent
	// (docs/SPEC-TABLES.md §2.5): a blob node at exactly its bytes, nil for null.
	// It is a NODE like Node is — identity by pointer, written once however
	// many slots name it — and a zero-length blob is a non-nil Blob whose Data
	// is empty, because null and empty are two values.
	Blob *Blob
}

// Blob is a BYTE BUFFER's node (docs/SPEC-TABLES.md §2.5): the bytes, at exactly
// their length. Whether it is a `*bytes` blob or a `*string` blob is the
// FIELD's fact, not the node's — the two reserved type ids come from the slot
// that names it (§3.1).
type Blob struct {
	Data []byte
}

// Field is one field's storage in an [Instance] — the value, the companions
// the generated struct carries beside it (a used count, a `?T` presence bool),
// and the declaration it belongs to.
type Field struct {
	Def *ir.Field

	// Present is the `<field>_present` companion of a `?T` field
	// (docs/SPEC-TABLES.md §2.3). PRESENCE, not content, decides whether the field
	// rides.
	Present bool

	// Count is the used extent companion: a counted array's element count, a
	// string's or bytes' used length. A fixed or enum-keyed array has none —
	// every slot exists.
	Count int

	Cell  Cell   // the value, when the field is not an array
	Elems []Cell // the slots, when it is: len == the declared bound

	// Entries are a MAP's entries (docs/SPEC-TABLES.md §2.8), each Cell.Tab
	// an instance of the generated `{ key, value }` table. A map declares no
	// extent, so this is the one field storage with no bound: it holds what
	// the value holds, in ASCENDING KEY ORDER with no key twice, which is the
	// order the wire and the text are both written in.
	Entries []Cell
}

// Instance is one table or type value with its fields in declaration order.
// A fresh instance holds exactly what the generated struct's member
// initializers hold, so a field the text or the wire never mentions keeps the
// default an absent field takes (docs/SPEC-TABLES.md §4).
type Instance struct {
	Def    *ir.Struct
	Fields []Field

	byKey  map[string]int // json key -> field index
	byName map[string]int // field name -> field index (guard evaluation)
}

// Model is the closure the walks run over: the unit, plus the lookups a walk
// needs at every node.
type Model struct {
	Unit *ir.Unit

	variable map[string]bool // the derived mode of every table (§2.2), on first use
}

// NewModel binds a checked unit for the text and wire walks.
func NewModel(u *ir.Unit) *Model { return &Model{Unit: u} }

// IsVariable reports whether a table derives the VARIABLE-LENGTH mode
// (docs/SPEC-TABLES.md §2.2): a pointer somewhere in its by-value closure.
func (m *Model) IsVariable(name string) bool {
	if m.variable == nil {
		m.variable = ir.VariableTables(m.Unit)
	}
	return m.variable[name]
}

// Lookup resolves a closure member by name — a `table` or the `type` a table
// reaches — exactly as the emitters resolve one.
func (m *Model) Lookup(name string) *ir.Struct {
	if st := m.Unit.Tables[name]; st != nil {
		return st
	}
	return m.Unit.Structs[name]
}

// Roots are the unit's table declarations by name, sorted — the set `--root`
// may name.
func (m *Model) Roots() []string {
	names := make([]string, 0, len(m.Unit.Tables))
	for name := range m.Unit.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// New builds a fresh instance of a closure member at its declared defaults.
func (m *Model) New(st *ir.Struct) *Instance {
	inst := &Instance{
		Def:    st,
		Fields: make([]Field, len(st.Fields)),
		byKey:  make(map[string]int, len(st.Fields)),
		byName: make(map[string]int, len(st.Fields)),
	}
	for i, f := range st.Fields {
		inst.Fields[i].Def = f
		inst.byKey[ir.TableFieldJsonKey(f)] = i
		inst.byName[f.Name] = i
		m.reset(&inst.Fields[i])
	}
	return inst
}

// reset returns one field to its declared defaults, mirroring the generated
// struct's member initializers exactly: a scalar takes its specified default,
// and an ARRAY's slots take the value-initialized element — zero for a scalar
// element, the element type's own defaults for a nested table or type. That
// asymmetry is C++'s `T x[N] = {}` and is mirrored, not invented.
func (m *Model) reset(fv *Field) {
	f := fv.Def
	fv.Present = false
	fv.Count = 0
	fv.Cell = Cell{}
	fv.Elems = nil
	fv.Entries = nil
	switch {
	case f.IsMap():
		return // an empty map: no entry, and the field elides (§3)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		// the declared default, when there is one, at its used length
		// (SPEC §4.2): the same bytes the generated initializer carries and
		// the same bytes the writer elides against
		if len(f.DefBytes) > 0 {
			fv.Cell.Str = append([]byte(nil), f.DefBytes...)
			fv.Count = len(f.DefBytes)
		}
		return
	case f.Type.Kind == ir.TWString:
		// WIDE TEXT TAKES NO SPECIFIED DEFAULT (SPEC.md §4.12), so the empty
		// value is the whole of it and there is nothing to establish
		return
	case f.Array != ir.ArrayNone:
		fv.Elems = make([]Cell, f.ArrayBound)
		for i := range fv.Elems {
			fv.Elems[i] = m.elementZero(f)
		}
		return
	}
	fv.Cell = m.fieldDefault(f)
}

// elementZero is one value-initialized array slot.
func (m *Model) elementZero(f *ir.Field) Cell {
	if f.Type.Pointer {
		return Cell{} // a fresh reference is null, and null is its only default
	}
	if f.Type.Kind == ir.TNamed {
		if st, ok := f.Type.Ref.(*ir.Struct); ok {
			return Cell{Tab: m.New(st)}
		}
		if un, ok := f.Type.Ref.(*ir.Union); ok {
			_ = un
			return Cell{}
		}
	}
	return Cell{}
}

// FieldDefaultCell is the value a field elides against on the write side — the
// same literal the generated NSDMI carries (docs/SPEC-TABLES.md §3, §4).
func (m *Model) FieldDefaultCell(f *ir.Field) Cell { return m.fieldDefault(f) }

// fieldDefault is a non-array field's declared default — the same literal the
// generated NSDMI carries, and the same value the writer elides against.
func (m *Model) fieldDefault(f *ir.Field) Cell {
	if f.Type.Pointer {
		// a pointer field takes no specified default: a fresh reference is
		// NULL, and null is the only value a default could name (§2.1). It is
		// also what keeps this walk finite — recursion through a pointer edge
		// is legal and expected, and materializing a pointee here would not
		// terminate.
		return Cell{}
	}
	switch f.Type.Kind {
	case ir.TBool:
		return Cell{B: f.HasDefault && f.DefBool}
	case ir.TFloat32, ir.TFloat64:
		if f.HasDefault {
			return Cell{F: f.DefFloat}
		}
		return Cell{}
	case ir.TInt, ir.TFixed:
		if ir.TableKindWide(ir.TableScalarKind(f)) {
			// a fixed default is held RAW in the IR (units × 2^F), which is
			// what the storage initializer carries (SPEC.md §4.6)
			if f.HasDefault && f.DefInt != nil && f.DefInt.Sign() != 0 {
				return Cell{Wide: new(big.Int).Set(f.DefInt)}
			}
			return Cell{}
		}
		if f.HasDefault && f.DefInt != nil {
			return intCell(f.DefInt, f.Type.Signed)
		}
		return Cell{}
	case ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			return intCell(f.DefInt, false)
		}
		return Cell{}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return Cell{U: uint64(EnumValue(ref, f.DefVariant))}
			}
			return Cell{}
		case *ir.Flags:
			if f.HasDefault && f.DefInt != nil {
				return Cell{U: f.DefInt.Uint64()} // the mask the brace list spells (SPEC §4.2)
			}
			return Cell{}
		case *ir.Struct:
			return Cell{Tab: m.New(ref)}
		case *ir.Union:
			return Cell{} // tag 0 is None; no arm is materialized until one is set
		}
	}
	return Cell{}
}

func intCell(v *big.Int, signed bool) Cell {
	if signed {
		return Cell{I: v.Int64(), U: uint64(v.Int64())}
	}
	if v.Sign() < 0 {
		return Cell{I: v.Int64(), U: uint64(v.Int64())}
	}
	return Cell{U: v.Uint64(), I: int64(v.Uint64())}
}

// EnumValue is the declaration-side value of a variant name: 0 is the implicit
// None, and the declared variants pack from 1 (SPEC §4.2). -1 when the enum
// has no such name.
func EnumValue(e *ir.Enum, name string) int64 {
	if name == "None" {
		return 0
	}
	for i, v := range e.Variants {
		if v == name {
			return int64(i + 1)
		}
	}
	return -1
}

// EnumName is the variant name a value spells, "" when no variant names it —
// a value with no name has no text spelling and no wire identity (§5).
func EnumName(e *ir.Enum, value int64) string {
	if value == 0 {
		return "None"
	}
	if value >= 1 && int(value) <= len(e.Variants) {
		return e.Variants[value-1]
	}
	return ""
}

// KindWidth is a fixed-width kind's payload size in bytes; 0 for the framed
// kinds (string, table, array, union, keyed). The kind vocabulary itself lives
// in ir (ir.TableKind*, ir.TableScalarKind): two copies of that mapping would
// be two wires, so this engine reads the same one the emitters do and adds only
// the width question they answer inline.
func KindWidth(kind int) int { return ir.TableKindWidth(kind) }

// ImpliedRange is the range a declaration implies without declaring one:
// `bits(N)` declares its bound by its WIDTH, `[0, 2^N - 1]`, and a value past
// it clamps and counts exactly as a declared `| max` would (docs/SPEC-TABLES.md
// §16.2). The generated descriptors carry it in the same `has_range` columns a
// declared range uses, so the two implementations clamp on the same numbers in
// the same order. ok is false when the field implies nothing.
func ImpliedRange(f *ir.Field) (lo, hi float64, ok bool) {
	if f.Type.Kind != ir.TBits || f.HasIntRange || f.Type.Width >= 64 {
		return 0, 0, false
	}
	return 0, float64(uint64(1)<<uint(f.Type.Width) - 1), true
}

// StorageBytes is the width of a field's own C++ storage slot, which the text
// form's integer clamp bounds against — the same `elem_size` the descriptors
// carry. It is the wire kind's width for a plain integer, and the declared
// storage for `bits(N)`, which is wider than the kind it rides in — which is
// why `bits(N)`'s own bound is [ImpliedRange]'s job and not this one's.
func StorageBytes(f *ir.Field) int {
	switch f.Type.Kind {
	case ir.TInt, ir.TFixed:
		return f.Type.Width / 8
	case ir.TBits:
		if f.Type.Width <= 32 {
			return 4
		}
		return 8
	}
	return KindWidth(ir.TableScalarKind(f))
}

// EnumOf returns the enum a field's values come from, or nil.
func EnumOf(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// FlagsOf returns the flags declaration a field's mask comes from, or nil.
func FlagsOf(f *ir.Field) *ir.Flags {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	fl, _ := f.Type.Ref.(*ir.Flags)
	return fl
}

// NewArm builds ONE ARM's storage, zero-established (SPEC §5): an arm is a
// field line (docs/SPEC-TABLES.md §2.6), so its storage is a field's, and an
// arm takes no specified default — the reset a fresh field takes IS the
// establish. A body arm holds an Instance in its cell instead, exactly as a
// nested field does.
func (m *Model) NewArm(v ir.UnionVariant) *Field {
	if v.Void() {
		return nil // a payload-free arm has no storage (§2.6)
	}
	fv := &Field{Def: v.F}
	m.reset(fv)
	return fv
}

// UnionOf returns the union a field holds, or nil.
func UnionOf(f *ir.Field) *ir.Union {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	un, _ := f.Type.Ref.(*ir.Union)
	return un
}

// StructOf returns the nested table or type a field holds, or nil.
func StructOf(f *ir.Field) *ir.Struct {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	st, _ := f.Type.Ref.(*ir.Struct)
	return st
}

// IsArrayShaped reports whether the field's payload is a wire ARRAY — a
// declared array, or `bytes(N)`, which rides as an array of u8 (§2.5).
func IsArrayShaped(f *ir.Field) bool {
	return f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes
}

// ---- guards ----

// GuardTerm is one conjunct of a guarded field's branch condition: a bool
// field of the same body, and the value it must hold.
type GuardTerm struct {
	Field string
	Want  bool
}

// Guards maps each guarded field's name to its branch condition, composed
// through nesting exactly as the C++ emitter composes it. A field with no
// entry is unguarded and always rides.
func Guards(st *ir.Struct) map[string][]GuardTerm {
	out := map[string][]GuardTerm{}
	var walk func(items []ir.Item, cond []GuardTerm)
	walk = func(items []ir.Item, cond []GuardTerm) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if len(cond) > 0 {
					terms := make([]GuardTerm, len(cond))
					copy(terms, cond)
					out[item.F.Name] = terms
				}
			case *ir.Branch:
				pos, neg := true, false
				if item.Neg {
					pos, neg = false, true
				}
				walk(item.Then, append(cond[:len(cond):len(cond)], GuardTerm{item.Cond, pos}))
				walk(item.Else, append(cond[:len(cond):len(cond)], GuardTerm{item.Cond, neg}))
			}
		}
	}
	walk(st.Items, nil)
	return out
}

// GuardHolds evaluates a guarded field's condition against the instance — the
// wire's own elision (§4) carried into both forms, so a text and a wire
// written from one instance say the same thing.
func (inst *Instance) GuardHolds(terms []GuardTerm) bool {
	for _, t := range terms {
		i, ok := inst.byName[t.Field]
		if !ok {
			return false
		}
		if inst.Fields[i].Cell.B != t.Want {
			return false
		}
	}
	return true
}

// FieldByKey finds the field a text key names (docs/SPEC-TABLES.md §16.3).
func (inst *Instance) FieldByKey(key string) (*Field, bool) {
	i, ok := inst.byKey[key]
	if !ok {
		return nil, false
	}
	return &inst.Fields[i], true
}

// FieldIndexByKey is [Instance.FieldByKey]'s index form, for duplicate
// tracking.
func (inst *Instance) FieldIndexByKey(key string) (int, bool) {
	i, ok := inst.byKey[key]
	return i, ok
}

// ---- enum-keyed arrays: the slot <-> variant mapping ----

// An enum-keyed array's STORAGE holds E.Max slots, ONE PER NAMED VARIANT
// (docs/SPEC-TABLES.md §2.4): None is the null key, so nothing is stored for it and
// the storage SHIFTS LEFT — the key k lives at slot k-1. Every rule that
// follows is one consequence of that: a None key never rides on the wire, a
// stored key of 0 is malformed, a "None" key in a text is unknown and counted,
// and a keyed field's directory has no `None.json`. The four functions below
// are the ONE place the mapping lives; the SHIFT appears nowhere else.

// KeyedSlotCount is the number of slots an enum-keyed array stores — the whole
// extent, every slot a named variant's. Iterate from [KeyedFirstSlot].
func KeyedSlotCount(f *ir.Field) int { return int(f.ArrayBound) }

// KeyedFirstSlot is the first slot a walk visits: the storage has no None slot
// to skip, so a walk starts at 0.
func KeyedFirstSlot() int { return 0 }

// KeyedSlotValue is the enum value slot i belongs to: the shift, in one place.
func KeyedSlotValue(f *ir.Field, slot int) int64 { return int64(slot) + 1 }

// KeyedValueSlot is the inverse: the slot an enum value owns, or -1 when the
// array has no slot for it — which None, value 0, never has.
func KeyedValueSlot(f *ir.Field, value int64) int {
	if value < 1 || value > int64(KeyedSlotCount(f)) {
		return -1
	}
	return int(value) - 1
}

// WideValue is a wide cell's raw integer, zero when nothing placed one.
func WideValue(cell *Cell) *big.Int {
	if cell.Wide == nil {
		return new(big.Int)
	}
	return cell.Wide
}

// ElementZero is one value-initialized array slot — what an UNBOUNDED ARRAY's
// decode appends per element it reaches (docs/SPEC-TABLES.md §2.9), since a
// list's slots are grown against the body rather than sized from the
// declaration.
func (m *Model) ElementZero(f *ir.Field) Cell { return m.elementZero(f) }
