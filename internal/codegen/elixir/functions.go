// Wire-function emission: write/read/measure/zero per type, the issue #167
// prescription — the serialize.elixir port's proven shapes with literal
// constant widths, nested types inlined into the caller's body (one function
// per top-level operation), bounds checks fused per maximal static run on the
// read side, and the measure side's static runs folded to literals at
// generation time.
//
// The wire math is the family group structure (32-bit groups, least
// significant first) through the port's small-integer machinery: the write
// side merges into a byte-granular scratch — after every merge the whole
// bytes flush to the output binary, so the scratch never holds more than
// 7 + 32 bits and no intermediate approaches the BEAM's 60-bit boxing
// boundary — and the read side decodes through the port's 40-bit windows.
// Refusal on the read side is a thrown :invalid caught at the function
// surface: an early exit with no per-operation result tuple (the port's
// measured 7x allocation tax), surfaced as the family verdict
// {:ok, value} | :error. Loops are tail-recursive function-head helpers
// threading the same plain accumulators; each loop costs one returned tuple,
// never one per element.
package elixir

import (
	"fmt"
	"maps"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) pf(format string, args ...any) {
	fmt.Fprintf(&g.fn, format, args...)
}

// maskHex renders the (1<<bits)-1 mask for bits in [1,64] — the port's
// uppercase-hex spelling.
func maskHex(bits int64) string {
	return fmt.Sprintf("0x%X", new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1)))
}

// writeGroupBits is the merge group budget: the most static bits the scratch
// carries between flushes. The BEAM's fixnum is 60-bit signed, so every
// intermediate must stay under 2^59 or it costs a heap bignum — measured, a
// boxed window is slower than the extra flush it saves. A flush leaves at
// most 7 bits behind, so 7 + 52 = 59 is the whole budget and 52 is the group.
const writeGroupBits = 52

// mergeW merges v (already inside bits) into the scratch. The whole bytes do
// NOT leave on every field: the generator knows every width statically, so it
// carries a group of up to writeGroupBits and spills once for the group,
// which is one bs_append BIF call for the group instead of one per field.
// Every statement rebinds ONE variable (an Elixir conditional cannot rebind
// several without a tuple, and the merge must allocate none). bits in [1, 32];
// pendW + bits <= 52 and scratch_bits <= 7 + pendW, so no intermediate reaches
// 2^59.
func (g *gen) mergeW(bits int64, ind string) {
	if g.pendW+bits > writeGroupBits {
		g.spillW(ind)
	}
	switch {
	case !g.sbKnown:
		g.pf("%sscratch = scratch ||| v <<< scratch_bits\n", ind)
		g.pf("%sscratch_bits = scratch_bits + %d\n", ind, bits)
	case g.scZero && g.sbVal == 0:
		// nothing to or against and nothing to shift past: a BIND, so the
		// surface's opening zero is not needed for this one
		g.pf("%sscratch = v\n", ind)
	case g.sbVal == 0:
		g.pf("%sscratch = scratch ||| v\n", ind)
	default:
		g.pf("%sscratch = scratch ||| v <<< %d\n", ind, g.sbVal)
	}
	g.sbVal += bits
	g.scZero = false
	g.scBound, g.sbBound = true, g.sbBound || !g.sbKnown
	g.pendW += bits
}

// wseg is one segment waiting to ride the barrier's binary construction: the
// register holding the bytes, and their width — a literal byte count where the
// offset is static, otherwise the name of the local that computed it.
type wseg struct {
	reg   string
	whole int64
	width string
}

// text renders the segment. A literal width is the form the BEAM's binary
// construction is built for; a dynamic one costs the same (measured: twelve
// appends of a dynamic-width segment cost 103.7 ns against 103.6 for literal
// widths — the append is the expense, not the arithmetic around it).
func (s wseg) text() string {
	if s.width != "" {
		return fmt.Sprintf("%s::little-size(%s)-unit(8)", s.reg, s.width)
	}
	return fmt.Sprintf("%s::little-size(%d)-unit(8)", s.reg, s.whole)
}

// spillW empties the scratch's whole bytes into a fresh register because the
// merge group is FULL, not because anything is about to observe data. The
// bytes are recorded as a pending segment and the append is deferred to the
// barrier, where one binary construction carries every register that spilled
// since the last one.
//
// The statement count is what it was — a bind and a shift where there was an
// append and a shift — and the fixnum envelope is untouched: a register is
// filled by exactly the spill that used to append it, so it holds what the
// scratch held, below 2^59. Registers stay live to the barrier; the BEAM has
// 1024 of them and the deepest run in the tree spends nine.
func (g *gen) spillW(ind string) {
	if g.pendW == 0 {
		return
	}
	g.pendW = 0
	reg := fmt.Sprintf("sc%d", g.segN)
	if !g.sbKnown {
		width := fmt.Sprintf("fl%d", g.segN)
		g.segN++
		g.pf("%s%s = scratch\n", ind, reg)
		g.pf("%s%s = scratch_bits >>> 3\n", ind, width)
		g.pf("%sscratch = scratch >>> (%s <<< 3)\n", ind, width)
		g.pf("%sscratch_bits = scratch_bits &&& 7\n", ind)
		g.segW = append(g.segW, wseg{reg: reg, width: width})
		return
	}
	whole, rest := g.sbVal>>3, g.sbVal&7
	g.sbVal = rest
	if whole == 0 {
		return // under a byte: the group carries, nothing leaves
	}
	g.segN++
	// the segment takes the low whole bytes, so the residual bits riding
	// above them are simply not in the segment and need no masking here
	g.pf("%s%s = scratch\n", ind, reg)
	g.segW = append(g.segW, wseg{reg: reg, whole: whole})
	if rest == 0 {
		g.scZero, g.scBound = true, false
		return
	}
	g.pf("%sscratch = scratch >>> %d\n", ind, whole<<3)
}

// flushW closes the group at a barrier: every register that spilled since the
// last barrier, plus the scratch's own whole bytes, ride ONE binary
// construction. It leaves the scratch invariant the group model rests on —
// scratch_bits in [0, 7] and scratch below 2^scratch_bits — and is a no-op
// when no group is open, which is what makes a barrier free where one is
// already closed.
//
// EVERY point that observes data or scratch_bits outside the merge — the
// function tail, an align, the bytes of a string, a loop helper's call and its
// own tail, and the joins of a branch or a union case — is a barrier and calls
// this first.
func (g *gen) flushW(ind string) {
	if g.pendW == 0 && len(g.segW) == 0 {
		return
	}
	g.pendW = 0
	segs := g.segW
	g.segW = nil
	if !g.sbKnown {
		g.pf("%sflush = scratch_bits >>> 3\n", ind)
		segs = append(segs, wseg{reg: "scratch", width: "flush"})
		g.segAppend(segs, ind)
		g.pf("%sscratch = scratch >>> (flush <<< 3)\n", ind)
		g.pf("%sscratch_bits = scratch_bits &&& 7\n", ind)
		return
	}
	// the width is a literal, so the segment's size is a literal and the
	// three bookkeeping statements are not emitted at all
	whole, rest := g.sbVal>>3, g.sbVal&7
	g.sbVal = rest
	if whole > 0 {
		g.scBound = true
		segs = append(segs, wseg{reg: "scratch", whole: whole})
	}
	if len(segs) == 0 {
		return // under a byte and nothing spilled: the group carries
	}
	g.segAppend(segs, ind)
	if whole == 0 {
		return // the scratch kept every bit it had; only registers left
	}
	if rest == 0 {
		// the scratch held exactly the bytes that left, so its value is now
		// zero — and it is not WRITTEN here, because the next merge of an
		// empty group is a bind. ensureScratch materializes the zero at
		// whatever reads it first, if anything does.
		g.scZero, g.scBound = true, false
		return
	}
	g.pf("%sscratch = scratch >>> %d\n", ind, whole<<3)
}

// segAppend prints the barrier's one binary construction. Where the one-line
// form outgrows the formatter's width the statement breaks after the `=` and
// the segments fill greedily, which is mix format's own shape for a long
// bitstring: `<<` at ind+2, continuations at ind+4, and each segment measured
// with the comma or the closing `>>` that will follow it.
func (g *gen) segAppend(segs []wseg, ind string) {
	parts := make([]string, 0, len(segs)+1)
	parts = append(parts, "data::binary")
	for _, s := range segs {
		parts = append(parts, s.text())
	}
	if one := ind + "data = <<" + strings.Join(parts, ", ") + ">>"; len(one) <= formatWidth {
		g.pf("%s\n", one)
		return
	}
	g.pf("\n%sdata =\n", ind)
	line := ind + "  <<" + parts[0]
	for i := 1; i < len(parts); i++ {
		tail := 1 // the comma that follows this segment
		if i == len(parts)-1 {
			tail = 2 // the closing >> instead
		}
		if len(line)+1+1+len(parts[i])+tail > formatWidth {
			g.pf("%s,\n", line)
			line = ind + "    " + parts[i]
			continue
		}
		line += ", " + parts[i]
	}
	g.pf("%s>>\n\n", line)
}

// syncSB materializes scratch_bits where the emitted code is about to read it
// at runtime — a loop helper's call, the tuple that leaves a branch arm. It
// is a no-op where the value is already the variable's.
func (g *gen) syncSB(ind string) {
	if g.sbKnown {
		g.pf("%sscratch_bits = %d\n", ind, g.sbVal)
		g.sbBound = true
	}
}

// ensureScratch binds scratch — and scratch_bits with it where asked —
// immediately before emitted code READS them with nothing having bound them
// yet. Under static offsets a write surface's first touch is normally a
// bind, so the zero the surface used to open with unconditionally is emitted
// here, where something needs it, and nowhere else. At every such point both
// are provably still zero.
func (g *gen) ensureScratch(ind string, alsoSB bool) {
	if !g.scBound {
		g.pf("%sscratch = 0\n", ind)
		g.scBound = true
	}
	if alsoSB && !g.sbBound {
		g.pf("%sscratch_bits = 0\n", ind)
		g.sbBound = true
	}
}

// sbState is the static scratch state, saved across an emission that has to
// be entered more than once (a branch's arms) or entered fresh (a helper).
type sbState struct {
	known   bool
	val     int64
	zero    bool
	pend    int64
	scBound bool
	sbBound bool
	segs    []wseg
	segN    int
}

func (g *gen) sbSave() sbState {
	return sbState{g.sbKnown, g.sbVal, g.scZero, g.pendW, g.scBound, g.sbBound, g.segW, g.segN}
}

// captureArm emits one arm of a branch or a union case into a detached
// buffer, entered at the state the join was entered in, and reports the
// state the arm ends in. The arms are captured before the join's shape is
// chosen, because the shape depends on whether they agree.
func (g *gen) captureArm(entry sbState, emit func()) (string, sbState) {
	saved := g.fn
	g.fn = strings.Builder{}
	g.sbRestore(entry)
	emit()
	text, end := g.fn.String(), g.sbSave()
	g.fn = saved
	return text, end
}

// sbAgree reports the literal offset every arm ends on, when they all end on
// a known one and it is the same. Then the join has nothing to publish and
// scratch_bits does not ride the tuple at all.
func sbAgree(arms []sbState) (int64, bool) {
	if len(arms) == 0 {
		return 0, false
	}
	for i, a := range arms {
		if !a.known || (i > 0 && a.val != arms[0].val) {
			return 0, false
		}
	}
	return arms[0].val, true
}

func (g *gen) sbRestore(s sbState) {
	g.sbKnown, g.sbVal, g.scZero, g.pendW = s.known, s.val, s.zero, s.pend
	g.scBound, g.sbBound = s.scBound, s.sbBound
	g.segW, g.segN = s.segs, s.segN
}

// joinArm is one captured emission path of a branch or a union case: the
// clause header that introduces it, the body, the indent its result tuple
// sits at, and the static state it ends in.
type joinArm struct {
	lead    string
	body    string
	tupleIn string
	trail   string
	end     sbState
}

// emitWriteJoin prints the multi-path assignment that closes a branch or a
// union case. What leaves the join is ONE invariant, and its shape is read
// off the arms: where every arm ends on the same literal offset, the offset
// stays static past the join and scratch_bits does not ride the tuple at
// all; otherwise each arm that knew its offset publishes it, and the
// emitter goes back to maintaining the variable.
func (g *gen) emitWriteJoin(ind, open, close string, arms []joinArm) {
	ends := make([]sbState, len(arms))
	for i, a := range arms {
		ends[i] = a.end
	}
	val, agree := sbAgree(ends)
	tuple := "{data, scratch, scratch_bits}"
	if agree {
		tuple = "{data, scratch}"
	}
	g.pf("\n")
	g.pf("%s%s =\n", ind, tuple)
	g.pf("%s%s", ind, open)
	entrySB := g.sbBound
	for _, a := range arms {
		g.fn.WriteString(a.lead)
		g.fn.WriteString(a.body)
		g.sbRestore(a.end)
		if !agree && a.end.known {
			g.pf("%sscratch_bits = %d\n", a.tupleIn, a.end.val)
			g.sbBound = true
		}
		// the tuple READS both, so an arm that bound neither binds here
		g.ensureScratch(a.tupleIn, !agree)
		g.pf("%s%s\n", a.tupleIn, tuple)
		g.pf("%s", a.trail)
	}
	g.pf("%s%s", ind, close)
	g.pf("\n")
	g.pendW, g.scZero = 0, false
	// segW must not survive a join: registers spilled BEFORE the join would
	// re-emit at the next barrier (silently — the wire-corruption class),
	// and registers spilled INSIDE an arm would fail to compile outside it.
	// Today every arm ends in a flush so this is always nil already; the
	// reset guards the shape a future emitter change could produce. segN
	// stays monotonic on purpose — resetting it renumbers registers after
	// every join, churning the generated text for no safety.
	g.segW = nil
	g.sbKnown, g.sbVal = agree, val
	g.scBound = true
	g.sbBound = entrySB || !agree
}

// rdWindowBits / rdwWindowBits are the two window decodes' usable widths: a
// 40-bit window less the 7-bit worst-case offset is 33, a 56-bit window less
// the same is 49. Both windows stay under the BEAM's 2^59 fixnum boundary; a
// 72-bit window does not, and measured on this shape it is SLOWER than the
// 40-bit one it would replace (29.4 vs 27.6 ns/element) because the box costs
// more than the reads it saves. 49 is therefore the read group's ceiling.
const (
	rdWindowBits  = 33
	rdwWindowBits = 49
)

// readR binds v to the next bits (in [1,32]) of the wire. The window decode
// does NOT open per field: the generator knows every width statically, so it
// reads a GROUP into rv once and cuts each field out of it with a static
// shift and mask. rdRun is the fused static run's remaining bits when the
// caller knows it, so a short run reads the cheap 40-bit window instead of
// the wide one.
//
// Reading a group wider than the fields the caller will use is safe by
// construction: rd/rdw never raise (the tail falls back to the bytes that
// exist), and bits past the bounds-checked run are discarded, never observed.
func (g *gen) readR(bits int64, ind string) {
	if g.feed != nil {
		g.readFeed(bits, ind)
		return
	}
	if g.rdAvail < bits {
		w := int64(rdwWindowBits)
		if g.rdRun > 0 && g.rdRun < w {
			w = g.rdRun
		}
		if w < bits {
			w = bits
		}
		if w <= rdWindowBits {
			g.needRd = true
			g.pf("%srv = rd(data, bits_read, %d)\n", ind, w)
		} else {
			g.needRdw = true
			g.pf("%srv = rdw(data, bits_read, %d)\n", ind, w)
		}
		g.rdOff, g.rdAvail = 0, w
	}
	switch {
	case g.rdAvail == bits && g.rdOff == 0:
		g.pf("%sv = rv\n", ind)
	case g.rdAvail == bits:
		// the group's top field: rv carries no bits above it
		g.pf("%sv = rv >>> %d\n", ind, g.rdOff)
	case g.rdOff == 0:
		g.pf("%sv = rv &&& %s\n", ind, maskHex(bits))
	default:
		g.pf("%sv = rv >>> %d &&& %s\n", ind, g.rdOff, maskHex(bits))
	}
	g.pf("%sbits_read = bits_read + %d\n", ind, bits)
	g.rdOff += bits
	g.rdAvail -= bits
	if g.rdRun > 0 {
		g.rdRun -= bits
	}
}

// rdBreak closes the read group. Every point where bits_read moves outside
// readR, or where emission paths join, is a barrier: the function surface, a
// loop helper's entry and every call to one, an align, the bytes of a string,
// and the arms of a branch or a union case.
func (g *gen) rdBreak() {
	g.rdOff, g.rdAvail, g.rdRun = 0, 0, 0
}

// throwIf emits a one-line refusal: `if cond, do: throw(:invalid)`.
func (g *gen) throwIf(cond, why, ind string) {
	if why != "" {
		g.pf("%s# %s\n", ind, why)
	}
	g.pf("%sif %s, do: throw(:invalid)\n", ind, cond)
}

// raiseIf emits a write-contract check: a block if raising ArgumentError —
// always on (the BEAM has no compile-out assert), O(1) by construction.
func (g *gen) raiseIf(cond, msg, ind string) {
	g.pf("\n")
	g.pf("%sif %s do\n", ind, cond)
	g.pf("%s  raise ArgumentError, \"%s\"\n", ind, msg)
	g.pf("%send\n", ind)
	g.pf("\n")
}

// callAssign emits `lhs = call`, broken after the = (and blank-padded) when
// the one-line form passes the line limit — mix format's own break.
func (g *gen) callAssign(lhs, call, ind string) {
	one := ind + lhs + " = " + call
	if len(one) <= formatWidth {
		g.pf("%s\n", one)
		return
	}
	g.pf("\n%s%s =\n%s  %s\n\n", ind, lhs, ind, call)
}

// condAssign emits `lhs = if cond, do: a, else: b`, in the formatter's block
// form when the one-line form passes the line limit.
func (g *gen) condAssign(lhs, cond, a, b, ind string) {
	one := fmt.Sprintf("%s%s = if %s, do: %s, else: %s", ind, lhs, cond, a, b)
	if len(one) <= formatWidth {
		g.pf("%s\n", one)
		return
	}
	g.pf("\n")
	g.pf("%s%s =\n", ind, lhs)
	g.pf("%s  if %s do\n", ind, cond)
	g.pf("%s    %s\n", ind, a)
	g.pf("%s  else\n", ind)
	g.pf("%s    %s\n", ind, b)
	g.pf("%s  end\n", ind)
	g.pf("\n")
}

// ---- static wire-width analysis for run fusing (the Dart backend's) ----

// staticBitsItem reports an item's exact wire bits when they are the same on
// every path (branches count only when both sides agree; strings, counted
// arrays, unions with any nonzero arm, and align are dynamic). Exactness
// matters: the fused read bound `bits_read + run > num_bits` must never
// overshoot a valid stream.
func (g *gen) staticBitsItem(item ir.Item) (int64, bool) {
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

func (g *gen) staticBitsItems(items []ir.Item) (int64, bool) {
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

func (g *gen) staticBitsField(f *ir.Field) (int64, bool) {
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

func (g *gen) staticBitsScalar(f *ir.Field) (int64, bool) {
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		return 0, false
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			return g.staticBitsItems(ref.Items)
		case *ir.Union:
			// a union's wire is tag + SELECTED arm — static only in the
			// degenerate no-variant case (zero bits); counting MaxBits here
			// would let a fused read bound refuse valid wire carrying a
			// smaller arm
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

func (g *gen) emitStructFunctions(st *ir.Struct) {
	maxBits := ir.MaxBitsStruct(st)
	snake := ir.RustSnake(st.Name)
	g.bpf("  # %s_max_bits is the longest wire path; align pads at worst case (SPEC §6.1).\n", snake)
	g.bpf("  # %s_max_bytes is rounded up to the family 8-byte write-buffer granularity.\n", snake)
	g.bpf("  def %s_max_bits, do: %s\n", snake, intLit64(maxBits))
	g.bpf("  def %s_max_bytes, do: %s\n\n", snake, intLit64(ir.MaxBytes(maxBits)))

	g.emitZeroFunction(st)
	g.emitWriteFunction(st.Name, st.Items)
	g.emitReadFunction(st.Name, st, st.Items)
	g.emitMeasureFunction(st.Name, st.Items)
}

// emitUnionFunctions emits the union's bounds and the same function surface
// as a type: the tag rides in minimal bits, then the selected arm only
// (SPEC §4.8).
func (g *gen) emitUnionFunctions(d *ir.Union) {
	maxBits := ir.MaxBitsUnion(d)
	snake := ir.RustSnake(d.Name)
	g.bpf("  # %s_max_bits is the tag plus the largest arm; None costs the tag only (SPEC §4.8).\n", snake)
	g.bpf("  # %s_max_bytes is rounded up to the family 8-byte write-buffer granularity.\n", snake)
	g.bpf("  def %s_max_bits, do: %s\n", snake, intLit64(maxBits))
	g.bpf("  def %s_max_bytes, do: %s\n\n", snake, intLit64(ir.MaxBytes(maxBits)))

	g.bpf("  # The §5 zero form — the empty union (None). Arms hold their construction\n")
	g.bpf("  # form: every arm is unselected at None, and unselected arms are\n")
	g.bpf("  # unspecified by rule (SPEC §4.8).\n")
	g.bpf("  def zero_%s, do: %%%s{}\n\n", snake, g.mod(d.Name))

	item := unionItem(d)
	g.emitWriteFunction(d.Name, []ir.Item{item})
	g.emitReadFunction(d.Name, nil, []ir.Item{item})
	g.emitMeasureFunction(d.Name, []ir.Item{item})
}

// unionItem wraps a union as a self-typed field item so the standalone union
// functions reuse the field emission with path "value" — the same bodies a
// union field inlines.
func unionItem(d *ir.Union) ir.Item {
	return &ir.FieldItem{F: &ir.Field{Name: "", Type: ir.FieldType{
		Kind: ir.TNamed, Name: d.Name, Ref: d,
	}}}
}

// emitZeroFunction emits the §5 zero form for a type — all-zero storage,
// specified defaults NOT reapplied (those live in construction only; the
// wire contract stays a pure function of the encodings).
func (g *gen) emitZeroFunction(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	g.bpf("  # The §5 zero form: all-zero storage; specified defaults live only in\n")
	g.bpf("  # construction (%%%s{}).\n", g.mod(st.Name))
	var fields []string
	for _, f := range st.Fields {
		fields = append(fields, fmt.Sprintf("%s: %s", elixirName(f.Name), g.zeroValue(f)))
	}
	lit := "%" + g.mod(st.Name) + "{" + strings.Join(fields, ", ") + "}"
	if len("  def zero_"+snake+", do: "+lit) <= formatWidth {
		g.bpf("  def zero_%s, do: %s\n\n", snake, lit)
		return
	}
	g.bpf("  def zero_%s do\n", snake)
	g.structLit(&g.body, "    ", "", st.Name, fields)
	g.bpf("  end\n\n")
}

// structLit writes `<ind><lead>%Mod{fields}` to w in the formatter's
// join-or-break form: one line when it fits, otherwise the literal opens on
// the lead line, holds one entry per line, and closes at <ind>} (mix
// format's own map-literal break).
func (g *gen) structLit(w *strings.Builder, ind, lead, typeName string, fields []string) {
	lit := "%" + g.mod(typeName) + "{" + strings.Join(fields, ", ") + "}"
	if len(ind+lead+lit) <= formatWidth {
		fmt.Fprintf(w, "%s%s%s\n", ind, lead, lit)
		return
	}
	fmt.Fprintf(w, "\n%s%s%%%s{\n", ind, lead, g.mod(typeName))
	for i, f := range fields {
		sep := ","
		if i == len(fields)-1 {
			sep = ""
		}
		fmt.Fprintf(w, "%s  %s%s\n", ind, f, sep)
	}
	fmt.Fprintf(w, "%s}\n\n", ind)
}

// zeroValue is a field's §5 zero form as an expression.
func (g *gen) zeroValue(f *ir.Field) string {
	switch f.Array {
	case ir.ArrayCounted:
		return "[]"
	case ir.ArrayFixed:
		return fmt.Sprintf("List.duplicate(%s, %d)", g.zeroScalar(f.Type), f.ArrayBound)
	}
	return g.zeroScalar(f.Type)
}

func (g *gen) zeroScalar(t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "false"
	case ir.TFloat32, ir.TFloat64:
		return "0.0"
	case ir.TString, ir.TBytes:
		return "<<>>"
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Struct:
			return g.zeroCall(t.Name)
		case *ir.Union:
			// zero IS None; arms are unspecified at None (SPEC §4.8)
			return fmt.Sprintf("%%%s{}", g.mod(t.Name))
		}
	}
	return "0"
}

// zeroCall is the zero_* call for a named struct — bare in its declaring
// file's module, qualified from any other file.
func (g *gen) zeroCall(typeName string) string {
	base := g.unit.DeclFile[typeName]
	call := "zero_" + ir.RustSnake(typeName) + "()"
	if base == g.file.Base {
		return call
	}
	return g.mod(ir.GoExportName(base)) + "." + call
}

func (g *gen) emitWriteFunction(name string, items []ir.Item) {
	g.usesImport = true
	snake := ir.RustSnake(name)
	g.fn.Reset()
	g.pendW, g.segW, g.segN = 0, nil, 0
	// the surface starts at a known empty scratch: everything up to the
	// first length the message's own data decides packs at literal offsets
	g.sbKnown, g.sbVal, g.scZero = true, 0, true
	g.scBound, g.sbBound = false, false
	g.bindW, g.bindDisp, g.bindUsed = nil, nil, map[string]bool{}
	g.withOwner(name, func() {
		g.emitWriteItems(items, "value", "    ")
		g.flushW("    ") // the tail is a barrier: the residual is < 8 bits
		// the return reads what it is about to test or append
		if !g.sbKnown {
			g.ensureScratch("    ", true)
		} else if g.sbVal != 0 {
			g.ensureScratch("    ", false)
		}
	})
	body := g.fn.String()

	g.bpf("  # write_%s packs value into a fresh binary — the trusted writer; the O(1)\n", snake)
	g.bpf("  # contract checks raise ArgumentError, always on (the BEAM has no\n")
	g.bpf("  # compile-out assert). Returns the wire bytes.\n")
	param := "value"
	if !strings.Contains(body, "value") {
		param = "_value"
	}
	g.bpf("  def write_%s(%s) do\n", snake, param)
	g.bpf("    data = <<>>\n")
	if body == "" {
		g.bpf("    # empty body — presence is the payload (SPEC §4.6)\n")
	}
	g.body.WriteString(body)
	switch {
	case !g.sbKnown:
		g.bpf("    if scratch_bits != 0, do: <<data::binary, scratch>>, else: data\n")
	case g.sbVal != 0:
		g.bpf("    # the residual byte, statically known to be there\n")
		g.bpf("    <<data::binary, scratch>>\n")
	default:
		g.bpf("    data\n")
	}
	g.bpf("  end\n\n")
}

// emitReadFunction emits read_<name>(data, num_bits) -> {:ok, value} |
// :error. st is the struct whose storage the locals rebuild; nil for the
// standalone union surface (the body's one item binds local v instead).
func (g *gen) emitReadFunction(name string, st *ir.Struct, items []ir.Item) {
	g.usesImport = true
	snake := ir.RustSnake(name)
	g.fn.Reset()
	g.rdBreak()
	g.withOwner(name, func() { g.emitReadItems(items, "v", "      ", false) })
	g.pf("      # the final position is unobserved — the verdict and value are the surface\n")
	g.pf("      _ = bits_read\n")
	if st != nil {
		var fields []string
		for _, f := range st.Fields {
			fields = append(fields, fmt.Sprintf("%s: %s", elixirName(f.Name), local("v", f)))
		}
		if len(fields) == 0 {
			g.pf("      {:ok, %%%s{}}\n", g.mod(name))
		} else {
			g.structLit(&g.fn, "      ", "value = ", name, fields)
			g.pf("      {:ok, value}\n")
		}
	} else {
		g.pf("      {:ok, v}\n")
	}
	body := g.fn.String()

	g.bpf("  # read_%s decodes the first num_bits of data — the family read verdict:\n", snake)
	g.bpf("  # :error rejects the wire (bounds, ranges, wire constants, padding);\n")
	g.bpf("  # hostile bytes never raise. No slack past the payload is required.\n")
	g.bpf("  def read_%s(data, num_bits) when is_binary(data) and is_integer(num_bits) do\n", snake)
	g.bpf("    try do\n")
	g.bpf("      if num_bits > byte_size(data) * 8 do\n")
	g.bpf("        # the payload cannot exceed the buffer behind it\n")
	g.bpf("        throw(:invalid)\n")
	g.bpf("      end\n\n")
	g.bpf("      bits_read = 0\n")
	g.body.WriteString(body)
	g.bpf("    catch\n")
	g.bpf("      :invalid -> :error\n")
	g.bpf("    end\n")
	g.bpf("  end\n\n")
}

// local is the read-body local bound for a field under prefix pre.
func local(pre string, f *ir.Field) string {
	if f.Name == "" {
		return pre // the standalone union functions' self item
	}
	return pre + "_" + elixirName(f.Name)
}

// ---- write emission ----

// wref is a write-side field reference: the local the scope's one map read
// bound, or the plain dotted access when the scope did not bind it.
func (g *gen) wref(path, field string) string {
	key := path + "." + elixirName(field)
	if l, ok := g.bindW[key]; ok {
		return l
	}
	return key
}

// dsp renders an expression the way the SCHEMA reads it, for the text of a
// contract raise: a scope local resolves back to the dotted access it stands
// for, so `%{delta: e_delta} = e` does not turn "e.delta is above the wire
// maximum" into a message naming a local the caller never wrote.
func (g *gen) dsp(expr string) string {
	head, rest := expr, ""
	if i := strings.Index(expr, "."); i >= 0 {
		head, rest = expr[:i], expr[i:]
	}
	if d, ok := g.bindDisp[head]; ok {
		return d + rest
	}
	return expr
}

// scopeBinds lists the fields a scope reads UNCONDITIONALLY at its own level:
// its field items and the conditions of its branches. A branch arm's fields
// are NOT here — an arm's own scope binds them when the arm is taken, so a
// value the wire never asks for is never demanded of the caller.
func scopeBinds(items []ir.Item) []string {
	var out []string
	seen := map[string]bool{}
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			add(elixirName(item.F.Name))
		case *ir.Branch:
			add(elixirName(item.Cond))
		}
	}
	return out
}

// bindScope emits the scope's ONE map read — `%{a: p_a, b: p_b} = path` —
// and registers the locals. Elixir's `.` is a get_map_elements of its own
// with its own raise branch per field; one pattern is one instruction for
// the whole scope. Below two fields it would not pay for itself.
//
// The bound locals are unconditional, which is exactly what the dotted
// accesses they replace were: a struct always carries every key, so the same
// inputs are refused. A map missing a key raises MatchError here where it
// raised KeyError before — a raise either way, never a wrong answer.
func (g *gen) bindScope(items []ir.Item, path, ind string) func() {
	saved, savedDisp := g.bindW, g.bindDisp
	names := scopeBinds(items)
	if len(names) < 2 {
		return func() {}
	}
	g.bindW = make(map[string]string, len(saved)+len(names))
	maps.Copy(g.bindW, saved)
	g.bindDisp = make(map[string]string, len(savedDisp)+len(names))
	maps.Copy(g.bindDisp, savedDisp)
	base := strings.NewReplacer(".", "_").Replace(path)
	shown := g.dsp(path)
	pairs := make([]string, 0, len(names))
	for _, n := range names {
		l := base + "_" + n
		for g.bindUsed[l] {
			l += "_"
		}
		g.bindUsed[l] = true
		g.bindW[path+"."+n] = l
		g.bindDisp[l] = shown + "." + n
		pairs = append(pairs, n+": "+l)
	}
	one := ind + "%{" + strings.Join(pairs, ", ") + "} = " + path
	if len(one) <= formatWidth {
		g.pf("%s\n", one)
	} else {
		g.pf("\n%s%%{\n", ind)
		for i, pr := range pairs {
			sep := ","
			if i == len(pairs)-1 {
				sep = ""
			}
			g.pf("%s  %s%s\n", ind, pr, sep)
		}
		g.pf("%s} = %s\n\n", ind, path)
	}
	return func() { g.bindW, g.bindDisp = saved, savedDisp }
}

func (g *gen) emitWriteItems(items []ir.Item, path, ind string) {
	defer g.bindScope(items, path, ind)()
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitWriteField(item.F, path, ind)
		case *ir.ConstItem:
			g.emitWriteRaw(item.Value, item.Bits, true, ind)
		case *ir.ReservedItem:
			g.emitWriteRaw(big.NewInt(0), item.Bits, false, ind)
		case *ir.AlignItem:
			g.emitWriteAlign(ind)
		case *ir.Branch:
			cond := g.wref(path, item.Cond)
			if item.Neg {
				cond = "not " + cond
			}
			// a branch joins two emission paths, so the group closes
			// before it and inside each arm: what leaves the if is one
			// invariant, not one per arm
			g.flushW(ind)
			entry := g.sbSave()
			thenText, thenEnd := g.captureArm(entry, func() {
				g.emitWriteItems(item.Then, path, ind+"    ")
				g.flushW(ind + "    ")
			})
			elseText, elseEnd := g.captureArm(entry, func() {
				if item.Else != nil {
					g.emitWriteItems(item.Else, path, ind+"    ")
					g.flushW(ind + "    ")
				}
			})
			g.emitWriteJoin(ind, fmt.Sprintf("  if %s do\n", cond), "  end\n",
				[]joinArm{
					{lead: "", body: thenText, tupleIn: ind + "    ", end: thenEnd},
					{lead: ind + "  else\n", body: elseText, tupleIn: ind + "    ", end: elseEnd},
				})
		}
	}
}

// emitWriteRaw merges a compile-time constant (const/reserved items) of up
// to 64 bits, split low dword first past 32 (the serialize group rule).
func (g *gen) emitWriteRaw(value *big.Int, bits int64, isConst bool, ind string) {
	what := "reserved bits ride as zeros (SPEC §4.3)"
	if isConst {
		what = fmt.Sprintf("const(0x%X, %d) rides the wire (SPEC §4.3)", value, bits)
	}
	g.pf("%s# %s\n", ind, what)
	masked := new(big.Int).And(value, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1)))
	if bits <= 32 {
		g.pf("%sv = %s\n", ind, intLit(masked))
		g.mergeW(bits, ind)
		return
	}
	lo := new(big.Int).And(masked, big.NewInt(0xffffffff))
	hi := new(big.Int).Rsh(masked, 32)
	g.pf("%sv = %s\n", ind, intLit(lo))
	g.mergeW(32, ind)
	g.pf("%sv = %s\n", ind, intLit(hi))
	g.mergeW(bits-32, ind)
}

// emitWriteAlign pads the write position to the next byte boundary with
// zeros: the partial byte (if any) flushes zero-padded, exactly the byte the
// merge's own flush would produce.
func (g *gen) emitWriteAlign(ind string) {
	g.flushW(ind) // align observes data: the group closes first
	g.pf("%s# align: zero-pad to the byte boundary (SPEC §4.3)\n", ind)
	switch {
	case !g.sbKnown:
		g.ensureScratch(ind, true)
		g.pf("%sdata = if scratch_bits != 0, do: <<data::binary, scratch>>, else: data\n", ind)
		// scratch_bits is NOT zeroed here, and neither is scratch: the
		// offset is static from this point, and whatever reads either of
		// them next publishes the zero it needs
	case g.sbVal != 0:
		g.ensureScratch(ind, false)
		g.pf("%sdata = <<data::binary, scratch>>\n", ind)
	default:
		g.pf("%s# already byte-aligned, and nothing is pending\n", ind)
	}
	// an align lands the position on a byte whatever the data did, so the
	// static offset is KNOWN again here even where it was not before, and
	// the scratch it leaves behind is empty
	g.sbKnown, g.sbVal, g.scZero, g.scBound = true, 0, true, false
}

func (g *gen) emitWriteField(f *ir.Field, path, ind string) {
	name := g.wref(path, f.Name)
	if f.Name == "" {
		name = path // the standalone union functions' self item
	}
	switch f.Array {
	case ir.ArrayFixed:
		g.raiseIf(fmt.Sprintf("length(%s) != %s", name, intLit64(f.ArrayBound)),
			fmt.Sprintf("%s must hold exactly %d elements", g.dsp(name), f.ArrayBound), ind)
		helper := g.writeHelper(f)
		g.flushW(ind) // the helper opens its own group
		g.syncSB(ind) // and reads the offset the caller was tracking statically
		g.ensureScratch(ind, true)
		g.callAssign("{data, scratch, scratch_bits}",
			fmt.Sprintf("%s(%s, data, scratch, scratch_bits)", helper, name), ind)
		// how many elements rode is the message's business, so the offset
		// past the loop is the wire's, not the generator's
		g.sbKnown, g.scZero = false, false
		g.scBound, g.sbBound = true, true
	case ir.ArrayCounted:
		g.pf("%sn = length(%s)\n", ind, name)
		// length/1 is never negative, so a zero floor has no violable side
		if f.ArrayMin > 0 {
			g.raiseIf(fmt.Sprintf("n < %s", intLit64(f.ArrayMin)),
				fmt.Sprintf("%s count is below the wire minimum", g.dsp(name)), ind)
		}
		g.raiseIf(fmt.Sprintf("n > %s", intLit64(f.ArrayBound)),
			fmt.Sprintf("%s count is above the wire maximum", g.dsp(name)), ind)
		g.emitWriteOffset("n", big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), ind)
		helper := g.writeHelper(f)
		g.flushW(ind) // the helper opens its own group
		g.syncSB(ind) // and reads the offset the caller was tracking statically
		g.ensureScratch(ind, true)
		g.callAssign("{data, scratch, scratch_bits}",
			fmt.Sprintf("%s(%s, data, scratch, scratch_bits)", helper, name), ind)
		// how many elements rode is the message's business, so the offset
		// past the loop is the wire's, not the generator's
		g.sbKnown, g.scZero = false, false
		g.scBound, g.sbBound = true, true
	default:
		g.emitWriteScalar(f, name, ind)
	}
}

// helperName keys a loop helper by owner type and field.
func (g *gen) helperName(kind string, f *ir.Field) string {
	return kind + "_" + ir.RustSnake(g.helperOwner) + "_" + elixirName(f.Name)
}

// writeHelper materializes (once per file) the tail-recursive write loop for
// an array field and returns its name.
func (g *gen) writeHelper(f *ir.Field) string {
	name := g.helperName("w", f)
	if _, done := g.helpers[name]; done {
		return name
	}
	g.helpers[name] = "" // claim before recursing (helpers may nest)
	saved := g.fn
	savedSB := g.sbSave()
	savedBind, savedDisp, savedUsed := g.bindW, g.bindDisp, g.bindUsed
	g.fn = strings.Builder{}
	g.pendW, g.segW, g.segN = 0, nil, 0 // entered with the caller's group closed
	// and at an offset the caller's element count decides, so the helper's
	// body maintains scratch_bits the way it did before the static pass
	g.sbKnown, g.scZero = false, false
	g.scBound, g.sbBound = true, true // both arrive as parameters
	// a helper is its own function: its own locals, its own name space
	g.bindW, g.bindDisp, g.bindUsed = nil, nil, map[string]bool{}
	base := fmt.Sprintf("  defp %s([], data, scratch, scratch_bits), do: {data, scratch, scratch_bits}", name)
	if len(base) <= formatWidth {
		g.pf("%s\n\n", base)
	} else {
		g.pf("  defp %s([], data, scratch, scratch_bits),\n    do: {data, scratch, scratch_bits}\n\n", name)
	}
	// the wide clause first: whole elements share one group and therefore
	// one append. The single-element clause behind it is the remainder, so
	// the list length never has to divide anything.
	if k := g.writeUnroll(f); k > 1 {
		g.writeClause(f, name, k)
	}
	g.writeClause(f, name, 1)
	g.helpers[name] = g.fn.String()
	g.fn = saved
	g.sbRestore(savedSB)
	g.bindW, g.bindDisp, g.bindUsed = savedBind, savedDisp, savedUsed
	g.helperOrder = append(g.helperOrder, name)
	return name
}

// writeUnrollMax bounds the generated code one array field can cost. Past a
// handful of elements per clause the appends saved are a smaller and smaller
// share and the clause body is a bigger and bigger one.
const writeUnrollMax = 4

// writeUnroll is how many elements one clause of the loop writes. What the
// clause shares is ONE APPEND, and the append is the barrier's, not the merge
// group's: a clause that outgrows the 52-bit group spills into a register and
// keeps merging, and every register it spilled rides the tail's single binary
// construction as its own segment. So the group budget no longer bounds k —
// only the unroll cap does, and a stats clause of 18-bit elements takes four
// (72 bits, two segments, one append) where the group alone allowed two.
//
// An element whose width the wire decides still gets one clause per element:
// the clause's own tail is where it would have to be measured, and there is
// nothing static to add up.
func (g *gen) writeUnroll(f *ir.Field) int64 {
	eb, ok := g.staticBitsScalar(f)
	if !ok || eb <= 0 || eb > writeGroupBits {
		return 1
	}
	return writeUnrollMax
}

// writeClause emits one clause of a write loop, taking k elements off the
// list. The k element bodies merge into ONE group, so the clause appends
// once where k separate clauses appended k times.
func (g *gen) writeClause(f *ir.Field, name string, k int64) {
	evs := make([]string, k)
	for i := range evs {
		evs[i] = "e"
		if k > 1 {
			evs[i] = fmt.Sprintf("e%d", i+1)
		}
	}
	// a clause is its own scope: its own locals, and its own group, entered
	// at an offset the caller's element count decides
	g.bindW, g.bindDisp, g.bindUsed = nil, map[string]string{}, map[string]bool{}
	for _, ev := range evs {
		// the raise text names the ELEMENT, never the clause's slot for it
		g.bindDisp[ev] = "e"
	}
	g.pendW, g.segW, g.segN = 0, nil, 0
	g.sbKnown, g.scZero = false, false
	g.scBound, g.sbBound = true, true
	g.pf("  defp %s([%s | rest], data, scratch, scratch_bits) do\n", name, strings.Join(evs, ", "))
	for _, ev := range evs {
		g.emitWriteElem(f, ev, "    ")
	}
	g.flushW("    ") // the clause's tail is a barrier: the next entry is < 8 bits
	g.syncSB("    ") // an align inside an element can have made it static again
	g.pf("    %s(rest, data, scratch, scratch_bits)\n", name)
	g.pf("  end\n\n")
}

// emitWriteElem writes one array element bound to ev.
func (g *gen) emitWriteElem(f *ir.Field, ev, ind string) {
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			g.withOwner(ref.Name, func() { g.emitWriteItems(ref.Items, ev, ind) })
			return
		case *ir.Union:
			g.emitWriteUnion(ref, ev, ind)
			return
		}
	}
	g.emitWriteScalar(f, ev, ind)
}

// withOwner runs fn with the helper-naming owner set to typeName (the type
// whose items are being inlined), restoring the caller's owner after.
func (g *gen) withOwner(typeName string, fn func()) {
	saved := g.helperOwner
	g.helperOwner = typeName
	fn()
	g.helperOwner = saved
}

// emitCheckRange emits the write contract for an integer expression in
// [min, max]: one block if per side, each condition and message short enough
// that no line outgrows the formatter at any nesting depth. No side is ever
// vacuous — a BEAM integer has no storage domain to elide against.
func (g *gen) emitCheckRange(expr, what string, min, max *big.Int, ind string) {
	g.raiseIf(fmt.Sprintf("%s < %s", expr, intLit(min)),
		fmt.Sprintf("%s is below the wire minimum", what), ind)
	g.raiseIf(fmt.Sprintf("%s > %s", expr, intLit(max)),
		fmt.Sprintf("%s is above the wire maximum", what), ind)
}

// emitWriteOffset merges (expr - min) in the folded bit count for the range:
// 32-bit groups, least significant first — the serialize group structure.
// The always-on contract check has already bounded expr, so the offset needs
// no width mask (a masked-but-unchecked value cannot exist here).
func (g *gen) emitWriteOffset(expr string, min, max *big.Int, ind string) {
	bits := ir.BitsRequired(min, max)
	if bits == 0 {
		return // degenerate range: ZERO bits — the check is the whole write
	}
	off := expr
	switch {
	case min.Sign() > 0:
		off = fmt.Sprintf("%s - %s", expr, intLit(min))
	case min.Sign() < 0:
		off = fmt.Sprintf("%s + %s", expr, intLit(new(big.Int).Neg(min)))
	}
	if bits <= 32 {
		g.pf("%sv = %s\n", ind, off)
		g.mergeW(bits, ind)
		return
	}
	if min.Sign() != 0 {
		g.pf("%sw = %s\n", ind, off)
		g.emitWriteWide("w", bits, ind)
		return
	}
	g.emitWriteWide(expr, bits, ind)
}

// emitWriteWide merges the low `bits` bits (33..128) of a non-negative
// integer expression under 2^bits as 32-bit groups, least significant
// first. The final group needs no mask — the value's own bound is its mask.
func (g *gen) emitWriteWide(expr string, bits int64, ind string) {
	for done := int64(0); done < bits; done += 32 {
		group := min(32, bits-done)
		switch {
		case done == 0:
			g.pf("%sv = %s &&& 0xFFFFFFFF\n", ind, expr)
		case done+group < bits:
			g.pf("%sv = %s >>> %d &&& 0xFFFFFFFF\n", ind, expr, done)
		default:
			g.pf("%sv = %s >>> %d\n", ind, expr, done)
		}
		g.mergeW(group, ind)
	}
}

// emitWriteWideRaw merges the low `bits` bits of a possibly-negative bare
// integer as 32-bit groups — the mask folds the sign into the same-width
// two's-complement pattern, exactly the family's bare-integer wire.
func (g *gen) emitWriteWideRaw(expr string, bits int64, ind string) {
	for done := int64(0); done < bits; done += 32 {
		group := min(32, bits-done)
		if done == 0 {
			g.pf("%sv = %s &&& %s\n", ind, expr, maskHex(group))
		} else {
			g.pf("%sv = %s >>> %d &&& %s\n", ind, expr, done, maskHex(group))
		}
		g.mergeW(group, ind)
	}
}

func (g *gen) emitWriteScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		g.emitWriteFixed(f, name, ind)
	case ir.TInt:
		g.emitWriteInt(f, name, ind)
	case ir.TBits:
		g.emitWriteWideRaw(name, int64(f.Type.Width), ind)
	case ir.TBool:
		g.pf("%sv = if %s, do: 1, else: 0\n", ind, name)
		g.mergeW(1, ind)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.emitWriteCompressedFloat(f, name, ind)
			return
		}
		g.needF32 = true
		g.pf("%sv = f32_bits(%s)\n", ind, name)
		g.mergeW(32, ind)
	case ir.TFloat64:
		g.needF64 = true
		g.pf("%sw = f64_bits(%s)\n", ind, name)
		g.emitWriteWide("w", 64, ind)
	case ir.TString, ir.TBytes:
		g.emitWriteBytesField(f, name, ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.emitCheckRange(name, g.dsp(name), big.NewInt(0), big.NewInt(ref.Max), ind)
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			if bits == 0 {
				return // degenerate [0, 0]: zero bits; the check still rides
			}
			g.pf("%sv = %s\n", ind, name)
			g.mergeW(bits, ind)
		case *ir.Flags:
			wb := int64(ref.WireBits)
			if wb < 64 {
				// a mask bit above the wire width cannot ride
				g.raiseIf(fmt.Sprintf("%s >>> %d != 0", name, wb),
					fmt.Sprintf("%s holds a mask bit above the %d-bit wire", g.dsp(name), wb), ind)
			} else {
				g.raiseIf(fmt.Sprintf("%s < 0 or %s >>> 64 != 0", name, name),
					fmt.Sprintf("%s holds a mask bit above the 64-bit wire", g.dsp(name)), ind)
			}
			if wb <= 32 {
				g.pf("%sv = %s\n", ind, name)
				g.mergeW(wb, ind)
				return
			}
			g.emitWriteWide(name, wb, ind)
		case *ir.Struct:
			g.withOwner(ref.Name, func() { g.emitWriteItems(ref.Items, name, ind) })
		case *ir.Union:
			g.emitWriteUnion(ref, name, ind)
		}
	}
}

// fixedRaw derives a fixed field's raw wire parameters: the whole-unit
// bounds scaled by F, and the offset bit count.
func fixedRaw(f *ir.Field) (rawMin, rawMax *big.Int, bits int64) {
	fb := uint(f.Type.FracBits)
	rawMin = shiftedRaw(f.IntMin, fb)
	rawMax = shiftedRaw(f.IntMax, fb)
	bits = int64(new(big.Int).Sub(rawMax, rawMin).BitLen())
	return rawMin, rawMax, bits
}

// shiftedRaw is a fixed-point whole-unit value scaled to raw storage:
// v << F, with negative values negated around the shift so the arithmetic is
// exact (the same derivation the wire bounds use).
func shiftedRaw(v *big.Int, fracBits uint) *big.Int {
	if v.Sign() < 0 {
		return new(big.Int).Neg(new(big.Int).Lsh(new(big.Int).Neg(v), fracBits))
	}
	return new(big.Int).Lsh(v, fracBits)
}

func (g *gen) emitWriteFixed(f *ir.Field, name, ind string) {
	rawMin, rawMax, _ := fixedRaw(f)
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// degenerate: zero bits — the check is the whole write (SPEC §4.6)
		g.raiseIf(fmt.Sprintf("%s != %s", name, intLit(rawMin)),
			fmt.Sprintf("%s must hold the locked value %s", g.dsp(name), rawMin), ind)
		return
	}
	g.emitCheckRange(name, g.dsp(name), rawMin, rawMax, ind)
	g.emitWriteOffset(name, rawMin, rawMax, ind)
}

func (g *gen) emitWriteInt(f *ir.Field, name, ind string) {
	w := int64(f.Type.Width)
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: ZERO bits — the check is the whole write
			g.raiseIf(fmt.Sprintf("%s != %s", name, intLit(f.IntMin)),
				fmt.Sprintf("%s must hold the locked value %s", g.dsp(name), f.IntMin), ind)
			return
		}
		g.emitCheckRange(name, g.dsp(name), f.IntMin, f.IntMax, ind)
		g.emitWriteOffset(name, f.IntMin, f.IntMax, ind)
		return
	}
	// bare integer at storage width; signed values mask to the same-width
	// two's-complement pattern (the family's bare-integer wire)
	g.emitWriteWideRaw(name, w, ind)
}

// f32lit renders the float32 rounding of v as an Elixir float literal
// (exact — f32 is a subset of f64).
func f32lit(v float64) string {
	return formatFloat32(float64(float32(v)))
}

// emitWriteCompressedFloat is the family's two-rounding float32 quantization
// with the declaration folded (SPEC §4.3), through the port's own
// quantizer — every step float32, overflow clamping, the normative integer
// clamp.
func (g *gen) emitWriteCompressedFloat(f *ir.Field, name, ind string) {
	g.needCfQ = true
	maxInt, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	minF := float32(f.FMin)
	deltaF := float32(f.FMax) - minF
	g.pf("%sv = cf_quantize(%s, %s, %s, %s, %s)\n", ind, name,
		f32lit(float64(minF)), f32lit(float64(deltaF)), intLitU64(maxInt), f32lit(float64(maxInt)))
	g.mergeW(bits, ind)
}

// emitWriteBytesField writes string(N)/bytes(N): checked ranged length,
// align (zero pad), then the used bytes appended whole — the classic
// serialize_string framing, byte-identical to every other target's.
func (g *gen) emitWriteBytesField(f *ir.Field, name, ind string) {
	g.raiseIf(fmt.Sprintf("byte_size(%s) > %d", name, f.Type.Size),
		fmt.Sprintf("%s longer than the declared %d-byte bound", g.dsp(name), f.Type.Size), ind)
	lenBits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
	g.pf("%sv = byte_size(%s)\n", ind, name)
	g.mergeW(lenBits, ind)
	g.emitWriteAlign(ind)
	g.pf("%s# the used bytes ride whole — the stream is byte-aligned here\n", ind)
	g.pf("%sdata = <<data::binary, %s::binary>>\n", ind, name)
}

// emitWriteUnion inlines a union (SPEC §4.8): the tag contract checked
// BEFORE it rides, the tag in minimal bits, then a case inlines each arm's
// items — the struct-inlining move, per arm.
func (g *gen) emitWriteUnion(u *ir.Union, expr, ind string) {
	// the tag validates BEFORE it rides (SPEC §4.8)
	g.emitCheckRange(expr+".type", g.dsp(expr)+".type", big.NewInt(0), big.NewInt(u.Max), ind)
	if u.Max == 0 {
		return // an empty union's degenerate tag range [0, 0] costs zero bits
	}
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	g.pf("%sv = %s.type\n", ind, expr)
	g.mergeW(bits, ind)
	// the case joins every arm, so the group closes before it and inside
	// each arm: what leaves the case is one invariant, not one per arm
	g.flushW(ind)
	entry := g.sbSave()
	var arms []joinArm
	for i, vr := range u.Variants {
		body, end := g.captureArm(entry, func() {
			if len(vr.Ref.Items) == 0 {
				g.pf("%s      # empty arm — presence is the payload (SPEC §4.6)\n", ind)
				return
			}
			g.withOwner(vr.Type, func() {
				g.emitWriteItems(vr.Ref.Items, expr+"."+elixirName(vr.Name), ind+"      ")
				g.flushW(ind + "      ")
			})
		})
		arms = append(arms, joinArm{
			lead: fmt.Sprintf("%s    %d ->\n", ind, i+1), body: body,
			tupleIn: ind + "      ", trail: "\n", end: end,
		})
	}
	none := fmt.Sprintf("%s      # None — the tag is the whole wire (SPEC §4.8)\n", ind)
	arms = append(arms, joinArm{
		lead: ind + "    _ ->\n", body: none, tupleIn: ind + "      ", end: entry,
	})
	g.emitWriteJoin(ind, fmt.Sprintf("  case %s.type do\n", expr), "  end\n", arms)
}

// ---- read emission ----

// emitReadItems walks a scope with run fusing: one bounds check covers each
// maximal run of statically-sized items (bounded=true suppresses checks —
// an enclosing scope already proved the bits).
func (g *gen) emitReadItems(items []ir.Item, pre, ind string, bounded bool) {
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
				g.throwIf(fmt.Sprintf("bits_read + %s > num_bits", intLit64(total)), "", ind)
			}
			// the run's own length sizes the read groups inside it —
			// unless a WIDER run is already open around this scope, as it
			// is inside an unrolled loop clause, where one window serves
			// every element of the clause
			outer := g.rdRun > 0
			if !outer {
				g.rdRun = total
			}
			for ; i < j; i++ {
				g.emitReadItem(items[i], pre, ind, true)
			}
			if !outer {
				g.rdRun = 0
			}
			continue
		}
		g.rdRun = 0
		g.emitReadItem(items[i], pre, ind, false)
		i++
	}
}

func (g *gen) emitReadItem(item ir.Item, pre, ind string, bounded bool) {
	switch item := item.(type) {
	case *ir.FieldItem:
		g.emitReadField(item.F, pre, ind, bounded)
	case *ir.ConstItem:
		g.emitReadRaw(item.Value, item.Bits, true, ind)
	case *ir.ReservedItem:
		g.emitReadRaw(big.NewInt(0), item.Bits, false, ind)
	case *ir.AlignItem:
		g.emitReadAlign(ind)
	case *ir.Branch:
		g.emitReadBranch(item, pre, ind, bounded)
	}
}

// emitReadBranch reads one wire branch: the taken side decodes, the untaken
// side's storage binds its §5 zero form, and the arm returns bits_read plus
// every local either side declares (an Elixir conditional's bindings live
// only inside it — the returned tuple is the one way out).
func (g *gen) emitReadBranch(item *ir.Branch, pre, ind string, bounded bool) {
	cond := pre + "_" + elixirName(item.Cond)
	if item.Neg {
		cond = "not " + cond
	}
	locals := append(g.collectLocals(item.Then, pre), g.collectLocals(item.Else, pre)...)
	tuple := "{bits_read, " + strings.Join(locals, ", ") + "}"
	g.pf("\n")
	g.pf("%s%s =\n", ind, tuple)
	g.pf("%s  if %s do\n", ind, cond)
	g.rdBreak()
	g.emitReadItems(item.Then, pre, ind+"    ", bounded)
	g.emitZeroLocals(item.Else, pre, ind+"    ")
	g.pf("%s    %s\n", ind, tuple)
	g.pf("%s  else\n", ind)
	g.rdBreak()
	if item.Else != nil {
		g.emitReadItems(item.Else, pre, ind+"    ", bounded)
	}
	g.emitZeroLocals(item.Then, pre, ind+"    ")
	g.pf("%s    %s\n", ind, tuple)
	g.pf("%s  end\n", ind)
	g.pf("\n")
	g.rdBreak()
}

// collectLocals lists the locals an item subtree binds, in declaration order.
func (g *gen) collectLocals(items []ir.Item, pre string) []string {
	var out []string
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			out = append(out, local(pre, item.F))
		case *ir.Branch:
			out = append(out, g.collectLocals(item.Then, pre)...)
			out = append(out, g.collectLocals(item.Else, pre)...)
		}
	}
	return out
}

// emitZeroLocals binds the §5 zero form for every field local of an untaken
// branch side.
func (g *gen) emitZeroLocals(items []ir.Item, pre, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.pf("%s%s = %s\n", ind, local(pre, item.F), g.zeroValue(item.F))
		case *ir.Branch:
			g.emitZeroLocals(item.Then, pre, ind)
			g.emitZeroLocals(item.Else, pre, ind)
		}
	}
}

// emitReadAlign verifies zero padding to the byte boundary and advances.
func (g *gen) emitReadAlign(ind string) {
	g.needRd = true
	g.pf("%spad = 8 - (bits_read &&& 7) &&& 7\n", ind)
	g.pf("\n")
	g.pf("%sif pad != 0 do\n", ind)
	g.pf("%s  if bits_read + pad > num_bits do\n", ind)
	g.pf("%s    throw(:invalid)\n", ind)
	g.pf("%s  end\n", ind)
	g.pf("\n")
	g.pf("%s  if rd(data, bits_read, pad) != 0 do\n", ind)
	g.pf("%s    # nonzero padding is refused (SPEC §4.3)\n", ind)
	g.pf("%s    throw(:invalid)\n", ind)
	g.pf("%s  end\n", ind)
	g.pf("%send\n", ind)
	g.pf("\n")
	g.pf("%sbits_read = bits_read + pad\n", ind)
	g.rdBreak()
}

// emitReadRaw reads a const/reserved item and rejects any other value.
func (g *gen) emitReadRaw(value *big.Int, bits int64, isConst bool, ind string) {
	what := "reserved bits must read zero (SPEC §4.3)"
	if isConst {
		what = "a read rejects any other value (SPEC §4.3)"
	}
	masked := new(big.Int).And(value, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1)))
	if bits <= 32 {
		g.readR(bits, ind)
		g.throwIf(fmt.Sprintf("v != %s", intLit(masked)), what, ind)
		return
	}
	g.emitReadWide(bits, ind)
	g.throwIf(fmt.Sprintf("w != %s", intLit(masked)), what, ind)
}

// emitReadWide assembles a 33..128-bit group sequence into w (32-bit groups,
// least significant first) — one plain integer; BEAM integers carry any
// width, so the (hi, lo) pair emulation of bounded-word targets never
// arises.
func (g *gen) emitReadWide(bits int64, ind string) {
	for done := int64(0); done < bits; done += 32 {
		group := min(32, bits-done)
		g.readR(group, ind)
		if done == 0 {
			g.pf("%sw = v\n", ind)
		} else {
			g.pf("%sw = w ||| v <<< %d\n", ind, done)
		}
	}
}

func (g *gen) emitReadField(f *ir.Field, pre, ind string, bounded bool) {
	lv := local(pre, f)
	switch f.Array {
	case ir.ArrayFixed:
		helper := g.readHelper(f, true)
		g.rdBreak()
		g.callAssign(fmt.Sprintf("{bits_read, %s}", lv),
			fmt.Sprintf("%s(%s, [], data, num_bits, bits_read)", helper, intLit64(f.ArrayBound)), ind)
	case ir.ArrayCounted:
		g.emitReadCounted(f, lv, ind)
	default:
		g.emitReadScalar(f, lv, ind, bounded)
	}
}

func (g *gen) emitReadCounted(f *ir.Field, lv, ind string) {
	countBits := ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound))
	if countBits > 0 {
		g.throwIf(fmt.Sprintf("bits_read + %s > num_bits", intLit64(countBits)), "", ind)
		g.readR(countBits, ind)
		diff := f.ArrayBound - f.ArrayMin
		if diff != (int64(1)<<countBits)-1 {
			g.throwIf(fmt.Sprintf("v > %s", intLit64(diff)), "the count guards the loop — reject, never clamp", ind)
		}
		if f.ArrayMin == 0 {
			g.pf("%sn = v\n", ind)
		} else {
			g.pf("%sn = v + %s\n", ind, intLit64(f.ArrayMin))
		}
	} else {
		g.pf("%sn = %s\n", ind, intLit64(f.ArrayMin))
	}
	if elemBits, ok := g.staticBitsScalar(f); ok {
		if elemBits > 0 {
			g.throwIf(fmt.Sprintf("bits_read + n * %s > num_bits", intLit64(elemBits)), "", ind)
		}
		helper := g.readHelper(f, true)
		g.rdBreak()
		g.callAssign(fmt.Sprintf("{bits_read, %s}", lv),
			fmt.Sprintf("%s(n, [], data, num_bits, bits_read)", helper), ind)
		return
	}
	helper := g.readHelper(f, false)
	g.rdBreak()
	g.callAssign(fmt.Sprintf("{bits_read, %s}", lv),
		fmt.Sprintf("%s(n, [], data, num_bits, bits_read)", helper), ind)
}

// readHelper materializes (once per file) the tail-recursive read loop for
// an array field. bounded elements skip per-element bounds checks — the call
// site proved the whole span (fixed static arrays sit inside fused runs;
// counted static arrays check count * elem at the site).
func (g *gen) readHelper(f *ir.Field, bounded bool) string {
	name := g.helperName("r", f)
	if _, done := g.helpers[name]; done {
		return g.readEntry(f, name)
	}
	g.helpers[name] = ""
	saved := g.fn
	savedOff, savedAvail, savedRun := g.rdOff, g.rdAvail, g.rdRun
	g.fn = strings.Builder{}
	g.rdBreak() // the element body opens its own group
	// the counter is `remaining`, never `n` — a nested counted array's count
	// local would shadow it
	base := fmt.Sprintf("  defp %s(0, acc, _data, _num_bits, bits_read), do: {bits_read, Enum.reverse(acc)}", name)
	if len(base) <= formatWidth {
		g.pf("%s\n\n", base)
	} else {
		g.pf("  defp %s(0, acc, _data, _num_bits, bits_read),\n    do: {bits_read, Enum.reverse(acc)}\n\n", name)
	}
	// the wide clause first, guarded on the count; the single-element clause
	// behind it is the remainder, so the count never has to divide anything
	if k := g.readUnroll(f); k > 1 {
		g.readClause(f, name, k, bounded)
	}
	g.readClause(f, name, 1, bounded)
	if m, ok := g.fastReadPlan(f); ok {
		g.fastReadClauses(f, name, m)
	}
	g.helpers[name] = g.fn.String()
	g.fn = saved
	g.rdOff, g.rdAvail, g.rdRun = savedOff, savedAvail, savedRun
	g.helperOrder = append(g.helperOrder, name)
	return g.readEntry(f, name)
}

// readEntry is the loop entry a call site invokes: the aligning entry of the
// single-match-context fast path when the element qualifies for one, the
// scalar loop itself otherwise.
func (g *gen) readEntry(f *ir.Field, name string) string {
	if _, ok := g.fastReadPlan(f); ok {
		return name + "_align"
	}
	return name
}

// readUnrollMax bounds the generated code one array field can cost, the same
// way writeUnrollMax does.
const readUnrollMax = 4

// readUnroll is how many elements one clause of the read loop decodes.
// Elements of a statically known width share ONE window decode while the
// wide window's usable width holds — the match context, not the shift and
// mask, is what a decode costs.
func (g *gen) readUnroll(f *ir.Field) int64 {
	eb, ok := g.staticBitsScalar(f)
	if !ok || eb <= 0 || eb > rdwWindowBits {
		return 1
	}
	return min(rdwWindowBits/eb, readUnrollMax)
}

// readClause emits one clause of a read loop, decoding k elements under one
// open window. The wide clause carries a guard on the count; the
// single-element clause behind it needs none.
func (g *gen) readClause(f *ir.Field, name string, k int64, bounded bool) {
	evs := make([]string, k)
	for i := range evs {
		evs[i] = "e"
		if k > 1 {
			evs[i] = fmt.Sprintf("e%d", i+1)
		}
	}
	g.rdBreak() // a clause opens its own group
	if k > 1 {
		g.pf("  defp %s(remaining, acc, data, num_bits, bits_read)\n", name)
		g.pf("       when remaining >= %d do\n", k)
	} else {
		g.pf("  defp %s(remaining, acc, data, num_bits, bits_read) do\n", name)
	}
	// a SCALAR element goes straight to the scalar read, which never passes
	// through the run fuser, and a named element's own scope would size the
	// window to ONE element. The clause's whole span is the run either way,
	// and without it a one-byte element opens the wide window for eight bits
	// and takes that window's longer tail fallback with it.
	if eb, ok := g.staticBitsScalar(f); ok && eb > 0 {
		g.rdRun = eb * k
	}
	for _, ev := range evs {
		g.emitReadElem(f, ev, "    ", bounded)
	}
	cons := make([]string, k)
	for i, ev := range evs {
		cons[int(k)-1-i] = ev // the accumulator is reversed at the end
	}
	g.pf("    %s(remaining - %d, [%s | acc], data, num_bits, bits_read)\n",
		name, k, strings.Join(cons, ", "))
	g.pf("  end\n\n")
	g.rdBreak()
}

// ---- the single-match-context fast read path (issue #174 round two) ----
//
// A scalar read loop re-enters the binary through rd/rdw on every window:
// each call is a fresh bs_start_match + skip + extract. When the element
// width is static and small, the loop can instead CONSUME the stream through
// one live match context — `<<s1::little-W1, s2::little-W2, rest::binary>>`
// in the clause head, `rest` threaded to the recursion, which the BEAM
// compiler keeps as a reused match context (verified +bin_opt_info) — and
// cut fields out of chained fixnum registers. The clause is body-recursive,
// so the list builds IN ORDER on the unwind and the fast portion never pays
// Enum.reverse.
//
// The stream is LSB-first, so a byte-boundary phase q (0..7) rides across
// iterations as a carry register: c = (8 - q) &&& 7 bits of the last
// consumed byte that belong to the NEXT element. m elements per iteration
// with m * eb ≡ 0 (mod 8) keep c CONSTANT, so the combining shifts are
// `c + <literal>` and every register stays under the 59-bit fixnum bound:
// segments are at most 40 bits and a chain never carries more than
// 52 - segment bits forward (fastFeedMaxReg).
//
// Entry, tail (< m left), and a truncated buffer (the 9-byte match failing
// near the end of data) all fall back to the scalar loop — same values,
// same refusals, only slower; the wide/scalar clauses stay emitted behind
// the fast ones.

// feedState is the compile-time cursor of a fast clause's element emission:
// which chained register holds the stream bits the next field cuts, and how
// many static bits of it are consumed. reg 0 is the carry itself.
type feedState struct {
	reg        int     // current register: 0 = carry, k = zk
	regBits    int64   // static bits the register holds beyond the carry's c
	pos        int64   // static bits already cut from the register
	streamLeft int64   // iteration stream bits not yet chained in
	segs       []int64 // match segment widths chosen so far (s1..sN)
}

const (
	// fastFeedMaxElem bounds the element width: it keeps a chain's leftover
	// under 20 bits, so a fresh 32-bit segment always fits the fixnum bound.
	fastFeedMaxElem = 20
	// fastFeedMaxBits bounds one iteration's stream bits (two segments).
	fastFeedMaxBits = 72
	// fastFeedMaxUnroll bounds the elements one clause decodes, the same way
	// readUnrollMax bounds the scalar clauses.
	fastFeedMaxUnroll = 16
	// fastFeedMaxReg is the fixnum budget left for `leftover + segment` after
	// the runtime carry's up-to-7 bits.
	fastFeedMaxReg = 52
)

func gcd64(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// fastReadPlan reports how many elements one fast clause decodes, or ok=false
// when the element does not qualify: the width must be static and at most
// fastFeedMaxElem bits, the element plain (fields the chained windows can
// cut — no branches, unions, strings, arrays, raw floats), realignment must
// fit the iteration budget, and the array must be able to fill one clause.
func (g *gen) fastReadPlan(f *ir.Field) (int64, bool) {
	eb, ok := g.staticBitsScalar(f)
	if !ok || eb <= 0 || eb > fastFeedMaxElem || !g.plainFastElement(f) {
		return 0, false
	}
	mMin := 8 / gcd64(eb, 8)
	k := min(fastFeedMaxBits/(mMin*eb), fastFeedMaxUnroll/mMin, f.ArrayBound/mMin)
	m := k * mMin
	if m < 2 || f.ArrayBound < m {
		return 0, false
	}
	return m, true
}

// plainFastElement is the fast path's element walker: scalar kinds whose read
// is a plain window cut (ranged/bare ints, bits, bool, fixed, enum, flags,
// compressed floats), or a struct of exactly those (consts and reserved bits
// ride the windows too). Branches are excluded even when their widths agree —
// the chain points are compile-time and a branch's layout is not.
//
// This exclusion carries a second load: it is what lets g.feed live without a
// save/restore, because nothing admitted here can re-enter read emission (the
// law at the feed field, elixir.go). Anything added to this walker that can
// nest a read must give feed that save/restore first.
func (g *gen) plainFastElement(f *ir.Field) bool {
	switch f.Type.Kind {
	case ir.TInt, ir.TBits, ir.TBool, ir.TFixed:
		return true
	case ir.TFloat32:
		return f.HasFloatRange
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return true
		case *ir.Struct:
			for _, item := range ref.Items {
				switch item := item.(type) {
				case *ir.ConstItem, *ir.ReservedItem:
					continue
				case *ir.FieldItem:
					if item.F.Array != ir.ArrayNone || !g.plainFastElement(item.F) {
						return false
					}
				default:
					return false
				}
			}
			return true
		}
	}
	return false
}

func feedReg(reg int) string {
	if reg == 0 {
		return "carry"
	}
	return fmt.Sprintf("z%d", reg)
}

// wrapCall emits `name(args...)` at ind: one line when it fits the format
// width, mix format's one-argument-per-line break otherwise.
func (g *gen) wrapCall(name string, args []string, ind string) {
	one := ind + name + "(" + strings.Join(args, ", ") + ")"
	if len(one) <= formatWidth {
		g.pf("%s\n", one)
		return
	}
	g.pf("%s%s(\n", ind, name)
	for i, a := range args {
		sep := ","
		if i == len(args)-1 {
			sep = ""
		}
		g.pf("%s  %s%s\n", ind, a, sep)
	}
	g.pf("%s)\n", ind)
}

// readFeed is readR under a fast clause: cut `bits` out of the chained
// registers, pulling the next match segment in when the current register
// runs dry. Every offset is static; only the carry width c is runtime, and
// it only ever appears as `c + <literal>` in a combining shift.
func (g *gen) readFeed(bits int64, ind string) {
	fs := g.feed
	if fs.pos+bits > fs.regBits {
		leftover := fs.regBits - fs.pos
		w := min(int64(40), fs.streamLeft, (fastFeedMaxReg-leftover)&^7)
		if w < bits-leftover {
			panic("fast read feed cannot chain a segment wide enough")
		}
		seg := fmt.Sprintf("s%d", len(fs.segs)+1)
		next := feedReg(fs.reg + 1)
		switch {
		case fs.reg == 0:
			g.pf("%s%s = carry ||| %s <<< c\n", ind, next, seg)
		case fs.pos == 0:
			g.pf("%s%s = %s ||| %s <<< (c + %d)\n", ind, next, feedReg(fs.reg), seg, leftover)
		default:
			g.pf("%s%s = %s >>> %d ||| %s <<< (c + %d)\n",
				ind, next, feedReg(fs.reg), fs.pos, seg, leftover)
		}
		fs.segs = append(fs.segs, w)
		fs.streamLeft -= w
		fs.reg++
		fs.regBits = leftover + w
		fs.pos = 0
	}
	if fs.pos == 0 {
		g.pf("%sv = %s &&& %s\n", ind, feedReg(fs.reg), maskHex(bits))
	} else {
		g.pf("%sv = %s >>> %d &&& %s\n", ind, feedReg(fs.reg), fs.pos, maskHex(bits))
	}
	fs.pos += bits
}

// fastReadBody emits the m element bodies of a fast clause under a fresh
// feed and returns the final feed state (segment plan, carry-out position).
func (g *gen) fastReadBody(f *ir.Field, m, total int64, evs []string) *feedState {
	g.feed = &feedState{streamLeft: total}
	for _, ev := range evs {
		g.emitReadElem(f, ev, "    ", true)
	}
	fs := g.feed
	g.feed = nil
	if fs.streamLeft != 0 || fs.pos != fs.regBits {
		panic("fast read feed did not consume its iteration exactly")
	}
	return fs
}

// fastReadClauses emits the aligning entry and the fast clause pair behind
// the scalar loop `name`. The segment plan the clause head needs is learned
// from a dry run of the same element emission it then repeats for real.
//
// The fast clause is BODY-recursive, so the list builds in order on unwind
// and no reverse is paid — but that is a new memory characteristic: it holds
// ceil(n/m) live stack frames where the scalar loop held one. ArrayBound
// bounds it, so the depth is a declaration constant rather than input-driven,
// and a declaration whose bound made that depth expensive would have to hold
// the decoded list at the same scale anyway.
func (g *gen) fastReadClauses(f *ir.Field, name string, m int64) {
	eb, _ := g.staticBitsScalar(f)
	T := m * eb
	evs := make([]string, m)
	for i := range evs {
		evs[i] = fmt.Sprintf("e%d", i+1)
	}

	// dry pass: the element bodies decide where segments chain in
	saved := g.fn
	savedOff, savedAvail, savedRun := g.rdOff, g.rdAvail, g.rdRun
	g.fn = strings.Builder{}
	plan := g.fastReadBody(f, m, T, evs)
	g.fn = saved
	g.rdOff, g.rdAvail, g.rdRun = savedOff, savedAvail, savedRun

	// the aligning entry: split bits_read into a byte position and a phase,
	// hand the fast clause the stream from the byte boundary with the
	// already-consumed low bits of the split byte dropped into the carry
	g.rdBreak()
	g.pf("  defp %s_align(remaining, acc, data, num_bits, bits_read)\n", name)
	g.pf("       when remaining >= %d do\n", m)
	g.pf("    i = bits_read >>> 3\n")
	g.pf("    q = bits_read &&& 7\n\n")
	g.pf("    if q == 0 do\n")
	g.pf("      case data do\n")
	g.pf("        <<_::binary-size(^i), rest::binary>> ->\n")
	g.wrapCall(name+"_fast",
		[]string{"remaining", "acc", "data", "num_bits", "bits_read", "rest", "0", "0"},
		"          ")
	g.pf("\n")
	g.pf("        _ ->\n")
	g.pf("          %s(remaining, acc, data, num_bits, bits_read)\n", name)
	g.pf("      end\n")
	g.pf("    else\n")
	g.pf("      case data do\n")
	g.pf("        <<_::binary-size(^i), b0, rest::binary>> ->\n")
	g.wrapCall(name+"_fast",
		[]string{"remaining", "acc", "data", "num_bits", "bits_read", "rest", "b0 >>> q", "8 - q"},
		"          ")
	g.pf("\n")
	g.pf("        _ ->\n")
	g.pf("          %s(remaining, acc, data, num_bits, bits_read)\n", name)
	g.pf("      end\n")
	g.pf("    end\n")
	g.pf("  end\n\n")
	g.pf("  defp %s_align(remaining, acc, data, num_bits, bits_read),\n", name)
	g.pf("    do: %s(remaining, acc, data, num_bits, bits_read)\n\n", name)

	// the fast clause: one head match per iteration, m elements per match
	var pat strings.Builder
	pat.WriteString("<<")
	for i, w := range plan.segs {
		fmt.Fprintf(&pat, "s%d::little-%d, ", i+1, w)
	}
	pat.WriteString("rest::binary>>")
	g.pf("  defp %s_fast(\n", name)
	g.pf("         remaining,\n")
	g.pf("         acc,\n")
	g.pf("         data,\n")
	g.pf("         num_bits,\n")
	g.pf("         bits_read,\n")
	g.pf("         %s,\n", pat.String())
	g.pf("         carry,\n")
	g.pf("         c\n")
	g.pf("       )\n")
	g.pf("       when remaining >= %d do\n", m)
	final := g.fastReadBody(f, m, T, evs)
	if len(final.segs) != len(plan.segs) {
		panic("fast read feed diverged from its dry run")
	}
	recArgs := []string{fmt.Sprintf("remaining - %d", m), "acc", "data", "num_bits",
		fmt.Sprintf("bits_read + %d", T), "rest",
		fmt.Sprintf("%s >>> %d", feedReg(final.reg), final.pos), "c"}
	call := name + "_fast(" + strings.Join(recArgs, ", ") + ")"
	g.pf("\n")
	switch {
	case len("    {bits_read, tail} = "+call) <= formatWidth:
		g.pf("    {bits_read, tail} = %s\n", call)
	case len("      "+call) <= formatWidth:
		g.pf("\n    {bits_read, tail} =\n      %s\n\n", call)
	default:
		g.pf("\n    {bits_read, tail} =\n")
		g.wrapCall(name+"_fast", recArgs, "      ")
		g.pf("\n")
	}
	g.pf("\n    {bits_read, [%s | tail]}\n", strings.Join(evs, ", "))
	g.pf("  end\n\n")
	g.pf("  defp %s_fast(remaining, acc, data, num_bits, bits_read, _rest, _carry, _c),\n", name)
	g.pf("    do: %s(remaining, acc, data, num_bits, bits_read)\n\n", name)
	g.rdBreak()
}

// emitReadElem reads one array element into local ev.
func (g *gen) emitReadElem(f *ir.Field, ev, ind string, bounded bool) {
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			g.withOwner(ref.Name, func() {
				g.emitReadItems(ref.Items, ev, ind, bounded)
				g.emitBuildStruct(ref, ev, ev, ind)
			})
			return
		case *ir.Union:
			g.emitReadUnion(ref, ev, ind, bounded)
			return
		}
	}
	g.emitReadScalar(f, ev, ind, bounded)
}

// emitBuildStruct binds lv to the struct literal assembled from the locals
// a read of ref's items left under pre.
func (g *gen) emitBuildStruct(ref *ir.Struct, pre, lv, ind string) {
	var fields []string
	for _, nf := range ref.Fields {
		fields = append(fields, fmt.Sprintf("%s: %s", elixirName(nf.Name), local(pre, nf)))
	}
	g.structLit(&g.fn, ind, lv+" = ", ref.Name, fields)
}

func (g *gen) emitReadScalar(f *ir.Field, lv, ind string, bounded bool) {
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		g.emitReadBytesField(f, lv, ind)
	case ir.TFixed:
		g.emitReadFixed(f, lv, ind)
	case ir.TInt:
		g.emitReadInt(f, lv, ind)
	case ir.TBits:
		w := int64(f.Type.Width)
		if w <= 32 {
			g.readR(w, ind)
			g.pf("%s%s = v\n", ind, lv)
			return
		}
		g.emitReadWide(w, ind)
		g.pf("%s%s = w\n", ind, lv)
	case ir.TBool:
		g.readR(1, ind)
		g.pf("%s%s = v == 1\n", ind, lv)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.emitReadCompressedFloat(f, lv, ind)
			return
		}
		g.needF32 = true
		g.readR(32, ind)
		g.pf("%s%s = f32_value(v)\n", ind, lv)
	case ir.TFloat64:
		g.needF64 = true
		g.emitReadWide(64, ind)
		g.pf("%s%s = f64_value(w)\n", ind, lv)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			if bits == 0 {
				g.pf("%s%s = 0\n", ind, lv)
				return
			}
			g.readR(bits, ind)
			if ref.Max != (int64(1)<<bits)-1 {
				g.throwIf(fmt.Sprintf("v > %s", intLit64(ref.Max)), "headroom above the wire range is refused", ind)
			}
			g.pf("%s%s = v\n", ind, lv)
		case *ir.Flags:
			wb := int64(ref.WireBits)
			if wb <= 32 {
				g.readR(wb, ind)
				g.pf("%s%s = v\n", ind, lv)
				return
			}
			g.emitReadWide(wb, ind)
			g.pf("%s%s = w\n", ind, lv)
		case *ir.Struct:
			g.withOwner(ref.Name, func() {
				g.emitReadItems(ref.Items, lv, ind, bounded)
				g.emitBuildStruct(ref, lv, lv, ind)
			})
		case *ir.Union:
			g.emitReadUnion(ref, lv, ind, bounded)
		}
	}
}

func (g *gen) emitReadCompressedFloat(f *ir.Field, lv, ind string) {
	maxInt, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	minF := float32(f.FMin)
	deltaF := float32(f.FMax) - minF
	g.readR(bits, ind)
	if maxInt != (uint64(1)<<bits)-1 {
		g.throwIf(fmt.Sprintf("v > %s", intLitU64(maxInt)), "headroom above the quantum count is refused", ind)
	}
	if maxInt <= cfTableMax {
		// the quantum index is already range-checked, so the decode is a
		// total function of a small integer domain: a compile-time table
		// lookup replaces the whole float32 chain (cfTable)
		g.pf("%s%s = elem(@cf_tab_%d, v)\n", ind, lv,
			g.cfTable(maxInt, f32lit(float64(maxInt)), f32lit(float64(deltaF)), f32lit(float64(minF))))
		return
	}
	g.needCfD = true
	g.pf("%s%s = cf_decode(v, %s, %s, %s)\n", ind, lv, f32lit(float64(maxInt)), f32lit(float64(deltaF)), f32lit(float64(minF)))
}

// cfTableMax bounds the quantum count a compressed-float read decodes through
// a compile-time table: past it, the table's memory outgrows what the decode
// chain costs, and cf_decode stays the shipped path.
const cfTableMax = 1024

// cfTable returns the index of the module attribute holding the decode table
// for one compressed-float declaration, materializing it on first need. The
// table is computed when the GENERATED module compiles, by literally the
// arithmetic cf_decode runs — fr's fast path, fr_slow's construct/match
// refusal, the non-finite mapping — so equality with the shipped chain is by
// construction, for every index the range check admits.
func (g *gen) cfTable(maxInt uint64, miv32, delta, min32 string) int {
	key := fmt.Sprintf("%d/%s/%s/%s", maxInt, miv32, delta, min32)
	if g.cfTabs == nil {
		g.cfTabs = map[string]int{}
	}
	if idx, ok := g.cfTabs[key]; ok {
		return idx
	}
	idx := len(g.cfTabDef)
	g.cfTabs[key] = idx
	g.usesImport = true // the table builder's fr_slow arm shifts sign bits
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	w("  @cf_tab_%d (fn ->\n", idx)
	w("               fr = fn value ->\n")
	w("                 ax = abs(value)\n")
	w("\n")
	w("                 if ax >= 1.1754943508222875e-38 and ax < 3.4028235677973366e38 do\n")
	w("                   y = value * 536_870_913.0\n")
	w("                   y - (y - value)\n")
	w("                 else\n")
	w("                   case <<value::float-32-little>> do\n")
	w("                     <<rounded::float-32-little>> -> rounded\n")
	w("                     <<bits::little-32>> -> if bits >>> 31 == 1, do: :neg_inf, else: :pos_inf\n")
	w("                   end\n")
	w("                 end\n")
	w("               end\n")
	w("\n")
	w("               List.to_tuple(\n")
	w("                 for integer <- 0..%d do\n", maxInt)
	w("                   quotient = fr.(fr.(integer * 1.0) / %s)\n", miv32)
	w("                   scaled = fr.(quotient * %s)\n", delta)
	w("\n")
	w("                   case fr.(scaled + %s) do\n", min32)
	w("                     :pos_inf -> {:nonfinite, 0x7F800000}\n")
	w("                     :neg_inf -> {:nonfinite, 0xFF800000}\n")
	w("                     value -> value\n")
	w("                   end\n")
	w("                 end\n")
	w("               )\n")
	w("             end).()\n")
	w("\n")
	g.cfTabDef = append(g.cfTabDef, b.String())
	return idx
}

func (g *gen) emitReadFixed(f *ir.Field, lv, ind string) {
	rawMin, rawMax, bits := fixedRaw(f)
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// degenerate: zero bits — the value is the range, raw min,
		// materialized with no wire read (SPEC §4.6)
		g.pf("%s%s = %s\n", ind, lv, intLit(rawMin))
		return
	}
	g.emitReadOffset(lv, rawMin, rawMax, bits, ind)
}

// emitReadOffset decodes a ranged value: the offset in `bits` bits, headroom
// rejected unless the range fills the width, min added back — exact at any
// width in BEAM integer arithmetic.
func (g *gen) emitReadOffset(lv string, min, max *big.Int, bits int64, ind string) {
	diff := new(big.Int).Sub(max, min)
	full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	src := "v"
	if bits <= 32 {
		g.readR(bits, ind)
	} else {
		g.emitReadWide(bits, ind)
		src = "w"
	}
	if diff.Cmp(full) != 0 {
		g.throwIf(fmt.Sprintf("%s > %s", src, intLit(diff)), "a smuggled offset is refused", ind)
	}
	switch {
	case min.Sign() == 0:
		g.pf("%s%s = %s\n", ind, lv, src)
	case min.Sign() < 0:
		g.pf("%s%s = %s - %s\n", ind, lv, src, intLit(new(big.Int).Neg(min)))
	default:
		g.pf("%s%s = %s + %s\n", ind, lv, src, intLit(min))
	}
}

func (g *gen) emitReadInt(f *ir.Field, lv, ind string) {
	w := int64(f.Type.Width)
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: zero bits — the value is the range,
			// materialized with no wire read (SPEC §4.6)
			g.pf("%s%s = %s\n", ind, lv, intLit(f.IntMin))
			return
		}
		g.emitReadOffset(lv, f.IntMin, f.IntMax, ir.BitsRequired(f.IntMin, f.IntMax), ind)
		return
	}
	// bare integer at storage width, sign recovered for signed storage
	src := "v"
	if w <= 32 {
		g.readR(w, ind)
	} else {
		g.emitReadWide(w, ind)
		src = "w"
	}
	if f.Type.Signed {
		half := new(big.Int).Lsh(big.NewInt(1), uint(w-1))
		whole := new(big.Int).Lsh(big.NewInt(1), uint(w))
		g.condAssign(lv, fmt.Sprintf("%s >= %s", src, intLit(half)),
			fmt.Sprintf("%s - %s", src, intLit(whole)), src, ind)
	} else {
		g.pf("%s%s = %s\n", ind, lv, src)
	}
}

// emitReadBytesField reads string(N)/bytes(N): ranged length, align with
// zero padding verified, bounds, the bytes as one sub-binary, and (strings)
// the interior-null and UTF-8 validity refusals.
func (g *gen) emitReadBytesField(f *ir.Field, lv, ind string) {
	lenBits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
	g.throwIf(fmt.Sprintf("bits_read + %s > num_bits", intLit64(lenBits)), "", ind)
	g.readR(lenBits, ind)
	if f.Type.Size != (int64(1)<<lenBits)-1 {
		g.throwIf(fmt.Sprintf("v > %s", intLit64(f.Type.Size)), "the length guards the slice — reject, never clamp", ind)
	}
	g.pf("%slen = v\n", ind)
	g.emitReadAlign(ind)
	g.throwIf("bits_read + len * 8 > num_bits", "", ind)
	g.pf("%s%s = binary_part(data, bits_read >>> 3, len)\n", ind, lv)
	g.pf("%sbits_read = bits_read + len * 8\n", ind)
	g.rdBreak()
	if f.Type.Kind == ir.TString {
		g.throwIf(fmt.Sprintf(":binary.match(%s, <<0>>) != :nomatch", lv),
			"an interior null is content the read refuses (SPEC §4.7)", ind)
	}
}

// emitReadUnion is the union read half: the tag reads in minimal bits and a
// value above the count is refused (SPEC §4.8); each arm decodes into fresh
// locals and the case returns the assembled union struct.
func (g *gen) emitReadUnion(u *ir.Union, lv, ind string, bounded bool) {
	if u.Max == 0 {
		g.pf("%s# zero wire bits — only None exists (SPEC §4.8)\n", ind)
		g.pf("%s%s = %%%s{}\n", ind, lv, g.mod(u.Name))
		return
	}
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	if !bounded {
		g.throwIf(fmt.Sprintf("bits_read + %d > num_bits", bits), "", ind)
	}
	g.readR(bits, ind)
	if u.Max != (int64(1)<<bits)-1 {
		g.throwIf(fmt.Sprintf("v > %s", intLit64(u.Max)), "not a wire-legal tag (SPEC §4.8)", ind)
	}
	g.pf("\n")
	g.pf("%s{bits_read, %s} =\n", ind, lv)
	g.pf("%s  case v do\n", ind)
	for i, vr := range u.Variants {
		arm := lv + "_" + elixirName(vr.Name)
		g.pf("%s    %d ->\n", ind, i+1)
		if len(vr.Ref.Items) == 0 {
			g.pf("%s      # empty arm — presence is the payload (SPEC §4.6)\n", ind)
		}
		g.rdBreak() // each arm opens its own group
		g.withOwner(vr.Type, func() {
			g.emitReadItems(vr.Ref.Items, arm, ind+"      ", false)
			g.emitBuildStruct(vr.Ref, arm, arm, ind+"      ")
		})
		g.structLit(&g.fn, ind+"      ", "u = ",
			u.Name, []string{fmt.Sprintf("type: %d", i+1), fmt.Sprintf("%s: %s", elixirName(vr.Name), arm)})
		g.pf("%s      {bits_read, u}\n", ind)
		g.pf("\n")
	}
	g.pf("%s    _ ->\n", ind)
	g.pf("%s      # None — the tag is the whole wire (SPEC §4.8)\n", ind)
	g.pf("%s      {bits_read, %%%s{}}\n", ind, g.mod(u.Name))
	g.pf("%s  end\n", ind)
	g.pf("\n")
	g.rdBreak()
}

// ---- measure emission ----

// emitMeasureFunction emits measure_<name>: exact wire bits for a value,
// static runs folded to generation-time literals; a fully static type folds
// to a single return.
func (g *gen) emitMeasureFunction(name string, items []ir.Item) {
	snake := ir.RustSnake(name)
	g.fn.Reset()
	pending := int64(0)
	g.withOwner(name, func() { g.emitMeasureItems(items, "value", "    ", &pending) })
	body := g.fn.String()

	g.bpf("  # measure_%s is the exact wire bits write_%s would produce for value —\n", snake, snake)
	g.bpf("  # trusted like the writer; static runs fold to literals at generation time.\n")
	if body == "" {
		// fully static: the whole wire folded to one constant
		g.bpf("  def measure_%s(_value), do: %s\n\n", snake, intLit64(pending))
		return
	}
	param := "value"
	if !strings.Contains(body, "value") {
		param = "_value"
	}
	g.bpf("  def measure_%s(%s) do\n", snake, param)
	g.bpf("    bits = 0\n")
	g.body.WriteString(body)
	if pending != 0 {
		g.bpf("    bits + %s\n", intLit64(pending))
	} else {
		g.bpf("    bits\n")
	}
	g.bpf("  end\n\n")
}

// flushMeasure adds the pending folded bits before dynamic code that needs
// the running position.
func (g *gen) flushMeasure(pending *int64, ind string) {
	if *pending != 0 {
		g.pf("%sbits = bits + %s\n", ind, intLit64(*pending))
		*pending = 0
	}
}

func (g *gen) emitMeasureItems(items []ir.Item, path, ind string, pending *int64) {
	for _, item := range items {
		if bits, ok := g.staticBitsItem(item); ok {
			*pending += bits
			continue
		}
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitMeasureField(item.F, path, ind, pending)
		case *ir.AlignItem:
			g.flushMeasure(pending, ind)
			g.pf("%sbits = bits + (8 - (bits &&& 7) &&& 7)\n", ind)
		case *ir.Branch:
			g.flushMeasure(pending, ind)
			cond := path + "." + elixirName(item.Cond)
			if item.Neg {
				cond = "not " + cond
			}
			g.pf("\n")
			g.pf("%sbits =\n", ind)
			g.pf("%s  if %s do\n", ind, cond)
			thenPending := int64(0)
			g.emitMeasureItems(item.Then, path, ind+"    ", &thenPending)
			if thenPending != 0 {
				g.pf("%s    bits + %s\n", ind, intLit64(thenPending))
			} else {
				g.pf("%s    bits\n", ind)
			}
			g.pf("%s  else\n", ind)
			elsePending := int64(0)
			if item.Else != nil {
				g.emitMeasureItems(item.Else, path, ind+"    ", &elsePending)
			}
			if elsePending != 0 {
				g.pf("%s    bits + %s\n", ind, intLit64(elsePending))
			} else {
				g.pf("%s    bits\n", ind)
			}
			g.pf("%s  end\n", ind)
			g.pf("\n")
		}
	}
}

func (g *gen) emitMeasureField(f *ir.Field, path, ind string, pending *int64) {
	name := path + "." + elixirName(f.Name)
	if f.Name == "" {
		name = path
	}
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		lenBits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
		*pending += lenBits
		g.flushMeasure(pending, ind)
		g.pf("%sbits = bits + (8 - (bits &&& 7) &&& 7)\n", ind)
		g.pf("%sbits = bits + byte_size(%s) * 8\n", ind, name)
	case f.Array == ir.ArrayCounted:
		countBits := ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound))
		*pending += countBits
		if elemBits, ok := g.staticBitsScalar(f); ok {
			g.flushMeasure(pending, ind)
			g.pf("%sbits = bits + length(%s) * %s\n", ind, name, intLit64(elemBits))
			return
		}
		g.flushMeasure(pending, ind)
		helper := g.measureHelper(f)
		g.pf("%sbits = %s(%s, bits)\n", ind, helper, name)
	case f.Array == ir.ArrayFixed:
		// a fixed array of dynamically-sized elements
		g.flushMeasure(pending, ind)
		helper := g.measureHelper(f)
		g.callAssign("bits", fmt.Sprintf("%s(%s, bits)", helper, name), ind)
	default:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			g.withOwner(ref.Name, func() { g.emitMeasureItems(ref.Items, name, ind, pending) })
		case *ir.Union:
			g.flushMeasure(pending, ind)
			g.emitMeasureUnion(ref, name, ind)
		}
	}
}

// measureHelper materializes the tail-recursive measure loop for an array
// field of dynamically-sized elements.
func (g *gen) measureHelper(f *ir.Field) string {
	name := g.helperName("m", f)
	if _, done := g.helpers[name]; done {
		return name
	}
	g.helpers[name] = ""
	saved := g.fn
	g.fn = strings.Builder{}
	g.pf("  defp %s([], bits), do: bits\n\n", name)
	g.pf("  defp %s([e | rest], bits) do\n", name)
	inner := int64(0)
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		g.withOwner(ref.Name, func() { g.emitMeasureItems(ref.Items, "e", "    ", &inner) })
	case *ir.Union:
		g.emitMeasureUnion(ref, "e", "    ")
	}
	if inner != 0 {
		g.pf("    %s(rest, bits + %s)\n", name, intLit64(inner))
	} else {
		g.pf("    %s(rest, bits)\n", name)
	}
	g.pf("  end\n\n")
	g.helpers[name] = g.fn.String()
	g.fn = saved
	g.helperOrder = append(g.helperOrder, name)
	return name
}

// emitMeasureUnion measures tag + selected arm through a case.
func (g *gen) emitMeasureUnion(u *ir.Union, expr, ind string) {
	if u.Max == 0 {
		return // zero wire bits — only None exists (SPEC §4.8)
	}
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	g.pf("%sbits = bits + %s\n", ind, intLit64(bits))
	g.pf("\n")
	g.pf("%sbits =\n", ind)
	g.pf("%s  case %s.type do\n", ind, expr)
	for i, vr := range u.Variants {
		arm := expr + "." + elixirName(vr.Name)
		g.pf("%s    %d ->\n", ind, i+1)
		inner := int64(0)
		before := g.fn.Len()
		g.withOwner(vr.Type, func() { g.emitMeasureItems(vr.Ref.Items, arm, ind+"      ", &inner) })
		if g.fn.Len() == before && inner == 0 {
			g.pf("%s      # empty arm — presence is the payload (SPEC §4.6)\n", ind)
		}
		if inner != 0 {
			g.pf("%s      bits + %s\n", ind, intLit64(inner))
		} else {
			g.pf("%s      bits\n", ind)
		}
		g.pf("\n")
	}
	g.pf("%s    _ ->\n", ind)
	g.pf("%s      # None — the tag is the whole wire (SPEC §4.8)\n", ind)
	g.pf("%s      bits\n", ind)
	g.pf("%s  end\n", ind)
	g.pf("\n")
}

// ---- the per-file private helpers ----

func (g *gen) emitLoopHelpers() {
	for _, name := range g.helperOrder {
		g.body.WriteString(g.helpers[name])
	}
}

func (g *gen) emitSupportHelpers() {
	if g.needRd {
		g.bpf("  # The port's 40-bit window decode (issue #167): enough for a 7-bit offset\n")
		g.bpf("  # plus a 32-bit group, small enough that no intermediate ever boxes. The\n")
		g.bpf("  # callers have already priced the read inside num_bits, so the tail\n")
		g.bpf("  # fallback decodes exactly the bytes that exist.\n")
		g.bpf("  defp rd(data, bits_read, bits) do\n")
		g.bpf("    i = bits_read >>> 3\n\n")
		g.bpf("    window =\n")
		g.bpf("      case data do\n")
		g.bpf("        <<_::binary-size(^i), w::little-40, _::binary>> -> w\n")
		g.bpf("        <<_::binary-size(^i), rest::binary>> -> :binary.decode_unsigned(rest, :little)\n")
		g.bpf("      end\n\n")
		g.bpf("    window >>> (bits_read &&& 7) &&& (1 <<< bits) - 1\n")
		g.bpf("  end\n\n")
	}
	if g.needRdw {
		g.bpf("  # The wide window decode: 56 bits, enough for a 7-bit offset plus a\n")
		g.bpf("  # 49-bit group, and still under the 2^59 fixnum boundary — one match\n")
		g.bpf("  # context serves a whole group of fields instead of one per field. A\n")
		g.bpf("  # 64-bit window would box and measures slower than the reads it saves.\n")
		g.bpf("  defp rdw(data, bits_read, bits) do\n")
		g.bpf("    i = bits_read >>> 3\n\n")
		g.bpf("    window =\n")
		g.bpf("      case data do\n")
		g.bpf("        <<_::binary-size(^i), w::little-56, _::binary>> -> w\n")
		g.bpf("        <<_::binary-size(^i), rest::binary>> -> :binary.decode_unsigned(rest, :little)\n")
		g.bpf("      end\n\n")
		g.bpf("    window >>> (bits_read &&& 7) &&& (1 <<< bits) - 1\n")
		g.bpf("  end\n\n")
	}
	if g.needF32 {
		g.bpf("  # The 32-bit IEEE-754 pattern of a float value for the write path — a\n")
		g.bpf("  # non-finite pattern travels as {:nonfinite, bits}, since no BEAM float\n")
		g.bpf("  # term can hold it (the serialize.elixir convention). Caller errors raise;\n")
		g.bpf("  # the checks are O(1).\n")
		g.bpf("  defp f32_bits(value) when is_float(value) do\n")
		g.bpf("    <<bits::little-32>> = <<value::float-32-little>>\n\n")
		g.bpf("    if (bits >>> 23 &&& 0xFF) == 0xFF do\n")
		g.bpf("      raise ArgumentError, \"float overflows float32; write {:nonfinite, bits} instead\"\n")
		g.bpf("    end\n\n")
		g.bpf("    bits\n")
		g.bpf("  end\n\n")
		g.bpf("  defp f32_bits({:nonfinite, bits}) when bits >= 0 and bits <= 0xFFFFFFFF do\n")
		g.bpf("    if (bits >>> 23 &&& 0xFF) != 0xFF do\n")
		g.bpf("      raise ArgumentError, \"{:nonfinite, bits} carries a finite float32 pattern; write the float\"\n")
		g.bpf("    end\n\n")
		g.bpf("    bits\n")
		g.bpf("  end\n\n")
		g.bpf("  # The value of a 32-bit pattern for the read path: the float for a finite\n")
		g.bpf("  # pattern, {:nonfinite, bits} otherwise. Never raises — the bits are\n")
		g.bpf("  # untrusted.\n")
		g.bpf("  defp f32_value(bits) do\n")
		g.bpf("    if (bits >>> 23 &&& 0xFF) != 0xFF do\n")
		g.bpf("      <<value::float-32-little>> = <<bits::little-32>>\n")
		g.bpf("      value\n")
		g.bpf("    else\n")
		g.bpf("      {:nonfinite, bits}\n")
		g.bpf("    end\n")
		g.bpf("  end\n\n")
	}
	if g.needF64 {
		g.bpf("  # The 64-bit IEEE-754 pattern of a double value and its inverse — the\n")
		g.bpf("  # same bit-transparent contract as f32, at width 64.\n")
		g.bpf("  defp f64_bits(value) when is_float(value) do\n")
		g.bpf("    <<bits::little-64>> = <<value::float-64-little>>\n")
		g.bpf("    bits\n")
		g.bpf("  end\n\n")
		g.bpf("  defp f64_bits({:nonfinite, bits}) when bits >= 0 and bits <= 0xFFFFFFFFFFFFFFFF do\n")
		g.bpf("    if (bits >>> 52 &&& 0x7FF) != 0x7FF do\n")
		g.bpf("      raise ArgumentError, \"{:nonfinite, bits} carries a finite float64 pattern; write the float\"\n")
		g.bpf("    end\n\n")
		g.bpf("    bits\n")
		g.bpf("  end\n\n")
		g.bpf("  defp f64_value(bits) do\n")
		g.bpf("    if (bits >>> 52 &&& 0x7FF) != 0x7FF do\n")
		g.bpf("      <<value::float-64-little>> = <<bits::little-64>>\n")
		g.bpf("      value\n")
		g.bpf("    else\n")
		g.bpf("      {:nonfinite, bits}\n")
		g.bpf("    end\n")
		g.bpf("  end\n\n")
	}
	if g.needCfQ || g.needCfD {
		g.bpf("  # A number rounded to its nearest float32 — one emulated float32 step\n")
		g.bpf("  # (exact: the double result carries 2x24+2 significant bits, so the second\n")
		g.bpf("  # rounding is innocuous). Overflow reports :pos_inf / :neg_inf so the\n")
		g.bpf("  # compressed-float clamps can resolve it the way the reference's float\n")
		g.bpf("  # arithmetic does.\n")
		g.bpf("  #\n")
		g.bpf("  # The fast path is arithmetic (Veltkamp/Dekker splitting, C = 2^29 + 1):\n")
		g.bpf("  # for a double whose magnitude is in [2^-126, 2^128 - 2^103) — the\n")
		g.bpf("  # float32 normal range, below the smallest magnitude that rounds to\n")
		g.bpf("  # infinity — y = value * C; y - (y - value) IS the value rounded to 24\n")
		g.bpf("  # significant bits, round-to-nearest-even, in three flops with nothing\n")
		g.bpf("  # allocated beyond the result. Outside that range (the subnormal grid,\n")
		g.bpf("  # overflow to the atoms, zero with its sign) fr_slow keeps the exact\n")
		g.bpf("  # semantics the construct/match pair defines.\n")
		g.bpf("  defp fr(value) do\n")
		g.bpf("    ax = abs(value)\n\n")
		g.bpf("    if ax >= 1.1754943508222875e-38 and ax < 3.4028235677973366e38 do\n")
		g.bpf("      y = value * 536_870_913.0\n")
		g.bpf("      y - (y - value)\n")
		g.bpf("    else\n")
		g.bpf("      fr_slow(value)\n")
		g.bpf("    end\n")
		g.bpf("  end\n\n")
		g.bpf("  # The slow path's refusal IS the test: a float segment does not match a\n")
		g.bpf("  # non-finite pattern, so a finite value costs one construction and one\n")
		g.bpf("  # match and never touches the exponent field, while the second clause\n")
		g.bpf("  # reads the sign of exactly the patterns the first one refused.\n")
		g.bpf("  defp fr_slow(value) do\n")
		g.bpf("    case <<value::float-32-little>> do\n")
		g.bpf("      <<rounded::float-32-little>> -> rounded\n")
		g.bpf("      <<bits::little-32>> -> if bits >>> 31 == 1, do: :neg_inf, else: :pos_inf\n")
		g.bpf("    end\n")
		g.bpf("  end\n\n")
	}
	if g.needCfQ {
		g.bpf("  defp cf_clamp01(:pos_inf), do: 1.0\n")
		g.bpf("  defp cf_clamp01(:neg_inf), do: 0.0\n")
		g.bpf("  defp cf_clamp01(n) when n < 0.0, do: 0.0\n")
		g.bpf("  defp cf_clamp01(n) when n > 1.0, do: 1.0\n")
		g.bpf("  defp cf_clamp01(n), do: n\n\n")
		g.bpf("  # The family's two-rounding float32 quantization (SPEC §4.3), the port's\n")
		g.bpf("  # own steps: normalize with every step rounding, clamp to [0, 1] (which\n")
		g.bpf("  # also grounds an overflowed value), scale, round BEFORE the +0.5, floor,\n")
		g.bpf("  # and the normative integer clamp to the step count.\n")
		g.bpf("  # miv32 is the float32 of the step count, folded at generation time: it\n")
		g.bpf("  # is a declaration constant, and recomputing its rounding per call was\n")
		g.bpf("  # one of the six float32 steps.\n")
		g.bpf("  defp cf_quantize(value, min32, delta, miv, miv32) do\n")
		g.bpf("    if not is_number(value) do\n")
		g.bpf("      raise ArgumentError, \"a compressed float writes a finite number\"\n")
		g.bpf("    end\n\n")
		g.bpf("    difference =\n")
		g.bpf("      case fr(value * 1.0) do\n")
		g.bpf("        :pos_inf -> :pos_inf\n")
		g.bpf("        :neg_inf -> :neg_inf\n")
		g.bpf("        v32 -> fr(v32 - min32)\n")
		g.bpf("      end\n\n")
		g.bpf("    normalized =\n")
		g.bpf("      case difference do\n")
		g.bpf("        :pos_inf -> 1.0\n")
		g.bpf("        :neg_inf -> 0.0\n")
		g.bpf("        d -> cf_clamp01(fr(d / delta))\n")
		g.bpf("      end\n\n")
		g.bpf("    scaled = fr(normalized * miv32)\n")
		g.bpf("    integer = floor(fr(scaled + 0.5))\n")
		g.bpf("    min(integer, miv)\n")
		g.bpf("  end\n\n")
	}
	if g.needCfD {
		g.bpf("  # The reader's arithmetic, pinned the same way: the quotient rounds, the\n")
		g.bpf("  # product rounds BEFORE min is added, and the sum rounds — float32\n")
		g.bpf("  # throughout, never fused, never widened. The final add cannot overflow\n")
		g.bpf("  # for a conforming declaration; the non-finite mapping keeps the\n")
		g.bpf("  # never-raise reader obligation airtight.\n")
		g.bpf("  defp cf_decode(integer, miv32, delta, min32) do\n")
		g.bpf("    quotient = fr(fr(integer * 1.0) / miv32)\n")
		g.bpf("    scaled = fr(quotient * delta)\n\n")
		g.bpf("    case fr(scaled + min32) do\n")
		g.bpf("      :pos_inf -> {:nonfinite, 0x7F800000}\n")
		g.bpf("      :neg_inf -> {:nonfinite, 0xFF800000}\n")
		g.bpf("      value -> value\n")
		g.bpf("    end\n")
		g.bpf("  end\n\n")
	}
}
