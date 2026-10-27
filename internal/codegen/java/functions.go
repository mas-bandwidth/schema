// Wire-function emission: write/read/zero/init per type, the issue #156
// prescription — the family bitpacker at every field with literal constant
// widths and masks, monomorphic static functions per type (zero dispatch),
// and bounds checks fused per maximal static run on the read side.
//
// Every type and union gets TWO entries per direction (SPEC §6.1, stream
// composition): write<Name>Bits(value, data, bitOffset) and
// read<Name>Bits(value, data, bitOffset, numBits) start at any bit position
// and return the bit position after the value (-1 on a refused read), so a
// type works standalone, nested, and beside hand-written bit code on the
// same buffer; write<Name>(value, data) and read<Name>(value, data, numBits)
// are the offset-0 wrappers. A nested type CALLS its own Bits entry: the
// caller stores its partial scratch word, the callee merges into it, and the
// caller reloads it after.
//
// The bit position is a plain int and the write scratch a plain long, both
// locals: C2 keeps them in registers, where a stream object would live in
// memory unless escape analysis scalar-replaced it through every call.
// The bitpacker steps are small private static helpers on the file's class
// (putBits, peekBits, alignBits, ...), each under the JIT's inline threshold,
// so the bodies read one line per field and compile to the inlined code.
//
// The wire math transliterates the Dart backend's kernels (themselves the
// flat JS tier's probe-proven forms) into Java's value domain: one 64-bit
// long carries every lane through width 64, the emitted Int128/UInt128 pair
// carries the 128-bit storage widths, and the 32-bit group structure (least
// significant first — serialize's own) is preserved exactly, so the wire is
// byte-identical to the other eight targets by construction and proven by
// the golden legs. The word stores and window loads go through the VarHandle
// little-endian view — serialize.java's measured-fast form — and statically
// byte-aligned [N]uint8 arrays and string/bytes payloads take the port's
// fused bulk copy (flush, arraycopy, reload), wire-identical to the
// per-byte merge.
//
// Writer contracts live in one private checkWrite<Name> predicate per type,
// invoked as `assert checkWriteX(value, data, bitOffset)` — issue #156 item
// 3: dormant assert bodies count against C2's inline thresholds, so the hot
// body carries a single small call and the contract walk (the same
// branch/loop structure as the write walk, asserts only) stays out of it. A
// nested type's contracts are its own predicate's, asserted by its own entry.
package java

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) pf(format string, args ...any) {
	fmt.Fprintf(&g.fn, format, args...)
}

// maskHexL renders the (1<<bits)-1 mask for bits in [1,64] as a long
// literal — the L suffix everywhere, so a 32-bit mask never sign-extends.
func maskHexL(bits int64) string {
	if bits == 64 {
		return "-1L" // all 64 bits — the bit-transparent spelling
	}
	return fmt.Sprintf("0x%xL", (uint64(1)<<uint(bits))-1)
}

// loadInt renders an integer storage expression widened to value semantics
// in a long context. Signed storage sign-extends on its own and unsigned
// storage is WIDER than its width (uint8/uint16 in int, uint32 in long), so
// both already hold the value; only a uint8 array element, which stays a
// byte[] (the byte-buffer idiom and the bulk copy), masks after widening.
func (g *gen) loadInt(t ir.FieldType, expr string) string {
	if t.Signed || t.Width >= 64 || g.intType(t) != "byte" {
		return expr
	}
	return fmt.Sprintf("(%s & %s)", expr, maskHexL(int64(t.Width)))
}

// loadUnsignedWidth masks a narrow storage expression to width bits after
// widening (enum and tag storage — always unsigned value semantics).
func loadUnsignedWidth(width int, expr string) string {
	if width >= 64 {
		return expr
	}
	return fmt.Sprintf("(%s & %s)", expr, maskHexL(int64(width)))
}

// storeCast is the narrowing cast that lands a decoded long back in its
// storage type ("" for long). The cast recovers signed narrows' sign bits —
// (byte) v is the (v << 56) >> 56 of the wider-domain targets.
func storeCast(typ string) string {
	if typ == "long" {
		return ""
	}
	return "(" + typ + ") "
}

// putW merges expr (a long-context value already masked to bits, bits in
// [1,32]) at the write position — the family bitpacker's writeBits through
// the putBits helper, constant width.
func (g *gen) putW(expr string, bits int64, ind string) {
	g.settle(ind)
	g.use("putBits")
	g.pf("%sscratch = putBits(data, pos, scratch, %s, %d);\n", ind, expr, bits)
	g.pf("%spos += %d;\n", ind, bits)
}

// readR reads bits (in [1,32]) from the 64-bit window at bitsRead into v,
// masked — the family bitpacker's readBits through the peekBits helper, with
// the tail window keeping every load inside the buffer (no slack contract).
func (g *gen) readR(bits int64, ind string) {
	g.needV = true
	g.usesRead = true
	g.usesPeek = true
	g.use("peekBits")
	g.pf("%sv = peekBits(data, bitsRead, tailBase, tailWord) & %s;\n", ind, maskHexL(bits))
	g.pf("%sbitsRead += %d;\n", ind, bits)
}

// failRead is the refusal every read check takes: -1 out of the Bits entry.
const failRead = "return -1;"

// boundRead emits the bounds check `bitsRead + need > numBits` -> refuse.
func (g *gen) boundRead(need, ind string) {
	g.usesRead = true
	g.pf("%sif (bitsRead + %s > numBits) {\n%s    %s\n%s}\n", ind, need, ind, failRead, ind)
}

// ---- nested types: a call to the callee's own Bits entry ----

// settle reloads the scratch word after nested writes left data holding the
// value so far (stored): before a merge, and at every block boundary, so
// each path into and out of a block agrees on where the partial word lives.
// Back-to-back nested writes skip the reload and store between them.
func (g *gen) settle(ind string) {
	if g.stored {
		g.use("loadBits")
		g.pf("%sscratch = loadBits(data, pos);\n", ind)
		g.stored = false
		g.usesScratch = true
	}
}

// store hands the partial scratch word to data ahead of a nested write or a
// bulk copy, unless it is there already.
func (g *gen) store(ind string) {
	if !g.stored {
		g.use("flushBits")
		g.pf("%sflushBits(data, pos, scratch);\n", ind)
		g.stored = true
	}
}

// bitsFn is the qualified Bits entry of a type or union for a verb.
func (g *gen) bitsFn(verb, typeName string) string {
	return g.qualify(typeName, verb+typeName+"Bits")
}

// emitWriteCall writes a nested type or union through its own entry: the
// partial scratch word goes to data first, the callee merges at pos, and
// the caller reloads the word it left when it next merges (settle).
func (g *gen) emitWriteCall(typeName, expr, ind string) {
	g.store(ind)
	g.pf("%spos = %s(%s, data, pos);\n", ind, g.bitsFn("write", typeName), expr)
}

// emitReadCall reads a nested type or union through its own entry, passing
// a refusal straight out.
func (g *gen) emitReadCall(typeName, expr, ind string) {
	g.pf("%sbitsRead = %s(%s, data, bitsRead, numBits);\n", ind, g.bitsFn("read", typeName), expr)
	g.pf("%sif (bitsRead < 0) {\n%s    %s\n%s}\n", ind, ind, failRead, ind)
}

// isNested reports a named struct or union reference: the field kinds that
// serialize through their own declaration's entries.
func isNested(t ir.FieldType) bool {
	return t.Kind == ir.TNamed && isClassRef(t.Ref)
}

// ---- static wire-width analysis for run fusing ----

// staticBitsItem reports an item's exact wire bits when they are the same on
// every path (branches count only when both sides agree; strings, counted
// arrays, unions with any nonzero arm, and align are dynamic). Exactness
// matters: the fused read bound `bitsRead + runBits > numBits` must never
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
	case ir.TString, ir.TBytes, ir.TWString:
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

// constName is the UPPER_SNAKE spelling of a generated constant — Java's
// static-final convention, and the same mapping the Rust target shares
// (ShipCreateMaxBits -> SHIP_CREATE_MAX_BITS), so the checker's registry for
// Rust constants covers these.
func constName(name string) string {
	return ir.RustConstName(name)
}

func (g *gen) emitBounds(name, what string, maxBits int64) {
	bits, bytes := constName(name+"MaxBits"), constName(name+"MaxBytes")
	g.bpf("    // %s is %s (SPEC §6.1).\n", bits, what)
	g.bpf("    // %s is rounded up to the 8-byte write-buffer granularity.\n", bytes)
	g.bpf("    public static final int %s = %d;\n", bits, maxBits)
	g.bpf("    public static final int %s = %d;\n\n", bytes, ir.MaxBytes(maxBits))
}

func (g *gen) emitStructFunctions(st *ir.Struct) {
	g.usesWire = true
	g.emitBounds(st.Name, "the longest wire path; align pads at worst case", ir.MaxBitsStruct(st))

	g.emitZeroFunction(st.Name, st.Fields)
	g.emitInitFunction(st.Name, st.Fields)
	g.emitCheckFunction(st.Name, func(ind string) { g.emitCheckItems(st.Items, "value", ind) })
	g.emitWriteFunction(st.Name, func(ind string) { g.emitWriteItems(st.Items, "value", ind) })
	g.emitReadFunction(st.Name, func(ind string) { g.emitReadItems(st.Items, "value", ind, false) })
}

// emitUnionFunctions emits the union's bounds and the same function surface
// as a type: the tag rides in minimal bits, then the selected arm only
// (SPEC §4.8).
func (g *gen) emitUnionFunctions(d *ir.Union) {
	g.usesWire = true
	g.emitBounds(d.Name, "the tag plus the largest arm; None costs the tag only", ir.MaxBitsUnion(d))

	g.bpf("    // zero%s resets value to the §5 zero form — the empty union. The tag alone\n", d.Name)
	g.bpf("    // resets: unselected arms are unspecified by rule (SPEC §4.8), and every arm\n")
	g.bpf("    // is unselected at None; an arm initializes at its next selection.\n")
	g.bpf("    public static void zero%s(%s value) {\n", d.Name, d.Name)
	g.bpf("        value.type = %sType.NONE;\n    }\n\n", d.Name)

	g.emitCheckFunction(d.Name, func(ind string) {
		// the tag contract; each arm's own contracts bind in its own entry
		g.assertRange(loadUnsignedWidth(enumStorageBitsForTag(d.Max), "value.type"), big.NewInt(0), big.NewInt(d.Max), true, ind)
	})
	g.emitWriteFunction(d.Name, func(ind string) { g.emitWriteUnion(d, "value", ind) })
	g.emitReadFunction(d.Name, func(ind string) { g.emitReadUnion(d, "value", ind) })
}

func (g *gen) resetFn() {
	g.fn.Reset()
	g.loopDepth = 0
	g.needV, g.needLo, g.needHi, g.usesRead, g.usesPeek = false, false, false, false, false
	g.stored, g.usesScratch = false, false
}

const bodyInd = "        " // function bodies sit two levels inside the outer class

func (g *gen) emitWriteFunction(name string, walk func(ind string)) {
	g.resetFn()
	// a write starts with data holding everything below bitOffset — the
	// stored state: the first merge loads the partial word (settle), and a
	// value that starts with a nested type hands data straight on
	g.stored = true
	walk(bodyInd)
	body := g.fn.String()
	if !g.stored {
		g.use("flushBits")
	}
	maxBytes := constName(name + "MaxBytes")

	g.bpf("    // write%sBits packs value into data from bit bitOffset on and returns the bit\n", name)
	g.bpf("    // position after it — the trusted writer (contracts in the checkWrite%s\n", name)
	g.bpf("    // predicate, one dormant assert call without -ea). Bits below bitOffset are kept;\n")
	g.bpf("    // data must be a multiple of 8 bytes long and hold bitOffset + %s.\n", constName(name+"MaxBits"))
	g.bpf("    public static int write%sBits(%s value, byte[] data, int bitOffset) {\n", name, name)
	g.bpf("        assert checkWrite%s(value, data, bitOffset);\n", name)
	if body == "" {
		g.bpf("        // empty body — presence is the payload (SPEC §4.6)\n")
		g.bpf("        return bitOffset;\n")
	} else {
		g.bpf("        int pos = bitOffset;\n")
		if g.usesScratch {
			g.bpf("        long scratch;\n")
		}
		if g.needV {
			g.bpf("        long v;\n")
		}
		g.body.WriteString(body)
		if !g.stored {
			g.bpf("        flushBits(data, pos, scratch);\n")
		}
		g.bpf("        return pos;\n")
	}
	g.bpf("    }\n\n")

	g.bpf("    // write%s packs value into data from its start and returns the bytes written;\n", name)
	g.bpf("    // data must hold %s.\n", maxBytes)
	g.bpf("    public static int write%s(%s value, byte[] data) {\n", name, name)
	g.bpf("        return (write%sBits(value, data, 0) + 7) >>> 3;\n", name)
	g.bpf("    }\n\n")
}

func (g *gen) emitReadFunction(name string, walk func(ind string)) {
	g.resetFn()
	walk(bodyInd)
	body := g.fn.String()

	g.bpf("    // read%sBits decodes value from data starting at bit bitOffset, the payload\n", name)
	g.bpf("    // ending at bit numBits, and returns the bit position after it — or -1, the\n")
	g.bpf("    // family read verdict refusing the wire (bounds, ranges, wire constants,\n")
	g.bpf("    // padding). Hostile bytes never throw. No slack past the payload is required.\n")
	g.bpf("    public static int read%sBits(%s value, byte[] data, int bitOffset, int numBits) {\n", name, name)
	if g.usesRead {
		g.bpf("        if (bitOffset < 0 || numBits > (long) data.length * 8) {\n")
		g.bpf("            return -1; // the payload cannot exceed the buffer behind data\n")
		g.bpf("        }\n")
	}
	if g.usesPeek {
		g.use("loadTail")
		g.bpf("        // the final 64-bit window, assembled once so every load stays inside\n")
		g.bpf("        // the buffer (the family's no-slack reader stance)\n")
		g.bpf("        final int tailBase = Math.max(data.length - 8, 0);\n")
		g.bpf("        final long tailWord = loadTail(data);\n")
	}
	if body == "" {
		g.bpf("        return bitOffset;\n")
	} else {
		g.bpf("        int bitsRead = bitOffset;\n")
		if g.needV {
			g.bpf("        long v;\n")
		}
		if g.needLo {
			g.bpf("        long lo;\n")
		}
		if g.needHi {
			g.bpf("        long hi;\n")
		}
		g.body.WriteString(body)
		g.bpf("        return bitsRead;\n")
	}
	g.bpf("    }\n\n")

	g.bpf("    // read%s decodes value from the first numBits of data — the family read\n", name)
	g.bpf("    // verdict: false refuses the wire.\n")
	g.bpf("    public static boolean read%s(%s value, byte[] data, int numBits) {\n", name, name)
	g.bpf("        return read%sBits(value, data, 0, numBits) >= 0;\n", name)
	g.bpf("    }\n\n")
}

// emitZeroFunction emits the §5 zero form for a class — all-zero storage,
// specified defaults NOT reapplied (those live in construction only; the
// wire contract stays a pure function of the encodings).
func (g *gen) emitZeroFunction(name string, fields []*ir.Field) {
	g.bpf("    // The §5 zero form: all-zero storage; specified defaults live only in\n")
	g.bpf("    // construction.\n")
	g.bpf("    public static void zero%s(%s value) {\n", name, name)
	if len(fields) == 0 {
		g.bpf("        // empty body — nothing to reset (SPEC §4.6)\n")
	}
	g.resetFn()
	for _, f := range fields {
		g.emitZeroField(f, "value", bodyInd)
	}
	g.body.WriteString(g.fn.String())
	g.bpf("    }\n\n")
}

// emitInitFunction restores construction defaults without replacing any
// preallocated objects or buffers; applications may call it before reuse.
func (g *gen) emitInitFunction(name string, fields []*ir.Field) {
	g.bpf("    // Restore construction defaults in place; buffers and objects are retained.\n")
	g.bpf("    public static void init%s(%s value) {\n", name, name)
	if len(fields) == 0 {
		g.bpf("        // empty body — nothing to initialize\n")
	}
	g.resetFn()
	for _, f := range fields {
		g.emitInitializeField(f, "value", bodyInd, true)
	}
	g.body.WriteString(g.fn.String())
	g.bpf("    }\n\n")
}

// ---- the checkWrite predicate (issue #156 item 3) ----

// emitCheckFunction emits checkWrite<Name>: the writer's whole contract walk
// in one private predicate, so the hot write body carries a single dormant
// `assert checkWriteX(value, data, bitOffset)` instead of every assert's
// bytecode. The walk mirrors the write walk's branch/loop/switch structure —
// only the taken side's contracts bind, exactly as the interleaved asserts
// would. Nested types assert their own contracts in their own entries.
func (g *gen) emitCheckFunction(name string, walk func(ind string)) {
	g.resetFn()
	walk(bodyInd)
	body := g.fn.String()

	g.bpf("    // checkWrite%s is the contract walk of write%sBits, called once\n", name, name)
	g.bpf("    // through assert — the predicate-extraction form: dormant assert bodies count against the\n")
	g.bpf("    // JIT's inline thresholds, so the hot body carries one small call and the\n")
	g.bpf("    // contracts live here (issue #156).\n")
	g.bpf("    private static boolean checkWrite%s(%s value, byte[] data, int bitOffset) {\n", name, name)
	g.bpf("        assert data.length %% 8 == 0;\n")
	g.bpf("        assert bitOffset >= 0;\n")
	g.bpf("        assert (long) data.length * 8 >= (long) bitOffset + %s;\n", constName(name+"MaxBits"))
	g.body.WriteString(body)
	g.bpf("        return true;\n")
	g.bpf("    }\n\n")
}

func (g *gen) hasCheckItems(items []ir.Item) bool {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			if g.hasCheckField(item.F) {
				return true
			}
		case *ir.Branch:
			if g.hasCheckItems(item.Then) || g.hasCheckItems(item.Else) {
				return true
			}
		}
	}
	return false
}

func (g *gen) hasCheckField(f *ir.Field) bool {
	if f.Array == ir.ArrayFixed && g.bulkBytes[f] {
		return false // bare uint8 elements carry no contract
	}
	if f.Array == ir.ArrayCounted {
		return true // the count's own range binds, whatever the elements are
	}
	return g.hasCheckScalar(f)
}

func (g *gen) hasCheckScalar(f *ir.Field) bool {
	switch f.Type.Kind {
	case ir.TString, ir.TBytes, ir.TWString:
		return true
	case ir.TFixed:
		return true
	case ir.TInt:
		return f.HasIntRange || g.widened(f)
	case ir.TFloat32:
		return f.HasFloatRange
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			return true
		case *ir.Flags:
			return ref.WireBits < 64
		}
		// a nested type or union asserts its own contracts in its own entry
	}
	return false
}

// widened reports a bare unsigned integer whose storage is wider than its
// width (uint8/uint16 in int, uint32 in long): the storage admits values the
// wire cannot carry, so the width's range is a writer contract. A uint8
// array element stays a byte and needs none.
func (g *gen) widened(f *ir.Field) bool {
	t := f.Type
	if t.Kind != ir.TInt || t.Signed || t.Width >= 64 || f.HasIntRange {
		return false
	}
	return f.Array == ir.ArrayNone || t.Width > 8
}

func (g *gen) emitCheckItems(items []ir.Item, path, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitCheckField(item.F, path, ind)
		case *ir.Branch:
			if !g.hasCheckItems(item.Then) && !g.hasCheckItems(item.Else) {
				continue
			}
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.pf("%sif (%s%s.%s) {\n", ind, neg, path, javaName(item.Cond))
			g.emitCheckItems(item.Then, path, ind+"    ")
			if item.Else != nil && g.hasCheckItems(item.Else) {
				g.pf("%s} else {\n", ind)
				g.emitCheckItems(item.Else, path, ind+"    ")
			}
			g.pf("%s}\n", ind)
		}
	}
}

func (g *gen) emitCheckField(f *ir.Field, path, ind string) {
	name := path + "." + javaName(f.Name)
	if f.Name == "" {
		name = path // the standalone union functions' self item
	}
	switch f.Array {
	case ir.ArrayFixed:
		if g.bulkBytes[f] || !g.hasCheckScalar(f) {
			return
		}
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (int %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
		g.emitCheckElem(f, name, iv, ind+"    ")
		g.pf("%s}\n", ind)
		g.loopDepth--
	case ir.ArrayCounted:
		// the count's own range is the first contract here (SPEC §4.6), then
		// the elements' contracts walk
		count := name + "Count"
		g.pf("%sassert %s >= %d;\n", ind, count, f.ArrayMin)
		g.pf("%sassert %s <= %d;\n", ind, count, f.ArrayBound)
		if !g.hasCheckScalar(f) {
			return
		}
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (int %s = 0; %s < %s; %s++) {\n", ind, iv, iv, count, iv)
		g.emitCheckElem(f, name, iv, ind+"    ")
		g.pf("%s}\n", ind)
		g.loopDepth--
	default:
		g.emitCheckScalar(f, name, ind)
	}
}

func (g *gen) emitCheckElem(f *ir.Field, name, iv, ind string) {
	g.inElem = true
	g.emitCheckScalar(f, fmt.Sprintf("%s[%s]", name, iv), ind)
	g.inElem = false
}

func (g *gen) emitCheckScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TString, ir.TBytes, ir.TWString:
		g.assertRange(name+"Length", big.NewInt(0), big.NewInt(f.Type.Size), true, ind)
	case ir.TFixed:
		rawMin, rawMax, _ := fixedRaw(f)
		if f.Type.Width == 128 {
			g.emitCheck128Ranged(f.Type.Signed, name, rawMin, rawMax, ind)
			return
		}
		loaded := g.loadInt(f.Type, name)
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate: zero bits — the contract is the whole write (SPEC §4.6)
			g.pf("%sassert %s == %s;\n", ind, loaded, javaIntLit(rawMin))
			return
		}
		g.assertRange(loaded, rawMin, rawMax, rawMax.Cmp(maxInt64) <= 0, ind)
	case ir.TInt:
		if f.Type.Width == 128 {
			if f.HasIntRange {
				g.emitCheck128Ranged(f.Type.Signed, name, f.IntMin, f.IntMax, ind)
			}
			return
		}
		if !f.HasIntRange {
			if g.intType(f.Type) != "byte" && !f.Type.Signed && f.Type.Width < 64 {
				// the width's own range: wider storage admits more (SPEC §6.1)
				max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
				g.assertRange(name, big.NewInt(0), max, true, ind)
			}
			return
		}
		loaded := g.loadInt(f.Type, name)
		if f.IntMin.Cmp(f.IntMax) == 0 {
			g.pf("%sassert %s == %s;\n", ind, loaded, javaIntLit(f.IntMin))
			return
		}
		g.assertRange(loaded, f.IntMin, f.IntMax, f.IntMax.Cmp(maxInt64) <= 0, ind)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.pf("%sassert %s - %s == 0.0f; // finite: NaN and both infinities fail this\n", ind, name, name)
		}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			// headroom above the wire range cannot ride
			g.assertRange(loadUnsignedWidth(ref.StorageBits, name), big.NewInt(0), big.NewInt(ref.Max), true, ind)
		case *ir.Flags:
			if ref.WireBits < 64 {
				// a mask bit above the wire width cannot ride
				g.pf("%sassert %s >>> %d == 0;\n", ind, name, ref.WireBits)
			}
		}
	}
}

// emitCheck128Ranged asserts the pair bounds of a ranged 128-bit-storage
// value; vacuous halves against the storage domain are elided.
func (g *gen) emitCheck128Ranged(signed bool, name string, min, max *big.Int, ind string) {
	loDomain, hiDomain := big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	cmp := "compareUnsigned"
	if signed {
		cmp = "compareTo"
		loDomain = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 127))
		hiDomain = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1))
	}
	if min.Cmp(max) == 0 {
		// degenerate range: zero bits — the contract is the whole write
		g.pf("%sassert %s.equals(%s);\n", ind, name, pairCtor(signed, min))
		return
	}
	guardLo := min.Cmp(loDomain) > 0
	guardHi := max.Cmp(hiDomain) < 0
	if !guardLo && !guardHi {
		return
	}
	g.pf("%s{\n", ind)
	if guardLo {
		g.pf("%s    final %s min = %s;\n", ind, pairType(signed), pairCtor(signed, min))
	}
	if guardHi {
		g.pf("%s    final %s max = %s;\n", ind, pairType(signed), pairCtor(signed, max))
	}
	if guardLo {
		g.pf("%s    assert %s.%s(min) >= 0;\n", ind, name, cmp)
	}
	if guardHi {
		g.pf("%s    assert %s.%s(max) <= 0;\n", ind, name, cmp)
	}
	g.pf("%s}\n", ind)
}

func pairType(signed bool) string {
	if signed {
		return "Int128"
	}
	return "UInt128"
}

// assertRange emits the write-contract asserts for a value-semantics long
// expression in [min, max]. signedDomain selects the comparison domain;
// vacuous halves are elided against that domain's edges.
func (g *gen) assertRange(expr string, min, max *big.Int, signedDomain bool, ind string) {
	if signedDomain {
		if min.Cmp(minInt64) > 0 {
			g.pf("%sassert %s >= %s;\n", ind, expr, javaIntLit(min))
		}
		if max.Cmp(maxInt64) < 0 {
			g.pf("%sassert %s <= %s;\n", ind, expr, javaIntLit(max))
		}
		return
	}
	// unsigned 64-bit domain: bit-transparent compares
	if min.Sign() > 0 {
		g.pf("%sassert Long.compareUnsigned(%s, %s) >= 0;\n", ind, expr, javaIntLit(min))
	}
	if max.Cmp(maxUint64Big) < 0 {
		g.pf("%sassert Long.compareUnsigned(%s, %s) <= 0;\n", ind, expr, javaIntLit(max))
	}
}

// enumStorageBitsForTag is the storage width backing a union tag, matching
// tagJavaType's signed-capacity rule.
func enumStorageBitsForTag(max int64) int {
	switch tagJavaType(max) {
	case "byte":
		return 8
	case "short":
		return 16
	case "int":
		return 32
	}
	return 64
}

// tagSwitchExpr renders a union tag as a switch selector — Java cannot
// switch on long, so a long tag narrows (cases are small variant ordinals).
func tagSwitchExpr(u *ir.Union, expr string) string {
	if tagJavaType(u.Max) == "long" {
		return "(int) " + expr + ".type"
	}
	return expr + ".type"
}

// ---- write emission ----

func (g *gen) emitWriteItems(items []ir.Item, path, ind string) {
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
			g.settle(ind)
			g.pf("%sif (%s%s.%s) {\n", ind, neg, path, javaName(item.Cond))
			g.emitWriteItems(item.Then, path, ind+"    ")
			g.settle(ind + "    ")
			if item.Else != nil {
				g.pf("%s} else {\n", ind)
				g.emitWriteItems(item.Else, path, ind+"    ")
				g.settle(ind + "    ")
			}
			g.pf("%s}\n", ind)
		}
	}
}

// emitWriteRaw merges a compile-time constant (const/reserved items) of up
// to 64 bits, split low dword first past 32 (the serialize group rule).
func (g *gen) emitWriteRaw(value *big.Int, bits int64, ind string) {
	masked := new(big.Int).And(value, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1)))
	if bits <= 32 {
		g.putW(javaIntLit(masked), bits, ind)
		return
	}
	lo := new(big.Int).And(masked, new(big.Int).SetUint64(0xffffffff))
	hi := new(big.Int).Rsh(masked, 32)
	g.putW(javaIntLit(lo), 32, ind)
	g.putW(javaIntLit(hi), bits-32, ind)
}

// emitWriteAlign pads the write position to the next byte boundary with
// zeros (nothing merges, so only the position moves — with the flush kept,
// exactly as the merge would keep it).
func (g *gen) emitWriteAlign(ind string) {
	g.settle(ind)
	g.use("alignBits")
	g.pf("%sscratch = alignBits(data, pos, scratch);\n", ind)
	g.pf("%spos = (pos + 7) & -8;\n", ind)
}

// emitWriteBulk lands a byte-aligned payload with the fused bulk copy —
// the port's writeBytes: store the partial scratch word, arraycopy the
// payload at the byte cursor, reload the trailing partial word. Wire-
// identical to the per-byte merge.
func (g *gen) emitWriteBulk(src, lenBytes, lenBits, ind string) {
	g.pf("%s// byte-aligned payload — the fused bulk copy (flush, copy, reload),\n", ind)
	g.pf("%s// wire-identical to the per-byte merge\n", ind)
	g.store(ind)
	g.pf("%sSystem.arraycopy(%s, 0, data, pos >>> 3, %s);\n", ind, src, lenBytes)
	g.pf("%spos += %s;\n", ind, lenBits)
}

func (g *gen) emitWriteField(f *ir.Field, path, ind string) {
	name := path + "." + javaName(f.Name)
	if f.Name == "" {
		name = path // the standalone union functions' self item
	}
	switch f.Array {
	case ir.ArrayFixed:
		if g.bulkBytes[f] {
			// statically byte-aligned [N]uint8 (ir.AlignedFixedByteArrays)
			g.emitWriteBulk(name, fmt.Sprintf("%d", f.ArrayBound), fmt.Sprintf("%d", f.ArrayBound*8), ind)
			return
		}
		g.emitWriteLoop(f, name, fmt.Sprintf("%d", f.ArrayBound), ind)
	case ir.ArrayCounted:
		count := name + "Count"
		// the count guards the loop; its range is a writer contract like any
		// other, asserted in the checkWrite predicate and gone without -ea
		// (SPEC §4.6, §5)
		g.emitWriteOffset(count, big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), ind)
		g.emitWriteLoop(f, name, count, ind)
	default:
		g.emitWriteScalar(f, name, ind)
	}
}

// emitWriteLoop writes an array's elements. Nested elements call their own
// entry, the scratch word stored once before the loop and reloaded once
// after it — nothing in between touches it.
func (g *gen) emitWriteLoop(f *ir.Field, name, bound, ind string) {
	iv := fmt.Sprintf("i%d", g.loopDepth)
	g.loopDepth++
	nested := isNested(f.Type)
	if nested {
		g.store(ind) // the loop body never touches the scratch word
	} else {
		g.settle(ind)
	}
	g.pf("%sfor (int %s = 0; %s < %s; %s++) {\n", ind, iv, iv, bound, iv)
	elem := fmt.Sprintf("%s[%s]", name, iv)
	if nested {
		g.pf("%s    pos = %s(%s, data, pos);\n", ind, g.bitsFn("write", f.Type.Name), elem)
	} else {
		g.inElem = true
		g.emitWriteScalar(f, elem, ind+"    ")
		g.inElem = false
		g.settle(ind + "    ")
	}
	g.pf("%s}\n", ind)
	g.loopDepth--
}

// emitWriteOffset merges (expr - min) in the folded bit count for a range
// inside the 64-bit domain: single group through 32 bits, two groups (low
// dword first) past it — the serialize group structure exactly. Offsets wrap
// two's complement, which is the unsigned-domain subtraction every family
// port performs. expr must already carry value semantics in a long context.
func (g *gen) emitWriteOffset(expr string, min, max *big.Int, ind string) {
	bits := ir.BitsRequired(min, max)
	if bits == 0 {
		return // degenerate range: ZERO bits — the contract is the whole write
	}
	off := expr
	if min.Sign() > 0 {
		off = fmt.Sprintf("%s - %s", expr, javaIntLit(min))
	} else if min.Sign() < 0 {
		// subtracting a negative min is adding its magnitude — the wrapping
		// arithmetic is identical and the spelling avoids `- -N`
		if neg := new(big.Int).Neg(min); neg.Cmp(maxInt64) <= 0 {
			off = fmt.Sprintf("%s + %s", expr, javaIntLit(neg))
		} else {
			off = fmt.Sprintf("%s - %s", expr, javaIntLit(min)) // int64 min, a hex literal
		}
	}
	if bits <= 32 {
		g.putW(fmt.Sprintf("(%s) & %s", off, maskHexL(bits)), bits, ind)
		return
	}
	if min.Sign() != 0 {
		g.pf("%s{\n", ind)
		g.pf("%s    final long off = %s;\n", ind, off)
		g.emitWriteWide64("off", bits, ind+"    ")
		g.pf("%s}\n", ind)
		return
	}
	g.emitWriteWide64(expr, bits, ind)
}

// emitWriteWide64 merges the low `bits` bits (33..64) of a long expression
// as two groups, low dword first.
func (g *gen) emitWriteWide64(expr string, bits int64, ind string) {
	g.putW(expr+" & 0xffffffffL", 32, ind)
	g.putW(fmt.Sprintf("(%s >>> 32) & %s", expr, maskHexL(bits-32)), bits-32, ind)
}

// emitWriteWide128 merges the low `bits` bits (1..128) of a (hi, lo) pair
// expression, 32-bit groups least significant first.
func (g *gen) emitWriteWide128(loExpr, hiExpr string, bits int64, ind string) {
	switch {
	case bits <= 32:
		g.putW(loExpr+" & "+maskHexL(bits), bits, ind)
	case bits <= 64:
		g.emitWriteWide64(loExpr, bits, ind)
	case bits <= 96:
		g.emitWriteWide64(loExpr, 64, ind)
		g.putW(hiExpr+" & "+maskHexL(bits-64), bits-64, ind)
	default:
		g.emitWriteWide64(loExpr, 64, ind)
		g.emitWriteWide64(hiExpr, bits-64, ind)
	}
}

func (g *gen) emitWriteScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		g.emitWriteFixed(f, name, ind)
	case ir.TInt:
		g.emitWriteInt(f, name, ind)
	case ir.TBits:
		w := int64(f.Type.Width)
		if w <= 32 {
			g.putW(name+" & "+maskHexL(w), w, ind)
			return
		}
		g.emitWriteWide64(name, w, ind)
	case ir.TBool:
		g.putW(name+" ? 1 : 0", 1, ind)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.emitWriteCompressedFloat(f, name, ind)
			return
		}
		// bit transparent: raw bits preserve every NaN payload
		g.putW(fmt.Sprintf("Float.floatToRawIntBits(%s) & 0xffffffffL", name), 32, ind)
	case ir.TFloat64:
		g.pf("%s{\n", ind)
		g.pf("%s    final long b = Double.doubleToRawLongBits(%s);\n", ind, name)
		g.emitWriteWide64("b", 64, ind+"    ")
		g.pf("%s}\n", ind)
	case ir.TWString:
		g.emitWriteWString(f, name, ind)
	case ir.TString, ir.TBytes:
		g.emitWriteBytesField(f, name, ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.emitWriteEnum(ref, name, ind)
		case *ir.Flags:
			g.emitWriteFlags(ref, name, ind)
		case *ir.Struct, *ir.Union:
			g.emitWriteCall(f.Type.Name, name, ind)
		}
	}
}

// f32lit renders the float32 rounding of v as a Java float literal body
// (exact); callers append the f suffix.
func f32lit(v float64) string {
	return formatFloat32(float64(float32(v)))
}

// emitWriteCompressedFloat is the family's two-rounding float32 quantization
// with the declaration folded: normalize, clamp (which also grounds NaN),
// quantize — native float arithmetic IS the family's float32 rounding at
// every step (SPEC §4.3), and Java's FP semantics are strict (no fused ops).
func (g *gen) emitWriteCompressedFloat(f *ir.Field, name, ind string) {
	maxInt, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	minF := float32(f.FMin)
	deltaF := float32(f.FMax) - minF
	mivF := float32(maxInt)
	g.pf("%s{\n", ind)
	g.pf("%s    float n = (%s - %sf) / %sf;\n", ind, name, f32lit(float64(minF)), f32lit(float64(deltaF)))
	g.pf("%s    if (!(n >= 0.0f)) {\n", ind)
	g.pf("%s        n = 0.0f;\n", ind)
	g.pf("%s    } else if (!(n <= 1.0f)) {\n", ind)
	g.pf("%s        n = 1.0f;\n", ind)
	g.pf("%s    }\n", ind)
	g.pf("%s    // two roundings, not one: the product rounds to float32 BEFORE 0.5\n", ind)
	g.pf("%s    // is added, and the sum rounds before the floor (SPEC §4.3)\n", ind)
	g.pf("%s    v = (long) Math.floor(n * %sf + 0.5f);\n", ind, f32lit(float64(mivF)))
	g.pf("%s    if (v > %dL) v = %dL;\n", ind, maxInt, maxInt)
	g.pf("%s}\n", ind)
	g.needV = true
	g.putW("v", bits, ind)
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

func (g *gen) emitWriteFixed(f *ir.Field, name, ind string) {
	rawMin, rawMax, bits := fixedRaw(f)
	if f.Type.Width == 128 {
		g.emitWrite128Ranged(f.Type.Signed, name, rawMin, rawMax, bits, ind)
		return
	}
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// degenerate: zero bits — the contract (in checkWrite) is the whole
		// write (SPEC §4.6)
		return
	}
	g.emitWriteOffset(g.loadInt(f.Type, name), rawMin, rawMax, ind)
}

func (g *gen) emitWriteInt(f *ir.Field, name, ind string) {
	w := int64(f.Type.Width)
	if w == 128 {
		if f.HasIntRange {
			g.emitWrite128Ranged(f.Type.Signed, name, f.IntMin, f.IntMax, ir.BitsRequired(f.IntMin, f.IntMax), ind)
			return
		}
		// uint128 raw: full 128 bits, 32-bit groups least significant first
		g.emitWriteWide128(name+".lo", name+".hi", 128, ind)
		return
	}
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: ZERO bits — the contract is the whole write
			return
		}
		g.emitWriteOffset(g.loadInt(f.Type, name), f.IntMin, f.IntMax, ind)
		return
	}
	// bare integer at storage width; the width mask lands the same-width
	// unsigned pattern whichever way the storage widened (the sign smear a
	// wider merge would spread corrupts neighboring wire data, as in C++)
	if w == 64 {
		g.emitWriteWide64(name, 64, ind)
		return
	}
	g.putW(name+" & "+maskHexL(w), w, ind)
}

// emitWrite128Ranged writes a ranged 128-bit-storage value (int128/uint128,
// fixed of width 128): the offset from min in the folded bit count, 32-bit
// groups least significant first (bounds asserted in checkWrite).
func (g *gen) emitWrite128Ranged(signed bool, name string, min, max *big.Int, b int64, ind string) {
	if min.Cmp(max) == 0 {
		return // degenerate range: ZERO bits — the contract is the whole write
	}
	switch {
	case min.Sign() == 0 && !signed:
		g.emitWriteWide128(name+".lo", name+".hi", b, ind)
	case min.Sign() == 0 && signed:
		g.pf("%s{\n", ind)
		g.pf("%s    final UInt128 off = %s.toUnsigned();\n", ind, name)
		g.emitWriteWide128("off.lo", "off.hi", b, ind+"    ")
		g.pf("%s}\n", ind)
	default:
		g.pf("%s{\n", ind)
		if signed {
			g.pf("%s    final UInt128 off = %s.subtract(%s).toUnsigned();\n", ind, name, pairCtor(true, min))
		} else {
			g.pf("%s    final UInt128 off = %s.subtract(%s);\n", ind, name, pairCtor(false, min))
		}
		g.emitWriteWide128("off.lo", "off.hi", b, ind+"    ")
		g.pf("%s}\n", ind)
	}
}

func (g *gen) emitWriteEnum(ref *ir.Enum, name, ind string) {
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
	if bits == 0 {
		return // degenerate [0, 0]: zero bits; the contract still binds in checkWrite
	}
	g.putW(name+" & "+maskHexL(bits), bits, ind)
}

func (g *gen) emitWriteFlags(ref *ir.Flags, name, ind string) {
	wb := int64(ref.WireBits)
	if wb <= 32 {
		g.putW(name+" & "+maskHexL(wb), wb, ind)
		return
	}
	g.emitWriteWide64(name, wb, ind)
}

// emitWriteBytesField writes string(N)/bytes(N): folded ranged length,
// align (zero pad), then the used bytes through the fused bulk copy — the
// classic serialize_string framing composed from primitives, byte-identical
// to every other target's.
func (g *gen) emitWriteBytesField(f *ir.Field, name, ind string) {
	length := name + "Length"
	g.emitWriteOffset(length, big.NewInt(0), big.NewInt(f.Type.Size), ind)
	g.emitWriteAlign(ind)
	g.emitWriteBulk(name, length, length+" * 8", ind)
}

// emitWriteUnion is a union's own write body (SPEC §4.8): the tag in
// minimal bits, then the selected arm through its type's own entry. The tag
// contract is asserted in checkWrite BEFORE anything rides.
func (g *gen) emitWriteUnion(u *ir.Union, expr, ind string) {
	if u.Max == 0 {
		return // an empty union's degenerate tag range [0, 0] costs zero bits
	}
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	g.putW(fmt.Sprintf("%s.type & %s", expr, maskHexL(bits)), bits, ind)
	g.store(ind) // every arm writes through its own entry
	g.pf("%sswitch (%s) {\n", ind, tagSwitchExpr(u, expr))
	for i, vr := range u.Variants {
		g.pf("%s    case %d:\n", ind, i+1)
		if vr.Void() {
			g.pf("%s        break; // a payload-free arm: the tag is the whole wire (SPEC §4.8)\n", ind)
			continue
		}
		if len(vr.Ref.Items) == 0 {
			g.pf("%s        break; // empty arm — presence is the payload (SPEC §4.6)\n", ind)
			continue
		}
		g.emitWriteCall(vr.Type, expr+"."+javaName(vr.Name), ind+"        ")
		g.pf("%s        break;\n", ind)
	}
	g.pf("%s}\n", ind)
}

// ---- read emission ----

// emitReadItems walks a scope with run fusing: one bounds check covers each
// maximal run of statically-sized items (bounded=true suppresses checks —
// an enclosing scope already proved the bits).
func (g *gen) emitReadItems(items []ir.Item, path, ind string, bounded bool) {
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
				g.boundRead(fmt.Sprint(total), ind)
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
func (g *gen) emitReadStaticItem(item ir.Item, path, ind string) {
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
		g.pf("%sif (%s%s.%s) {\n", ind, neg, path, javaName(item.Cond))
		g.emitReadItems(item.Then, path, ind+"    ", true)
		g.emitZeroItems(item.Else, path, ind+"    ")
		g.pf("%s} else {\n", ind)
		if item.Else != nil {
			g.emitReadItems(item.Else, path, ind+"    ", true)
		}
		g.emitZeroItems(item.Then, path, ind+"    ")
		g.pf("%s}\n", ind)
	}
}

func (g *gen) emitReadDynamicItem(item ir.Item, path, ind string) {
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
		g.pf("%sif (%s%s.%s) {\n", ind, neg, path, javaName(item.Cond))
		g.emitReadItems(item.Then, path, ind+"    ", false)
		g.emitZeroItems(item.Else, path, ind+"    ")
		g.pf("%s} else {\n", ind)
		if item.Else != nil {
			g.emitReadItems(item.Else, path, ind+"    ", false)
		}
		g.emitZeroItems(item.Then, path, ind+"    ")
		g.pf("%s}\n", ind)
	}
}

// emitReadAlign verifies zero padding to the byte boundary and advances.
func (g *gen) emitReadAlign(ind string) {
	g.usesRead = true
	g.use("alignRead")
	g.pf("%sbitsRead = alignRead(data, bitsRead, numBits);\n", ind)
	g.pf("%sif (bitsRead < 0) {\n%s    %s\n%s}\n", ind, ind, failRead, ind)
}

// emitReadRaw reads a const/reserved item and rejects any other value.
func (g *gen) emitReadRaw(value *big.Int, bits int64, isConst bool, ind string) {
	what := "reserved bits must read zero"
	if isConst {
		what = "a read rejects any other value"
	}
	masked := new(big.Int).And(value, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1)))
	if bits <= 32 {
		g.readR(bits, ind)
		g.pf("%sif (v != %s) {\n%s    return -1; // %s (SPEC §4.3)\n%s}\n", ind, javaIntLit(masked), ind, what, ind)
		return
	}
	g.emitReadWide64(bits, ind)
	g.pf("%sif (lo != %s) {\n%s    return -1; // %s (SPEC §4.3)\n%s}\n", ind, javaIntLit(masked), ind, what, ind)
}

func (g *gen) emitReadStaticField(f *ir.Field, path, ind string) {
	name := path + "." + javaName(f.Name)
	if f.Name == "" {
		name = path
	}
	if f.Array == ir.ArrayFixed {
		if g.bulkBytes[f] {
			// statically byte-aligned [N]uint8: the fused bulk copy — the
			// port's readBytes, wire-identical to the per-byte loop
			g.usesRead = true
			g.pf("%sSystem.arraycopy(data, bitsRead >>> 3, %s, 0, %d);\n", ind, name, f.ArrayBound)
			g.pf("%sbitsRead += %d;\n", ind, f.ArrayBound*8)
			return
		}
		g.emitReadLoop(f, name, fmt.Sprint(f.ArrayBound), ind, true)
		return
	}
	g.emitReadScalar(f, name, ind, true)
}

// emitReadLoop reads an array's elements; nested elements call their own
// entry.
func (g *gen) emitReadLoop(f *ir.Field, name, bound, ind string, bounded bool) {
	iv := fmt.Sprintf("i%d", g.loopDepth)
	g.loopDepth++
	g.pf("%sfor (int %s = 0; %s < %s; %s++) {\n", ind, iv, iv, bound, iv)
	elem := fmt.Sprintf("%s[%s]", name, iv)
	switch {
	case isNested(f.Type):
		g.emitReadCall(f.Type.Name, elem, ind+"    ")
	case is128(f.Type):
		g.emitRead128Scalar(f, elem+" = ", ind+"    ")
	default:
		g.inElem = true
		g.emitReadScalar(f, elem, ind+"    ", bounded)
		g.inElem = false
	}
	g.pf("%s}\n", ind)
	g.loopDepth--
}

func (g *gen) emitReadDynamicField(f *ir.Field, path, ind string) {
	name := path + "." + javaName(f.Name)
	if f.Name == "" {
		name = path
	}
	switch {
	case f.Type.Kind == ir.TWString:
		g.emitReadWString(f, name, ind)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.emitReadBytesField(f, name, ind)
	case f.Array == ir.ArrayCounted:
		count := name + "Count"
		countBits := ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound))
		if countBits > 0 {
			g.boundRead(fmt.Sprint(countBits), ind)
			g.readR(countBits, ind)
			diff := f.ArrayBound - f.ArrayMin
			if diff != (int64(1)<<countBits)-1 {
				g.pf("%sif (v > %d) {\n%s    return -1; // the count guards the loop — reject, never clamp\n%s}\n", ind, diff, ind, ind)
			}
			if f.ArrayMin == 0 {
				g.pf("%s%s = (int) v;\n", ind, count)
			} else {
				g.pf("%s%s = (int) (v + %d);\n", ind, count, f.ArrayMin)
			}
		} else {
			g.pf("%s%s = %d;\n", ind, count, f.ArrayMin)
		}
		if elemBits, ok := g.staticBitsScalar(f); ok {
			if elemBits > 0 {
				g.boundRead(fmt.Sprintf("%s * %dL", count, elemBits), ind)
			}
			g.emitReadLoop(f, name, count, ind, true)
		} else {
			g.emitReadLoop(f, name, count, ind, false)
		}
	case f.Array == ir.ArrayFixed:
		// a fixed array of dynamically-sized elements (branches, unions,
		// strings)
		g.emitReadLoop(f, name, fmt.Sprint(f.ArrayBound), ind, false)
	default:
		// a scalar whose size is dynamic: a nested struct with dynamic
		// items, or a union
		g.emitReadCall(f.Type.Name, name, ind)
	}
}

// emitReadBytesField reads string(N)/bytes(N): ranged length, align with
// zero padding verified, bounds, the bulk byte copy, and (strings) the
// interior-null refusal.
func (g *gen) emitReadBytesField(f *ir.Field, name, ind string) {
	g.usesRead = true
	length := name + "Length"
	lenBits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
	g.boundRead(fmt.Sprint(lenBits), ind)
	g.readR(lenBits, ind)
	if f.Type.Size != (int64(1)<<lenBits)-1 {
		g.pf("%sif (v > %d) {\n%s    return -1; // the length guards the copy — reject, never clamp\n%s}\n", ind, f.Type.Size, ind, ind)
	}
	g.pf("%s%s = (int) v;\n", ind, length)
	g.emitReadAlign(ind)
	g.boundRead(length+" * 8L", ind)
	g.pf("%sSystem.arraycopy(data, bitsRead >>> 3, %s, 0, %s);\n", ind, name, length)
	g.pf("%sbitsRead += %s * 8;\n", ind, length)
	if f.Type.Kind == ir.TString {
		g.emitReadUTF8(name, length, ind)
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (int %s = 0; %s < %s; %s++) {\n", ind, iv, iv, length, iv)
		g.pf("%s    if (%s[%s] == 0) {\n", ind, name, iv)
		g.pf("%s        return -1; // an interior null is content the read refuses (SPEC §4.7)\n", ind)
		g.pf("%s    }\n", ind)
		g.pf("%s}\n", ind)
		g.loopDepth--
	}
}

// emitReadWide64 assembles a 33..64-bit group pair into lo (low dword
// first — the serialize group order).
func (g *gen) emitReadWide64(bits int64, ind string) {
	g.needLo = true
	g.readR(32, ind)
	g.pf("%slo = v;\n", ind)
	g.readR(bits-32, ind)
	g.pf("%slo |= v << 32;\n", ind)
}

// emitReadWide128 assembles a 65..128-bit offset into (hi, lo), 32-bit
// groups least significant first.
func (g *gen) emitReadWide128(bits int64, ind string) {
	g.needLo, g.needHi = true, true
	g.emitReadWide64(64, ind)
	if bits <= 96 {
		g.readR(bits-64, ind)
		g.pf("%shi = v;\n", ind)
		return
	}
	g.readR(32, ind)
	g.pf("%shi = v;\n", ind)
	g.readR(bits-96, ind)
	g.pf("%shi |= v << 32;\n", ind)
}

func (g *gen) emitReadScalar(f *ir.Field, name, ind string, bounded bool) {
	if is128(f.Type) {
		g.emitRead128Scalar(f, name+" = ", ind)
		return
	}
	switch f.Type.Kind {
	case ir.TWString:
		g.emitReadWString(f, name, ind)
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
		g.emitReadWide64(w, ind)
		g.pf("%s%s = lo;\n", ind, name)
	case ir.TBool:
		g.readR(1, ind)
		g.pf("%s%s = v != 0;\n", ind, name)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.emitReadCompressedFloat(f, name, ind)
			return
		}
		g.readR(32, ind)
		g.pf("%s%s = Float.intBitsToFloat((int) v);\n", ind, name)
	case ir.TFloat64:
		g.emitReadWide64(64, ind)
		g.pf("%s%s = Double.longBitsToDouble(lo);\n", ind, name)
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
				g.pf("%sif (v > %d) {\n%s    return -1; // headroom above the wire range is refused\n%s}\n", ind, ref.Max, ind, ind)
			}
			g.pf("%s%s = %sv;\n", ind, name, storeCast(enumJavaType(ref.StorageBits)))
		case *ir.Flags:
			wb := int64(ref.WireBits)
			if wb <= 32 {
				g.readR(wb, ind)
				g.pf("%s%s = v;\n", ind, name)
				return
			}
			g.emitReadWide64(wb, ind)
			g.pf("%s%s = lo;\n", ind, name)
		case *ir.Struct, *ir.Union:
			g.emitReadCall(f.Type.Name, name, ind)
		}
	}
}

func (g *gen) emitReadCompressedFloat(f *ir.Field, name, ind string) {
	maxInt, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	minF := float32(f.FMin)
	deltaF := float32(f.FMax) - minF
	mivF := float32(maxInt)
	g.readR(bits, ind)
	if maxInt != (uint64(1)<<bits)-1 {
		g.pf("%sif (v > %dL) {\n%s    return -1; // headroom above the quantum count is refused\n%s}\n", ind, maxInt, ind, ind)
	}
	// every step rounds to float32: the quotient, the product BEFORE min is
	// added, and the sum — no widening, no fused multiply-add (SPEC §4.3)
	g.pf("%s%s = (float) v / %sf * %sf + %sf;\n",
		ind, name, f32lit(float64(mivF)), f32lit(float64(deltaF)), f32lit(float64(minF)))
}

func (g *gen) emitReadFixed(f *ir.Field, name, ind string) {
	rawMin, rawMax, bits := fixedRaw(f)
	if f.Type.Width == 128 {
		g.emitRead128Ranged(f.Type.Signed, name+" = ", rawMin, rawMax, bits, ind)
		return
	}
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// degenerate: zero bits — the value is the range, raw min << F,
		// materialized with no wire read (SPEC §4.6)
		g.pf("%s%s = %s;\n", ind, name, narrowLit(g.intType(f.Type), rawMin))
		return
	}
	g.emitReadOffset(name, g.intType(f.Type), rawMin, rawMax, bits, ind)
}

// emitReadOffset decodes a ranged value inside the 64-bit domain: the
// offset in `bits` bits, headroom rejected unless the range fills the
// width, min added back in the wrapping domain (exact — decoded values are
// in [min, max], which the storage type holds by declaration).
func (g *gen) emitReadOffset(name, styp string, min, max *big.Int, bits int64, ind string) {
	diff := new(big.Int).Sub(max, min)
	full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	src := "v"
	if bits <= 32 {
		g.readR(bits, ind)
	} else {
		g.emitReadWide64(bits, ind)
		src = "lo"
	}
	if diff.Cmp(full) != 0 {
		if bits == 64 {
			g.pf("%sif (Long.compareUnsigned(%s, %s) > 0) {\n%s    return -1; // a smuggled offset is refused\n%s}\n",
				ind, src, javaIntLit(diff), ind, ind)
		} else {
			g.pf("%sif (%s > %s) {\n%s    return -1; // a smuggled offset is refused\n%s}\n",
				ind, src, javaIntLit(diff), ind, ind)
		}
	}
	if min.Sign() == 0 {
		g.pf("%s%s = %s%s;\n", ind, name, storeCast(styp), src)
	} else {
		g.pf("%s%s = %s(%s + %s);\n", ind, name, storeCast(styp), src, javaIntLit(min))
	}
}

func (g *gen) emitReadInt(f *ir.Field, name, ind string) {
	w := int64(f.Type.Width)
	styp := g.intType(f.Type)
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: zero bits — the value is the range,
			// materialized with no wire read (SPEC §4.6)
			g.pf("%s%s = %s;\n", ind, name, narrowLit(styp, f.IntMin))
			return
		}
		g.emitReadOffset(name, styp, f.IntMin, f.IntMax, ir.BitsRequired(f.IntMin, f.IntMax), ind)
		return
	}
	// bare integer at storage width; the narrowing store cast recovers the
	// sign bits of signed narrows
	if w == 64 {
		g.emitReadWide64(64, ind)
		g.pf("%s%s = lo;\n", ind, name)
		return
	}
	g.readR(w, ind)
	g.pf("%s%s = %sv;\n", ind, name, storeCast(styp))
}

// emitRead128Scalar decodes a 128-bit-storage scalar into the assignment
// target `assign` (a "path = " prefix).
func (g *gen) emitRead128Scalar(f *ir.Field, assign, ind string) {
	if f.Type.Kind == ir.TFixed {
		rawMin, rawMax, bits := fixedRaw(f)
		g.emitRead128Ranged(f.Type.Signed, assign, rawMin, rawMax, bits, ind)
		return
	}
	if f.HasIntRange {
		g.emitRead128Ranged(f.Type.Signed, assign, f.IntMin, f.IntMax, ir.BitsRequired(f.IntMin, f.IntMax), ind)
		return
	}
	// uint128 raw: full 128 bits
	g.emitReadWide128(128, ind)
	g.pf("%s%snew UInt128(hi, lo);\n", ind, assign)
}

// emitRead128Ranged decodes a ranged 128-bit-storage value: the offset in
// the folded bit count, headroom rejected, min added back in 128-bit
// two's-complement arithmetic.
func (g *gen) emitRead128Ranged(signed bool, assign string, min, max *big.Int, bits int64, ind string) {
	if min.Cmp(max) == 0 {
		// degenerate: zero bits — the value is the range (SPEC §4.6)
		g.pf("%s%s%s;\n", ind, assign, pairCtor(signed, min))
		return
	}
	diff := new(big.Int).Sub(max, min)
	full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	if bits <= 64 {
		src := "v"
		if bits <= 32 {
			g.readR(bits, ind)
		} else {
			g.emitReadWide64(bits, ind)
			src = "lo"
		}
		if diff.Cmp(full) != 0 {
			if bits == 64 {
				g.pf("%sif (Long.compareUnsigned(%s, %s) > 0) {\n%s    return -1; // a smuggled offset is refused\n%s}\n",
					ind, src, javaIntLit(new(big.Int).And(diff, maxUint64Big)), ind, ind)
			} else {
				g.pf("%sif (%s > %s) {\n%s    return -1; // a smuggled offset is refused\n%s}\n",
					ind, src, javaIntLit(diff), ind, ind)
			}
		}
		g.emitAssign128(assign, signed, min, "0", src, ind)
		return
	}
	g.emitReadWide128(bits, ind)
	if diff.Cmp(full) != 0 {
		g.pf("%sif (%s.compareUnsigned(new UInt128(hi, lo)) < 0) {\n", ind, pairCtor(false, diff))
		g.pf("%s    return -1; // a smuggled offset is refused\n", ind)
		g.pf("%s}\n", ind)
	}
	g.emitAssign128(assign, signed, min, "hi", "lo", ind)
}

// emitAssign128 assigns min + (hiExpr, loExpr) to the target, in the
// unsigned pair domain (two's complement — exact for both signednesses).
func (g *gen) emitAssign128(assign string, signed bool, min *big.Int, hiExpr, loExpr, ind string) {
	off := fmt.Sprintf("new UInt128(%s, %s)", hiExpr, loExpr)
	if min.Sign() == 0 {
		if signed {
			g.pf("%s%s%s.toSigned();\n", ind, assign, off)
		} else {
			g.pf("%s%s%s;\n", ind, assign, off)
		}
		return
	}
	if signed {
		g.pf("%s%s%s.add(%s).toSigned();\n", ind, assign, pairCtor(false, min), off)
	} else {
		g.pf("%s%s%s.add(%s);\n", ind, assign, pairCtor(false, min), off)
	}
}

// emitReadUnion is a union's own read body: the tag reads in minimal bits
// and a value above the count is refused (SPEC §4.8); the selected arm
// starts from construction defaults, then reads through its type's own
// entry, whose -1 is the union's.
func (g *gen) emitReadUnion(u *ir.Union, expr, ind string) {
	if u.Max == 0 {
		g.pf("%s%s.type = 0; // zero wire bits — only None exists (SPEC §4.8)\n", ind, expr)
		return
	}
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	g.boundRead(fmt.Sprint(bits), ind)
	g.readR(bits, ind)
	if u.Max != (int64(1)<<bits)-1 {
		g.pf("%sif (v > %d) {\n%s    %s // not a wire-legal tag (SPEC §4.8)\n%s}\n", ind, u.Max, ind, failRead, ind)
	}
	g.pf("%s%s.type = %sv;\n", ind, expr, storeCast(tagJavaType(u.Max)))
	g.pf("%sswitch (%s) {\n", ind, tagSwitchExpr(u, expr))
	for i, vr := range u.Variants {
		arm := expr + "." + javaName(vr.Name)
		g.pf("%s    case %d:\n", ind, i+1)
		switch {
		case vr.Void():
			g.pf("%s        break; // a payload-free arm: the tag is the whole wire (SPEC §4.8)\n", ind)
			continue
		case len(vr.Ref.Fields) == 0 && len(vr.Ref.Items) == 0:
			g.pf("%s        break; // empty arm — presence is the payload (SPEC §4.6)\n", ind)
			continue
		}
		// every selection starts from construction defaults
		g.pf("%s        %s(%s);\n", ind, g.qualify(vr.Type, "init"+vr.Type), arm)
		if len(vr.Ref.Items) > 0 {
			g.pf("%s        bitsRead = %s(%s, data, bitsRead, numBits);\n", ind, g.bitsFn("read", vr.Type), arm)
		}
		g.pf("%s        break;\n", ind)
	}
	g.pf("%s}\n", ind)
}

// ---- zeroing (SPEC §5: untaken branch sides read as ZERO) ----

func (g *gen) emitZeroItems(items []ir.Item, path, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitZeroField(item.F, path, ind)
		case *ir.Branch:
			g.emitZeroItems(item.Then, path, ind)
			g.emitZeroItems(item.Else, path, ind)
		}
	}
}

// emitZeroField zeroes one field's storage; a nested type through its own
// zero<Name>.
func (g *gen) emitZeroField(f *ir.Field, path, ind string) {
	g.emitInitializeField(f, path, ind, false)
}

// emitInitializeField shares the storage walk between explicit zero resets
// and construction initialization: nested types call their own zero<Name> or
// init<Name>, nested unions reset only their tags.
func (g *gen) emitInitializeField(f *ir.Field, path, ind string, defaults bool) {
	structInit := "zero"
	if defaults {
		structInit = "init"
	}
	name := path + "." + javaName(f.Name)
	switch {
	case f.Type.Kind == ir.TWString:
		g.usesArrays = true
		g.pf("%sArrays.fill(%s, (char) 0);\n%s%sLength = 0;\n", ind, name, ind, name)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.usesArrays = true
		g.pf("%sArrays.fill(%s, (byte) 0);\n", ind, name)
		g.pf("%s%sLength = 0;\n", ind, name)
		if defaults {
			for _, line := range byteDefaultLines(f, name, ind) {
				g.pf("%s", line)
			}
		}
	case f.Array != ir.ArrayNone:
		switch f.Type.Ref.(type) {
		case *ir.Struct:
			iv := fmt.Sprintf("i%d", g.loopDepth)
			g.loopDepth++
			g.pf("%sfor (int %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
			g.pf("%s    %s(%s[%s]);\n", ind, g.qualify(f.Type.Name, structInit+f.Type.Name), name, iv)
			g.pf("%s}\n", ind)
			g.loopDepth--
		case *ir.Union:
			// zero IS None per element: the tag resets; arms are unspecified
			// at None (SPEC §4.8)
			iv := fmt.Sprintf("i%d", g.loopDepth)
			g.loopDepth++
			g.pf("%sfor (int %s = 0; %s < %d; %s++) {\n", ind, iv, iv, f.ArrayBound, iv)
			g.pf("%s    %s[%s].type = 0;\n", ind, name, iv)
			g.pf("%s}\n", ind)
			g.loopDepth--
		default:
			g.usesArrays = true
			g.pf("%sArrays.fill(%s, %s);\n", ind, name, zeroElem(f.Type))
		}
		if f.Array == ir.ArrayCounted {
			count := int64(0)
			if defaults {
				count = f.BornCount()
			}
			g.pf("%s%sCount = %d;\n", ind, name, count)
		}
	default:
		if defaults && f.HasDefault {
			_, init := g.scalarStorage(f)
			if init != "" {
				g.pf("%s%s = %s;\n", ind, name, init)
				return
			}
		}
		if f.Type.Kind == ir.TNamed {
			switch f.Type.Ref.(type) {
			case *ir.Struct:
				g.pf("%s%s(%s);\n", ind, g.qualify(f.Type.Name, structInit+f.Type.Name), name)
				return
			case *ir.Union:
				// zero IS None: the tag resets; arms are unspecified at None
				g.pf("%s%s.type = 0;\n", ind, name)
				return
			}
		}
		g.pf("%s%s = %s;\n", ind, name, zeroScalar(f.Type))
	}
}

// zeroElem is the Arrays.fill zero of an array element; the byte/short
// fills need the cast (fill has no narrow overload the constant reaches).
func zeroElem(t ir.FieldType) string {
	if is128(t) {
		return pairType(t.Signed) + ".zero"
	}
	switch scalarJavaType(t) {
	case "byte":
		return "(byte) 0"
	case "short":
		return "(short) 0"
	case "long":
		return "0L"
	case "float":
		return "0.0f"
	case "double":
		return "0.0"
	}
	return "0"
}

// zeroScalar is the §5 zero form of a scalar storage slot (enum references
// fold to 0 — wire bodies reference nothing for zeroing; constant zero
// narrows to every integer storage by assignment conversion).
func zeroScalar(t ir.FieldType) string {
	switch {
	case t.Kind == ir.TBool:
		return "false"
	case t.Kind == ir.TFloat32:
		return "0.0f"
	case t.Kind == ir.TFloat64:
		return "0.0"
	case is128(t):
		return pairType(t.Signed) + ".zero"
	}
	return "0"
}
