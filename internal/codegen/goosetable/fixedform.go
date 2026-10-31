// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, for Goose. This file
// is the fixed form's card: it builds the layout (§1), the hash (§5.2), the
// constant body size (§3) and the per-table Goose code that writes and reads
// a record — following docs/PORT-GOOSE.md (THE RUNTIME LAYOUT, THE NUMBER
// MODEL, FLOAT32) and docs/FIXED-FORM-ALGORITHM.md. It registers with the table
// surface through [Register] so it joins every <Base>Table.goose module; the
// shared framing, refusals and word moves are runtime/fixed.goose.
//
// The compile-time walk is ir.TableFixedWalkRoot's, the ONE walk every target
// shares (docs/FIXED-FORM-ALGORITHM.md §1): a port that re-derived it would be
// a second opinion on the wire. The emitted constants are the walk's bytes
// exactly as they ride, so the corpus comparison in fixedform_test.go is a
// comparison of the reference's bytes with the bytes this build writes.
//
// THE PLAN IS CODE (docs/FIXED-FORM-ALGORITHM.md §4.2): Goose has no
// reflection, no offsetof and no function pointers, and its storage is not
// the wire's (a text field's length sits behind its bytes), so the identity
// plan takes §4.2's second permitted shape — the record image read in place,
// then a straight-line scatter — laid down at build time as one function per
// type: <T>'s _<T>FixedRead (the run, every byte of the image landed), its
// _<T>FixedScan (§4.5's refusals, run over the whole batch before the first
// landing), its _<T>FixedClamp (§4.6's bounds pass over the live extent) and,
// on the write side, _<T>FixedCheck (§3.1's contract) and _<T>FixedWrite (the
// live extent onto zero). The lineage plans are
// schema-goose-fixed-form-evolution's.
package goosetable

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedLayoutBytes is the layout exactly as it rides: a u32 entry count and a
// run of seventeen-byte entries, every number little-endian
// (docs/FIXED-FORM-ALGORITHM.md §1; ir.TableFixedLayoutBytes).
func fixedLayoutBytes(entries []ir.TableFixedLayoutEntry) []byte {
	return ir.TableFixedLayoutBytes(entries)
}

// fixedRootFacts is everything a fixed root bakes at build time: its layout
// bytes, the hash over those bytes and the definitions digest
// (docs/FIXED-FORM-ALGORITHM.md §1, §5.2), and its constant body size (§3).
func fixedRootFacts(st *ir.Struct) (layout []byte, hash uint64, body int64) {
	entries := ir.TableFixedWalkRoot(st)
	layout = fixedLayoutBytes(entries)
	hash = ir.TableFixedLayoutHash(layout, st)
	body = ir.TableFixedTypeBytes(st)
	return layout, hash, body
}

// fixedGooseBytes renders bytes as the body of a Goose string literal, one
// \xNN escape per byte, so the layout is baked as data rather than rebuilt at
// run time (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
func fixedGooseBytes(data []byte) string {
	var sb strings.Builder
	for _, b := range data {
		fmt.Fprintf(&sb, "\\x%02x", b)
	}
	return sb.String()
}

// A Writer is the emit sink this card needs: the port-design skeleton's
// *goose.Gen satisfies it (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
type Writer interface {
	Pf(format string, args ...any)
	Declare(name string)
}

// EmitFixedForm is this card's constants for one fixed root: the constant
// body size, the record size, the hash, and the layout bytes and length
// (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT; docs/FIXED-FORM-ALGORITHM.md §1,
// §3, §5.2; docs/SPEC-TABLES.md §3.4).
func EmitFixedForm(w Writer, st *ir.Struct) error {
	layout, hash, body := fixedRootFacts(st)
	w.Pf("// ---- %s, the fixed form, form byte 3 (docs/SPEC-TABLES.md §3.4) ----\n", st.Name)
	w.Pf("let %sFixedBodyBytes: i64 = %d;\n", st.Name, body)
	w.Pf("let %sFixedRecordBytes: i64 = 8 + %sFixedBodyBytes;\n", st.Name, st.Name)
	w.Pf("let %sFixedHash: u64 = 0x%016x;\n", st.Name, hash)
	w.Pf("let %sFixedLayout: const u8[:] = \"%s\";\n", st.Name, fixedGooseBytes(layout))
	w.Pf("let %sFixedLayoutLength: i64 = %d;\n", st.Name, len(layout))
	for _, suffix := range []string{"FixedBodyBytes", "FixedRecordBytes", "FixedHash", "FixedLayout", "FixedLayoutLength"} {
		w.Declare(st.Name + suffix)
	}
	return nil
}

// fixedRootsOf is the fixed roots this module declares, in declaration
// order (docs/FIXED-FORM-ALGORITHM.md §2.2: a form is a whole unit's, and the
// fixed roots are [ir.TableFixedFormRoots]).
func fixedRootsOf(g *goose.Gen) []*ir.Struct {
	roots := ir.TableFixedFormRoots(g.Unit)
	byName := map[string]*ir.Struct{}
	for _, st := range roots {
		byName[st.Name] = st
	}
	var out []*ir.Struct
	for _, t := range g.File.Tables {
		if st := byName[t.Name]; st != nil {
			out = append(out, st)
		}
	}
	return out
}

// fixedClosure is every struct and union a fixed root of the unit reaches BY
// VALUE — a field, an element, an arm — each of which rides inline in the
// record (docs/FIXED-FORM-ALGORITHM.md §3: a nested table or type is C(T)
// inline; a union is its tag then its widest arm).
func fixedClosure(u *ir.Unit) (map[string]*ir.Struct, map[string]*ir.Union) {
	structs, unions := map[string]*ir.Struct{}, map[string]*ir.Union{}
	var visitStruct func(st *ir.Struct)
	var visitField func(f *ir.Field)
	visitField = func(f *ir.Field) {
		if f == nil || f.Type.Kind != ir.TNamed {
			return
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			visitStruct(r)
		case *ir.Union:
			if unions[r.Name] != nil {
				return
			}
			unions[r.Name] = r
			for _, v := range r.Variants {
				visitField(v.F)
			}
		}
	}
	visitStruct = func(st *ir.Struct) {
		if structs[st.Name] != nil {
			return
		}
		structs[st.Name] = st
		for _, f := range st.Fields {
			visitField(f)
		}
	}
	for _, st := range ir.TableFixedFormRoots(u) {
		visitStruct(st)
	}
	return structs, unions
}

// emitFixedFormPart is the [Register]d part: every struct and union of the
// fixed closure this file declares gets its record functions, and every fixed
// root it declares gets its constants and its batch API (docs/PORT-GOOSE.md,
// THE RUNTIME LAYOUT).
func emitFixedFormPart(g *goose.Gen) error {
	structs, unions := fixedClosure(g.Unit)
	if len(structs) == 0 {
		return nil
	}
	e := &fixedEmit{g: g}
	var mine []string
	for _, t := range g.File.Tables {
		if structs[t.Name] != nil {
			mine = append(mine, t.Name)
		}
	}
	for _, un := range g.File.TableUnions {
		if unions[un.Name] != nil {
			mine = append(mine, un.Name)
		}
	}
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if structs[d.Name] != nil && !d.IsTable {
				mine = append(mine, d.Name)
			}
		case *ir.Union:
			if unions[d.Name] != nil && g.Unit.TableUnions[d.Name] == nil {
				mine = append(mine, d.Name)
			}
		}
	}
	if len(mine) > 0 {
		// the record functions name the fixed runtime's types
		// (schema_rt::FixedReport, ...), so the module imports it itself and
		// compiles on its own (make/goose-gates.mk, THE STRICT GATE)
		e.g.Import("fixed")
	}
	for _, name := range mine {
		if st := structs[name]; st != nil {
			if err := e.emitStruct(st); err != nil {
				return err
			}
		} else if err := e.emitUnion(unions[name]); err != nil {
			return err
		}
	}
	roots := fixedRootsOf(g)
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].Name < roots[j].Name })
	for _, st := range roots {
		if err := EmitFixedForm(g, st); err != nil {
			return err
		}
		e.emitRoot(st)
	}
	return nil
}

func init() {
	Register("fixedform", emitFixedFormPart)
}

// fixedEmit writes one module's record functions.
type fixedEmit struct {
	g    *goose.Gen
	loop int // the function's loop variables, j1, j2, ... (never iN: i8 … i64 are Goose types)
	err  error
}

func (e *fixedEmit) p(ind int, format string, args ...any) {
	e.g.Pf(strings.Repeat("    ", ind)+format+"\n", args...)
}

func (e *fixedEmit) loopVar() string {
	e.loop++
	return fmt.Sprintf("j%d", e.loop)
}

// home imports the table module that declares a struct's or a union's record
// functions: the Table module of the file that declares it.
func (e *fixedEmit) home(name string) {
	for _, f := range e.g.Unit.Files {
		hit := false
		for _, t := range f.Tables {
			hit = hit || t.Name == name
		}
		for _, un := range f.TableUnions {
			hit = hit || un.Name == name
		}
		if b, ok := e.g.Unit.DeclFile[name]; ok && b == f.Base {
			hit = true
		}
		if hit {
			e.g.Import(f.Base + "Table")
			return
		}
	}
}

func (e *fixedEmit) structType(st *ir.Struct) string { return e.g.TypeRef(st.Name, st.IsTable) }

func (e *fixedEmit) unionType(un *ir.Union) string {
	return e.g.TypeRef(un.Name, e.g.Unit.TableUnions[un.Name] != nil)
}

// a place is a value's storage: the value slot and, for text, its Length.
type place struct{ val, length string }

func fieldPlace(base string, f *ir.Field) place {
	n := base + "." + ir.GoExportName(f.Name)
	return place{val: n, length: n + "Length"}
}

func (pl place) at(i string) place {
	return place{val: pl.val + "[" + i + "]", length: pl.length + "[" + i + "]"}
}

func presentOf(base string, f *ir.Field) string {
	return base + "." + ir.GoExportName(f.Name+"_present")
}

// elemOf is one element of a field: the field with its array and optional
// wrappers taken off.
func elemOf(f *ir.Field) *ir.Field {
	e := *f
	e.Array = ir.ArrayNone
	e.KeyEnum = ""
	e.Type.Optional = false
	return &e
}

// slotCount is a fixed or enum-keyed array's slot count, every one live
// (docs/FIXED-FORM-ALGORITHM.md §3: `[N]T`, `[Enum]T`).
func slotCount(f *ir.Field) int64 {
	if f.KeyEnum != "" {
		return f.KeyEnumRef.Max
	}
	return f.ArrayBound
}

// payloadBytes is C(f) without the optional's present byte.
func payloadBytes(f *ir.Field) int64 {
	n := ir.TableFixedFieldBytes(f)
	if f.Type.Optional {
		n -= ir.TableFixedPresentBytes
	}
	return n
}

func offAt(base string, c int64) string {
	if c == 0 {
		return base
	}
	return fmt.Sprintf("%s + %d", base, c)
}

func offStep(base, i string, stride int64) string {
	return fmt.Sprintf("%s + %s * %d", base, i, stride)
}

// the element kinds a record carries (docs/FIXED-FORM-ALGORITHM.md §3).
const (
	kBool = iota
	kInt
	kInt128
	kF32
	kF64
	kStr
	kBytes
	kStruct
	kUnion
)

func (e *fixedEmit) kindOf(f *ir.Field) int {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		return kBool
	case ir.TFloat32:
		return kF32
	case ir.TFloat64:
		return kF64
	case ir.TString:
		return kStr
	case ir.TBytes:
		return kBytes
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Struct:
			return kStruct
		case *ir.Union:
			return kUnion
		}
	}
	if ir.TableFixedStorageBytes(t) > 8 {
		return kInt128
	}
	return kInt
}

// storageType is an integer-shaped element's Goose storage (docs/PORT-GOOSE.md,
// THE TYPE MAPPING): the declared width, an enum's or a flags set's own type,
// bits(N) at u32 or u64.
func (e *fixedEmit) storageType(f *ir.Field) string {
	t := f.Type
	switch t.Kind {
	case ir.TNamed:
		return e.g.TypeRef(t.Name, false)
	case ir.TBits:
		if t.Width > 32 {
			return "u64"
		}
		return "u32"
	}
	return goose.IntType(t.Signed, t.Width)
}

// primType is the Goose primitive under storageType: an enum's alias is the
// unsigned integer sized to its max, a flags set's is u64 (goose.go,
// emitEnum and emitFlags).
func (e *fixedEmit) primType(f *ir.Field) string {
	t := f.Type
	if t.Kind == ir.TNamed {
		switch r := t.Ref.(type) {
		case *ir.Enum:
			return goose.IntType(false, ir.StorageBitsFor(r.Max))
		case *ir.Flags:
			return "u64"
		}
	}
	return e.storageType(f)
}

// widensToU64 reports whether a value of the Goose integer type prim reaches
// a u64 with no cast: it is one, or it is a narrower unsigned integer, which
// widens implicitly (the pinned Goose spec, §6.3). A cast there is a redundant-cast
// warning, which the strict gate refuses (make/goose-gates.mk).
func widensToU64(prim string) bool {
	switch prim {
	case "u8", "u16", "u32", "u64":
		return true
	}
	return false
}

// asU64 is expr as the u64 fixed_put takes: bare where it widens implicitly,
// an unchecked cast (the bits as they ride) where it does not.
func asU64(expr, prim string) string {
	if widensToU64(prim) {
		return expr
	}
	return expr + " as! u64"
}

// ---------------------------------------------------------------------------
// THE WRITE (docs/FIXED-FORM-ALGORITHM.md §3, §3.1): each field in declared
// order at its constant size; the live extent onto zero.
// ---------------------------------------------------------------------------

func (e *fixedEmit) writeField(ind int, base string, f *ir.Field) {
	pl := fieldPlace(base, f)
	if f.Type.Optional {
		present := presentOf(base, f)
		e.p(ind, "schema_rt::fixed_put_bool(out, %s);", present)
		e.p(ind, "if %s {", present)
		e.writeValue(ind+1, f, pl)
		e.p(ind, "} else {")
		e.p(ind+1, "schema_rt::fixed_put_zeros(out, %d);", payloadBytes(f))
		e.p(ind, "}")
		return
	}
	e.writeValue(ind, f, pl)
}

func (e *fixedEmit) writeValue(ind int, f *ir.Field, pl place) {
	el := elemOf(f)
	switch {
	case f.KeyEnum != "" || f.Array == ir.ArrayFixed:
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %d {", i, slotCount(f))
		e.writeElem(ind+1, el, pl.at(i))
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	case f.Array == ir.ArrayCounted:
		cnt := pl.val + "Count"
		i := e.loopVar()
		e.p(ind, "schema_rt::fixed_put(out, %s as! u32, 4);", cnt)
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %s {", i, cnt)
		e.writeElem(ind+1, el, pl.at(i))
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
		e.p(ind, "schema_rt::fixed_put_zeros(out, (%d - (%s as i64)) * %d);", f.ArrayBound, cnt, ir.TableFixedElementBytes(f))
	default:
		e.writeElem(ind, el, pl)
	}
}

func (e *fixedEmit) writeElem(ind int, f *ir.Field, pl place) {
	switch e.kindOf(f) {
	case kBool:
		e.p(ind, "schema_rt::fixed_put_bool(out, %s);", pl.val)
	case kInt:
		e.p(ind, "schema_rt::fixed_put(out, %s, %d);", asU64(pl.val, e.primType(f)), ir.TableFixedStorageBytes(f.Type))
	case kInt128:
		e.p(ind, "schema_rt::fixed_put(out, %s.lo, 8);", pl.val)
		e.p(ind, "schema_rt::fixed_put(out, %s.hi, 8);", pl.val)
	case kF32:
		e.p(ind, "schema_rt::fixed_put(out, schema_rt::fixed_f32_bits(%s), 4);", pl.val)
	case kF64:
		e.p(ind, "schema_rt::fixed_put(out, schema_rt::fixed_f64_bits(%s), 8);", pl.val)
	case kStr, kBytes:
		e.p(ind, "schema_rt::fixed_put_text(out, %s, %s, %d);", pl.val, pl.length, f.Type.Size)
	case kStruct:
		e.home(f.Type.Name)
		e.p(ind, "_%sFixedWrite(out, %s);", f.Type.Name, pl.val)
	case kUnion:
		e.home(f.Type.Name)
		e.p(ind, "_%sFixedWrite(out, %s);", f.Type.Name, pl.val)
	}
}

// ---------------------------------------------------------------------------
// THE WRITE CONTRACT (docs/FIXED-FORM-ALGORITHM.md §3.1): a count outside
// 0..Max, a length outside 0..N, a value off its declared min/max, an ordinal
// or a union tag past its set, text breaking its content rule — over the
// LIVE extent only — refuse the batch before a byte is stored.
// ---------------------------------------------------------------------------

func (e *fixedEmit) checkField(ind int, base string, f *ir.Field) {
	pl := fieldPlace(base, f)
	if f.Type.Optional {
		if !elemNeedsCheck(elemOf(f)) && f.Array != ir.ArrayCounted {
			return
		}
		e.p(ind, "if %s {", presentOf(base, f))
		e.checkValue(ind+1, f, pl)
		e.p(ind, "}")
		return
	}
	e.checkValue(ind, f, pl)
}

func (e *fixedEmit) checkValue(ind int, f *ir.Field, pl place) {
	el := elemOf(f)
	switch {
	case f.KeyEnum != "" || f.Array == ir.ArrayFixed:
		if !elemNeedsCheck(el) {
			return
		}
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %d {", i, slotCount(f))
		e.checkElem(ind+1, el, pl.at(i))
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	case f.Array == ir.ArrayCounted:
		cnt := pl.val + "Count"
		e.p(ind, "if %s < 0 || (%s as i64) > %d { return false; }", cnt, cnt, f.ArrayBound)
		if !elemNeedsCheck(el) {
			return
		}
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %s {", i, cnt)
		e.checkElem(ind+1, el, pl.at(i))
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	default:
		e.checkElem(ind, el, pl)
	}
}

func (e *fixedEmit) checkElem(ind int, f *ir.Field, pl place) {
	switch e.kindOf(f) {
	case kStr:
		e.p(ind, "if !schema_rt::fixed_text_ok(%s, %s, %d) { return false; }", pl.val, pl.length, f.Type.Size)
	case kBytes:
		e.p(ind, "if !schema_rt::fixed_len_ok(%s, %d) { return false; }", pl.length, f.Type.Size)
	case kStruct, kUnion:
		if elemNeedsCheck(f) {
			e.home(f.Type.Name)
			e.p(ind, "if !_%sFixedCheck(%s) { return false; }", f.Type.Name, pl.val)
		}
	case kInt:
		for _, b := range scalarBounds(f) {
			e.p(ind, "if %s { return false; }", b.over(pl.val))
		}
	}
}

// ---------------------------------------------------------------------------
// THE SCAN (docs/FIXED-FORM-ALGORITHM.md §4.5, §5.3 "TOTAL OVER THE BATCH,
// TOO"): every count, length and text content rule the run would meet,
// asked of the record's bytes before any destination byte is written. It
// walks what the identity plan's run walks — every slot, an optional's payload
// whatever its flag — and a union's taken arm only (its entries are guarded).
// ---------------------------------------------------------------------------

func (e *fixedEmit) scanField(ind int, f *ir.Field, off string) {
	if f.Type.Optional {
		off += " + 1"
	}
	el := elemOf(f)
	switch {
	case f.KeyEnum != "" || f.Array == ir.ArrayFixed:
		if !elemNeedsScan(el) {
			return
		}
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %d {", i, slotCount(f))
		e.scanElem(ind+1, el, offStep(off, i, ir.TableFixedElementBytes(f)))
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	case f.Array == ir.ArrayCounted:
		e.p(ind, "r = schema_rt::fixed_scan_count(b, %s, %d);", off, f.ArrayBound)
		e.p(ind, "if r != 0 { return r; }")
		if !elemNeedsScan(el) {
			return
		}
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %d {", i, f.ArrayBound)
		e.scanElem(ind+1, el, offStep(off+" + 4", i, ir.TableFixedElementBytes(f)))
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	default:
		e.scanElem(ind, el, off)
	}
}

func (e *fixedEmit) scanElem(ind int, f *ir.Field, off string) {
	flavour := 0
	switch e.kindOf(f) {
	case kStr:
		flavour = 1
	case kBytes:
		flavour = 3
	case kStruct, kUnion:
		if elemNeedsScan(f) {
			e.home(f.Type.Name)
			e.p(ind, "r = _%sFixedScan(b, %s);", f.Type.Name, off)
			e.p(ind, "if r != 0 { return r; }")
		}
		return
	default:
		return
	}
	e.p(ind, "r = schema_rt::fixed_scan_text(b, %s, %d, %d);", off, f.Type.Size, flavour)
	e.p(ind, "if r != 0 { return r; }")
}

// ---------------------------------------------------------------------------
// THE RUN (docs/FIXED-FORM-ALGORITHM.md §4.4, §4.5): every byte of the record
// image lands in place — a bool and a present flag as `byte != 0`, a float's
// bits through the one route, a text field's whole declared span — and a union
// lands its taken arm over an arm-reset value (§4.3: the identity plan's holes
// are the unselected arms, which take their declared defaults).
// ---------------------------------------------------------------------------

func (e *fixedEmit) landField(ind int, base string, f *ir.Field, off string) {
	pl := fieldPlace(base, f)
	if f.Type.Optional {
		e.p(ind, "%s = b[%s] != 0;", presentOf(base, f), off)
		off += " + 1"
	}
	e.landValue(ind, f, pl, off)
}

func (e *fixedEmit) landValue(ind int, f *ir.Field, pl place, off string) {
	el := elemOf(f)
	switch {
	case f.KeyEnum != "" || f.Array == ir.ArrayFixed:
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %d {", i, slotCount(f))
		e.landElem(ind+1, el, pl.at(i), offStep(off, i, ir.TableFixedElementBytes(f)))
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	case f.Array == ir.ArrayCounted:
		e.p(ind, "%sCount = schema_rt::fixed_i32(b, %s);", pl.val, off)
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %d {", i, f.ArrayBound)
		e.landElem(ind+1, el, pl.at(i), offStep(off+" + 4", i, ir.TableFixedElementBytes(f)))
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	default:
		e.landElem(ind, el, pl, off)
	}
}

func (e *fixedEmit) landElem(ind int, f *ir.Field, pl place, off string) {
	switch e.kindOf(f) {
	case kBool:
		e.p(ind, "%s = b[%s] != 0;", pl.val, off)
	case kInt:
		if e.primType(f) == "u64" {
			// fixed_get is a u64 already: a u64 (or a flags set's u64 alias) takes it bare
			e.p(ind, "%s = schema_rt::fixed_get(b, %s, %d);", pl.val, off, ir.TableFixedStorageBytes(f.Type))
		} else {
			e.p(ind, "%s = schema_rt::fixed_get(b, %s, %d) as! %s;", pl.val, off, ir.TableFixedStorageBytes(f.Type), e.storageType(f))
		}
	case kInt128:
		e.p(ind, "%s.lo = schema_rt::fixed_u64(b, %s);", pl.val, off)
		e.p(ind, "%s.hi = schema_rt::fixed_u64(b, %s + 8);", pl.val, off)
	case kF32:
		e.p(ind, "%s = schema_rt::fixed_bits_f32(schema_rt::fixed_u32(b, %s));", pl.val, off)
	case kF64:
		e.p(ind, "%s = schema_rt::fixed_bits_f64(schema_rt::fixed_u64(b, %s));", pl.val, off)
	case kStr, kBytes:
		e.p(ind, "%s = schema_rt::fixed_i32(b, %s);", pl.length, off)
		e.p(ind, "schema_rt::fixed_get_text(b, %s + 4, %s);", off, pl.val)
	case kStruct, kUnion:
		e.home(f.Type.Name)
		e.p(ind, "_%sFixedRead(b, %s, %s);", f.Type.Name, off, pl.val)
	}
}

// ---------------------------------------------------------------------------
// THE BOUNDS PASS (docs/FIXED-FORM-ALGORITHM.md §4.6): straight-line code
// after the run, over storage, over what a read can have written — a counted
// array's live elements, an optional's payload under its flag, a union's
// taken arm. A ranged scalar clamps to its declared min and max (a fixed
// field's in raw units), a bits(N) to 2^N-1; an enum ordinal or a union tag
// past its set lands None; each counts clamped once.
// ---------------------------------------------------------------------------

func (e *fixedEmit) clampField(ind int, base string, f *ir.Field) {
	el := elemOf(f)
	if !elemNeedsClamp(el) {
		return
	}
	pl := fieldPlace(base, f)
	if f.Type.Optional {
		e.p(ind, "if %s {", presentOf(base, f))
		e.clampValue(ind+1, f, pl)
		e.p(ind, "}")
		return
	}
	e.clampValue(ind, f, pl)
}

func (e *fixedEmit) clampValue(ind int, f *ir.Field, pl place) {
	el := elemOf(f)
	switch {
	case f.KeyEnum != "" || f.Array == ir.ArrayFixed:
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %d {", i, slotCount(f))
		e.clampElem(ind+1, el, pl.at(i))
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	case f.Array == ir.ArrayCounted:
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %sCount {", i, pl.val)
		e.clampElem(ind+1, el, pl.at(i))
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	default:
		e.clampElem(ind, el, pl)
	}
}

func (e *fixedEmit) clampElem(ind int, f *ir.Field, pl place) {
	switch e.kindOf(f) {
	case kStruct, kUnion:
		if elemNeedsClamp(f) {
			e.home(f.Type.Name)
			e.p(ind, "c += _%sFixedClamp(%s);", f.Type.Name, pl.val)
		}
	case kInt:
		for _, b := range scalarBounds(f) {
			e.p(ind, "if %s { %s = %s; c += 1; }", b.over(pl.val), pl.val, b.land)
		}
	}
}

// a scalarBound is one declared bound of an integer-shaped value: the test
// that it is broken, and the value a read lands when it is.
type scalarBound struct {
	test func(v string) string
	land string
}

func (b scalarBound) over(v string) string { return b.test(v) }

// scalarBounds is an integer-shaped element's bounds (docs/FIXED-FORM-ALGORITHM.md
// §4.6): an enum's top ordinal (None past it), a declared range in raw units
// (ir.TableRawRange, a fixed field's shifted by F), a bits(N) below its
// storage. A 128-bit range is lane arithmetic the 128-bit kinds' card owns.
func scalarBounds(f *ir.Field) []scalarBound {
	t := f.Type
	if t.Kind == ir.TNamed {
		if r, ok := t.Ref.(*ir.Enum); ok {
			return []scalarBound{{test: func(v string) string { return fmt.Sprintf("(%s as u64) > %d", v, r.Max) }, land: "0"}}
		}
		return nil
	}
	if ir.TableFixedStorageBytes(t) > 8 {
		return nil
	}
	var out []scalarBound
	if lo, hi, ok := ir.TableRawRange(f); ok {
		signed := t.Signed && t.Kind != ir.TBits
		if signed || lo.Sign() > 0 {
			l := goose.IntLit(lo)
			out = append(out, scalarBound{test: func(v string) string { return fmt.Sprintf("%s < %s", v, l) }, land: l})
		}
		h := goose.IntLit(hi)
		out = append(out, scalarBound{test: func(v string) string { return fmt.Sprintf("%s > %s", v, h) }, land: h})
		return out
	}
	if t.Kind == ir.TBits && int64(t.Width) < 8*ir.TableFixedStorageBytes(t) {
		m := goose.IntLit(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.Width)), big.NewInt(1)))
		out = append(out, scalarBound{test: func(v string) string { return fmt.Sprintf("%s > %s", v, m) }, land: m})
	}
	return out
}

// elemNeedsClamp / elemNeedsCheck / elemNeedsScan answer whether a pass has
// anything to do for one element, so a type that bounds nothing emits no
// loop for it (docs/FIXED-FORM-ALGORITHM.md §4.6: "A type that bounds nothing
// emits no pass at all").
func elemNeedsClamp(f *ir.Field) bool {
	return needs(f, map[string]bool{}, func(f *ir.Field) bool { return len(scalarBounds(f)) > 0 }, true)
}

func elemNeedsCheck(f *ir.Field) bool {
	return needs(f, map[string]bool{}, func(f *ir.Field) bool {
		switch f.Type.Kind {
		case ir.TString, ir.TBytes:
			return true
		}
		return len(scalarBounds(f)) > 0
	}, true)
}

func elemNeedsScan(f *ir.Field) bool {
	return needs(f, map[string]bool{}, func(f *ir.Field) bool {
		switch f.Type.Kind {
		case ir.TString, ir.TBytes:
			return true
		}
		return false
	}, false)
}

// needs walks an element through structs and union arms: leaf answers a
// scalar or text element; a counted array always needs its count asked;
// a union's tag is a bound of its own when unionTag says so.
func needs(f *ir.Field, seen map[string]bool, leaf func(*ir.Field) bool, unionTag bool) bool {
	if f.Array == ir.ArrayCounted {
		return true
	}
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if seen[r.Name] {
				return false
			}
			seen[r.Name] = true
			for _, sf := range r.Fields {
				if needs(sf, seen, leaf, unionTag) || needs(elemOf(sf), seen, leaf, unionTag) {
					return true
				}
			}
			return false
		case *ir.Union:
			if unionTag {
				return true
			}
			if seen[r.Name] {
				return false
			}
			seen[r.Name] = true
			for _, v := range r.Variants {
				if v.F != nil && (needs(v.F, seen, leaf, unionTag) || needs(elemOf(v.F), seen, leaf, unionTag)) {
					return true
				}
			}
			return false
		}
	}
	return leaf(f)
}

// ---------------------------------------------------------------------------
// THE PER-TYPE FUNCTIONS
// ---------------------------------------------------------------------------

func (e *fixedEmit) declare(names ...string) {
	for _, n := range names {
		e.g.Declare(n)
	}
}

// emitStruct writes a struct's five record functions
// (docs/FIXED-FORM-ALGORITHM.md §3.1, §4.4, §4.5, §4.6): fields in declared
// order, each at its constant offset.
func (e *fixedEmit) emitStruct(st *ir.Struct) error {
	for _, f := range st.Fields {
		if f.Guard != "" {
			return fmt.Errorf("goose fixed form: %s.%s is under a guarded branch, which the fixed form refuses (docs/SPEC-TABLES.md §3.4)", st.Name, f.Name)
		}
	}
	n := st.Name
	typ := e.structType(st)
	e.declare("_"+n+"FixedWrite", "_"+n+"FixedCheck", "_"+n+"FixedScan", "_"+n+"FixedRead", "_"+n+"FixedClamp")
	e.p(0, "// ---- %s's record image, C = %d bytes (docs/FIXED-FORM-ALGORITHM.md §3) ----", n, ir.TableFixedTypeBytes(st))

	e.loop = 0
	e.p(0, "// _%sFixedWrite appends %s's body: each field in declared order at its constant size (§3.1).", n, n)
	e.p(0, "fn _%sFixedWrite(out: u8[>..]&, v: const %s&) {", n, typ)
	for _, f := range st.Fields {
		e.writeField(1, "v", f)
	}
	e.p(0, "}\n")

	e.loop = 0
	e.p(0, "// _%sFixedCheck is %s's write contract over its live extent (§3.1); false is invalid_value.", n, n)
	e.p(0, "fn _%sFixedCheck(v: const %s&) -> bool {", n, typ)
	for _, f := range st.Fields {
		e.checkField(1, "v", f)
	}
	e.p(1, "return true;")
	e.p(0, "}\n")

	e.loop = 0
	e.p(0, "// _%sFixedScan asks a record's bytes at `at` every refusal the run would meet (§4.5); 0 is none.", n)
	e.p(0, "fn _%sFixedScan(b: const u8[:], at: i64) -> i64 {", n)
	e.p(1, "var r: i64 = 0;")
	var off int64
	for _, f := range st.Fields {
		e.scanField(1, f, offAt("at", off))
		off += ir.TableFixedFieldBytes(f)
	}
	e.p(1, "return r;")
	e.p(0, "}\n")

	e.loop = 0
	e.p(0, "// _%sFixedRead lands %s's record image at `at` into v: the identity plan's run (§4.4).", n, n)
	e.p(0, "fn _%sFixedRead(b: const u8[:], at: i64, v: %s&) {", n, typ)
	off = 0
	for _, f := range st.Fields {
		e.landField(1, "v", f, offAt("at", off))
		off += ir.TableFixedFieldBytes(f)
	}
	e.p(0, "}\n")

	e.loop = 0
	e.p(0, "// _%sFixedClamp is %s's bounds pass over what a read wrote (§4.6); it returns the clamps.", n, n)
	e.p(0, "fn _%sFixedClamp(v: %s&) -> i64 {", n, typ)
	e.p(1, "var c: i64 = 0;")
	for _, f := range st.Fields {
		e.clampField(1, "v", f)
	}
	e.p(1, "return c;")
	e.p(0, "}\n")
	return e.err
}

// armField is a union arm as the field line it is (docs/SPEC-TABLES.md §2.6),
// stored under the arm's own name.
func armField(v ir.UnionVariant) *ir.Field {
	f := *v.F
	f.Name = v.Name
	return &f
}

// emitUnion writes a union's five record functions: the tag at its width, then
// the widest arm's span, the taken arm's bytes onto zero
// (docs/FIXED-FORM-ALGORITHM.md §3, a UNION; §4.6 for the tag past the set).
func (e *fixedEmit) emitUnion(un *ir.Union) error {
	n := un.Name
	typ := e.unionType(un)
	tw := ir.TableFixedUnionTagBytes(un)
	widest := ir.TableFixedUnionBytes(un) - tw
	e.declare("_"+n+"FixedWrite", "_"+n+"FixedCheck", "_"+n+"FixedScan", "_"+n+"FixedRead", "_"+n+"FixedClamp")
	e.p(0, "// ---- union %s's record image: a %d-byte tag, then %d bytes of arm (docs/FIXED-FORM-ALGORITHM.md §3) ----", n, tw, widest)
	arms := func(fn func(ind int, tag int, v ir.UnionVariant)) {
		first := true
		for i, v := range un.Variants {
			if first {
				e.p(1, "if v.Type == %d {", i+1)
			} else {
				e.p(1, "} else if v.Type == %d {", i+1)
			}
			first = false
			fn(2, i+1, v)
		}
	}

	e.loop = 0
	e.p(0, "fn _%sFixedWrite(out: u8[>..]&, v: const %s&) {", n, typ)
	e.p(1, "schema_rt::fixed_put(out, v.Type, %d);", tw) // a tag is unsigned: it widens to u64
	arms(func(ind, tag int, v ir.UnionVariant) {
		if v.F == nil {
			e.p(ind, "schema_rt::fixed_put_zeros(out, %d);", widest)
			return
		}
		f := armField(v)
		e.writeField(ind, "v", f)
		if pad := widest - ir.TableFixedFieldBytes(f); pad > 0 {
			e.p(ind, "schema_rt::fixed_put_zeros(out, %d);", pad)
		}
	})
	e.p(1, "} else {")
	e.p(2, "schema_rt::fixed_put_zeros(out, %d);", widest)
	e.p(1, "}")
	e.p(0, "}\n")

	e.loop = 0
	e.p(0, "fn _%sFixedCheck(v: const %s&) -> bool {", n, typ)
	e.p(1, "if (v.Type as u64) > %d { return false; }", un.Max)
	arms(func(ind, tag int, v ir.UnionVariant) {
		if v.F != nil {
			e.checkField(ind, "v", armField(v))
		}
	})
	e.p(1, "}")
	e.p(1, "return true;")
	e.p(0, "}\n")

	e.loop = 0
	e.p(0, "fn _%sFixedScan(b: const u8[:], at: i64) -> i64 {", n)
	e.p(1, "var r: i64 = 0;")
	e.p(1, "let tag = schema_rt::fixed_get(b, at, %d);", tw)
	for i, v := range un.Variants {
		if v.F == nil || !elemNeedsScan(armField(v)) && armField(v).Array != ir.ArrayCounted {
			continue
		}
		e.p(1, "if tag == %d {", i+1)
		e.scanField(2, armField(v), offAt("at", tw))
		e.p(1, "}")
	}
	e.p(1, "return r;")
	e.p(0, "}\n")

	e.loop = 0
	e.p(0, "fn _%sFixedRead(b: const u8[:], at: i64, v: %s&) {", n, typ)
	e.p(1, "v = %s { .. };", typ)
	e.p(1, "v.Type = schema_rt::fixed_get(b, at, %d) as! %sType;", tw, n)
	for i, v := range un.Variants {
		if v.F == nil {
			continue
		}
		e.p(1, "if v.Type == %d {", i+1)
		e.landField(2, "v", armField(v), offAt("at", tw))
		e.p(1, "}")
	}
	e.p(0, "}\n")

	e.loop = 0
	e.p(0, "fn _%sFixedClamp(v: %s&) -> i64 {", n, typ)
	e.p(1, "if (v.Type as u64) > %d {", un.Max)
	e.p(2, "v.Type = 0;")
	e.p(2, "return 1;")
	e.p(1, "}")
	e.p(1, "var c: i64 = 0;")
	for i, v := range un.Variants {
		if v.F == nil || !elemNeedsClamp(elemOf(armField(v))) {
			continue
		}
		e.p(1, "if v.Type == %d {", i+1)
		e.clampField(2, "v", armField(v))
		e.p(1, "}")
	}
	e.p(1, "return c;")
	e.p(0, "}\n")
	return e.err
}

// emitRoot writes a fixed root's batch API (docs/FIXED-FORM-ALGORITHM.md §2,
// §3.1, §5.3; docs/SPEC-TABLES.md §3.4, §6.5): the measure, the save (the
// whole batch held to the contract before the first byte), the tolerant load
// (the framing, the scan of every record, then the run and the bounds pass)
// and the strict load.
func (e *fixedEmit) emitRoot(st *ir.Struct) {
	n := st.Name
	typ := e.structType(st)
	e.declare(n+"FixedMeasure", n+"FixedSave", n+"FixedLoadTolerant", n+"FixedLoad")
	e.p(0, "// %sFixedMeasure is a file of count records: the header, the layout, the records (§2).", n)
	e.p(0, "fn %sFixedMeasure(count: i64) -> i64 {", n)
	e.p(1, "return schema_rt::FixedHeaderBytes + 4 + %sFixedLayoutLength + count * %sFixedRecordBytes;", n, n)
	e.p(0, "}\n")

	e.p(0, "// %sFixedSave appends a fixed-form file of values to out and returns its byte length, or -1", n)
	e.p(0, "// (invalid_value) when any value breaks its declared contract, with nothing appended (§3.1).")
	e.p(0, "fn %sFixedSave(values: const %s[:], out: u8[>..]&) -> i64 {", n, typ)
	e.p(1, "var k: i64 = 0;")
	e.p(1, "while k < values.len {")
	e.p(2, "if !_%sFixedCheck(values[k]) { return -1; }", n)
	e.p(2, "k += 1;")
	e.p(1, "}")
	e.p(1, "let start = out.len;")
	e.p(1, "schema_rt::fixed_write_header(out, %sFixedLayout, %sFixedHash);", n, n)
	e.p(1, "k = 0;")
	e.p(1, "while k < values.len {")
	e.p(2, "schema_rt::fixed_put(out, %sFixedHash, 8);", n)
	e.p(2, "_%sFixedWrite(out, values[k]);", n)
	e.p(2, "k += 1;")
	e.p(1, "}")
	e.p(1, "return out.len - start;")
	e.p(0, "}\n")

	e.p(0, "// %sFixedLoadTolerant reads a fixed-form file into values, whose length is the caller's", n)
	e.p(0, "// capacity, and returns the record count, or -1 with report.reason naming the refusal;")
	e.p(0, "// a refusal lands nothing (§5.3, TOTAL OVER THE BATCH). Clamps are counted onto report.")
	e.p(0, "fn %sFixedLoadTolerant(values: %s[:], data: const u8[:], report: schema_rt::FixedReport&) -> i64 {", n, typ)
	e.p(1, "report = schema_rt::FixedReport { .. };")
	e.p(1, "let n = schema_rt::fixed_load_frame(data, %sFixedLayout, %sFixedHash, %sFixedRecordBytes, values.len, report);", n, n, n)
	e.p(1, "if n < 0 { return -1; }")
	e.p(1, "let first = schema_rt::FixedHeaderBytes + 4 + %sFixedLayoutLength + 8;", n)
	e.p(1, "var k: i64 = 0;")
	e.p(1, "while k < n {")
	e.p(2, "let r = _%sFixedScan(data, first + k * %sFixedRecordBytes);", n, n)
	e.p(2, "if r != 0 { return schema_rt::fixed_refuse(report, r, false); }")
	e.p(2, "k += 1;")
	e.p(1, "}")
	e.p(1, "k = 0;")
	e.p(1, "while k < n {")
	e.p(2, "_%sFixedRead(data, first + k * %sFixedRecordBytes, values[k]);", n, n)
	e.p(2, "report.clamped += _%sFixedClamp(values[k]);", n)
	e.p(2, "k += 1;")
	e.p(1, "}")
	e.p(1, "return n;")
	e.p(0, "}\n")

	e.p(0, "// %sFixedLoad is the STRICT read (docs/SPEC-TABLES.md §6.5): every refusal of", n)
	e.p(0, "// %sFixedLoadTolerant, and a read that clamped, is -1 with every value it wrote reset.", n)
	e.p(0, "fn %sFixedLoad(values: %s[:], data: const u8[:], report: schema_rt::FixedReport&) -> i64 {", n, typ)
	e.p(1, "let n = %sFixedLoadTolerant(values, data, report);", n)
	e.p(1, "if n < 0 { return -1; }")
	e.p(1, "if report.clamped > 0 {")
	e.p(2, "var k: i64 = 0;")
	e.p(2, "while k < n {")
	e.p(3, "values[k] = %s { .. };", typ)
	e.p(3, "k += 1;")
	e.p(2, "}")
	e.p(2, "report.reason = schema_rt::FixedRefuseClamped;")
	e.p(2, "return -1;")
	e.p(1, "}")
	e.p(1, "return n;")
	e.p(0, "}\n")
}
