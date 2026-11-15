package gotable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The read emitters' canonical-spelling checks (docs/SPEC-TABLES.md §3, §3.1,
// §3.2, §3.3; docs/rulings.json reads-refuse-non-canonical) live here, so the
// core files carry one call each: a field stored at the default its writer
// elides (g.elides), an element riding as its default where its array or
// slot elides whole (g.sink), keyed slots in variant order, and the id
// table's first-use order (tableFirstUseSource).

// pointerNullCheck is a pointer read's check of its node index: an explicit
// null at a field is refused (null elides, §3.1); a null element of an [N]
// array that elides whole leaves its sink set; any other null is lawful.
func pointerNullCheck(f *ir.Field, elide bool, sink, onBad string) string {
	switch {
	case sink != "":
		return "if index!=0 {" + sinkOff(sink) + "};"
	case elide && f.Array == ir.ArrayNone:
		return fmt.Sprintf("if index==0 {%s};", onBad)
	}
	return ""
}

// enumNoneRead is an enum read's statement for variant reference 0, None: a
// field whose default is None elides there, so it is refused; an element
// whose default is not None is not at it.
func enumNoneRead(f *ir.Field, expr string, elide bool, sink, onBad string) string {
	none := f.Type.Name + "None"
	switch {
	case elide && fieldDefaultExpr(f) == none:
		return onBad
	case fieldDefaultExpr(f) != none:
		return expr + " = " + none + ";" + sinkOff(sink)
	}
	return expr + " = " + none
}

// enumVariantDefault is the tail of an enum read's known-variant chain: a
// field naming its default's variant is refused, and an element naming any
// other variant is not at its default (§2.15: the default is read off the
// variant the wire names, not off the value an unknown variant lands).
func enumVariantDefault(f *ir.Field, expr string, elide bool, sink, onBad string) string {
	def := fieldDefaultExpr(f)
	switch {
	case elide && def != f.Type.Name+"None":
		return fmt.Sprintf(" else if %s == %s { %s }", expr, def, onBad)
	case sink != "":
		return fmt.Sprintf(" else if %s != %s { %s }", expr, def, sinkOff(sink))
	}
	return ""
}

// keyedSlotOpen holds a known keyed slot to ascending variant order and, for
// an element that is not a table, opens its sink (§3.2): the writer writes
// slots by ordinal and elides one at its default.
func keyedSlotOpen(table bool, ind, stop string) string {
	open := fmt.Sprintf("%sif int(slot) <= lastSlot { %s }; lastSlot = int(slot)\n", ind, stop)
	if !table {
		open += ind + "slotDefault := true\n"
	}
	return open
}

// emitKeyedScalarSlot reads a keyed slot's scalar, enum or pointer element and
// refuses it at its default.
func (g *tableGen) emitKeyedScalarSlot(f *ir.Field, expr, ind, stop string) {
	if f.Type.Pointer || enumRef(f) != nil {
		g.sink = "slotDefault"
	}
	g.emitReadScalar(f, expr, "elem", "elemKind", ind, stop)
	if !f.Type.Pointer && enumRef(f) == nil {
		g.pf("%sif %s { slotDefault = false }\n", ind, g.elideDiffers(f, expr, fieldDefaultExpr(f)))
	}
	g.pf("%sif slotDefault { %s }\n", ind, stop)
}

// emitFixedArraySinkOpen opens an [N] array's sink (fixedArraySink) before its
// element loop and names it, "" for none; emitFixedArraySinkClose refuses the
// array whose every element rode as its default.
func (g *tableGen) emitFixedArraySinkOpen(f *ir.Field, elide bool, ind string) string {
	sink := fixedArraySink(f, elide)
	if sink != "" {
		g.pf("%s%s := true\n", ind, sink)
	}
	return sink
}

func (g *tableGen) emitFixedArraySinkClose(sink, ind, stop string) {
	if sink != "" {
		g.pf("%sif %s { %s }\n", ind, sink, stop)
	}
}

// tableFirstUseReaderSource is the reader's half of the first-use count: a
// reference it resolves, or one the wire check walks, is met on the load's
// report.
const tableFirstUseReaderSource = `
func (r *TableReader) meetFirstUse(ref uint64) {
	if r.Report != nil { tableFirstUseMeet(r.Report, ref) }
}
`

// takeElides returns whether the field read being emitted is one its writer
// elides at its default (g.elides), and clears it, so the first emitter that
// reads the field's wire takes it and no element or nested read sees it.
func (g *tableGen) takeElides() bool {
	elide := g.elides
	g.elides = false
	return elide
}

// takeSink returns the flag the element read being emitted clears when the
// element on the wire is not its default spelling (g.sink), and clears it: an
// [N] array of pointers, enums or unions every element of which rode as its
// explicit default, and a keyed slot at its default, are spellings no writer
// produces (docs/SPEC-TABLES.md §3, §3.2), and only the wire says so, since an
// unknown target, variant or arm lands the default but did not ride as it.
func (g *tableGen) takeSink() string {
	sink := g.sink
	g.sink = ""
	return sink
}

// sinkOff clears a sink in emitted code, "" where there is none.
func sinkOff(sink string) string {
	if sink == "" {
		return ""
	}
	return sink + "=false;"
}

// quantizedMessageFloat is a float32 the message form carries as a step index
// (ir.TableMessageQuantized): its writer elides on the HELD value, so a step
// equal to the default's is what a held neighbour of the default writes too,
// and the value read cannot say the field was elided (docs/SPEC-TABLES.md §3.3).
func quantizedMessageFloat(f *ir.Field) bool {
	return f.Type.Kind == ir.TFloat32 && f.HasFloatRange
}

// emitElidedRefusal refuses a field read whose value is the one its writer
// elides (docs/SPEC-TABLES.md §3, §4), for every field shape whose stored value
// says so exactly: the writer's own ride test (emitFieldRides) on the value
// read. The shapes whose stored value cannot say it — an enum (an unknown or a
// deprecated variant lands None or the default), a pointer (a record of a type
// this reader cannot name lands null), a union (an unknown arm lands None), a
// nested table, a keyed array, a list, a map, unbounded text and an array of
// enums, unions or pointers — are refused where their emitter reads the wire
// (g.elides, g.sink).
func (g *tableGen) emitElidedRefusal(f *ir.Field, expr, ind string) {
	g.emitElidedRefusalWhen(f, expr, ind, "")
}

// emitElidedRefusalWhen is emitElidedRefusal under a condition the read
// decides at run time, the message form's same build version; "" for none,
// which is the variable form. A message read holds no quantized float to it.
func (g *tableGen) emitElidedRefusalWhen(f *ir.Field, expr, ind, when string) {
	switch {
	case f.Required || f.Type.Optional, f.IsList(), f.IsMap(), f.KeyEnum != "", f.Type.TextUnbounded():
		return
	case when != "" && quantizedMessageFloat(f):
		return
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		return
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
	case f.Array == ir.ArrayFixed:
		if enumRef(f) != nil || isUnionRef(f.Type) || isStructRef(f.Type) || f.Type.Pointer {
			return
		}
	case f.Array == ir.ArrayCounted:
	case enumRef(f) != nil || isUnionRef(f.Type) || isStructRef(f.Type):
		return
	}
	rides := g.emitFieldRides(f, expr, ind)
	g.pf("%sif %s!(%s) { %s; return false }\n", ind, when, rides, damaged)
}

// fixedArraySink is the [N] field's sink: an array of pointers, enums or unions
// whose writer elides it when every element is the default, "" for any other.
func fixedArraySink(f *ir.Field, elide bool) string {
	if elide && f.Array == ir.ArrayFixed && (f.Type.Pointer || enumRef(f) != nil || isUnionRef(f.Type)) {
		return "allDefault"
	}
	return ""
}

// tableFirstUseSource holds the id table to first-use order (docs/SPEC-TABLES.md
// §3): an entry no reference names, and one out of its place, are spellings no
// conforming writer produces. The load's own walk meets every reference as it
// decodes it (TableReader.Resolve, and the wire check where it walks a payload
// this reader cannot name): first each node record in the table's order, then
// the root body, whose fields come before the node table on the wire. Each part
// is met in byte order, so each keeps two numbers, the highest reference met
// and where the last jump past it landed, and the verdict is: the root meets
// 1..m in order, the records meet nothing new or m+1 onward in order, and every
// entry is met. Work is constant per reference and nothing is allocated. The
// count rides on the report, which every reader of one load shares.
const tableFirstUseSource = `
type tableFirstUse struct {
	usePhase uint8 // 0 not counting, 1 the node records, 2 the root body
	useNodes, useRoot tableFirstUseRun
}

type tableFirstUseRun struct { useHigh, useJump uint64 }

func (u *tableFirstUseRun) useMeet(ref uint64) {
	if ref > u.useHigh { if ref != u.useHigh+1 { u.useJump = ref }; u.useHigh = ref }
}

// tableFirstUseCount starts counting one part of a load's walk: 1 the node
// records, which starts the count afresh, and 2 the root body; 0 stops it.
func tableFirstUseCount(r *TableReport, phase uint8) {
	if phase != 2 { r.firstUse = tableFirstUse{} }
	r.firstUse.usePhase = phase
}

func tableFirstUseMeet(r *TableReport, ref uint64) {
	switch r.firstUse.usePhase {
	case 1: r.firstUse.useNodes.useMeet(ref)
	case 2: r.firstUse.useRoot.useMeet(ref)
	}
}

// tableFirstUseWhole answers the count's verdict over an id table of n
// entries, and stops it.
func tableFirstUseWhole(r *TableReport, n uint64) bool {
	u := r.firstUse
	r.firstUse = tableFirstUse{}
	root, nodes := u.useRoot, u.useNodes
	return root.useJump <= 1 && (nodes.useHigh <= root.useHigh || nodes.useJump <= root.useHigh+1) && max(root.useHigh, nodes.useHigh) == n
}
`
