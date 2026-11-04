// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, in Python: every
// fixed table is a class with the type value shape (docs/PORT-PYTHON.md, THE
// VALUE MAPPING), and every fixed root lays its form down as data — its wire
// hash, its record body size, its lineage (the lock's layouts, oldest first,
// its own last, and the floor) and its SHAPE, the reader's own fields as
// nodes of the runtime's fixed module (runtime/_schema/fixed.py), which reads
// and writes the form from that data (docs/FIXED-FORM-ALGORITHM.md). The
// layout bytes and every hash are the shared ir walk's, laid down as
// constants: the runtime never derives a hash.
package python

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedLineageEntry is ONE locked layout of one fixed table: Wire, the eight
// bytes the header and every record carry; Layout, the bytes verbatim;
// Record, the WHOLE record (the eight hash bytes and the body), as the
// compiler hands it; Retired, which moves the floor. Writer is the writer's
// root when the build holds its declaration — the unit's own entry, or an
// older unit a test locks — and nil for an entry read from a lock. A read
// through an entry with a Writer holds each ranged root field to the WRITER's
// declared range (docs/FIXED-FORM-ALGORITHM.md §4.6); without one, to the
// reader's.
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
	Writer  *ir.Struct
}

const fixedHashBytes = 8

// FixedLineageOf is the entry a unit's OWN build locks for table name.
func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name != name {
			continue
		}
		layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
		return FixedLineageEntry{
			Wire:   ir.TableFixedLayoutHash(layout, st),
			Layout: layout,
			Record: fixedHashBytes + ir.TableFixedTypeBytes(st),
			Writer: st,
		}, true
	}
	return FixedLineageEntry{}, false
}

// refuseVariableTables refuses, by name, every table the fixed form does not
// carry: a table not declared fixed, a fixed table past the form's record
// ceiling or depth bound (which keeps form 1), and a union with a table arm.
// They ride the variable wire, which arrives with P6.
func refuseVariableTables(u *ir.Unit) error {
	form := map[string]bool{}
	for _, st := range ir.TableFixedFormRoots(u) {
		form[st.Name] = true
	}
	for _, f := range u.Files {
		for _, t := range f.Tables {
			if !t.FixedDeclared {
				return fmt.Errorf("python: %s.schema declares table %s, a variable table, and the Python target carries the fixed form only (docs/PORT-PYTHON.md, THE LAYERS: P6 variable tables)", f.Base, t.Name)
			}
			if !form[t.Name] {
				return fmt.Errorf("python: %s.schema declares fixed table %s, past the fixed form's record ceiling or depth bound, so it keeps form 1, the variable wire, which the Python target does not carry yet (docs/SPEC-TABLES.md §3.4; docs/PORT-PYTHON.md, THE LAYERS: P6)", f.Base, t.Name)
			}
		}
		for _, un := range f.TableUnions {
			return fmt.Errorf("python: %s.schema declares union %s with a table arm, and the Python target does not carry it yet (docs/PORT-PYTHON.md, THE LAYERS: P6 variable tables)", f.Base, un.Name)
		}
	}
	return nil
}

// refuseFixedLineage fails the build on a handed entry that is a LOCK BUG
// (§5.9 #8, #26): a layout that does not frame with a table root, or a record
// size that is not the hash plus that root's body. It is never a wire event.
func refuseFixedLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) error {
	emitted := map[string]bool{}
	for _, st := range ir.TableFixedFormRoots(u) {
		emitted[st.Name] = true
	}
	names := make([]string, 0, len(lineage))
	for name := range lineage {
		if emitted[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		for _, e := range lineage[name] {
			root, ok := fixedLayoutRoot(e.Layout)
			if !ok {
				return fmt.Errorf("fixed table %s: the lineage entry 0x%016x is not a parseable layout — it is a bug in the LOCK and never a wire event, so the build fails here (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #8)", name, e.Wire)
			}
			if want := fixedHashBytes + root; e.Record != want {
				return fmt.Errorf("fixed table %s: the lineage entry 0x%016x records a record size of %d, and its own layout accounts for %d (the %d-byte hash and a %d-byte body) — it is a bug in the LOCK and the build fails here (§5.9 #26)", name, e.Wire, e.Record, want, fixedHashBytes, root)
			}
		}
	}
	return nil
}

func fixedLayoutRoot(b []byte) (int64, bool) {
	if len(b) < ir.TableFixedLayoutHeaderBytes {
		return 0, false
	}
	count := int(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
	if count == 0 || ir.TableFixedLayoutHeaderBytes+count*ir.TableFixedEntryBytes != len(b) {
		return 0, false
	}
	at := ir.TableFixedLayoutHeaderBytes
	if b[at+8] != ir.TableKindTable {
		return 0, false
	}
	size := int64(uint32(b[at+9]) | uint32(b[at+10])<<8 | uint32(b[at+11])<<16 | uint32(b[at+12])<<24)
	if size == 0 || size > ir.TableFixedRecordMaxBytes {
		return 0, false
	}
	return size, true
}

// fixedLineageFor is the lineage laid down for one table: the handed entries,
// OLDEST FIRST, with the table's own entry appended last when the lineage does
// not already end on it, and the floor, 1 + the highest retired index (§5.2).
func fixedLineageFor(handed []FixedLineageEntry, own FixedLineageEntry) ([]FixedLineageEntry, int) {
	entries := append([]FixedLineageEntry(nil), handed...)
	if n := len(entries); n == 0 || entries[n-1].Wire != own.Wire {
		entries = append(entries, own)
	}
	floor := 0
	for i, e := range entries {
		if e.Retired {
			floor = i + 1
		}
	}
	return entries, floor
}

// fixedClosure is every struct, table and union a fixed root of the unit
// reaches by value: the declarations that need a shape.
func fixedClosure(u *ir.Unit) map[string]bool {
	seen := map[string]bool{}
	var walkStruct func(st *ir.Struct)
	var walkUnion func(un *ir.Union)
	walkField := func(f *ir.Field) {
		if f == nil || f.Type.Kind != ir.TNamed {
			return
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			walkStruct(r)
		case *ir.Union:
			walkUnion(r)
		}
	}
	walkStruct = func(st *ir.Struct) {
		if seen[st.Name] {
			return
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			walkField(f)
		}
	}
	walkUnion = func(un *ir.Union) {
		if seen[un.Name] {
			return
		}
		seen[un.Name] = true
		for _, v := range un.Variants {
			walkField(v.F)
		}
	}
	for _, st := range ir.TableFixedFormRoots(u) {
		walkStruct(st)
	}
	return seen
}

// ---------------------------------------------------------------------------
// the table classes

// tableFieldType is a table field's annotation: optional is `T | None`, a
// keyed array a KeyedArray.
func (g *gen) tableFieldType(f *ir.Field) string {
	t := g.pyType(f)
	switch {
	case f.KeyEnum != "":
		t = "_s_fx.KeyedArray"
	case f.Type.Optional:
		t = g.pyElemType(f.Type) + " | None"
	}
	return t
}

// tableFieldDefault is a table field's value at construction: absent for an
// optional (None), one slot per variant for a keyed array.
func (g *gen) tableFieldDefault(f *ir.Field) string {
	switch {
	case f.Type.Optional:
		return "None"
	case f.KeyEnum != "":
		return "_s_fx.KeyedArray(" + g.listOf(f.ArrayBound, g.elemFill(f), f.Type) + ")"
	}
	return g.defaultExpr(f)
}

// elemFill is one element of an array field at the element type's own
// construction: a declaration's constructor, else its zero. An array field
// takes no default of its own (SPEC §4.2).
func (g *gen) elemFill(f *ir.Field) string {
	if isComposite(f.Type) {
		return g.ref(f.Type.Name) + "()"
	}
	return g.scalarZero(f.Type)
}

// emitTable emits a fixed table's class: __slots__, the constructor at the
// declared defaults, and the fixed form (load_fixed, save_fixed) through the
// root's data.
func (g *gen) emitTable(d *ir.Struct) {
	g.pf("class %s:\n", d.Name)
	if doc := docstring(d.Doc, "    "); doc != "" {
		g.pf("%s\n", doc)
	} else {
		g.pf("    \"\"\"fixed table %s.\"\"\"\n\n", d.Name)
	}
	g.pf("    __slots__ = (")
	for i, f := range d.Fields {
		if i > 0 {
			g.pf(", ")
		}
		g.pf("%q", f.Name)
	}
	if len(d.Fields) == 1 {
		g.pf(",")
	}
	g.pf(")\n\n")
	g.pf("    def __init__(self) -> None:\n")
	if len(d.Fields) == 0 {
		g.pf("        pass\n")
	}
	for _, f := range d.Fields {
		g.pf("%s", comment(f.Doc, "        "))
		g.pf("        self.%s: %s = %s\n", f.Name, g.tableFieldType(f), g.tableFieldDefault(f))
	}
	g.pf("\n    @classmethod\n")
	g.pf("    def load_fixed(cls, data: bytes | bytearray | memoryview, report: _s_fx.Report | None = None) -> list[%s]:\n", d.Name)
	g.pf("        \"\"\"Every record of a form-3 file (docs/SPEC-TABLES.md §3.4), or Refused with no value.\"\"\"\n")
	g.pf("        return _s_t.cast(\"list[%s]\", _s_fixed_%s.load(data, report))\n\n", d.Name, d.Name)
	g.pf("    @staticmethod\n")
	g.pf("    def save_fixed(values: _s_t.Sequence[%s]) -> bytes:\n", d.Name)
	g.pf("        \"\"\"The form-3 file of values, or InvalidValue naming the field and the rule, with no bytes.\"\"\"\n")
	g.pf("        return _s_fixed_%s.save(values)\n\n", d.Name)
	g.pf("    def __eq__(self, other: object) -> bool:\n")
	g.pf("        return _s_rt.equal(self, other)\n\n")
	g.pf("    def __repr__(self) -> str:\n")
	g.pf("        return _s_rt.describe(self)\n\n\n")
}

// ---------------------------------------------------------------------------
// the shapes

// shapeOf is a field's payload node: what rides at its slot, the optional's
// present byte aside.
func (g *gen) shapeOf(f *ir.Field, inTable bool) string {
	switch {
	case f.KeyEnum != "":
		e := f.KeyEnumRef
		return fmt.Sprintf("_s_fx.Keyed(%d, 0x%016x, %s, %s, lambda: %s, %s)",
			ir.TableFixedFieldBytes(f), ir.TableWireId(f.KeyEnum), variantIds(e), g.elemShape(f), g.elemFill(f), pyBool(inTable))
	case f.Array != ir.ArrayNone:
		return fmt.Sprintf("_s_fx.Array(%d, %s, %d, %s, lambda: %s)",
			ir.TableFixedFieldBytes(f), pyBool(f.Array == ir.ArrayCounted), f.ArrayBound, g.elemShape(f), g.elemFill(f))
	case f.Type.Kind == ir.TBytes:
		return fmt.Sprintf("_s_fx.Bytes(%d)", ir.TableFixedFieldBytes(f))
	case f.Type.Kind == ir.TString:
		return fmt.Sprintf("_s_fx.Text(%d, %d)", ir.TableKindString, ir.TableFixedFieldBytes(f))
	case f.Type.Kind == ir.TWString:
		return fmt.Sprintf("_s_fx.Text(%d, %d)", ir.TableKindWstring, ir.TableFixedFieldBytes(f))
	}
	return g.elemShape(f)
}

// elemShape is one element's node (the field's own when it is no array).
func (g *gen) elemShape(f *ir.Field) string {
	size := ir.TableFixedElementBytes(f)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return g.fn("_s_fs_", r.Name) + "()"
		case *ir.Union:
			return g.fn("_s_fs_", r.Name) + "()"
		case *ir.Enum:
			return fmt.Sprintf("_s_fx.Enum(%d, %s)", size, variantIds(r))
		}
	}
	kind := ir.TableFixedLeafKind(f)
	switch f.Type.Kind {
	case ir.TBool:
		return "_s_fx.Bool()"
	case ir.TFloat32, ir.TFloat64:
		if f.HasFloatRange {
			return fmt.Sprintf("_s_fx.Float(%d, %s, %s)", kind, pyFloat(f.FMin), pyFloat(f.FMax))
		}
		return fmt.Sprintf("_s_fx.Float(%d)", kind)
	}
	lo, hi := rawBounds(f)
	if lo == "None" && hi == "None" {
		return fmt.Sprintf("_s_fx.Int(%d, %d)", kind, size)
	}
	return fmt.Sprintf("_s_fx.Int(%d, %d, %s, %s)", kind, size, lo, hi)
}

// rawBounds is an integer field's declared raw bounds: a range (a fixed-point
// one shifted by F), and a bits(N)'s 2^N - 1.
func rawBounds(f *ir.Field) (lo, hi string) {
	lo, hi = "None", "None"
	if l, h, ok := ir.TableRawRange(f); ok {
		lo, hi = l.String(), h.String()
	}
	if f.Type.Kind == ir.TBits {
		top := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		if hi == "None" {
			lo, hi = "0", top.String()
		}
	}
	return lo, hi
}

func variantIds(e *ir.Enum) string {
	var b strings.Builder
	b.WriteString("(")
	for i := range e.Variants {
		fmt.Fprintf(&b, "0x%016x, ", ir.TableWireId(e.VariantWireName(i)))
	}
	b.WriteString(")")
	return b.String()
}

func pyBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

// emitShapes emits a shape function for every struct, table and union of this
// file that a fixed root reaches. Each is cached and built on first call, after
// every module of the unit has loaded, so no module order matters.
func (g *gen) emitShapes(closure map[string]bool) {
	var names []string
	for name := range closure {
		if g.u.DeclFile[name] == g.f.Base {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if st, ok := g.u.Tables[name]; ok {
			g.emitStructShape(st, true)
			continue
		}
		if st, ok := g.u.Structs[name]; ok {
			g.emitStructShape(st, false)
			continue
		}
		if un, ok := g.u.Unions[name]; ok {
			g.emitUnionShape(un)
		}
	}
}

func (g *gen) emitStructShape(st *ir.Struct, table bool) {
	g.pf("@_s_fx.shape\n")
	g.pf("def _s_fs_%s() -> _s_fx.Table:\n", st.Name)
	g.pf("    return _s_fx.Table(%s, %d, (\n", st.Name, ir.TableFixedTypeBytes(st))
	for _, f := range st.Fields {
		g.pf("        _s_fx.Field(%q, 0x%016x, %s%s),\n", f.Name, ir.TableFieldWireId(f), g.shapeOf(f, table), map[bool]string{true: ", True", false: ""}[f.Type.Optional])
	}
	g.pf("    ))\n\n\n")
}

func (g *gen) emitUnionShape(un *ir.Union) {
	g.pf("@_s_fx.shape\n")
	g.pf("def _s_fs_%s() -> _s_fx.Union:\n", un.Name)
	g.pf("    return _s_fx.Union(%d, %d, %s, (\n", ir.TableFixedUnionBytes(un), ir.TableFixedUnionTagBytes(un), un.Name)
	for _, v := range un.Variants {
		id := ir.TableWireId(v.WireName())
		if v.F == nil {
			g.pf("        _s_fx.Arm(0x%016x, None, None),\n", id)
			continue
		}
		g.pf("        _s_fx.Arm(0x%016x, %q, %s),\n", id, v.Name, g.shapeOf(v.F, false))
	}
	g.pf("    ))\n\n\n")
}

// emitFixedRoots lays down, for each fixed root this file declares, its
// _s_fixed_<Name> data: the identity hash and body, and the lineage oldest
// first with the floor.
func (g *gen) emitFixedRoots(lineage map[string][]FixedLineageEntry) {
	for _, st := range ir.TableFixedFormRoots(g.u) {
		if g.u.DeclFile[st.Name] != g.f.Base {
			continue
		}
		own, _ := FixedLineageOf(g.u, st.Name)
		entries, floor := fixedLineageFor(lineage[st.Name], own)
		g.pf("# %s's fixed form: the identity hash and record body, and the lineage oldest\n", st.Name)
		g.pf("# first, this build's own layout last (docs/FIXED-FORM-ALGORITHM.md §5.2).\n")
		g.pf("_s_fixed_%s: _s_t.Final = _s_fx.Root(%q, 0x%016x, %d, %d, (\n", st.Name, st.Name, own.Wire, own.Record-fixedHashBytes, floor)
		for _, e := range entries {
			g.pf("    _s_fx.Entry(0x%016x, %d, %s, %s%s),\n", e.Wire, e.Record-fixedHashBytes, pyBytes(e.Layout), pyBool(e.Retired), writerRanges(st, e.Writer, e.Wire == own.Wire))
		}
		g.pf("), _s_fs_%s, _s_fl_%s)\n\n\n", st.Name, st.Name)
	}
}

// writerRanges is one lineage entry's WRITER ranges by root field wire id:
// for each ranged integer or float field of the reader's root that the writer
// declared ranged at the same kind, the writer's bounds. The build's own
// entry and an entry with no writer lay down nothing: the reader's own bounds
// stand.
func writerRanges(reader, writer *ir.Struct, own bool) string {
	if writer == nil || own {
		return ""
	}
	byID := map[uint64]*ir.Field{}
	for _, f := range writer.Fields {
		if f != nil {
			byID[ir.TableFieldWireId(f)] = f
		}
	}
	var parts []string
	for _, rf := range reader.Fields {
		if rf == nil {
			continue
		}
		wf := byID[ir.TableFieldWireId(rf)]
		if wf == nil {
			continue
		}
		switch {
		case rf.HasIntRange && wf.HasIntRange && ir.TableScalarKind(wf) == ir.TableScalarKind(rf):
			lo, hi, ok := ir.TableRawRange(wf)
			if !ok {
				continue
			}
			parts = append(parts, fmt.Sprintf("0x%016x: (%s, %s)", ir.TableFieldWireId(rf), lo, hi))
		case rf.HasFloatRange && wf.HasFloatRange:
			parts = append(parts, fmt.Sprintf("0x%016x: (%s, %s)", ir.TableFieldWireId(rf), pyFloat(wf.FMin), pyFloat(wf.FMax)))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return ", {" + strings.Join(parts, ", ") + "}"
}
