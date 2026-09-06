// Write/Read function emission for types and messages (SPEC §6.1 items 2-4,
// §6.2): straight-line split static functions against the serialize.cs ref
// API — every call's bool is checked and early-outs, the C++ twin's shape
// (§6.3 C# row), so counts and lengths are validated before the loop or slice
// they guard. The runtime's error latch (stream.Error) rides beneath the bool
// for callers; generated validation refusals return false without latching.
// The wire is byte-identical to the C++ target's, construct by construct.
//
// Write-side guards: serialize.cs's WriteStream refuses out-of-range values
// on the ranged calls (SerializeInt/SerializeInt64 latch ValueOutOfRange and
// return false — Serialize.cs:866-884, 887-917), exactly like serialize.go,
// and its raw bit writer masks silently (WriteBitsUnchecked, Serialize.cs:439)
// exactly like serialize.go's — so this emitter carries the Go target's
// leaner guard set: generated refusals exist only where the generated code
// bypasses the ranged calls (the flags wire-width guard and the full-range
// unsigned raw-offset path).
package csharp

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitStructFunctions emits MaxBits/MaxBytes, the Zero helper, and the split
// Write/Read pair for a type or message.
func (g *gen) emitStructFunctions(st *ir.Struct) {
	g.needsSerialize = true
	g.owner = st.Name // member references escape exactly as the class emitted them
	g.bulkBytes = ir.AlignedFixedByteArrays(st)
	maxBits := ir.MaxBitsStruct(st)
	g.sf("// %sMaxBits is the longest wire path; align pads at worst case (SPEC §6.1).\n", st.Name)
	g.sf("// %sMaxBytes is rounded up to the 8-byte write-buffer granularity.\n", st.Name)
	g.sf("public const long %sMaxBits = %d;\n", st.Name, maxBits)
	g.sf("public const long %sMaxBytes = %d;\n\n", st.Name, ir.MaxBytes(maxBits))

	g.emitZeroFunction(st)
	g.emitInitFunction(st)

	g.emitPair(st.Name, st.Name,
		func() {
			if len(st.Items) == 0 {
				g.sf("    // empty body — presence is the payload (SPEC §4.6)\n")
			} else {
				g.emitWriteItems(st.Items, "    ")
			}
			g.sf("    return true;\n")
		},
		func() {
			if len(st.Items) > 0 {
				g.emitReadItems(st.Items, "    ")
			}
			g.sf("    return true;\n")
		})
}

// emitPair emits the Write/Read pair named pair over the C# type typ, honoring
// the batch plan: a batched pair keeps its public stream signature but runs an
// AggressiveInlining batch-form core against register-resident stream state
// (serialize.cs WriteBatch/ReadBatch); a pair composed under a batched one
// gets the core beside its stream form. The two measured laws the shape
// obeys: inline-only composition and per-type opt-in by scalar density — see
// batch.go for the density threshold. Wire bytes and the error model are
// identical on every path.
func (g *gen) emitPair(pair, typ string, writeBody, readBody func()) {
	batched := g.batched[pair]
	if batched {
		g.sf("// batch form: stream state stays in registers across the body and End\n")
		g.sf("// publishes it — same wire bytes, same validation, same error model.\n")
		g.sf("public static bool Write%s(WriteStream stream, %s value)\n{\n", pair, typ)
		g.sf("    WriteBatch batch = stream.BeginBatch();\n")
		g.sf("    bool result = Write%sBatch(ref batch, value);\n", pair)
		g.sf("    batch.End();\n")
		g.sf("    return result;\n}\n\n")
	} else {
		g.sf("public static bool Write%s(WriteStream stream, %s value)\n{\n", pair, typ)
		writeBody()
		g.sf("}\n\n")
	}
	if g.needCore[pair] {
		g.emitCoreAttr()
		g.sf("private static bool Write%sBatch(ref WriteBatch batch, %s value)\n{\n", pair, typ)
		g.inBatch = true
		writeBody()
		g.inBatch = false
		g.sf("}\n\n")
	}
	if batched {
		g.sf("public static bool Read%s(ReadStream stream, %s value)\n{\n", pair, typ)
		g.sf("    ReadBatch batch = stream.BeginBatch();\n")
		g.sf("    bool result = Read%sBatch(ref batch, value);\n", pair)
		g.sf("    batch.End();\n")
		g.sf("    return result;\n}\n\n")
	} else {
		g.sf("public static bool Read%s(ReadStream stream, %s value)\n{\n", pair, typ)
		readBody()
		g.sf("}\n\n")
	}
	if g.needCore[pair] {
		g.emitCoreAttr()
		g.sf("private static bool Read%sBatch(ref ReadBatch batch, %s value)\n{\n", pair, typ)
		g.inBatch = true
		readBody()
		g.inBatch = false
		g.sf("}\n\n")
	}
}

// emitCoreAttr marks a batch core INLINE-ONLY. The law, measured: a real
// call taking `ref WriteBatch` address-exposes the ref struct and
// enregistration dies for the whole calling scope — 0.71x, slower than no
// batch at all. Composition is core-to-core by ref, always inlined; the
// emitted comment states the rule once per core without the numbers.
func (g *gen) emitCoreAttr() {
	g.needsCompiler = true
	g.sf("// inline-only batch core — a real call would address-expose the batch\n")
	g.sf("[MethodImpl(MethodImplOptions.AggressiveInlining)]\n")
}

// batchScopeChild names the nested pair a composition site calls into when
// that call earns a SCOPED BATCH — a batch opened and ended around the
// composition site alone, inside a body that is otherwise on the stream.
// Returns "" when the site does not earn one.
//
// The site earns one when the child carries a core AND passed the density
// rule as a pair of its own: reaching that core by ref costs one capture and
// one restore for the WHOLE site — an array's entire loop included — where
// the child's stream entry costs one capture and one restore PER ELEMENT,
// plus a real call the JIT will not inline through. On the bench shape that
// is 88 capture/restore round trips (8 entities + 80 stats) traded for 3.
//
// A body already inside a core needs no scope: it reaches its children
// core-to-core by ref already.
func (g *gen) batchScopeChild(f *ir.Field) string {
	if g.inBatch || f.Type.Kind != ir.TNamed {
		return ""
	}
	switch f.Type.Ref.(type) {
	case *ir.Struct, *ir.Union:
	default:
		return "" // enums and flags are scalars, not composition
	}
	name := f.Type.Name
	if !g.needCore[name] || !g.batched[name] {
		return ""
	}
	return name
}

// emitBatchScope emits one scoped batch around a composition site. ONLY the
// composition call — or the loop of them — rides inside the scope: an array's
// count site stays on the stream ahead of it, so the scope's body has exactly
// one way to fail and End always runs before the refusal leaves the function.
func (g *gen) emitBatchScope(writing bool, f *ir.Field, child, ind string) {
	base := g.fieldBase(f)
	name := "value." + base
	dir, batchType := "Read", "ReadBatch"
	if writing {
		dir, batchType = "Write", "WriteBatch"
	}
	bound := g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "int", false)
	count := bound
	if f.Array == ir.ArrayCounted {
		count = "value." + g.m(base+"Count")
		if writing {
			g.emitWriteFoldedRange(count, fmt.Sprintf("%d", f.ArrayMin), bound,
				big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), false, true, true,
				" // the count guards the loop (§6.3); out-of-contract writes are refused", ind)
		} else {
			g.call(ind, fmt.Sprintf("stream.SerializeInt(ref %s, %d, %s)", count, f.ArrayMin, bound),
				" // the count guards the loop (§6.3)")
		}
	}
	g.sf("%s{\n", ind)
	g.sf("%s    // scoped batch: one capture and one restore for the whole site\n", ind)
	g.sf("%s    %s batch = stream.BeginBatch();\n", ind, batchType)
	if f.Array == ir.ArrayNone {
		g.sf("%s    bool batchOk = %s%sBatch(ref batch, %s);\n", ind, dir, child, name)
	} else {
		g.sf("%s    bool batchOk = true;\n", ind)
		g.sf("%s    for (int i = 0; i < %s; i++)\n%s    {\n", ind, count, ind)
		g.sf("%s        if (!%s%sBatch(ref batch, %s[i]))\n%s        {\n", ind, dir, child, name, ind)
		g.sf("%s            batchOk = false;\n%s            break;\n%s        }\n", ind, ind, ind)
		g.sf("%s    }\n", ind)
	}
	g.sf("%s    batch.End();\n", ind)
	g.sf("%s    if (!batchOk)\n%s    {\n%s        return false;\n%s    }\n", ind, ind, ind, ind)
	g.sf("%s}\n", ind)
}

// emitZeroFunction emits the §5 ZERO form for a class — all-zero storage,
// specified defaults NOT reapplied (those live in construction only; the wire
// contract stays a pure function of the encodings). It is the C# twin of the
// C++ target's memset: branch zeroing and explicit zero resets go through here.
func (g *gen) emitZeroFunction(st *ir.Struct) {
	g.sf("// The §5 zero form: all-zero storage; specified defaults live only in construction.\n")
	g.sf("public static void Zero%s(%s value)\n{\n", st.Name, st.Name)
	if len(st.Fields) == 0 {
		g.sf("    _ = value; // empty body — nothing to reset (SPEC §4.6)\n")
	}
	for _, f := range st.Fields {
		g.emitZeroField(f, "    ")
	}
	g.sf("}\n\n")
}

// emitInitFunction restores the construction form in existing storage. It is
// also the application entry point for reusing a selected union payload.
func (g *gen) emitInitFunction(st *ir.Struct) {
	g.sf("// Restore construction defaults in place; buffers and objects are retained.\n")
	g.sf("public static void Init%s(%s value)\n{\n", st.Name, st.Name)
	if len(st.Fields) == 0 {
		g.sf("    _ = value; // empty body — nothing to initialize\n")
	}
	for _, f := range st.Fields {
		g.emitInitializeField(f, "    ", true)
	}
	g.sf("}\n\n")
}

// emitWriteItems walks a body, accumulating maximal FLAT RUNS of statically
// sized items (flat.go) and emitting everything else through the per-field
// path. A run that does not reduce the packer-step count, or that a
// non-flattenable item interrupts, falls back ITEM BY ITEM — never piece by
// piece: the item is the only unit the per-field path can re-emit.
func (g *gen) emitWriteItems(items []ir.Item, ind string) {
	var run []flatPiece
	var runBits int64
	flush := func() {
		if len(run) == 0 {
			return
		}
		pieces, bits := run, runBits
		run, runBits = nil, 0
		if flatWorthwhile(pieces, bits) {
			g.emitFlatWriteRun(pieces, bits, ind)
			return
		}
		for _, p := range pieces {
			g.emitWriteItem(p.item, ind)
		}
	}
	for _, item := range items {
		p, ok := g.flatWritePieceOf(item)
		if ok && runBits+p.bits <= flatMaxRunBits {
			run = append(run, p)
			runBits += p.bits
			continue
		}
		flush()
		if ok {
			run, runBits = []flatPiece{p}, p.bits
			continue
		}
		g.emitWriteItem(item, ind)
	}
	flush()
}

// emitWriteItem is the per-field path for one item — the run's fallback and
// the emitter for everything a run cannot hold.
func (g *gen) emitWriteItem(item ir.Item, ind string) {
	switch item := item.(type) {
	case *ir.FieldItem:
		g.emitWriteField(item.F, ind)
	case *ir.ConstItem:
		g.emitConstItem(item, ind, true)
	case *ir.ReservedItem:
		g.emitReservedItem(item, ind, true)
	case *ir.AlignItem:
		g.call(ind, g.rv()+".SerializeAlign()", "")
	case *ir.Branch:
		neg := ""
		if item.Neg {
			neg = "!"
		}
		g.sf("%sif (%svalue.%s)\n%s{\n", ind, neg, g.m(ir.GoExportName(item.Cond)), ind)
		g.emitWriteItems(item.Then, ind+"    ")
		g.sf("%s}\n", ind)
		if item.Else != nil {
			g.sf("%selse\n%s{\n", ind, ind)
			g.emitWriteItems(item.Else, ind+"    ")
			g.sf("%s}\n", ind)
		}
	}
}

// emitReadItems is emitWriteItems' twin over read runs — same accumulation,
// same item-by-item fallback, a stricter classifier (flat.go's read half
// names what it will not absorb and why).
func (g *gen) emitReadItems(items []ir.Item, ind string) {
	var run []flatPiece
	var runBits int64
	flush := func() {
		if len(run) == 0 {
			return
		}
		pieces, bits := run, runBits
		run, runBits = nil, 0
		if flatWorthwhile(pieces, bits) {
			g.emitFlatReadRun(pieces, bits, ind)
			return
		}
		for _, p := range pieces {
			g.emitReadItem(p.item, ind)
		}
	}
	for _, item := range items {
		p, ok := g.flatReadPieceOf(item)
		if ok && runBits+p.bits <= flatMaxRunBits {
			run = append(run, p)
			runBits += p.bits
			continue
		}
		flush()
		if ok {
			run, runBits = []flatPiece{p}, p.bits
			continue
		}
		g.emitReadItem(item, ind)
	}
	flush()
}

// emitReadItem is the per-field path for one item — the run's fallback and
// the emitter for everything a run cannot hold.
func (g *gen) emitReadItem(item ir.Item, ind string) {
	switch item := item.(type) {
	case *ir.FieldItem:
		g.emitReadField(item.F, ind)
	case *ir.ConstItem:
		g.emitConstItem(item, ind, false)
	case *ir.ReservedItem:
		g.emitReservedItem(item, ind, false)
	case *ir.AlignItem:
		g.call(ind, g.rv()+".SerializeAlign()", " // rejects nonzero padding (SPEC §4.3)")
	case *ir.Branch:
		neg := ""
		if item.Neg {
			neg = "!"
		}
		g.sf("%sif (%svalue.%s)\n%s{\n", ind, neg, g.m(ir.GoExportName(item.Cond)), ind)
		g.emitReadItems(item.Then, ind+"    ")
		// the untaken side reads as zero values (SPEC §5)
		g.emitZeroItems(item.Else, ind+"    ")
		g.sf("%s}\n%selse\n%s{\n", ind, ind, ind)
		if item.Else != nil {
			g.emitReadItems(item.Else, ind+"    ")
		}
		g.emitZeroItems(item.Then, ind+"    ")
		g.sf("%s}\n", ind)
	}
}

// call emits the C++-style bool early-out around one serialize call; comment
// (leading " // ...") rides the if line.
// emitUnionFunctions emits the union's bounds and wire pair (SPEC §4.8),
// under the same batch plan every other pair obeys: the entry keeps its
// stream signature, and a *Batch core is emitted whenever the union is
// batched or composed under something that is. The write validates the tag
// BEFORE it rides; the read rejects a tag above the count and
// initializes exactly the selected arm from construction defaults.
func (g *gen) emitUnionFunctions(d *ir.Union) {
	g.needsSerialize = true
	g.owner = d.Name
	maxBits := ir.MaxBitsUnion(d)
	g.sf("// %sMaxBits is the tag plus the largest arm; None costs the tag only (SPEC §4.8).\n", d.Name)
	g.sf("// %sMaxBytes is rounded up to the 8-byte write-buffer granularity.\n", d.Name)
	g.sf("public const long %sMaxBits = %d;\n", d.Name, maxBits)
	g.sf("public const long %sMaxBytes = %d;\n\n", d.Name, ir.MaxBytes(maxBits))

	g.sf("// Zero%s resets value to the §5 zero form — the empty union. The tag alone\n", d.Name)
	g.sf("// resets: unselected arms are unspecified by rule (SPEC §4.8), and every arm\n")
	g.sf("// is unselected at None; an arm initializes at its next selection.\n")
	g.sf("public static void Zero%s(%s value)\n{\n", d.Name, d.Name)
	g.sf("    value.Type = %sType.None;\n}\n\n", d.Name)

	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(d.Max))
	g.emitPair(d.Name, d.Name,
		func() {
			if d.Max == 0 {
				g.sf("    // an empty union holds only None; its degenerate tag range [0, 0]\n")
				g.sf("    // costs zero bits (SPEC §4.8)\n")
				g.sf("    return value.Type == %sType.None;\n", d.Name)
				return
			}
			g.sf("    uint tagValue = (uint)value.Type;\n")
			g.sf("    if (tagValue > %d) // the tag validates BEFORE it rides (SPEC §4.8)\n", d.Max)
			g.sf("    {\n        return false;\n    }\n")
			g.call("    ", fmt.Sprintf("%s.SerializeBits(ref tagValue, %d)", g.rv(), bits), "")
			g.sf("    switch (value.Type)\n    {\n")
			for _, v := range d.Variants {
				if v.Void() {
					g.sf("        case %sType.%s:\n            return true; // a payload-free arm: the tag is the whole wire (SPEC §4.8)\n",
						d.Name, ir.GoExportName(v.Name))
					continue
				}
				g.sf("        case %sType.%s:\n            return %s;\n",
					d.Name, ir.GoExportName(v.Name), g.armCall("Write", v))
			}
			g.sf("    }\n    return true; // None — the tag is the whole wire (SPEC §4.8)\n")
		},
		func() {
			if d.Max == 0 {
				g.sf("    value.Type = %sType.None; // zero wire bits — only None exists (SPEC §4.8)\n", d.Name)
				g.sf("    return true;\n")
				return
			}
			g.sf("    int tagValue = 0;\n")
			g.call("    ", fmt.Sprintf("%s.SerializeInt(ref tagValue, 0, %d)", g.rv(), d.Max), " // rejects a tag above the count (SPEC §4.8)")
			g.sf("    value.Type = (%sType)tagValue;\n", d.Name)
			g.sf("    switch (value.Type)\n    {\n")
			for _, v := range d.Variants {
				g.sf("        case %sType.%s:\n", d.Name, ir.GoExportName(v.Name))
				if v.Void() {
					g.sf("            return true; // a payload-free arm: the tag is the whole wire (SPEC §4.8)\n")
					continue
				}
				g.sf("            Init%s(value.%s); // every selection starts from construction defaults\n", v.Type, ir.GoExportName(v.Name))
				g.sf("            return %s;\n", g.armCall("Read", v))
			}
			g.sf("    }\n    return true; // None\n")
		})
}

// armCall renders the call to one union arm's wire function: core-to-core by
// ref inside a batch core (rule 1 — the arm dispatch composes exactly like a
// nested struct field), the plain stream entry otherwise.
func (g *gen) armCall(dir string, v ir.UnionVariant) string {
	if g.inBatch {
		return fmt.Sprintf("%s%sBatch(ref batch, value.%s)", dir, v.Type, ir.GoExportName(v.Name))
	}
	return fmt.Sprintf("%s%s(stream, value.%s)", dir, v.Type, ir.GoExportName(v.Name))
}

func (g *gen) call(ind, expr, comment string) {
	g.sf("%sif (!%s)%s\n%s{\n%s    return false;\n%s}\n", ind, expr, comment, ind, ind, ind)
}

// emitConstItem writes const(value, bits) on the wire; a read rejects any
// other value (SPEC §4.3).
func (g *gen) emitConstItem(item *ir.ConstItem, ind string, writing bool) {
	typ, fn := "uint", "SerializeBits"
	if item.Bits > 32 {
		typ, fn = "ulong", "SerializeBits64"
	}
	if writing {
		g.sf("%s{\n%s    %s constValue = %s;\n", ind, ind, typ, item.Value.String())
		g.call(ind+"    ", fmt.Sprintf("%s.%s(ref constValue, %d)", g.rv(), fn, item.Bits),
			fmt.Sprintf(" // const(%s, %d) — SPEC §4.3", item.Value.String(), item.Bits))
		g.sf("%s}\n", ind)
		return
	}
	g.sf("%s{\n%s    %s constValue = 0;\n", ind, ind, typ)
	g.call(ind+"    ", fmt.Sprintf("%s.%s(ref constValue, %d)", g.rv(), fn, item.Bits), "")
	g.sf("%s    if (constValue != %s) // const(%s, %d): a read rejects any other value (SPEC §4.3)\n",
		ind, item.Value.String(), item.Value.String(), item.Bits)
	g.sf("%s    {\n%s        return false;\n%s    }\n%s}\n", ind, ind, ind, ind)
}

// emitReservedItem writes reserved(bits) as zeros; a read rejects nonzero.
func (g *gen) emitReservedItem(item *ir.ReservedItem, ind string, writing bool) {
	typ, fn := "uint", "SerializeBits"
	if item.Bits > 32 {
		typ, fn = "ulong", "SerializeBits64"
	}
	if writing {
		g.sf("%s{\n%s    %s reservedValue = 0;\n", ind, ind, typ)
		g.call(ind+"    ", fmt.Sprintf("%s.%s(ref reservedValue, %d)", g.rv(), fn, item.Bits),
			fmt.Sprintf(" // reserved(%d) — zeros on the wire", item.Bits))
		g.sf("%s}\n", ind)
		return
	}
	g.sf("%s{\n%s    %s reservedValue = 0;\n", ind, ind, typ)
	g.call(ind+"    ", fmt.Sprintf("%s.%s(ref reservedValue, %d)", g.rv(), fn, item.Bits), "")
	g.sf("%s    if (reservedValue != 0) // reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, item.Bits)
	g.sf("%s    {\n%s        return false;\n%s    }\n%s}\n", ind, ind, ind, ind)
}

// emitZeroItems zero-initializes every field under an untaken branch side
// (SPEC §5: ZERO values, not specified defaults — the Zero* helpers are that
// zero form, so this is the memset twin exactly).
func (g *gen) emitZeroItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitZeroField(item.F, ind)
		case *ir.Branch:
			g.emitZeroItems(item.Then, ind)
			g.emitZeroItems(item.Else, ind)
		}
	}
}

func (g *gen) emitZeroField(f *ir.Field, ind string) {
	g.emitInitializeField(f, ind, false)
}

// emitInitializeField is the common storage walk for Zero and Init. Only
// construction initialization reapplies scalar defaults and born counts.
func (g *gen) emitInitializeField(f *ir.Field, ind string, defaults bool) {
	structInit := "Zero"
	if defaults {
		structInit = "Init"
	}
	base := g.fieldBase(f)
	name := "value." + base
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.needsSystem = true
		g.sf("%sArray.Clear(%s, 0, %s);\n%svalue.%s = 0;\n", ind, name, g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "int", false), ind, g.m(base+"Length"))
	case f.Array != ir.ArrayNone:
		if f.Type.Kind == ir.TNamed && isClassRef(f.Type.Ref) {
			// clearing a class array would null the pre-allocated elements —
			// zero through them instead (the SCHEMA bound, like the wire loops)
			init := structInit
			if _, union := f.Type.Ref.(*ir.Union); union {
				init = "Zero"
			}
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "int", false), ind)
			g.sf("%s    %s%s(%s[i]);\n%s}\n", ind, init, f.Type.Name, name, ind)
		} else {
			g.needsSystem = true
			g.sf("%sArray.Clear(%s, 0, %s);\n", ind, name, g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "int", false))
		}
		if f.Array == ir.ArrayCounted {
			count := int64(0)
			if defaults {
				count = f.BornCount()
			}
			g.sf("%svalue.%s = %d;\n", ind, g.m(base+"Count"), count)
		}
	default:
		if defaults && f.HasDefault {
			g.sf("%s%s = %s;\n", ind, name, g.defaultValue(f, false))
			return
		}
		switch f.Type.Kind {
		case ir.TBool:
			g.sf("%s%s = false;\n", ind, name)
		case ir.TFloat32:
			g.sf("%s%s = 0.0f;\n", ind, name)
		case ir.TFloat64:
			g.sf("%s%s = 0.0;\n", ind, name)
		case ir.TNamed:
			switch f.Type.Ref.(type) {
			case *ir.Enum:
				g.sf("%s%s = %s.None;\n", ind, name, f.Type.Name)
			case *ir.Flags:
				g.sf("%s%s = 0;\n", ind, name)
			case *ir.Struct:
				// Initialize the existing object in the same mode as its parent.
				g.sf("%s%s%s(%s);\n", ind, structInit, f.Type.Name, name)
			case *ir.Union:
				// through Zero<Union> — the tag resets to None; arms are
				// initialized at their next selection (SPEC §4.8)
				g.sf("%sZero%s(%s);\n", ind, f.Type.Name, name)
			}
		default:
			g.sf("%s%s = 0;\n", ind, name)
		}
	}
}

// rangeArgs renders the min/max arguments symbolically where possible, cast
// to the runtime call family's argument type.
func (g *gen) rangeArgs(f *ir.Field, typ string) (string, string) {
	return g.renderArg(f.IntMinExpr, f.IntMin, typ, false), g.renderArg(f.IntMaxExpr, f.IntMax, typ, false)
}

// ---- generation-time bound folding on the write path (the C++ fold pass.s
// mechanism, carried to C#) ----
//
// A ranged integer's min/max/bit count are schema constants, so the generator
// folds them: the write emits the offset from min in a bit count computed at
// generation time through the raw SerializeBits/SerializeBits64 calls — no
// runtime BitsRequired, no min/max parameter traffic, and the 32/64 dword
// split resolves here, not at run time. The wire bytes are identical to the
// runtime SerializeInt/SerializeInt64 forms (offset-from-min in
// BitsRequired(min, max) bits — the wire-identity property the C++ backend proved,
// re-proven here by the wire golden gate). The runtime ranged write's
// out-of-range refusal moves into the generated code with the fold: the guard
// returns false WITHOUT latching — the family's generated-guard form (the
// flags wire-width guard and the full-range unsigned raw path already refuse
// this way); vacuous halves (bounds the storage type cannot escape) are
// elided. Reads stay on the runtime ranged calls: the measurements found nothing to
// gain on the read path, and the read side's latched ValueOutOfRange
// semantics (pinned by test/cs) stay untouched.

// storageBounds is the representable range of an integer storage type.
func storageBounds(t ir.FieldType) (*big.Int, *big.Int) {
	w := uint(t.Width)
	if t.Signed {
		half := new(big.Int).Lsh(big.NewInt(1), w-1)
		return new(big.Int).Neg(half), new(big.Int).Sub(half, big.NewInt(1))
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), w), big.NewInt(1))
}

// emitWriteFoldedRange writes the integer expression expr, in | min, max, as
// offset-from-min in a generation-time bit count. lo/hi are the rendered
// bound expressions (typed so they compare against expr legally); wide picks
// the 64-bit call family; guardLo/guardHi say whether each half of the range
// refusal is non-vacuous for expr's storage type. Byte-identical to the
// runtime SerializeInt/SerializeInt64 forms.
func (g *gen) emitWriteFoldedRange(expr, lo, hi string, min, max *big.Int, wide, guardLo, guardHi bool, comment, ind string) {
	switch {
	case guardLo && guardHi:
		g.sf("%sif (%s < %s || %s > %s)%s\n%s{\n%s    return false;\n%s}\n", ind, expr, lo, expr, hi, comment, ind, ind, ind)
	case guardLo:
		g.sf("%sif (%s < %s)%s\n%s{\n%s    return false;\n%s}\n", ind, expr, lo, comment, ind, ind, ind)
	case guardHi:
		g.sf("%sif (%s > %s)%s\n%s{\n%s    return false;\n%s}\n", ind, expr, hi, comment, ind, ind, ind)
	}
	bits := ir.BitsRequired(min, max)
	if bits == 0 {
		// A degenerate range costs ZERO BITS — the value is known from the
		// range alone; the refusal above is the whole write (SPEC §4.6,
		// decided deliberately)
		return
	}
	typ, fn := "uint", "SerializeBits"
	if wide {
		typ, fn = "ulong", "SerializeBits64"
	}
	// the offset subtraction lives in the unsigned domain, exactly as the
	// runtime forms compute it; a negative constant bound needs unchecked()
	// because C# refuses checked constant narrowing at compile time
	loCast := fmt.Sprintf("(%s)(%s)", typ, lo)
	if min.Sign() < 0 {
		loCast = fmt.Sprintf("unchecked((%s)(%s))", typ, lo)
	}
	if min.Sign() == 0 {
		g.sf("%s{\n%s    %s offsetValue = (%s)(%s);\n", ind, ind, typ, typ, expr)
	} else {
		g.sf("%s{\n%s    %s offsetValue = (%s)(%s) - %s;\n", ind, ind, typ, typ, expr, loCast)
	}
	g.call(ind+"    ", fmt.Sprintf("%s.%s(ref offsetValue, %d)", g.rv(), fn, bits), "")
	g.sf("%s}\n", ind)
}

// emitWriteFoldedInt is emitWriteFoldedRange for a ranged TInt field: it
// derives the guard vacuousness from the field's storage type and renders the
// bounds in that storage's comparison domain.
func (g *gen) emitWriteFoldedInt(f *ir.Field, name, ind string) {
	sMin, sMax := storageBounds(f.Type)
	guardLo := f.IntMin.Cmp(sMin) > 0
	guardHi := f.IntMax.Cmp(sMax) < 0
	wide := intRangePath(f.IntMin, f.IntMax) != "int32"
	typ := "int"
	if wide {
		typ = "long"
		if !f.Type.Signed && f.Type.Width == 64 {
			// ulong storage compares against ulong-typed bounds (a long-cast
			// symbolic bound would be an illegal ulong/long comparison)
			typ = "ulong"
		}
	}
	lo, hi := g.rangeArgs(f, typ)
	g.emitWriteFoldedRange(name, lo, hi, f.IntMin, f.IntMax, wide, guardLo, guardHi,
		"", ind)
}

// maxUint64 is 2^64 - 1, the top of unsigned-64 storage — the bound against
// which a range guard becomes vacuous.
var maxUint64 = new(big.Int).SetUint64(math.MaxUint64)

// intRangePath picks the runtime call family for a ranged integer — the same
// switch as the other targets, so all five emit identical wire.
func intRangePath(min, max *big.Int) string {
	i32 := big.NewInt(math.MaxInt32)
	i32lo := big.NewInt(math.MinInt32)
	if min.Cmp(i32lo) >= 0 && max.Cmp(i32) <= 0 {
		return "int32"
	}
	i64 := big.NewInt(math.MaxInt64)
	i64lo := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63))
	if min.Cmp(i64lo) >= 0 && max.Cmp(i64) <= 0 {
		return "int64"
	}
	return "bits64" // full-range unsigned: width-computed raw bits over value - min
}

func (g *gen) emitWriteField(f *ir.Field, ind string) {
	base := g.fieldBase(f)
	name := "value." + base
	if child := g.batchScopeChild(f); child != "" {
		g.emitBatchScope(true, f, child, ind)
		return
	}
	if f.Array != ir.ArrayNone {
		bound := g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "int", false)
		if g.bulkBytes[f] {
			// statically byte-aligned [N]uint8 (ir.AlignedFixedByteArrays):
			// the bulk path aligns zero bits here and memcpys — byte-identical
			// to the per-byte loop. The SCHEMA bound slices the span, so
			// reassigned-short storage faults loudly, as the loop did.
			g.needsSystem = true
			g.call(ind, fmt.Sprintf("%s.SerializeBytes(%s.AsSpan(0, %s))", g.rv(), name, bound),
				" // byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop")
			return
		}
		if f.Array == ir.ArrayCounted {
			count := "value." + g.m(base+"Count")
			g.emitWriteFoldedRange(count, fmt.Sprintf("%d", f.ArrayMin), bound,
				big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), false, true, true,
				" // the count guards the loop (§6.3); out-of-contract writes are refused", ind)
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, count, ind)
		} else {
			// the SCHEMA bound, never the storage's Length: reassigned-short
			// storage must fault loudly, not silently write fewer elements
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, bound, ind)
		}
		g.emitWriteScalar(f, name+"[i]", ind+"    ")
		g.sf("%s}\n", ind)
		return
	}
	g.emitWriteScalar(f, name, ind)
}

func (g *gen) emitWriteScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: ZERO bits — the folded range refusal and no
			// wire call at all, so no runtime degenerate support is needed
			// (SPEC §4.6). The one legal raw is min << F,
			// compared in the storage's own signedness (a wide ufixed raw can
			// live above long.MaxValue).
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			switch {
			case f.Type.Width == 128 && f.Type.Signed:
				g.sf("%sif (%s != (Int128Value)(%s))\n%s{\n%s    return false;\n%s}\n",
					ind, name, csRender128(rawMin), ind, ind, ind)
			case f.Type.Width == 128:
				g.sf("%sif (%s != (UInt128Value)(%s))\n%s{\n%s    return false;\n%s}\n",
					ind, name, csRenderU128(rawMin), ind, ind, ind)
			case f.Type.Signed:
				g.sf("%sif ((long)%s != %sL)\n%s{\n%s    return false;\n%s}\n",
					ind, name, rawMin.String(), ind, ind, ind)
			default:
				g.sf("%sif ((ulong)%s != %sUL)\n%s{\n%s    return false;\n%s}\n",
					ind, name, rawMin.String(), ind, ind, ind)
			}
			return
		}
		// the Q format and whole-unit bounds are compile-time constants of the
		// call site, exactly like a ranged integer's bounds (STANDARD.md, fixed)
		lo, hi := g.rangeArgs(f, "long")
		if f.Type.Width == 8 {
			// 8-bit storage sits below serialize.cs's narrowest SerializeFixed
			// overload (short/ushort): widen through a temp — lossless, the
			// raw value fits I+F = 8 bits by construction (the golang
			// emitter's narrower-than-the-library pattern, mirrored)
			g.sf("%s{\n%s    %s fixedValue = %s;\n", ind, ind, csFixed8Temp(f), name)
			g.call(ind+"    ", fmt.Sprintf("%s.SerializeFixed(ref fixedValue, %d, %d, %s, %s)", g.rv(), f.Type.IntBits, f.Type.FracBits, lo, hi), "")
			g.sf("%s}\n", ind)
			return
		}
		g.call(ind, fmt.Sprintf("%s.SerializeFixed(ref %s, %d, %d, %s, %s)", g.rv(), name, f.Type.IntBits, f.Type.FracBits, lo, hi), "")
	case ir.TInt:
		if f.Type.Width == 128 {
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					// degenerate range: ZERO bits — refusal only (SPEC §4.6)
					g.sf("%sif (%s != (Int128Value)(%s))\n%s{\n%s    return false;\n%s}\n",
						ind, name, csRender128(f.IntMin), ind, ind, ind)
					return
				}
				// int128 is ALWAYS ranged (SPEC §4.3): offset from min —
				// identical bytes to SerializeInt64 wherever the range fits
				g.call(ind, fmt.Sprintf("%s.SerializeInt128(ref %s, %s, %s)", g.rv(), name,
					csRender128(f.IntMin), csRender128(f.IntMax)), "")
			} else {
				// uint128 is the raw field: 128 bits, low 64-bit half first
				g.call(ind, fmt.Sprintf("%s.SerializeUInt128(ref %s)", g.rv(), name), "")
			}
			return
		}
		if f.HasIntRange {
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32", "int64":
				g.emitWriteFoldedInt(f, name, ind)
			default:
				// full-range unsigned: raw offset bits (ulong storage only —
				// no narrower storage can hold a range past long). This path
				// bypasses the runtime's ranged calls, so it supplies their
				// write-side range refusal (a misuse value must not wrap into
				// valid-looking wire); vacuous halves are elided. Nothing
				// latches — bool is the whole verdict here.
				lo, _ := g.rangeArgs(f, "ulong")
				loVacuous := f.IntMin.Sign() == 0
				hiVacuous := f.IntMax.Cmp(maxUint64) == 0
				switch {
				case !loVacuous && !hiVacuous:
					g.sf("%sif (%s < %s || %s > %s)\n", ind, name, lo, name, f.IntMax.String())
					g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
				case !loVacuous:
					g.sf("%sif (%s < %s)\n", ind, name, lo)
					g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
				case !hiVacuous:
					g.sf("%sif (%s > %s)\n", ind, name, f.IntMax.String())
					g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
				}
				if loVacuous {
					g.sf("%s{\n%s    ulong offsetValue = %s;\n", ind, ind, name)
				} else {
					g.sf("%s{\n%s    ulong offsetValue = %s - %s;\n", ind, ind, name, lo)
				}
				g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits64(ref offsetValue, %d)", g.rv(), ir.BitsRequired(f.IntMin, f.IntMax)), "")
				g.sf("%s}\n", ind)
			}
			return
		}
		g.emitWriteBareInt(f, name, ind)
	case ir.TBits:
		if f.Type.Width <= 32 {
			g.call(ind, fmt.Sprintf("%s.SerializeBits(ref %s, %d)", g.rv(), name, f.Type.Width), "")
		} else {
			g.call(ind, fmt.Sprintf("%s.SerializeBits64(ref %s, %d)", g.rv(), name, f.Type.Width), "")
		}
	case ir.TBool:
		g.call(ind, fmt.Sprintf("%s.SerializeBool(ref %s)", g.rv(), name), "")
	case ir.TFloat32:
		if f.HasFloatRange {
			steps, wireBits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
			min32 := float32(f.FMin)
			delta := float32(f.FMax) - min32
			// a temp so the wire quantization cannot write back into the input
			g.sf("%s{\n%s    float compressedValue = %s;\n", ind, ind, name)
			g.call(ind+"    ", fmt.Sprintf("%s.SerializeCompressedFloatPrecomputed(ref compressedValue, %du, %d, %s, %s)",
				g.rv(), steps, wireBits, formatFloat32(float64(delta)), formatFloat32(float64(min32))), "")
			g.sf("%s}\n", ind)
			return
		}
		g.call(ind, fmt.Sprintf("%s.SerializeFloat(ref %s)", g.rv(), name), "")
	case ir.TFloat64:
		g.call(ind, fmt.Sprintf("%s.SerializeDouble(ref %s)", g.rv(), name), "")
	case ir.TString, ir.TBytes:
		// length in [0, N], align, then the used bytes — the classic
		// serialize_string framing over a buffer of N + 1 (SPEC §4.7).
		// The length guard runs BEFORE the slice, so a stale length never
		// reaches AsSpan (§6.3). Interior nulls are writer misuse; the read
		// side rejects them (§4.7).
		g.needsSystem = true
		length := "value." + g.m(g.fieldBase(f)+"Length")
		g.emitWriteFoldedRange(length, "0", g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "int", false),
			big.NewInt(0), big.NewInt(f.Type.Size), false, true, true,
			" // the length guards the slice (§6.3); out-of-contract writes are refused", ind)
		g.call(ind, fmt.Sprintf("%s.SerializeBytes(%s.AsSpan(0, %s))", g.rv(), name, length), "")
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			// headroom storage can exceed the wire range [0, Max]: the
			// generated guard refuses it — bool alone, nothing latches
			g.emitWriteFoldedEnum(ref, name, ind)
		case *ir.Flags:
			g.emitWriteFlagsValue(name, ref.WireBits, ind)
		case *ir.Struct:
			if g.inBatch {
				g.call(ind, fmt.Sprintf("Write%sBatch(ref batch, %s)", f.Type.Name, name), "")
			} else {
				g.call(ind, fmt.Sprintf("Write%s(stream, %s)", f.Type.Name, name), "")
			}
		case *ir.Union:
			if g.inBatch {
				g.call(ind, fmt.Sprintf("Write%sBatch(ref batch, %s)", f.Type.Name, name), "")
			} else {
				g.call(ind, fmt.Sprintf("Write%s(stream, %s)", f.Type.Name, name), "")
			}
		}
	}
}

// emitWriteFoldedEnum writes an enum in [0, Max] with a generation-time bit
// count — byte-identical to the runtime SerializeInt form it replaces. The
// unsigned temp doubles as the headroom guard's comparison domain.
func (g *gen) emitWriteFoldedEnum(ref *ir.Enum, name, ind string) {
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
	typ, fn := "uint", "SerializeBits"
	if ref.StorageBits > 32 {
		typ, fn = "ulong", "SerializeBits64"
	}
	g.sf("%s{\n%s    %s enumValue = (%s)%s;\n", ind, ind, typ, typ, name)
	// the guard is vacuous only when the wire range fills the backing type
	if big.NewInt(ref.Max).Cmp(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(ref.StorageBits)), big.NewInt(1))) < 0 {
		g.sf("%s    if (enumValue > %d) // headroom above the wire range cannot ride\n", ind, ref.Max)
		g.sf("%s    {\n%s        return false;\n%s    }\n", ind, ind, ind)
	}
	// A degenerate range (an enum with only the implicit None) costs ZERO BITS
	// -- the value is known from the range alone. The bit packer requires at
	// least one bit, so the call is skipped entirely; the headroom guard above
	// still rides, because a value above the range must still be refused.
	if bits > 0 {
		g.call(ind+"    ", fmt.Sprintf("%s.%s(ref enumValue, %d)", g.rv(), fn, bits), "")
	}
	g.sf("%s}\n", ind)
}

// emitWriteBareInt writes a bare integer at its storage width. Signed values
// cast through the same-width unsigned first — the sign extension into a
// wider temp would corrupt neighboring wire data, exactly as in C++.
func (g *gen) emitWriteBareInt(f *ir.Field, name, ind string) {
	if f.Type.Width == 64 {
		if f.Type.Signed {
			g.sf("%s{\n%s    ulong rawValue = (ulong)%s;\n", ind, ind, name)
			g.call(ind+"    ", g.rv()+".SerializeBits64(ref rawValue, 64)", "")
			g.sf("%s}\n", ind)
			return
		}
		g.call(ind, fmt.Sprintf("%s.SerializeBits64(ref %s, 64)", g.rv(), name), "")
		return
	}
	if !f.Type.Signed && f.Type.Width == 32 {
		g.call(ind, fmt.Sprintf("%s.SerializeBits(ref %s, 32)", g.rv(), name), "")
		return
	}
	g.sf("%s{\n%s    uint rawValue = %s;\n", ind, ind, fmt32Cast(f, name))
	g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits(ref rawValue, %d)", g.rv(), f.Type.Width), "")
	g.sf("%s}\n", ind)
}

// fmt32Cast renders the value-to-uint conversion for a sub-32 (or signed
// 32-bit) bare integer: signed narrows go through the same-width unsigned.
func fmt32Cast(f *ir.Field, name string) string {
	if f.Type.Signed && f.Type.Width < 32 {
		return "(" + csUint(f.Type.Width) + ")" + name
	}
	if f.Type.Signed {
		return "(uint)" + name
	}
	return name // byte/ushort widen implicitly
}

// csFixed8Temp is the temp type an 8-bit fixed field widens through:
// serialize.cs's narrowest SerializeFixed overload pair is short/ushort
// (sbyte/byte have none), so the emitter carries the raw value across the
// call in the matching 16-bit type. sbyte/byte widen into it implicitly.
func csFixed8Temp(f *ir.Field) string {
	if f.Type.Signed {
		return "short"
	}
	return "ushort"
}

func (g *gen) emitReadField(f *ir.Field, ind string) {
	base := g.fieldBase(f)
	name := "value." + base
	if child := g.batchScopeChild(f); child != "" {
		g.emitBatchScope(false, f, child, ind)
		return
	}
	if f.Array != ir.ArrayNone {
		bound := g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "int", false)
		if g.bulkBytes[f] {
			// the write twin's bulk path exactly: zero align bits consumed
			// here, then a bulk copy of the N bytes (see emitWriteField)
			g.needsSystem = true
			g.call(ind, fmt.Sprintf("%s.SerializeBytes(%s.AsSpan(0, %s))", g.rv(), name, bound),
				" // byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop")
			return
		}
		if f.Array == ir.ArrayCounted {
			count := "value." + g.m(base+"Count")
			g.call(ind, fmt.Sprintf("%s.SerializeInt(ref %s, %d, %s)", g.rv(), count, f.ArrayMin, bound),
				" // the count guards the loop (§6.3)")
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, count, ind)
		} else {
			// the SCHEMA bound, never the storage's Length (see the write twin)
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, bound, ind)
		}
		g.emitReadScalar(f, name+"[i]", ind+"    ")
		g.sf("%s}\n", ind)
		return
	}
	g.emitReadScalar(f, name, ind)
}

func (g *gen) emitReadScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: zero bits — the value is the range, raw
			// min << F, materialized with no wire call (SPEC §4.6), in the
			// storage's own signedness
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			switch {
			case f.Type.Width == 128 && f.Type.Signed:
				g.sf("%s%s = %s;\n", ind, name, csRender128(rawMin))
			case f.Type.Width == 128:
				g.sf("%s%s = %s;\n", ind, name, csRenderU128(rawMin))
			case f.Type.Signed:
				g.sf("%s%s = unchecked((%s)(%sL));\n", ind, name, g.csFieldType(f.Type), rawMin.String())
			default:
				g.sf("%s%s = unchecked((%s)(%sUL));\n", ind, name, g.csFieldType(f.Type), rawMin.String())
			}
			return
		}
		// validates the raw offset against the raw bounds and rejects — never
		// clamps — returning false on a hostile stream
		lo, hi := g.rangeArgs(f, "long")
		if f.Type.Width == 8 {
			// 8-bit storage sits below serialize.cs's narrowest SerializeFixed
			// overload (short/ushort): read through a temp and narrow on the
			// member assignment — lossless, a decoded raw value is inside the
			// raw bounds or the read already failed
			g.sf("%s{\n%s    %s fixedValue = 0;\n", ind, ind, csFixed8Temp(f))
			g.call(ind+"    ", fmt.Sprintf("%s.SerializeFixed(ref fixedValue, %d, %d, %s, %s)", g.rv(), f.Type.IntBits, f.Type.FracBits, lo, hi), "")
			g.sf("%s    %s = (%s)fixedValue;\n%s}\n", ind, name, g.csFieldType(f.Type), ind)
			return
		}
		g.call(ind, fmt.Sprintf("%s.SerializeFixed(ref %s, %d, %d, %s, %s)", g.rv(), name, f.Type.IntBits, f.Type.FracBits, lo, hi), "")
	case ir.TInt:
		if f.Type.Width == 128 {
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					// degenerate range: zero bits — materialize (SPEC §4.6)
					g.sf("%s%s = %s;\n", ind, name, csRender128(f.IntMin))
					return
				}
				// rejects a decoded offset beyond max - min (reject, never clamp)
				g.call(ind, fmt.Sprintf("%s.SerializeInt128(ref %s, %s, %s)", g.rv(), name,
					csRender128(f.IntMin), csRender128(f.IntMax)), "")
			} else {
				g.call(ind, fmt.Sprintf("%s.SerializeUInt128(ref %s)", g.rv(), name), "")
			}
			return
		}
		if f.HasIntRange {
			if f.IntMin.Cmp(f.IntMax) == 0 {
				// degenerate range: zero bits — the value is the range,
				// materialized with no wire call (SPEC §4.6)
				lit := f.IntMin.String() + "L"
				if !f.Type.Signed && !f.IntMin.IsInt64() {
					lit = f.IntMin.String() + "UL" // above the signed-literal domain
				}
				g.sf("%s%s = unchecked((%s)(%s));\n", ind, name, g.csFieldType(f.Type), lit)
				return
			}
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				lo, hi := g.rangeArgs(f, "int")
				if f.Type.Signed && f.Type.Width == 32 {
					g.call(ind, fmt.Sprintf("%s.SerializeInt(ref %s, %s, %s)", g.rv(), name, lo, hi), "")
					return
				}
				g.sf("%s{\n%s    int rangeValue = 0;\n", ind, ind)
				g.call(ind+"    ", fmt.Sprintf("%s.SerializeInt(ref rangeValue, %s, %s)", g.rv(), lo, hi), "")
				g.sf("%s    %s = (%s)rangeValue;\n%s}\n", ind, name, g.csFieldType(f.Type), ind)
			case "int64":
				lo, hi := g.rangeArgs(f, "long")
				if f.Type.Signed && f.Type.Width == 64 {
					g.call(ind, fmt.Sprintf("%s.SerializeInt64(ref %s, %s, %s)", g.rv(), name, lo, hi), "")
					return
				}
				g.sf("%s{\n%s    long rangeValue = 0;\n", ind, ind)
				g.call(ind+"    ", fmt.Sprintf("%s.SerializeInt64(ref rangeValue, %s, %s)", g.rv(), lo, hi), "")
				g.sf("%s    %s = (%s)rangeValue;\n%s}\n", ind, name, g.csFieldType(f.Type), ind)
			default:
				lo, _ := g.rangeArgs(f, "ulong")
				diff := new(big.Int).Sub(f.IntMax, f.IntMin)
				g.sf("%s{\n%s    ulong offsetValue = 0;\n", ind, ind)
				g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits64(ref offsetValue, %d)", g.rv(), ir.BitsRequired(f.IntMin, f.IntMax)), "")
				if diff.Cmp(maxUint64) != 0 {
					// a full-width diff cannot overflow its own read — elided
					g.sf("%s    if (offsetValue > %s) // a read rejects out-of-range (SPEC §5) — not latched\n", ind, diff.String())
					g.sf("%s    {\n%s        return false;\n%s    }\n", ind, ind, ind)
				}
				if f.IntMin.Sign() == 0 {
					g.sf("%s    %s = offsetValue;\n%s}\n", ind, name, ind)
				} else {
					g.sf("%s    %s = offsetValue + %s;\n%s}\n", ind, name, lo, ind)
				}
			}
			return
		}
		g.emitReadBareInt(f, name, ind)
	case ir.TBits:
		if f.Type.Width <= 32 {
			g.call(ind, fmt.Sprintf("%s.SerializeBits(ref %s, %d)", g.rv(), name, f.Type.Width), "")
		} else {
			g.call(ind, fmt.Sprintf("%s.SerializeBits64(ref %s, %d)", g.rv(), name, f.Type.Width), "")
		}
	case ir.TBool:
		g.call(ind, fmt.Sprintf("%s.SerializeBool(ref %s)", g.rv(), name), "")
	case ir.TFloat32:
		if f.HasFloatRange {
			steps, wireBits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
			min32 := float32(f.FMin)
			delta := float32(f.FMax) - min32
			g.call(ind, fmt.Sprintf("%s.SerializeCompressedFloatPrecomputed(ref %s, %du, %d, %s, %s)",
				g.rv(), name, steps, wireBits, formatFloat32(float64(delta)), formatFloat32(float64(min32))), "")
			return
		}
		g.call(ind, fmt.Sprintf("%s.SerializeFloat(ref %s)", g.rv(), name), "")
	case ir.TFloat64:
		g.call(ind, fmt.Sprintf("%s.SerializeDouble(ref %s)", g.rv(), name), "")
	case ir.TString, ir.TBytes:
		// the bool is checked BEFORE the slice: a hostile length never
		// reaches AsSpan (a successful ranged read guarantees [0, N])
		g.needsSystem = true
		length := "value." + g.m(g.fieldBase(f)+"Length")
		g.call(ind, fmt.Sprintf("%s.SerializeInt(ref %s, 0, %s)",
			g.rv(), length, g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "int", false)),
			" // the length guards the slice (§6.3)")
		g.call(ind, fmt.Sprintf("%s.SerializeBytes(%s.AsSpan(0, %s))", g.rv(), name, length), "")
		if f.Type.Kind == ir.TString {
			// the interior-null rule is generated-code validation (SPEC §4.7);
			// the SerializeBytes bool above already surfaced a truncated
			// stream as the stream's own latched error, so this verdict only
			// ever judges bytes that arrived — false, nothing latched
			g.sf("%sfor (int i = 0; i < %s; i++)\n%s{\n", ind, length, ind)
			g.sf("%s    if (%s[i] == 0) // an interior null is content the read refuses (SPEC §4.7)\n", ind, name)
			g.sf("%s    {\n%s        return false;\n%s    }\n%s}\n", ind, ind, ind, ind)
		}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.sf("%s{\n%s    int enumValue = 0;\n", ind, ind)
			g.call(ind+"    ", fmt.Sprintf("%s.SerializeInt(ref enumValue, 0, %d)", g.rv(), ref.Max), "")
			g.sf("%s    %s = (%s)enumValue;\n%s}\n", ind, name, f.Type.Name, ind)
		case *ir.Flags:
			g.emitReadFlags(name, ref.WireBits, ind)
		case *ir.Struct:
			if g.inBatch {
				g.call(ind, fmt.Sprintf("Read%sBatch(ref batch, %s)", f.Type.Name, name), "")
			} else {
				g.call(ind, fmt.Sprintf("Read%s(stream, %s)", f.Type.Name, name), "")
			}
		case *ir.Union:
			if g.inBatch {
				g.call(ind, fmt.Sprintf("Read%sBatch(ref batch, %s)", f.Type.Name, name), "")
			} else {
				g.call(ind, fmt.Sprintf("Read%s(stream, %s)", f.Type.Name, name), "")
			}
		}
	}
}

func (g *gen) emitReadBareInt(f *ir.Field, name, ind string) {
	if f.Type.Width == 64 {
		if f.Type.Signed {
			g.sf("%s{\n%s    ulong rawValue = 0;\n", ind, ind)
			g.call(ind+"    ", g.rv()+".SerializeBits64(ref rawValue, 64)", "")
			g.sf("%s    %s = (long)rawValue;\n%s}\n", ind, name, ind)
			return
		}
		g.call(ind, fmt.Sprintf("%s.SerializeBits64(ref %s, 64)", g.rv(), name), "")
		return
	}
	if !f.Type.Signed && f.Type.Width == 32 {
		g.call(ind, fmt.Sprintf("%s.SerializeBits(ref %s, 32)", g.rv(), name), "")
		return
	}
	g.sf("%s{\n%s    uint rawValue = 0;\n", ind, ind)
	g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits(ref rawValue, %d)", g.rv(), f.Type.Width), "")
	if f.Type.Signed && f.Type.Width < 32 {
		// back through the same-width unsigned so the sign bit lands right
		g.sf("%s    %s = (%s)(%s)rawValue;\n%s}\n", ind, name, csInt(f.Type.Width), csUint(f.Type.Width), ind)
		return
	}
	g.sf("%s    %s = (%s)rawValue;\n%s}\n", ind, name, g.csFieldType(f.Type), ind)
}

// emitReadFlags reads a flags value through an unsigned temp; past 32 wire
// bits the ulong storage takes the read directly.
func (g *gen) emitReadFlags(name string, wireBits int, ind string) {
	if wireBits <= 32 {
		g.sf("%s{\n%s    uint flagsValue = 0;\n", ind, ind)
		g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits(ref flagsValue, %d)", g.rv(), wireBits), "")
		g.sf("%s    %s = flagsValue;\n%s}\n", ind, name, ind)
		return
	}
	g.call(ind, fmt.Sprintf("%s.SerializeBits64(ref %s, %d)", g.rv(), name, wireBits), "")
}

// emitWriteFlagsValue is the write half used by emitWriteScalar. Storage is
// wider than the wire wherever WireBits < 64, so a mask bit above the wire
// width is refused rather than silently truncated — the raw bit calls mask,
// so this refusal is generated (nothing latches; bool is the verdict).
func (g *gen) emitWriteFlagsValue(name string, wireBits int, ind string) {
	if wireBits < 64 {
		g.sf("%sif (%s >= 1ul << %d) // a mask bit above the wire width cannot ride\n", ind, name, wireBits)
		g.sf("%s{\n%s    return false;\n%s}\n", ind, ind, ind)
	}
	if wireBits <= 32 {
		g.sf("%s{\n%s    uint flagsValue = (uint)%s;\n", ind, ind, name)
		g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits(ref flagsValue, %d)", g.rv(), wireBits), "")
		g.sf("%s}\n", ind)
		return
	}
	g.sf("%s{\n%s    ulong flagsValue = %s;\n", ind, ind, name)
	g.call(ind+"    ", fmt.Sprintf("%s.SerializeBits64(ref flagsValue, %d)", g.rv(), wireBits), "")
	g.sf("%s}\n", ind)
}
