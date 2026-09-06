// The FLAT tier: the second emission surface of the js backend — pure
// generated JavaScript with a single-word 32-bit bitpacker inlined at every
// field (byte-identical wire to serialize.js's packer), compile-time-constant
// widths and masks, zero function calls. THE JavaScript path under the ruling ("whichever correct
// implementation is fastest is the one we use for JavaScript"): the
// flattened-JS probe measured it 8-10x the runtime-call tier and within
// 3.2-5.1x of native C (era flatjs-probe, M2 Air, node 26.7).
// The runtime tier (Base.js + serialize.js) remains the compat/debug/
// reference surface and the CI oracle this tier is held byte-identical to.
//
// Per schema file Base.schema, BaseFlat.js is emitted beside Base.js
// whenever the file declares types. Per struct Name:
//
//	Write<Name>Flat(value, view)          -> bytes written | -1 (checked refusal)
//	Read<Name>Flat(value, view, numBits)  -> bool
//
// Check model (the design's §4, the family's JavaScript #ifdef):
//   - The READ side is never configurable. Reader obligations are format:
//     per-run fused bounds checks, headroom rejects wherever the range does
//     not fill the width, const/reserved verification, interior-null refusal
//     on strings, align padding verified zero, untaken branch sides zeroed
//     inline — C's release read semantics, emitted once.
//   - The WRITE side forks at module load exactly as serialize.js src/mode.js
//     does: NODE_ENV read once, whole variants selected at export. The
//     production writer is the trusted-writer release shape (zero caller
//     validation; width masks stay — wire arithmetic). The checked writer
//     carries the runtime tier's fold-guard set verbatim in meaning and
//     refuses out-of-contract values with -1, writing byte-identical wire
//     for every in-contract value.
//
// Allocation contract (the C++ buffer stance):
//   - Write: the buffer behind view must hold at least <Name>MaxBytes
//     (already rounded to the 8-byte write-buffer granularity — the final
//     whole-word flush is covered exactly). Batch: the packets' total bytes
//     plus <Name>MaxBytes for the packet being written.
//   - Read: the buffer must extend at least FLAT_READ_SLACK = 8 bytes past
//     the payload — 64-bit windows load unconditionally. Exactly-sized
//     receive buffers copy into a persistent MaxBytes + 8 scratch first.
//
// Wire identity is not asserted here — it is proven by the golden legs: the
// flat tier writes and reads every pinned corpus instance against the same
// C++-pinned testdata/wire goldens the six languages are held to, plus a
// standing cross-tier equivalence gate against the runtime tier (bytes,
// fields AND verdicts). The write merge stages one 32-bit word (mergeW); the
// read side keeps the probe's 64-bit-window kernels; the wide-op lane orders
// (low dword first; 32-bit groups least significant first) are serialize.js
// src/streams.js's own.
package js

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fgen emits one BaseFlat.js module.
type fgen struct {
	unit *ir.Unit
	file *ir.File
	home bool // the unit's first flat module — carries the tier-level notes

	body    strings.Builder
	imports map[string]map[string]bool

	// per-function emission state
	fn        strings.Builder
	checked   bool // write side: emit the fold-guard set
	needX     bool // float32 write temp
	needN     bool // compressed-float / narrowing temp
	needT     bool // float32 NaN lane temps
	needBg    bool // BigInt lane temp
	needNw    bool // read side: the wide-offset Number temp
	loopDepth int

	// the write-side chunk accumulator: adjacent constant-width fields whose
	// value expressions are pure (field loads, literals — never a mutable
	// temp) pack into ONE staged word with literal relative shifts, so a
	// chunk costs one merge where its fields used to cost one each. Relative
	// offsets inside a chunk are static even where the absolute cursor is
	// dynamic (loop bodies), which is what makes this reach the hot arrays.
	chunk     []chunkPiece
	chunkBits int64

	// the read-side window, the write chunker's mirror: one 64-bit pair load
	// carries 32 valid bits from the cursor, so consecutive fields whose
	// widths sum to 32 or less all extract from the SAME `out` at literal
	// relative shifts — static even where the absolute cursor is dynamic,
	// which is what reaches the loop bodies. rwUsed is the cursor's pending
	// advance: while a window is open `br` LAGS the true position by it, so
	// every emission that names br, and every scope boundary, closes first
	// (pf does this by inspection — see closesReadWindow).
	rwOpen bool
	rwUsed int64
	rwInd  string
}

// chunkPiece is one field's contribution to a pending write chunk: a pure,
// parenthesized JS number expression already masked to bits.
type chunkPiece struct {
	expr string
	bits int64
}

// chunkAdd registers a pure value expression of the given width, flushing
// first when it cannot join the pending chunk. Every caller's expression
// must stay valid until the flush point: field paths, hoisted element refs
// and literals qualify; v/n/x/bg-style temps never do (a temp-based merge
// flushes around itself instead).
func (g *fgen) chunkAdd(expr string, bits int64, ind string) {
	if bits == 0 {
		return
	}
	if g.chunkBits+bits > 32 {
		g.chunkFlush(ind)
	}
	g.chunk = append(g.chunk, chunkPiece{expr: expr, bits: bits})
	g.chunkBits += bits
}

// chunkFlush merges the pending chunk as one staged word. A scope boundary
// (loop, branch, switch, function end) and every statement-form merge MUST
// flush first — wire order is emission order.
func (g *fgen) chunkFlush(ind string) {
	if len(g.chunk) == 0 {
		return
	}
	parts := make([]string, len(g.chunk))
	shift := int64(0)
	for i, p := range g.chunk {
		if shift == 0 {
			parts[i] = p.expr
		} else {
			parts[i] = fmt.Sprintf("(%s << %d)", p.expr, shift)
		}
		shift += p.bits
	}
	g.pf("%sv = (%s) >>> 0;\n", ind, strings.Join(parts, " | "))
	g.mergeW(g.chunkBits, ind)
	g.chunk = g.chunk[:0]
	g.chunkBits = 0
}

// flatFileHasSurface reports whether a file gets a Flat module: any file
// declaring structs.
func flatFileHasSurface(f *ir.File) bool {
	for _, d := range f.Decls {
		if _, ok := d.(*ir.Struct); ok {
			return true
		}
	}
	return false
}

// generateFlat returns basename+"Flat.js" -> contents for every carrying
// file. The tier-level notes ride the FIRST carrying file (basename order)
// only — said once per unit, not once per file.
func generateFlat(u *ir.Unit) map[string][]byte {
	out := map[string][]byte{}
	first := true
	for _, f := range u.Files {
		if !flatFileHasSurface(f) {
			continue
		}
		g := &fgen{unit: u, file: f, home: first, imports: map[string]map[string]bool{}}
		first = false
		g.emitModule()
		out[f.Base+"Flat.js"] = g.assemble()
	}
	return out
}

// brToken matches a bare `br` identifier — the read cursor — anywhere in a
// line of emitted JavaScript.
var brToken = regexp.MustCompile(`\bbr\b`)

// closesReadWindow reports whether emitting this text must first settle the
// open read window. Two things force it and nothing else can: naming the
// cursor (the emission would see a br that still lags by rwUsed) and opening
// or closing a scope (a window may not outlive the block it was loaded in).
// The value-refusal guards are the deliberate exception — they carry braces
// but only ever test v or bg and only ever `return false`, so they neither
// read the cursor nor let control fall past them with the window stale; they
// emit through rawf and keep the chunk alive across a checked field.
func closesReadWindow(s string) bool {
	return strings.ContainsAny(s, "{}") || brToken.MatchString(s)
}

func (g *fgen) pf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if g.rwOpen && closesReadWindow(s) {
		g.readClose()
	}
	g.fn.WriteString(s)
}

// rawf emits without the read-window inspection: for the window's own lines
// and for the value-refusal guards.
func (g *fgen) rawf(format string, args ...any) {
	fmt.Fprintf(&g.fn, format, args...)
}

// bpf writes module-level text directly to the body (the per-function
// builder g.fn is reset between variants; section text must not ride it).
func (g *fgen) bpf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

func (g *fgen) assemble() []byte {
	var h strings.Builder
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — protocol id 0x%016x\n", g.unit.Package, g.unit.ProtocolId)
	if g.home {
		h.WriteString("//\n")
		h.WriteString("// THE FLAT TIER — the shipped JavaScript wire path: a single-word 32-bit\n")
		h.WriteString("// bitpacker inlined at every field (byte-identical wire to serialize.js),\n")
		h.WriteString("// constant widths and masks, zero function calls. Same generated classes,\n")
		h.WriteString("// same bytes as the runtime tier (a standing CI gate); the runtime tier\n")
		h.WriteString("// remains the diagnostic and reference surface — re-read a failing buffer\n")
		h.WriteString("// through it to learn WHICH operation failed and why.\n")
		h.WriteString("//\n")
		h.WriteString("// Write<Name>Flat(value, view) -> bytes written, or -1 when the checked\n")
		h.WriteString("// writer refuses an out-of-contract value (the production writer trusts\n")
		h.WriteString("// the caller — serialize.js src/mode.js's own NODE_ENV fork, frozen at\n")
		h.WriteString("// module load). Read<Name>Flat(value, view, numBits) -> bool, the family\n")
		h.WriteString("// read verdict; reader obligations ride in every mode.\n")
		h.WriteString("//\n")
		h.WriteString("// Buffers are caller-owned DataViews. Write buffers: at least <Name>MaxBytes\n")
		h.WriteString("// (the constants live in the runtime-tier module). Read buffers: at least\n")
		h.WriteString("// FLAT_READ_SLACK = 8 bytes past the payload — 64-bit windows load\n")
		h.WriteString("// unconditionally; copy exactly-sized receive buffers into a persistent\n")
		h.WriteString("// MaxBytes + 8 scratch first.\n")
	}
	h.WriteString("\n")
	if len(g.imports) > 0 {
		bases := make([]string, 0, len(g.imports))
		for b := range g.imports {
			bases = append(bases, b)
		}
		sort.Strings(bases)
		for _, b := range bases {
			syms := make([]string, 0, len(g.imports[b]))
			for s := range g.imports[b] {
				syms = append(syms, s)
			}
			sort.Strings(syms)
			fmt.Fprintf(&h, "import { %s } from \"./%s.js\";\n", strings.Join(syms, ", "), b)
		}
		h.WriteString("\n")
	}
	h.WriteString("// The 8-byte conversion scratch — serialize.js's FLOAT_SCRATCH twin. Module\n")
	h.WriteString("// scope is safe: single threaded per realm, consumed in the same op that\n")
	h.WriteString("// fills it.\n")
	h.WriteString("const SC = new DataView(new ArrayBuffer(8));\n\n")
	h.WriteString("// The JavaScript #ifdef, frozen at module load (serialize.js src/mode.js):\n")
	h.WriteString("// bundlers statically replace NODE_ENV, so production bundles tree-shake\n")
	h.WriteString("// the checked writers out. Existence-guarded for browsers without process.\n")
	h.WriteString("const PRODUCTION = typeof process !== \"undefined\" && !!process.env && process.env.NODE_ENV === \"production\";\n\n")
	h.WriteString("// Read buffers must extend at least this many bytes past the payload.\n")
	h.WriteString("export const FLAT_READ_SLACK = 8;\n\n")
	h.WriteString(g.body.String())
	return []byte(h.String())
}

func (g *fgen) emitModule() {
	for _, d := range ir.EmissionOrder(g.file) {
		st, ok := d.(*ir.Struct)
		if !ok {
			continue
		}
		g.emitStructFlat(st)
	}
}

// ---- shared kernel text (the probe's gen_flat.mjs, transliterated) ----

// maskHex renders the (1<<bits)-1 mask for bits in [1,32].
func maskHex(bits int64) string {
	if bits == 32 {
		return "0xffffffff"
	}
	return fmt.Sprintf("0x%x", (int64(1)<<bits)-1)
}

// mergeW merges v (already masked to bits) into the single 32-bit staging
// word — same wire bytes as serialize.js's packer (LSB-first into consecutive
// little-endian words), one data-dependent branch per field. The invariant is
// that lo's bits at and above sb are zero: `v << sb` contributes bits
// [sb, min(sb+bits, 32)) (a JS shift drops the rest), and the flush recovers
// the dropped high bits as the new word's low bits, where `bits - sb` (the
// post-flush sb) is the recovered count. The sb === 0 guard covers the one
// case where the recovery shift would be 32 (a no-op shift in JS, not zero).
// Measured against the previous two-lane 64-bit staging form: 1.44x on a
// mixed-width field group (node 26, M2).
func (g *fgen) mergeW(bits int64, ind string) {
	g.pf("%slo = (lo | (v << sb)) >>> 0;\n", ind)
	g.pf("%ssb += %d;\n", ind, bits)
	g.pf("%sif (sb >= 32) {\n", ind)
	g.pf("%s  view.setUint32(wi, lo, true);\n", ind)
	g.pf("%s  wi += 4;\n", ind)
	g.pf("%s  sb -= 32;\n", ind)
	g.pf("%s  lo = sb === 0 ? 0 : v >>> (%d - sb);\n", ind, bits)
	g.pf("%s}\n", ind)
}

// readR reads bits (in [1,32]) into v, from the open read window when the
// field still fits inside its 32 valid bits and from a fresh window when it
// does not. A field at relative offset r > 0 costs one shift and one mask;
// only a field that opens a window pays the two loads and the shift-or.
// r > 0 implies bits <= 31, so the mask is at most 0x7fffffff and the
// extraction stays non-negative without a further `>>> 0`.
func (g *fgen) readR(bits int64, ind string) {
	rel := g.readWin(bits, ind)
	switch {
	case rel > 0:
		g.rawf("%sv = (out >>> %d) & %s;\n", ind, rel, maskHex(bits))
	case bits == 32:
		g.rawf("%sv = out >>> 0;\n", ind)
	default:
		g.rawf("%sv = (out & %s) >>> 0;\n", ind, maskHex(bits))
	}
}

// readWin returns the relative bit offset this field occupies in the open
// window, loading a new one first when the field cannot join. The indent
// test is the second lock on the scope rule: a window never spans a block
// even if some future emission forgets to name a brace.
func (g *fgen) readWin(bits int64, ind string) int64 {
	if g.rwOpen && g.rwInd == ind && g.rwUsed+bits <= 32 {
		rel := g.rwUsed
		g.rwUsed += bits
		return rel
	}
	g.readClose()
	g.rawf("%sbi = br >>> 3;\n", ind)
	g.rawf("%swlo = view.getUint32(bi, true);\n", ind)
	g.rawf("%swhi = view.getUint32(bi + 4, true);\n", ind)
	g.rawf("%ss2 = br & 7;\n", ind)
	g.rawf("%sout = s2 === 0 ? wlo : ((wlo >>> s2) | (whi << (32 - s2)));\n", ind)
	g.rwOpen, g.rwUsed, g.rwInd = true, bits, ind
	return 0
}

// readClose settles the window: the cursor advances by everything the window
// served, at the window's own indent, before whatever forced the close.
func (g *fgen) readClose() {
	if !g.rwOpen {
		return
	}
	used, ind := g.rwUsed, g.rwInd
	g.rwOpen, g.rwUsed, g.rwInd = false, 0, ""
	if used > 0 {
		g.rawf("%sbr += %d;\n", ind, used)
	}
}

// readDrop abandons a window's pending advance instead of emitting it. Legal
// at exactly one place — the end of a read function, where the only
// statement left is `return true`: the flat reader answers a verdict, not a
// length, so nothing downstream can observe the cursor.
func (g *fgen) readDrop() {
	g.rwOpen, g.rwUsed, g.rwInd = false, 0, ""
}

// readRefuse emits a value-refusal guard without settling the read window.
// cond must test decoded values (v, bg) and never the cursor — the read
// window's whole safety argument rests on it.
func (g *fgen) readRefuse(cond, comment, ind string) {
	if brToken.MatchString(cond) {
		panic("readRefuse: condition names the read cursor: " + cond)
	}
	g.rawf("%sif (%s) {%s\n%s  return false;\n%s}\n", ind, cond, comment, ind, ind)
}

// guard emits a checked-writer refusal; a no-op in the production variant.
func (g *fgen) guard(cond, comment, ind string) {
	if !g.checked {
		return
	}
	g.pf("%sif (%s) {%s\n%s  return -1;\n%s}\n", ind, cond, comment, ind, ind)
}

// ---- static wire-width analysis for run fusing ----

// staticBitsItem reports an item's exact wire bits when they are the same on
// every path (branches count only when both sides agree; strings, counted
// arrays and align are dynamic). Exactness matters: the fused read bound
// `br + runBits > numBits` must never overshoot a valid stream.
func (g *fgen) staticBitsItem(item ir.Item) (int64, bool) {
	switch item := item.(type) {
	case *ir.FieldItem:
		return g.staticBitsField(item.F)
	case *ir.ConstItem:
		return item.Bits, true
	case *ir.ReservedItem:
		return item.Bits, true
	case *ir.AlignItem:
		return 0, false
	case *ir.Branch:
		then, ok := g.staticBitsItems(item.Then)
		if !ok {
			return 0, false
		}
		els := int64(0)
		if item.Else != nil {
			var ok2 bool
			els, ok2 = g.staticBitsItems(item.Else)
			if !ok2 {
				return 0, false
			}
		}
		if then == els {
			return then, true
		}
		return 0, false
	}
	return 0, false
}

func (g *fgen) staticBitsItems(items []ir.Item) (int64, bool) {
	var total int64
	for _, item := range items {
		bits, ok := g.staticBitsItem(item)
		if !ok {
			return 0, false
		}
		total += bits
	}
	return total, true
}

func (g *fgen) staticBitsField(f *ir.Field) (int64, bool) {
	elem, ok := g.staticBitsScalar(f)
	if !ok {
		return 0, false
	}
	switch f.Array {
	case ir.ArrayFixed:
		return f.ArrayBound * elem, true
	case ir.ArrayCounted:
		return 0, false
	default:
		return elem, true
	}
}

func (g *fgen) staticBitsScalar(f *ir.Field) (int64, bool) {
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		return 0, false
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			return g.staticBitsItems(ref.Items)
		case *ir.Union:
			// a union's wire is tag + SELECTED arm — static only in the
			// degenerate no-variant case (zero bits). Counting MaxBitsUnion
			// here let a fused read bound overshoot valid wire carrying a
			// smaller arm and refuse it (the ProbeCollider None-arm class).
			if ref.Max == 0 {
				return 0, true
			}
			return 0, false
		}
	}
	// every remaining scalar kind is fixed-width and ir.MaxBitsField is exact
	// for it (ranged/bare ints, fixed, bits, bool, floats, enum, flags)
	return ir.MaxBitsField(&ir.Field{Type: f.Type, HasIntRange: f.HasIntRange,
		IntMin: f.IntMin, IntMax: f.IntMax, HasFloatRange: f.HasFloatRange,
		FMin: f.FMin, FMax: f.FMax, Resolution: f.Resolution}), true
}

// ---- per-struct emission ----

func (g *fgen) emitStructFlat(st *ir.Struct) {
	g.bpf("// ---- type %s: the flat codec ----\n\n", st.Name)

	g.emitWriteVariant(st, false)
	g.emitWriteVariant(st, true)
	g.bpf("// Write%sFlat(value, view) -> bytes written (>= 0), or -1 on a refusal: a\n", st.Name)
	g.bpf("// count outside its wire range in every build (SPEC §4.6), and any other\n")
	g.bpf("// contract in the checked build. The buffer behind view must hold %sMaxBytes.\n", st.Name)
	g.bpf("export const Write%sFlat = PRODUCTION ? write%sFlatProduction : write%sFlatChecked;\n\n", st.Name, st.Name, st.Name)

	g.emitReadFlat(st)
}

func (g *fgen) resetNeeds() {
	g.needX, g.needN, g.needT, g.needBg = false, false, false, false
	g.needNw = false
	g.loopDepth = 0
	g.chunk = g.chunk[:0]
	g.chunkBits = 0
	g.rwOpen, g.rwUsed, g.rwInd = false, 0, ""
}

// writeLocals renders the write-side local declarations the body needs.
func (g *fgen) writeLocals(ind string) string {
	var b strings.Builder
	locals := []string{"v = 0"}
	if g.needX {
		locals = append(locals, "x = 0")
	}
	if g.needN {
		locals = append(locals, "n = 0")
	}
	if g.needT {
		locals = append(locals, "t0 = 0", "t1 = 0")
	}
	fmt.Fprintf(&b, "%slet %s;\n", ind, strings.Join(locals, ", "))
	if g.needBg {
		fmt.Fprintf(&b, "%slet bg = 0n;\n", ind)
	}
	return b.String()
}

func (g *fgen) readLocals(ind string) string {
	var b strings.Builder
	locals := []string{"v = 0", "bi = 0", "wlo = 0", "whi = 0", "s2 = 0", "out = 0"}
	if g.needNw {
		locals = append(locals, "nw = 0", "nl = 0")
	}
	if g.needX {
		locals = append(locals, "x = 0")
	}
	fmt.Fprintf(&b, "%slet %s;\n", ind, strings.Join(locals, ", "))
	if g.needBg {
		fmt.Fprintf(&b, "%slet bg = 0n;\n", ind)
	}
	return b.String()
}

func variantName(checked bool) string {
	if checked {
		return "Checked"
	}
	return "Production"
}

func (g *fgen) emitWriteVariant(st *ir.Struct, checked bool) {
	g.fn.Reset()
	g.resetNeeds()
	g.checked = checked
	g.emitWriteItems(st.Items, "value", "  ")
	g.chunkFlush("  ")
	body := g.fn.String()
	g.fn.Reset()

	g.pf("function write%sFlat%s(value, view) {\n", st.Name, variantName(checked))
	g.body.WriteString(g.fn.String())
	g.fn.Reset()
	g.body.WriteString(g.writeLocals("  "))
	g.body.WriteString("  let lo = 0, sb = 0, wi = 0;\n")
	g.body.WriteString(body)
	g.body.WriteString("  if (sb !== 0) {\n")
	g.body.WriteString("    view.setUint32(wi, lo, true);\n")
	g.body.WriteString("  }\n")
	g.body.WriteString("  return ((wi * 8 + sb) + 7) >> 3;\n")
	g.body.WriteString("}\n\n")
}

func (g *fgen) emitReadFlat(st *ir.Struct) {
	g.fn.Reset()
	g.resetNeeds()
	g.emitReadItems(st.Items, "value", "  ", false)
	// the last window's advance is dead: `return true` is all that follows
	g.readDrop()
	body := g.fn.String()
	g.fn.Reset()

	g.pf("// Read%sFlat(value, view, numBits) -> bool. The buffer behind view must\n", st.Name)
	g.pf("// extend FLAT_READ_SLACK bytes past the payload.\n")
	g.pf("export function Read%sFlat(value, view, numBits) {\n", st.Name)
	g.body.WriteString(g.fn.String())
	g.fn.Reset()
	g.body.WriteString(g.readLocals("  "))
	g.body.WriteString("  let br = 0;\n")
	g.body.WriteString(body)
	g.body.WriteString("  return true;\n")
	g.body.WriteString("}\n\n")
}

// ---- write emission ----

func (g *fgen) emitWriteItems(items []ir.Item, path, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitWriteField(item.F, path, ind)
		case *ir.ConstItem:
			g.emitWriteRaw(item.Value, item.Bits, ind)
		case *ir.ReservedItem:
			g.emitWriteRaw(big.NewInt(0), item.Bits, ind)
		case *ir.AlignItem:
			g.emitWriteAlign(ind)
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.chunkFlush(ind)
			g.pf("%sif (%s%s.%s) {\n", ind, neg, path, ir.GoExportName(item.Cond))
			g.emitWriteItems(item.Then, path, ind+"  ")
			g.chunkFlush(ind + "  ")
			if item.Else != nil {
				g.pf("%s} else {\n", ind)
				g.emitWriteItems(item.Else, path, ind+"  ")
				g.chunkFlush(ind + "  ")
			}
			g.pf("%s}\n", ind)
		}
	}
}

// emitWriteRaw merges a compile-time constant (const/reserved items) of up
// to 64 bits, split low dword first past 32 (the serialize.js group rule).
func (g *fgen) emitWriteRaw(value *big.Int, bits int64, ind string) {
	if bits <= 32 {
		lo := new(big.Int).And(value, big.NewInt((int64(1)<<uint(bits))-1))
		if bits == 32 {
			lo = new(big.Int).And(value, new(big.Int).SetUint64(0xffffffff))
		}
		g.chunkAdd(lo.String(), bits, ind)
		return
	}
	loMask := new(big.Int).SetUint64(0xffffffff)
	lo := new(big.Int).And(value, loMask)
	hi := new(big.Int).Rsh(value, 32)
	hi.And(hi, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits-32)), big.NewInt(1)))
	g.chunkAdd(lo.String(), 32, ind)
	g.chunkAdd(hi.String(), bits-32, ind)
}

// emitWriteAlign pads the write position to the next byte boundary with
// zeros (v contributes nothing, so only the counter moves — with the flush
// kept, exactly as the merge would).
func (g *fgen) emitWriteAlign(ind string) {
	g.chunkFlush(ind)
	g.pf("%ssb = (sb + 7) & -8;\n", ind)
	g.pf("%sif (sb === 32) {\n", ind)
	g.pf("%s  view.setUint32(wi, lo, true);\n", ind)
	g.pf("%s  wi += 4;\n", ind)
	g.pf("%s  lo = 0;\n", ind)
	g.pf("%s  sb = 0;\n", ind)
	g.pf("%s}\n", ind)
}

func (g *fgen) emitWriteField(f *ir.Field, path, ind string) {
	name := path + "." + ir.GoExportName(f.Name)
	switch f.Array {
	case ir.ArrayFixed:
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.chunkFlush(ind)
		g.pf("%sfor (let %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
		g.emitWriteElem(f, name, iv, ind+"  ")
		g.chunkFlush(ind + "  ")
		g.pf("%s}\n", ind)
		g.loopDepth--
	case ir.ArrayCounted:
		count := name + "Count"
		// the count guards the loop, and a count outside its wire range is
		// refused in EVERY build, the production writer included: a wrapped
		// count is bytes no reader accepts (SPEC §4.6). The checked writer
		// also holds the count to an integer, its contract on every number.
		cond := fmt.Sprintf("%s < %d || %s > %d", count, f.ArrayMin, count, f.ArrayBound)
		if g.checked {
			cond = fmt.Sprintf("!Number.isInteger(%s) || %s", count, cond)
		}
		g.pf("%sif (%s) { // the count guards the loop; a count outside its wire range is refused in every build (SPEC §4.6)\n%s  return -1;\n%s}\n", ind, cond, ind, ind)
		g.emitWriteRangedNum(count, f.ArrayMin, f.ArrayBound, ind)
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.chunkFlush(ind)
		g.pf("%sfor (let %s = 0; %s < %s; %s++) {\n", ind, iv, iv, count, iv)
		g.emitWriteElem(f, name, iv, ind+"  ")
		g.chunkFlush(ind + "  ")
		g.pf("%s}\n", ind)
		g.loopDepth--
	default:
		g.emitWriteScalar(f, name, ind)
	}
}

// emitWriteElem writes one array element; struct elements hoist a const ref.
func (g *fgen) emitWriteElem(f *ir.Field, name, iv, ind string) {
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			ev := fmt.Sprintf("e%d", g.loopDepth-1)
			g.pf("%sconst %s = %s[%s];\n", ind, ev, name, iv)
			g.emitWriteItems(ref.Items, ev, ind)
			return
		case *ir.Union:
			ev := fmt.Sprintf("e%d", g.loopDepth-1)
			g.pf("%sconst %s = %s[%s];\n", ind, ev, name, iv)
			g.emitWriteUnionFlat(ref, ev, ind)
			return
		}
	}
	g.emitWriteScalar(f, fmt.Sprintf("%s[%s]", name, iv), ind)
}

// emitWriteRangedNum writes a Number-domain offset from min in the folded
// bit count — the int32 call family's unsigned-domain subtraction where the
// bounds fit int32, the plain (exact) subtraction for uint32-storage ranges
// beyond it.
func (g *fgen) emitWriteRangedNum(expr string, min, max int64, ind string) {
	bits := ir.BitsRequired(big.NewInt(min), big.NewInt(max))
	if bits == 0 {
		return
	}
	var off string
	switch {
	case min == 0:
		off = expr
	case min >= math.MinInt32 && max <= math.MaxInt32:
		off = fmt.Sprintf("((%s >>> 0) - %d) >>> 0", expr, uint32(int32(min)))
	default:
		off = fmt.Sprintf("%s - %d", expr, min)
	}
	if bits == 32 {
		g.chunkAdd(fmt.Sprintf("((%s) >>> 0)", off), bits, ind)
	} else {
		g.chunkAdd(fmt.Sprintf("((%s) & %s)", off, maskHex(bits)), bits, ind)
	}
}

// emitWriteWideOffset merges an unsigned BigInt offset expression of the
// given bit count: 32-bit groups least significant first — the serialize.js
// group structure exactly. rawExpr is the UNREDUCED BigInt offset:
// setBigUint64's own ToBigUint64 wrap is the asUintN(64) reduction, and for
// the >64 groups `>> 64n` before the wrap equals the asUintN(128) high half
// (floor(x/2^64) mod 2^64 is shift-then-wrap and wrap-then-shift alike). The
// scratch route is the measured discipline: DataView BigInt stores consume
// the value without allocating, where each BigInt `&`/`>>`/Number() step
// allocates and runs an order of magnitude slower (node 26, M2: 5.6x on the
// 128-bit split, 6.1x on a sub-32-bit truncation).
func (g *fgen) emitWriteWideOffset(rawExpr string, bits int64, ind string) {
	g.chunkFlush(ind)
	switch {
	case bits <= 32:
		g.pf("%sSC.setBigUint64(0, %s, true);\n", ind, rawExpr)
		if bits == 32 {
			g.pf("%sv = SC.getUint32(0, true);\n", ind)
		} else {
			g.pf("%sv = SC.getUint32(0, true) & %s;\n", ind, maskHex(bits))
		}
		g.mergeW(bits, ind)
	case bits <= 64:
		g.pf("%sSC.setBigUint64(0, %s, true);\n", ind, rawExpr)
		g.pf("%sv = SC.getUint32(0, true);\n", ind)
		g.mergeW(32, ind)
		if bits == 64 {
			g.pf("%sv = SC.getUint32(4, true);\n", ind)
		} else {
			g.pf("%sv = SC.getUint32(4, true) & %s;\n", ind, maskHex(bits-32))
		}
		g.mergeW(bits-32, ind)
	default:
		g.needBg = true
		g.pf("%sbg = %s;\n", ind, rawExpr)
		g.pf("%sSC.setBigUint64(0, bg, true);\n", ind)
		g.pf("%sv = SC.getUint32(0, true);\n", ind)
		g.mergeW(32, ind)
		g.pf("%sv = SC.getUint32(4, true);\n", ind)
		g.mergeW(32, ind)
		g.pf("%sSC.setBigUint64(0, bg >> 64n, true);\n", ind)
		if bits <= 96 {
			if bits == 96 {
				g.pf("%sv = SC.getUint32(0, true);\n", ind)
			} else {
				g.pf("%sv = SC.getUint32(0, true) & %s;\n", ind, maskHex(bits-64))
			}
			g.mergeW(bits-64, ind)
			return
		}
		g.pf("%sv = SC.getUint32(0, true);\n", ind)
		g.mergeW(32, ind)
		if bits == 128 {
			g.pf("%sv = SC.getUint32(4, true);\n", ind)
		} else {
			g.pf("%sv = SC.getUint32(4, true) & %s;\n", ind, maskHex(bits-96))
		}
		g.mergeW(bits-96, ind)
	}
}

func (g *fgen) emitWriteScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		g.emitWriteFixed(f, name, ind)
	case ir.TInt:
		g.emitWriteInt(f, name, ind)
	case ir.TBits:
		w := int64(f.Type.Width)
		if w <= 32 {
			if w == 32 {
				g.chunkAdd(fmt.Sprintf("(%s >>> 0)", name), w, ind)
			} else {
				g.chunkAdd(fmt.Sprintf("(%s & %s)", name, maskHex(w)), w, ind)
			}
			return
		}
		g.emitWriteWideOffset(name, w, ind)
	case ir.TBool:
		g.chunkAdd(fmt.Sprintf("(%s ? 1 : 0)", name), 1, ind)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.emitWriteCompressedFloat(f, name, ind)
			return
		}
		g.emitWriteF32(name, ind)
	case ir.TFloat64:
		g.chunkFlush(ind)
		g.pf("%sSC.setFloat64(0, %s, true);\n", ind, name)
		g.pf("%sv = SC.getUint32(0, true);\n", ind)
		g.mergeW(32, ind)
		g.pf("%sv = SC.getUint32(4, true);\n", ind)
		g.mergeW(32, ind)
	case ir.TString, ir.TBytes:
		g.emitWriteBytesField(f, name, ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.emitWriteEnum(ref, name, ind)
		case *ir.Flags:
			g.emitWriteFlags(ref, name, ind)
		case *ir.Struct:
			g.emitWriteItems(ref.Items, name, ind)
		case *ir.Union:
			g.emitWriteUnionFlat(ref, name, ind)
		}
	}
}

// emitWriteF32 is serialize.js float32BitsFromNumber inlined: hardware
// conversion for non-NaN, software payload narrowing for NaN (quiet bit not
// forced except the all-low-payload case).
func (g *fgen) emitWriteF32(name, ind string) {
	g.chunkFlush(ind)
	g.needX, g.needT, g.needBg = true, true, true
	g.pf("%sx = %s;\n", ind, name)
	g.pf("%sif (x === x) {\n", ind)
	g.pf("%s  SC.setFloat32(0, x, true);\n", ind)
	g.pf("%s  v = SC.getUint32(0, true);\n", ind)
	g.pf("%s} else {\n", ind)
	g.pf("%s  SC.setFloat64(0, x, true);\n", ind)
	g.pf("%s  bg = SC.getBigUint64(0, true);\n", ind)
	g.pf("%s  t0 = Number(bg >> 63n) << 31;\n", ind)
	g.pf("%s  t1 = Number((bg >> 29n) & 0x7fffffn);\n", ind)
	g.pf("%s  if (t1 === 0) { t1 = 0x400000; }\n", ind)
	g.pf("%s  v = (t0 | 0x7f800000 | t1) >>> 0;\n", ind)
	g.pf("%s}\n", ind)
	g.mergeW(32, ind)
}

// f32lit renders the float32 rounding of v as a JS literal (exact as a
// double — f32 is a subset of f64).
func f32lit(v float64) string {
	return formatFloat32(float64(float32(v)))
}

// emitWriteCompressedFloat is the runtime's two-rounding float32
// quantization with the declaration folded: value32 non-finite refused in
// checked mode (the runtime's always-on assert), clamp, quantize.
func (g *fgen) emitWriteCompressedFloat(f *ir.Field, name, ind string) {
	g.chunkFlush(ind)
	g.needX, g.needN = true, true
	maxInt, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	minF := float32(f.FMin)
	deltaF := float32(f.FMax) - minF
	mivF := float32(maxInt)
	g.pf("%sx = Math.fround(%s);\n", ind, name)
	g.guard("!Number.isFinite(x)", "", ind)
	g.pf("%sn = Math.fround(Math.fround(x - %s) / %s);\n", ind, f32lit(float64(minF)), f32lit(float64(deltaF)))
	g.pf("%sif (!(n >= 0.0)) { n = 0.0; } else if (!(n <= 1.0)) { n = 1.0; }\n", ind)
	g.pf("%sv = Math.floor(Math.fround(Math.fround(n * %s) + 0.5));\n", ind, f32lit(float64(mivF)))
	g.mergeW(bits, ind)
}

func (g *fgen) emitWriteFixed(f *ir.Field, name, ind string) {
	fb := uint(f.Type.FracBits)
	rawMin := new(big.Int).Lsh(f.IntMin, fb)
	if f.IntMin.Sign() < 0 {
		rawMin = new(big.Int).Neg(new(big.Int).Lsh(new(big.Int).Neg(f.IntMin), fb))
	}
	rawMax := new(big.Int).Lsh(f.IntMax, fb)
	if f.IntMax.Sign() < 0 {
		rawMax = new(big.Int).Neg(new(big.Int).Lsh(new(big.Int).Neg(f.IntMax), fb))
	}
	rawRange := new(big.Int).Sub(rawMax, rawMin)
	bits := int64(rawRange.BitLen())
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// degenerate: zero bits — the checked refusal is the whole write
		if f.Type.Width > 32 {
			g.guard(fmt.Sprintf("%s !== %sn", name, rawMin.String()), "", ind)
		} else {
			g.guard(fmt.Sprintf("%s !== %s", name, rawMin.String()), "", ind)
		}
		return
	}
	if f.Type.Width <= 32 {
		g.guard(fmt.Sprintf("!Number.isInteger(%s) || %s < %s || %s > %s", name, name, rawMin.String(), name, rawMax.String()),
			"", ind)
		var off string
		switch {
		case rawMin.Sign() == 0:
			off = name
		case rawMin.Cmp(big.NewInt(math.MinInt32)) >= 0 && rawMax.Cmp(big.NewInt(math.MaxInt32)) <= 0:
			off = fmt.Sprintf("((%s >>> 0) - %d) >>> 0", name, uint32(int32(rawMin.Int64())))
		default:
			off = fmt.Sprintf("%s - %s", name, rawMin.String())
		}
		if bits == 32 {
			g.chunkAdd(fmt.Sprintf("((%s) >>> 0)", off), bits, ind)
		} else {
			g.chunkAdd(fmt.Sprintf("((%s) & %s)", off, maskHex(bits)), bits, ind)
		}
		return
	}
	// wide lane: BigInt raw storage, offset in 32-bit groups
	g.guard(fmt.Sprintf("%s < %sn || %s > %sn", name, rawMin.String(), name, rawMax.String()), "", ind)
	off := fmt.Sprintf("%s - %sn", name, rawMin.String())
	if rawMin.Sign() == 0 {
		off = name
	}
	g.emitWriteWideOffset(off, bits, ind)
}

func (g *fgen) emitWriteInt(f *ir.Field, name, ind string) {
	w := int64(f.Type.Width)
	if w == 128 {
		if f.HasIntRange {
			if f.IntMin.Cmp(f.IntMax) == 0 {
				g.guard(fmt.Sprintf("%s !== %sn", name, f.IntMin.String()), "", ind)
				return
			}
			bits := ir.BitsRequired(f.IntMin, f.IntMax)
			g.guard(fmt.Sprintf("%s < %sn || %s > %sn", name, f.IntMin.String(), name, f.IntMax.String()), "", ind)
			off := fmt.Sprintf("%s - %sn", name, f.IntMin.String())
			if f.IntMin.Sign() == 0 {
				off = name
			}
			g.emitWriteWideOffset(off, bits, ind)
			return
		}
		// uint128 raw: full 128 bits, 32-bit groups least significant first
		g.emitWriteWideOffset(name, 128, ind)
		return
	}
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			if w > 32 {
				g.guard(fmt.Sprintf("%s !== %sn", name, f.IntMin.String()), "", ind)
			} else {
				g.guard(fmt.Sprintf("%s !== %s", name, f.IntMin.String()), "", ind)
			}
			return
		}
		switch intRangePath(f.IntMin, f.IntMax) {
		case "int32":
			if w > 32 {
				// BigInt storage, int32-family range: truncate to the call
				// family's domain first (the C# (int) cast's twin), then the
				// Number-domain offset
				g.needN = true
				g.chunkFlush(ind)
				g.pf("%sSC.setBigUint64(0, %s, true);\n", ind, name)
				g.pf("%sn = SC.getInt32(0, true);\n", ind)
				g.guard(fmt.Sprintf("n < %s || n > %s", f.IntMin.String(), f.IntMax.String()), "", ind)
				// n is a mutable temp: its piece merges before anything reuses it
				g.emitWriteRangedNum("n", f.IntMin.Int64(), f.IntMax.Int64(), ind)
				g.chunkFlush(ind)
				return
			}
			g.guard(fmt.Sprintf("!Number.isInteger(%s) || %s < %s || %s > %s", name, name, f.IntMin.String(), name, f.IntMax.String()),
				"", ind)
			g.emitWriteRangedNum(name, f.IntMin.Int64(), f.IntMax.Int64(), ind)
		case "int64":
			if w <= 32 {
				// uint32 storage whose range escapes int32: values and offsets
				// stay below 2^32 — exact in doubles
				g.guard(fmt.Sprintf("!Number.isInteger(%s) || %s < %s || %s > %s", name, name, f.IntMin.String(), name, f.IntMax.String()),
					"", ind)
				g.emitWriteRangedNum(name, f.IntMin.Int64(), f.IntMax.Int64(), ind)
				return
			}
			g.emitWriteRangedBig(f, name, ind)
		default:
			g.emitWriteRangedBig(f, name, ind)
		}
		return
	}
	// bare integer at storage width
	if w == 64 {
		g.emitWriteWideOffset(name, 64, ind)
		return
	}
	cast := name
	if f.Type.Signed {
		switch f.Type.Width {
		case 8:
			cast = name + " & 0xff"
		case 16:
			cast = name + " & 0xffff"
		default:
			cast = name + " >>> 0"
		}
	}
	if w == 32 {
		g.chunkAdd(fmt.Sprintf("((%s) >>> 0)", cast), w, ind)
	} else {
		g.chunkAdd(fmt.Sprintf("((%s) & %s)", cast, maskHex(w)), w, ind)
	}
}

// emitWriteRangedBig is the BigInt-domain ranged write (int64 family and
// full-range unsigned): vacuous guard halves elided per the storage bounds
// (the C# discipline), offset via asUintN.
func (g *fgen) emitWriteRangedBig(f *ir.Field, name, ind string) {
	bits := ir.BitsRequired(f.IntMin, f.IntMax)
	sMin, sMax := storageBoundsBig(f.Type)
	guardLo := f.IntMin.Cmp(sMin) > 0
	guardHi := f.IntMax.Cmp(sMax) < 0
	switch {
	case guardLo && guardHi:
		g.guard(fmt.Sprintf("%s < %sn || %s > %sn", name, f.IntMin.String(), name, f.IntMax.String()), "", ind)
	case guardLo:
		g.guard(fmt.Sprintf("%s < %sn", name, f.IntMin.String()), "", ind)
	case guardHi:
		g.guard(fmt.Sprintf("%s > %sn", name, f.IntMax.String()), "", ind)
	}
	off := fmt.Sprintf("%s - %sn", name, f.IntMin.String())
	if f.IntMin.Sign() == 0 {
		off = name
	}
	g.emitWriteWideOffset(off, bits, ind)
}

func (g *fgen) emitWriteEnum(ref *ir.Enum, name, ind string) {
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
	g.guard(fmt.Sprintf("!Number.isInteger(%s) || %s < 0 || %s > %d", name, name, name, ref.Max),
		" // headroom above the wire range cannot ride", ind)
	if bits == 0 {
		return
	}
	g.chunkAdd(fmt.Sprintf("(%s & %s)", name, maskHex(bits)), bits, ind)
}

func (g *fgen) emitWriteFlags(ref *ir.Flags, name, ind string) {
	wb := int64(ref.WireBits)
	if g.checked && wb < 64 {
		lim := new(big.Int).Lsh(big.NewInt(1), uint(wb))
		g.guard(fmt.Sprintf("BigInt.asUintN(64, %s) >= %sn", name, lim.String()),
			" // a mask bit above the wire width cannot ride", ind)
	}
	if wb <= 32 {
		g.chunkFlush(ind)
		g.pf("%sSC.setBigUint64(0, %s, true);\n", ind, name)
		if wb == 32 {
			g.pf("%sv = SC.getUint32(0, true);\n", ind)
		} else {
			g.pf("%sv = SC.getUint32(0, true) & %s;\n", ind, maskHex(wb))
		}
		g.mergeW(wb, ind)
		return
	}
	g.emitWriteWideOffset(name, wb, ind)
}

// emitWriteBytesField writes string(N)/bytes(N): folded ranged length,
// align (zero pad), then the used bytes as a fused byte loop — the classic
// serialize_string framing composed from primitives, byte-identical to the
// runtime tier's.
func (g *fgen) emitWriteBytesField(f *ir.Field, name, ind string) {
	length := name + "Length"
	g.guard(fmt.Sprintf("!Number.isInteger(%s) || %s < 0 || %s > %d", length, length, length, f.Type.Size),
		" // the length guards the slice; out-of-contract writes are refused", ind)
	g.emitWriteRangedNum(length, 0, f.Type.Size, ind)
	g.emitWriteAlign(ind) // flushes the pending chunk (the length prefix)
	iv := fmt.Sprintf("i%d", g.loopDepth)
	g.loopDepth++
	// four bytes per merge, then the byte-at-a-time tail — measured +3.6%
	// on the leg's write row over the plain byte loop (node 26, M2). The
	// brace scope keeps the loop counter local (two byte fields can share
	// a nesting depth).
	g.pf("%s{\n", ind)
	g.pf("%s  let %s = 0;\n", ind, iv)
	g.pf("%s  for (; %s + 4 <= %s; %s += 4) {\n", ind, iv, length, iv)
	g.pf("%s    v = (%s[%s] | (%s[%s + 1] << 8) | (%s[%s + 2] << 16) | (%s[%s + 3] << 24)) >>> 0;\n",
		ind, name, iv, name, iv, name, iv, name, iv)
	g.mergeW(32, ind+"    ")
	g.pf("%s  }\n", ind)
	g.pf("%s  for (; %s < %s; %s++) {\n", ind, iv, length, iv)
	g.pf("%s    v = %s[%s];\n", ind, name, iv)
	g.mergeW(8, ind+"    ")
	g.pf("%s  }\n", ind)
	g.pf("%s}\n", ind)
	g.loopDepth--
}

// ---- read emission ----

// emitReadItems walks a scope with run fusing: one bounds check covers each
// maximal run of statically-sized items (bounded=true suppresses checks —
// an enclosing scope already proved the bits).
func (g *fgen) emitReadItems(items []ir.Item, path, ind string, bounded bool) {
	i := 0
	for i < len(items) {
		if _, ok := g.staticBitsItem(items[i]); ok {
			total := int64(0)
			j := i
			for j < len(items) {
				bits, ok2 := g.staticBitsItem(items[j])
				if !ok2 {
					break
				}
				total += bits
				j++
			}
			if !bounded && total > 0 {
				g.pf("%sif (br + %d > numBits) {\n%s  return false;\n%s}\n", ind, total, ind, ind)
			}
			for ; i < j; i++ {
				g.emitReadStaticItem(items[i], path, ind)
			}
			continue
		}
		g.emitReadDynamicItem(items[i], path, ind)
		i++
	}
}

// emitReadStaticItem reads one statically-sized item; bounds already proven.
func (g *fgen) emitReadStaticItem(item ir.Item, path, ind string) {
	switch item := item.(type) {
	case *ir.FieldItem:
		g.emitReadStaticField(item.F, path, ind)
	case *ir.ConstItem:
		g.emitReadRaw(item.Value, item.Bits, true, ind)
	case *ir.ReservedItem:
		g.emitReadRaw(big.NewInt(0), item.Bits, false, ind)
	case *ir.Branch:
		// both sides statically equal (the run test admitted it)
		neg := ""
		if item.Neg {
			neg = "!"
		}
		g.pf("%sif (%s%s.%s) {\n", ind, neg, path, ir.GoExportName(item.Cond))
		g.emitReadItems(item.Then, path, ind+"  ", true)
		g.emitZeroItems(item.Else, path, ind+"  ")
		g.pf("%s} else {\n", ind)
		if item.Else != nil {
			g.emitReadItems(item.Else, path, ind+"  ", true)
		}
		g.emitZeroItems(item.Then, path, ind+"  ")
		g.pf("%s}\n", ind)
	}
}

func (g *fgen) emitReadDynamicItem(item ir.Item, path, ind string) {
	switch item := item.(type) {
	case *ir.FieldItem:
		g.emitReadDynamicField(item.F, path, ind)
	case *ir.AlignItem:
		g.emitReadAlign(ind)
	case *ir.Branch:
		neg := ""
		if item.Neg {
			neg = "!"
		}
		g.pf("%sif (%s%s.%s) {\n", ind, neg, path, ir.GoExportName(item.Cond))
		g.emitReadItems(item.Then, path, ind+"  ", false)
		g.emitZeroItems(item.Else, path, ind+"  ")
		g.pf("%s} else {\n", ind)
		if item.Else != nil {
			g.emitReadItems(item.Else, path, ind+"  ", false)
		}
		g.emitZeroItems(item.Then, path, ind+"  ")
		g.pf("%s}\n", ind)
	}
}

// emitReadAlign verifies zero padding to the byte boundary and advances.
func (g *fgen) emitReadAlign(ind string) {
	g.pf("%ss2 = br & 7;\n", ind)
	g.pf("%sif (s2 !== 0) {\n", ind)
	g.pf("%s  if (br + (8 - s2) > numBits) {\n%s    return false;\n%s  }\n", ind, ind, ind)
	g.pf("%s  if ((view.getUint8(br >>> 3) >>> s2) !== 0) { // nonzero padding is refused\n", ind)
	g.pf("%s    return false;\n%s  }\n", ind, ind)
	g.pf("%s  br += 8 - s2;\n", ind)
	g.pf("%s}\n", ind)
}

// emitReadRaw reads a const/reserved item and rejects any other value.
func (g *fgen) emitReadRaw(value *big.Int, bits int64, isConst bool, ind string) {
	what := "reserved bits must read zero"
	if isConst {
		what = "a read rejects any other value"
	}
	if bits <= 32 {
		lo := new(big.Int).And(value, new(big.Int).SetUint64((uint64(1)<<uint(bits))-1))
		g.readR(bits, ind)
		g.readRefuse(fmt.Sprintf("v !== %s", lo.String()), " // "+what, ind)
		return
	}
	g.needBg = true
	g.readR(32, ind)
	g.pf("%sSC.setUint32(0, v, true);\n", ind)
	g.readR(bits-32, ind)
	g.pf("%sSC.setUint32(4, v, true);\n", ind)
	g.pf("%sbg = SC.getBigUint64(0, true);\n", ind)
	masked := new(big.Int).And(value, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1)))
	g.readRefuse(fmt.Sprintf("bg !== %sn", masked.String()), " // "+what, ind)
}

func (g *fgen) emitReadStaticField(f *ir.Field, path, ind string) {
	name := path + "." + ir.GoExportName(f.Name)
	if f.Array == ir.ArrayFixed {
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (let %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
		g.emitReadElem(f, name, iv, ind+"  ", true)
		g.pf("%s}\n", ind)
		g.loopDepth--
		return
	}
	g.emitReadScalar(f, name, ind, true)
}

func (g *fgen) emitReadElem(f *ir.Field, name, iv, ind string, bounded bool) {
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			ev := fmt.Sprintf("e%d", g.loopDepth-1)
			g.pf("%sconst %s = %s[%s];\n", ind, ev, name, iv)
			g.emitReadItems(ref.Items, ev, ind, bounded)
			return
		case *ir.Union:
			ev := fmt.Sprintf("e%d", g.loopDepth-1)
			g.pf("%sconst %s = %s[%s];\n", ind, ev, name, iv)
			g.emitReadUnionFlat(ref, ev, ind, bounded)
			return
		}
	}
	g.emitReadScalar(f, fmt.Sprintf("%s[%s]", name, iv), ind, bounded)
}

func (g *fgen) emitReadDynamicField(f *ir.Field, path, ind string) {
	name := path + "." + ir.GoExportName(f.Name)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.emitReadBytesField(f, name, ind)
	case f.Array == ir.ArrayCounted:
		count := name + "Count"
		countBits := ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound))
		if countBits > 0 {
			g.pf("%sif (br + %d > numBits) {\n%s  return false;\n%s}\n", ind, countBits, ind, ind)
			g.readR(countBits, ind)
			diff := f.ArrayBound - f.ArrayMin
			if diff != (int64(1)<<countBits)-1 {
				g.readRefuse(fmt.Sprintf("v > %d", diff), " // the count guards the loop — reject, never clamp", ind)
			}
			if f.ArrayMin == 0 {
				g.pf("%s%s = v;\n", ind, count)
			} else {
				g.pf("%s%s = v + %d;\n", ind, count, f.ArrayMin)
			}
		} else {
			g.pf("%s%s = %d;\n", ind, count, f.ArrayMin)
		}
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		if elemBits, ok := g.staticBitsScalar(f); ok {
			if elemBits > 0 {
				g.pf("%sif (br + %s * %d > numBits) {\n%s  return false;\n%s}\n", ind, count, elemBits, ind, ind)
			}
			g.pf("%sfor (let %s = 0; %s < %s; %s++) {\n", ind, iv, iv, count, iv)
			g.emitReadElem(f, name, iv, ind+"  ", true)
			g.pf("%s}\n", ind)
		} else {
			g.pf("%sfor (let %s = 0; %s < %s; %s++) {\n", ind, iv, iv, count, iv)
			g.emitReadElem(f, name, iv, ind+"  ", false)
			g.pf("%s}\n", ind)
		}
		g.loopDepth--
	case f.Array == ir.ArrayFixed:
		// a fixed array of dynamically-sized elements (branches, strings)
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (let %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
		g.emitReadElem(f, name, iv, ind+"  ", false)
		g.pf("%s}\n", ind)
		g.loopDepth--
	default:
		// a scalar whose size is dynamic: a nested struct with branches, or
		// a union (tag + selected arm)
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			g.emitReadItems(ref.Items, name, ind, false)
		case *ir.Union:
			g.emitReadUnionFlat(ref, name, ind, false)
		}
	}
}

// emitReadBytesField reads string(N)/bytes(N): ranged length, align with
// zero padding verified, bounds, the bytes, and (strings) the interior-null
// refusal — the runtime tier's exact obligations.
func (g *fgen) emitReadBytesField(f *ir.Field, name, ind string) {
	length := name + "Length"
	lenBits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
	g.pf("%sif (br + %d > numBits) {\n%s  return false;\n%s}\n", ind, lenBits, ind, ind)
	g.readR(lenBits, ind)
	if f.Type.Size != (int64(1)<<lenBits)-1 {
		g.readRefuse(fmt.Sprintf("v > %d", f.Type.Size), " // the length guards the slice — reject, never clamp", ind)
	}
	g.pf("%s%s = v;\n", ind, length)
	g.emitReadAlign(ind)
	g.pf("%sif (br + %s * 8 > numBits) {\n%s  return false;\n%s}\n", ind, length, ind, ind)
	iv := fmt.Sprintf("i%d", g.loopDepth)
	g.loopDepth++
	g.pf("%sbi = br >>> 3;\n", ind)
	g.pf("%sfor (let %s = 0; %s < %s; %s++) {\n", ind, iv, iv, length, iv)
	g.pf("%s  %s[%s] = view.getUint8(bi + %s);\n", ind, name, iv, iv)
	g.pf("%s}\n", ind)
	g.pf("%sbr += %s * 8;\n", ind, length)
	if f.Type.Kind == ir.TString {
		g.pf("%sfor (let %s = 0; %s < %s; %s++) {\n", ind, iv, iv, length, iv)
		g.pf("%s  if (%s[%s] === 0) { // an interior null is content the read refuses\n", ind, name, iv)
		g.pf("%s    return false;\n%s  }\n%s}\n", ind, ind, ind)
	}
	g.loopDepth--
}

// emitReadWide reads a BigInt offset of the given bit count into bg —
// lanes through the scratch through 64 bits, 32-bit groups accumulated
// past that (value-identical to serialize.js's group assembly).
func (g *fgen) emitReadWide(bits int64, ind string) {
	g.needBg = true
	switch {
	case bits <= 32:
		g.readR(bits, ind)
		g.pf("%sbg = BigInt(v);\n", ind)
	case bits <= 64:
		g.readR(32, ind)
		g.pf("%sSC.setUint32(0, v, true);\n", ind)
		g.readR(bits-32, ind)
		g.pf("%sSC.setUint32(4, v, true);\n", ind)
		g.pf("%sbg = SC.getBigUint64(0, true);\n", ind)
	case bits <= 96:
		g.readR(32, ind)
		g.pf("%sSC.setUint32(0, v, true);\n", ind)
		g.readR(32, ind)
		g.pf("%sSC.setUint32(4, v, true);\n", ind)
		g.pf("%sbg = SC.getBigUint64(0, true);\n", ind)
		g.readR(bits-64, ind)
		g.pf("%sbg |= BigInt(v) << 64n;\n", ind)
	default:
		// two scratch-assembled 64-bit halves, one shift-or — each BigInt
		// step here allocates, so the assembly uses two, not seven (the
		// write-side scratch discipline's read twin; 3.2x on the 128-bit
		// assembly, node 26, M2)
		g.readR(32, ind)
		g.pf("%sSC.setUint32(0, v, true);\n", ind)
		g.readR(32, ind)
		g.pf("%sSC.setUint32(4, v, true);\n", ind)
		g.pf("%sbg = SC.getBigUint64(0, true);\n", ind)
		g.readR(32, ind)
		g.pf("%sSC.setUint32(0, v, true);\n", ind)
		g.readR(bits-96, ind)
		g.pf("%sSC.setUint32(4, v, true);\n", ind)
		g.pf("%sbg |= SC.getBigUint64(0, true) << 64n;\n", ind)
	}
}

func (g *fgen) emitReadScalar(f *ir.Field, name, ind string, bounded bool) {
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		// only reachable as an array element (never inside a fused run —
		// staticBitsScalar calls it dynamic)
		g.emitReadBytesField(f, name, ind)
	case ir.TFixed:
		g.emitReadFixed(f, name, ind)
	case ir.TInt:
		g.emitReadInt(f, name, ind)
	case ir.TBits:
		w := int64(f.Type.Width)
		if w <= 32 {
			g.readR(w, ind)
			g.pf("%s%s = v;\n", ind, name)
			return
		}
		g.emitReadWide(w, ind)
		g.pf("%s%s = bg;\n", ind, name)
	case ir.TBool:
		g.readR(1, ind)
		g.pf("%s%s = v !== 0;\n", ind, name)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.emitReadCompressedFloat(f, name, ind)
			return
		}
		g.readR(32, ind)
		g.pf("%sif ((v & 0x7f800000) === 0x7f800000 && (v & 0x7fffff) !== 0) {\n", ind)
		g.pf("%s  SC.setBigUint64(0, (BigInt(v >>> 31) << 63n) | 0x7ff0000000000000n | (BigInt(v & 0x7fffff) << 29n), true);\n", ind)
		g.pf("%s  %s = SC.getFloat64(0, true);\n", ind, name)
		g.pf("%s} else {\n", ind)
		g.pf("%s  SC.setUint32(0, v, true);\n", ind)
		g.pf("%s  %s = SC.getFloat32(0, true);\n", ind, name)
		g.pf("%s}\n", ind)
	case ir.TFloat64:
		g.readR(32, ind)
		g.pf("%sSC.setUint32(0, v, true);\n", ind)
		g.readR(32, ind)
		g.pf("%sSC.setUint32(4, v, true);\n", ind)
		g.pf("%s%s = SC.getFloat64(0, true);\n", ind, name)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			if bits == 0 {
				g.pf("%s%s = 0;\n", ind, name)
				return
			}
			g.readR(bits, ind)
			if ref.Max != (int64(1)<<bits)-1 {
				g.readRefuse(fmt.Sprintf("v > %d", ref.Max), " // headroom above the wire range is refused", ind)
			}
			g.pf("%s%s = v;\n", ind, name)
		case *ir.Flags:
			wb := int64(ref.WireBits)
			if wb <= 32 {
				g.readR(wb, ind)
				g.pf("%s%s = BigInt(v);\n", ind, name)
				return
			}
			g.emitReadWide(wb, ind)
			g.pf("%s%s = bg;\n", ind, name)
		case *ir.Struct:
			g.emitReadItems(ref.Items, name, ind, bounded)
		case *ir.Union:
			g.emitReadUnionFlat(ref, name, ind, bounded)
		}
	}
}

func (g *fgen) emitReadCompressedFloat(f *ir.Field, name, ind string) {
	maxInt, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	minF := float32(f.FMin)
	deltaF := float32(f.FMax) - minF
	mivF := float32(maxInt)
	g.readR(bits, ind)
	if maxInt != (uint64(1)<<bits)-1 {
		g.readRefuse(fmt.Sprintf("v > %d", maxInt), " // headroom above the quantum count is refused", ind)
	}
	g.pf("%s%s = Math.fround(Math.fround(Math.fround(Math.fround(v) / %s) * %s) + %s);\n",
		ind, name, f32lit(float64(mivF)), f32lit(float64(deltaF)), f32lit(float64(minF)))
}

func (g *fgen) emitReadFixed(f *ir.Field, name, ind string) {
	fb := uint(f.Type.FracBits)
	shiftBig := func(v *big.Int) *big.Int {
		if v.Sign() < 0 {
			return new(big.Int).Neg(new(big.Int).Lsh(new(big.Int).Neg(v), fb))
		}
		return new(big.Int).Lsh(v, fb)
	}
	rawMin := shiftBig(f.IntMin)
	rawMax := shiftBig(f.IntMax)
	rawRange := new(big.Int).Sub(rawMax, rawMin)
	bits := int64(rawRange.BitLen())
	if f.IntMin.Cmp(f.IntMax) == 0 {
		if f.Type.Width > 32 {
			g.pf("%s%s = %sn;\n", ind, name, rawMin.String())
		} else {
			g.pf("%s%s = %s;\n", ind, name, rawMin.String())
		}
		return
	}
	if f.Type.Width <= 32 {
		g.readR(bits, ind)
		full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
		if rawRange.Cmp(full) != 0 {
			g.readRefuse(fmt.Sprintf("v > %s", rawRange.String()), " // a smuggled raw offset is refused", ind)
		}
		if rawMin.Sign() == 0 {
			g.pf("%s%s = v;\n", ind, name)
		} else {
			g.pf("%s%s = %s + v;\n", ind, name, rawMin.String())
		}
		return
	}
	g.emitReadWide(bits, ind)
	full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	if rawRange.Cmp(full) != 0 {
		g.readRefuse(fmt.Sprintf("bg > %sn", rawRange.String()), " // a smuggled raw offset is refused", ind)
	}
	if rawMin.Sign() == 0 {
		g.pf("%s%s = bg;\n", ind, name)
	} else {
		g.pf("%s%s = %sn + bg;\n", ind, name, rawMin.String())
	}
}

func (g *fgen) emitReadInt(f *ir.Field, name, ind string) {
	w := int64(f.Type.Width)
	if w == 128 {
		if f.HasIntRange {
			if f.IntMin.Cmp(f.IntMax) == 0 {
				g.pf("%s%s = %sn;\n", ind, name, f.IntMin.String())
				return
			}
			bits := ir.BitsRequired(f.IntMin, f.IntMax)
			diff := new(big.Int).Sub(f.IntMax, f.IntMin)
			full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
			if g.emitReadWideOffsetNum(f, name, bits, diff, full, ind) {
				return
			}
			g.emitReadWide(bits, ind)
			if diff.Cmp(full) != 0 {
				g.readRefuse(fmt.Sprintf("bg > %sn", diff.String()), " // a smuggled offset is refused", ind)
			}
			if f.IntMin.Sign() == 0 {
				g.pf("%s%s = bg;\n", ind, name)
			} else {
				g.pf("%s%s = %sn + bg;\n", ind, name, f.IntMin.String())
			}
			return
		}
		g.emitReadWide(128, ind)
		g.pf("%s%s = bg;\n", ind, name)
		return
	}
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			if w > 32 {
				g.pf("%s%s = %sn;\n", ind, name, f.IntMin.String())
			} else {
				g.pf("%s%s = %s;\n", ind, name, f.IntMin.String())
			}
			return
		}
		bits := ir.BitsRequired(f.IntMin, f.IntMax)
		switch intRangePath(f.IntMin, f.IntMax) {
		case "int32":
			umin := uint32(int32(f.IntMin.Int64()))
			urange := uint32(int32(f.IntMax.Int64())) - umin
			g.readR(bits, ind)
			if bits < 32 && int64(urange) != (int64(1)<<bits)-1 {
				g.readRefuse(fmt.Sprintf("v > %d", urange), " // a smuggled offset is refused", ind)
			} else if bits == 32 && urange != math.MaxUint32 {
				g.readRefuse(fmt.Sprintf("v > %d", urange), " // a smuggled offset is refused", ind)
			}
			var decoded string
			if umin == 0 {
				decoded = "v | 0"
			} else {
				decoded = fmt.Sprintf("(v + %d) | 0", umin)
			}
			if w > 32 {
				g.pf("%s%s = BigInt(%s);\n", ind, name, decoded)
			} else {
				g.pf("%s%s = %s;\n", ind, name, decoded)
			}
		case "int64":
			if w <= 32 {
				// uint32 storage: offsets below 2^32, exact in doubles
				min := f.IntMin.Int64()
				urange := new(big.Int).Sub(f.IntMax, f.IntMin)
				g.readR(bits, ind)
				full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
				if urange.Cmp(full) != 0 {
					g.readRefuse(fmt.Sprintf("v > %s", urange.String()), " // a smuggled offset is refused", ind)
				}
				if min == 0 {
					g.pf("%s%s = v;\n", ind, name)
				} else {
					g.pf("%s%s = v + %d;\n", ind, name, min)
				}
				return
			}
			g.emitReadRangedBig(f, name, bits, ind)
		default:
			g.emitReadRangedBig(f, name, bits, ind)
		}
		return
	}
	// bare integer at storage width
	if w == 64 {
		g.emitReadWide(64, ind)
		if f.Type.Signed {
			g.pf("%s%s = BigInt.asIntN(64, bg);\n", ind, name)
		} else {
			g.pf("%s%s = bg;\n", ind, name)
		}
		return
	}
	g.readR(w, ind)
	switch {
	case f.Type.Signed && w == 32:
		g.pf("%s%s = v | 0;\n", ind, name)
	case f.Type.Signed:
		shift := 32 - w
		g.pf("%s%s = (v << %d) >> %d;\n", ind, name, shift, shift)
	default:
		g.pf("%s%s = v;\n", ind, name)
	}
}

// emitReadRangedBig decodes a BigInt-domain ranged integer (int64 family
// and full-range unsigned): offset read wide, headroom rejected unless the
// range fills the width, min added in the value domain (exact — decoded
// values are in | min, max).
func (g *fgen) emitReadRangedBig(f *ir.Field, name string, bits int64, ind string) {
	diff := new(big.Int).Sub(f.IntMax, f.IntMin)
	full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	if bits <= 32 {
		g.readR(bits, ind)
		if diff.Cmp(full) != 0 {
			g.readRefuse(fmt.Sprintf("v > %s", diff.String()), " // a smuggled offset is refused", ind)
		}
		if f.IntMin.Sign() == 0 {
			g.pf("%s%s = BigInt(v);\n", ind, name)
		} else {
			g.pf("%s%s = %sn + BigInt(v);\n", ind, name, f.IntMin.String())
		}
		return
	}
	if g.emitReadWideOffsetNum(f, name, bits, diff, full, ind) {
		return
	}
	g.emitReadWide(bits, ind)
	if diff.Cmp(full) != 0 {
		g.readRefuse(fmt.Sprintf("bg > %sn", diff.String()), " // a smuggled offset is refused", ind)
	}
	if f.IntMin.Sign() == 0 {
		g.pf("%s%s = bg;\n", ind, name)
	} else {
		g.pf("%s%s = %sn + bg;\n", ind, name, f.IntMin.String())
	}
}

// numExact is 2^53: the largest magnitude at which consecutive integers are
// all exactly representable as JavaScript Numbers.
var numExact = new(big.Int).Lsh(big.NewInt(1), 53)

// emitReadWideOffsetNum decodes a wide OFFSET field of more than 64 bits
// whose high half is small enough to be a Number, doing the range refusal
// and the offset in the NUMBER domain and materialising exactly ONE BigInt
// for the high half. It reports whether it emitted; a shape it cannot prove
// falls through to the general wide path unchanged.
//
// A 128-bit-domain BigInt op is multi-digit and allocating: today's shape
// costs a second getBigUint64, a shift by 64, an or, a BigInt comparison and
// a BigInt add. Here the high half arrives as a Number, the comparison is
// numeric, the offset is a numeric add, and one signed 64-bit BigInt comes
// out of the scratch — a shift and an add is all that is left.
//
// Conditions, every one of them required for exactness:
//   - bits in (64, 117]: the high half is bits-64 <= 53 bits, so its raw
//     value and every intermediate below are exact Numbers;
//   - min is a nonzero multiple of 2^64: the offset touches ONLY the high
//     half, so the low 64 bits pass through with no borrow;
//   - min's high half, and the offset-adjusted high half, stay inside 2^53.
//
// The wide fields this does not cover — a full-width 128-bit high half, or
// an offset with low bits — keep the general path; extending it there is a
// named follow-on, not this pass.
func (g *fgen) emitReadWideOffsetNum(f *ir.Field, name string, bits int64, diff, full *big.Int, ind string) bool {
	hw := bits - 64
	if bits <= 64 || hw > 53 || f.IntMin.Sign() == 0 {
		return false
	}
	shift64 := uint(64)
	loMask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), shift64), big.NewInt(1))
	// min must land wholly in the high half: a low remainder would borrow
	minAbs := new(big.Int).Abs(f.IntMin)
	if new(big.Int).And(minAbs, loMask).Sign() != 0 {
		return false
	}
	minHi := new(big.Int).Rsh(minAbs, shift64)
	if f.IntMin.Sign() < 0 {
		minHi.Neg(minHi)
	}
	// the adjusted high half spans [minHi, minHi + 2^hw); both ends exact
	hiTop := new(big.Int).Add(minHi, new(big.Int).Lsh(big.NewInt(1), uint(hw)))
	if new(big.Int).Abs(minHi).Cmp(numExact) >= 0 || new(big.Int).Abs(hiTop).Cmp(numExact) >= 0 {
		return false
	}

	g.needBg = true
	g.needNw = true

	// the low 64 bits, through the scratch, exactly as the general path
	g.readR(32, ind)
	g.pf("%sSC.setUint32(0, v, true);\n", ind)
	g.readR(32, ind)
	g.pf("%sSC.setUint32(4, v, true);\n", ind)
	g.pf("%sbg = SC.getBigUint64(0, true);\n", ind)

	// the high half, in the number domain
	if hw <= 32 {
		g.readR(hw, ind)
		g.pf("%snw = v;\n", ind)
	} else {
		g.readR(32, ind)
		g.pf("%snw = v;\n", ind)
		g.readR(hw-32, ind)
		g.pf("%snw = v * 4294967296 + nw;\n", ind)
	}

	// the range refusal, split across the halves: the high comparison is
	// numeric, and the low one only runs on the exact-high boundary
	if diff.Cmp(full) != 0 {
		diffHi := new(big.Int).Rsh(diff, shift64)
		diffLo := new(big.Int).And(diff, loMask)
		g.readRefuse(
			fmt.Sprintf("nw > %s || (nw === %s && bg > %sn)", diffHi.String(), diffHi.String(), diffLo.String()),
			" // a smuggled offset is refused", ind)
	}

	// the offset, then ONE signed 64-bit BigInt for the high half. `nw >>> 0`
	// is ToUint32 — nw mod 2^32, the correct low word for a negative nw too —
	// so `nw - nl` is an exact multiple of 2^32 and the division is exact
	// with no rounding mode in the argument: the high word is what it is,
	// whichever way a rounding function would have gone. It is inside int32
	// range because |nw| < 2^53.
	g.pf("%snw += %s;\n", ind, minHi.String())
	g.pf("%snl = nw >>> 0;\n", ind)
	g.pf("%sSC.setUint32(0, nl, true);\n", ind)
	g.pf("%sSC.setInt32(4, (nw - nl) / 4294967296, true);\n", ind)
	g.pf("%s%s = (SC.getBigInt64(0, true) << 64n) + bg;\n", ind, name)
	return true
}

// ---- inline zeroing (SPEC §5: untaken branch sides read as ZERO) ----

// emitWriteUnionFlat inlines a union field (SPEC §4.8): the checked guard
// validates the tag BEFORE it rides, the tag merges in minimal bits, then a
// switch inlines each arm's items — the struct-inlining move, per arm. expr
// is the union object (a field path or a hoisted element ref).
func (g *fgen) emitWriteUnionFlat(u *ir.Union, expr, ind string) {
	g.guard(fmt.Sprintf("!Number.isInteger(%s.Type) || %s.Type < 0 || %s.Type > %d", expr, expr, expr, u.Max),
		" // the tag validates BEFORE it rides (SPEC §4.8)", ind)
	if u.Max == 0 {
		return // an empty union's degenerate tag range [0, 0] costs zero bits
	}
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	g.chunkAdd(fmt.Sprintf("(%s.Type & %s)", expr, maskHex(bits)), bits, ind)
	g.chunkFlush(ind)
	g.pf("%sswitch (%s.Type) {\n", ind, expr)
	for i, vr := range u.Variants {
		g.pf("%s  case %d: {\n", ind, i+1)
		if vr.Void() {
			g.pf("%s    break; // a payload-free arm: the tag is the whole wire (SPEC §4.8)\n%s  }\n", ind, ind)
			continue
		}
		g.emitWriteItems(vr.Ref.Items, expr+"."+ir.GoExportName(vr.Name), ind+"    ")
		g.chunkFlush(ind + "    ")
		g.pf("%s    break;\n%s  }\n", ind, ind)
	}
	g.pf("%s}\n", ind)
}

// emitReadUnionFlat is the read half: the tag reads in minimal bits and a
// value above the count is refused (SPEC §4.8); the selected arm receives
// fresh construction values in place, then its items inline — byte- and
// acceptance-identical to the runtime tier's Read<Union>.
func (g *fgen) emitReadUnionFlat(u *ir.Union, expr, ind string, bounded bool) {
	if u.Max == 0 {
		g.pf("%s%s.Type = 0; // zero wire bits — only None exists (SPEC §4.8)\n", ind, expr)
		return
	}
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	if !bounded {
		g.pf("%sif (br + %d > numBits) {\n%s  return false;\n%s}\n", ind, bits, ind, ind)
	}
	g.readR(bits, ind)
	if u.Max != (int64(1)<<bits)-1 {
		g.readRefuse(fmt.Sprintf("v > %d", u.Max), " // not a wire-legal tag (SPEC §4.8)", ind)
	}
	g.pf("%s%s.Type = v;\n", ind, expr)
	g.pf("%sswitch (%s.Type) {\n", ind, expr)
	for i, vr := range u.Variants {
		arm := expr + "." + ir.GoExportName(vr.Name)
		g.pf("%s  case %d: {\n", ind, i+1)
		if vr.Void() {
			g.pf("%s    break; // a payload-free arm: the tag is the whole wire (SPEC §4.8)\n%s  }\n", ind, ind)
			continue
		}
		// Use the runtime helper's initialization emitter, inlined with
		// resolved values so this module needs no schema-constant imports.
		init := &gen{unit: g.unit, file: g.file, inlineInit: true}
		for _, nf := range vr.Ref.Fields {
			init.emitInitField(nf, arm, ind+"    ")
		}
		g.pf("%s", init.body.String())
		// The arm's runs re-check bounds; the tag's proof does not extend to it.
		g.emitReadItems(vr.Ref.Items, arm, ind+"    ", false)
		g.pf("%s    break;\n%s  }\n", ind, ind)
	}
	g.pf("%s}\n", ind)
}

func (g *fgen) emitZeroItems(items []ir.Item, path, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitZeroFieldFlat(item.F, path, ind)
		case *ir.Branch:
			g.emitZeroItems(item.Then, path, ind)
			g.emitZeroItems(item.Else, path, ind)
		}
	}
}

func (g *fgen) emitZeroFieldFlat(f *ir.Field, path, ind string) {
	name := path + "." + ir.GoExportName(f.Name)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.pf("%s%s.fill(0);\n%s%sLength = 0;\n", ind, name, ind, name)
	case f.Array != ir.ArrayNone:
		if st, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed {
			iv := fmt.Sprintf("i%d", g.loopDepth)
			g.loopDepth++
			g.pf("%sfor (let %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
			ev := fmt.Sprintf("e%d", g.loopDepth-1)
			g.pf("%s  const %s = %s[%s];\n", ind, ev, name, iv)
			for _, nf := range st.Fields {
				g.emitZeroFieldFlat(nf, ev, ind+"  ")
			}
			g.pf("%s}\n", ind)
			g.loopDepth--
		} else if _, isUnion := f.Type.Ref.(*ir.Union); isUnion && f.Type.Kind == ir.TNamed {
			// zero IS None per element: the tag resets; arms are unspecified
			// at None (SPEC §4.8)
			iv := fmt.Sprintf("i%d", g.loopDepth)
			g.loopDepth++
			g.pf("%sfor (let %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
			g.pf("%s  %s[%s].Type = 0;\n", ind, name, iv)
			g.pf("%s}\n", ind)
			g.loopDepth--
		} else {
			g.pf("%s%s.fill(%s);\n", ind, name, g.flatZeroValue(f.Type))
		}
		if f.Array == ir.ArrayCounted {
			g.pf("%s%sCount = 0;\n", ind, name)
		}
	default:
		if st, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed {
			for _, nf := range st.Fields {
				g.emitZeroFieldFlat(nf, name, ind)
			}
			return
		}
		if _, isUnion := f.Type.Ref.(*ir.Union); isUnion && f.Type.Kind == ir.TNamed {
			// zero IS None: the tag resets; arms are unspecified at None
			g.pf("%s%s.Type = 0;\n", ind, name)
			return
		}
		g.pf("%s%s = %s;\n", ind, name, g.flatZeroValue(f.Type))
	}
}

// flatZeroValue is zeroValue with enum references folded to 0 (flat modules
// import nothing for wire bodies).
func (g *fgen) flatZeroValue(t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "false"
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Enum:
			return "0"
		case *ir.Flags:
			return "0n"
		}
		return "0"
	}
	if isBigStorage(t) {
		return "0n"
	}
	return "0"
}
