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

// mergeW merges v (already inside bits) into the scratch and flushes every
// whole byte — the port's 32-bit-group packing restated at byte granularity,
// branchless, every statement rebinding ONE variable (an Elixir conditional
// cannot rebind several without a tuple, and the merge must allocate none).
// bits in [1, 32]; v < 2^32 and scratch_bits <= 7, so no intermediate
// reaches 2^40.
func (g *gen) mergeW(bits int64, ind string) {
	g.pf("%sscratch = scratch ||| v <<< scratch_bits\n", ind)
	g.pf("%sscratch_bits = scratch_bits + %d\n", ind, bits)
	g.pf("%sflush = scratch_bits >>> 3\n", ind)
	g.pf("%sdata = <<data::binary, scratch::little-size(flush)-unit(8)>>\n", ind)
	g.pf("%sscratch = scratch >>> (flush <<< 3)\n", ind)
	g.pf("%sscratch_bits = scratch_bits &&& 7\n", ind)
}

// readR reads bits (in [1,32]) from the 40-bit window at bits_read into v,
// masked. The caller has already priced the read inside num_bits.
func (g *gen) readR(bits int64, ind string) {
	g.needRd = true
	g.pf("%sv = rd(data, bits_read, %d)\n", ind, bits)
	g.pf("%sbits_read = bits_read + %d\n", ind, bits)
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
	g.withOwner(name, func() { g.emitWriteItems(items, "value", "    ") })
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
	g.bpf("    scratch = 0\n")
	g.bpf("    scratch_bits = 0\n")
	if body == "" {
		g.bpf("    # empty body — presence is the payload (SPEC §4.6)\n")
	}
	g.body.WriteString(body)
	g.bpf("    if scratch_bits != 0, do: <<data::binary, scratch>>, else: data\n")
	g.bpf("  end\n\n")
}

// emitReadFunction emits read_<name>(data, num_bits) -> {:ok, value} |
// :error. st is the struct whose storage the locals rebuild; nil for the
// standalone union surface (the body's one item binds local v instead).
func (g *gen) emitReadFunction(name string, st *ir.Struct, items []ir.Item) {
	g.usesImport = true
	snake := ir.RustSnake(name)
	g.fn.Reset()
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

func (g *gen) emitWriteItems(items []ir.Item, path, ind string) {
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
			cond := path + "." + elixirName(item.Cond)
			if item.Neg {
				cond = "not " + cond
			}
			g.pf("\n")
			g.pf("%s{data, scratch, scratch_bits} =\n", ind)
			g.pf("%s  if %s do\n", ind, cond)
			g.emitWriteItems(item.Then, path, ind+"    ")
			g.pf("%s    {data, scratch, scratch_bits}\n", ind)
			g.pf("%s  else\n", ind)
			if item.Else != nil {
				g.emitWriteItems(item.Else, path, ind+"    ")
			}
			g.pf("%s    {data, scratch, scratch_bits}\n", ind)
			g.pf("%s  end\n", ind)
			g.pf("\n")
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
	g.pf("%s# align: zero-pad to the byte boundary (SPEC §4.3)\n", ind)
	g.pf("%sdata = if scratch_bits != 0, do: <<data::binary, scratch>>, else: data\n", ind)
	g.pf("%sscratch = 0\n", ind)
	g.pf("%sscratch_bits = 0\n", ind)
}

func (g *gen) emitWriteField(f *ir.Field, path, ind string) {
	name := path + "." + elixirName(f.Name)
	if f.Name == "" {
		name = path // the standalone union functions' self item
	}
	switch f.Array {
	case ir.ArrayFixed:
		g.raiseIf(fmt.Sprintf("length(%s) != %s", name, intLit64(f.ArrayBound)),
			fmt.Sprintf("%s must hold exactly %d elements", name, f.ArrayBound), ind)
		helper := g.writeHelper(f)
		g.callAssign("{data, scratch, scratch_bits}",
			fmt.Sprintf("%s(%s, data, scratch, scratch_bits)", helper, name), ind)
	case ir.ArrayCounted:
		g.pf("%sn = length(%s)\n", ind, name)
		// length/1 is never negative, so a zero floor has no violable side
		if f.ArrayMin > 0 {
			g.raiseIf(fmt.Sprintf("n < %s", intLit64(f.ArrayMin)),
				fmt.Sprintf("%s count is below the wire minimum", name), ind)
		}
		g.raiseIf(fmt.Sprintf("n > %s", intLit64(f.ArrayBound)),
			fmt.Sprintf("%s count is above the wire maximum", name), ind)
		g.emitWriteOffset("n", big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), ind)
		helper := g.writeHelper(f)
		g.callAssign("{data, scratch, scratch_bits}",
			fmt.Sprintf("%s(%s, data, scratch, scratch_bits)", helper, name), ind)
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
	g.fn = strings.Builder{}
	base := fmt.Sprintf("  defp %s([], data, scratch, scratch_bits), do: {data, scratch, scratch_bits}", name)
	if len(base) <= formatWidth {
		g.pf("%s\n\n", base)
	} else {
		g.pf("  defp %s([], data, scratch, scratch_bits),\n    do: {data, scratch, scratch_bits}\n\n", name)
	}
	g.pf("  defp %s([e | rest], data, scratch, scratch_bits) do\n", name)
	g.emitWriteElem(f, "    ")
	g.pf("    %s(rest, data, scratch, scratch_bits)\n", name)
	g.pf("  end\n\n")
	g.helpers[name] = g.fn.String()
	g.fn = saved
	g.helperOrder = append(g.helperOrder, name)
	return name
}

// emitWriteElem writes one array element bound to e.
func (g *gen) emitWriteElem(f *ir.Field, ind string) {
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			g.withOwner(ref.Name, func() { g.emitWriteItems(ref.Items, "e", ind) })
			return
		case *ir.Union:
			g.emitWriteUnion(ref, "e", ind)
			return
		}
	}
	g.emitWriteScalar(f, "e", ind)
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
			g.emitCheckRange(name, name, big.NewInt(0), big.NewInt(ref.Max), ind)
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
					fmt.Sprintf("%s holds a mask bit above the %d-bit wire", name, wb), ind)
			} else {
				g.raiseIf(fmt.Sprintf("%s < 0 or %s >>> 64 != 0", name, name),
					fmt.Sprintf("%s holds a mask bit above the 64-bit wire", name), ind)
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
			fmt.Sprintf("%s must hold the locked value %s", name, rawMin), ind)
		return
	}
	g.emitCheckRange(name, name, rawMin, rawMax, ind)
	g.emitWriteOffset(name, rawMin, rawMax, ind)
}

func (g *gen) emitWriteInt(f *ir.Field, name, ind string) {
	w := int64(f.Type.Width)
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: ZERO bits — the check is the whole write
			g.raiseIf(fmt.Sprintf("%s != %s", name, intLit(f.IntMin)),
				fmt.Sprintf("%s must hold the locked value %s", name, f.IntMin), ind)
			return
		}
		g.emitCheckRange(name, name, f.IntMin, f.IntMax, ind)
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
	g.needCf = true
	maxInt, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	minF := float32(f.FMin)
	deltaF := float32(f.FMax) - minF
	g.pf("%sv = cf_quantize(%s, %s, %s, %s)\n", ind, name, f32lit(float64(minF)), f32lit(float64(deltaF)), intLitU64(maxInt))
	g.mergeW(bits, ind)
}

// emitWriteBytesField writes string(N)/bytes(N): checked ranged length,
// align (zero pad), then the used bytes appended whole — the classic
// serialize_string framing, byte-identical to every other target's.
func (g *gen) emitWriteBytesField(f *ir.Field, name, ind string) {
	g.raiseIf(fmt.Sprintf("byte_size(%s) > %d", name, f.Type.Size),
		fmt.Sprintf("%s longer than the declared %d-byte bound", name, f.Type.Size), ind)
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
	g.emitCheckRange(expr+".type", expr+".type", big.NewInt(0), big.NewInt(u.Max), ind)
	if u.Max == 0 {
		return // an empty union's degenerate tag range [0, 0] costs zero bits
	}
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	g.pf("%sv = %s.type\n", ind, expr)
	g.mergeW(bits, ind)
	g.pf("\n")
	g.pf("%s{data, scratch, scratch_bits} =\n", ind)
	g.pf("%s  case %s.type do\n", ind, expr)
	for i, vr := range u.Variants {
		g.pf("%s    %d ->\n", ind, i+1)
		if len(vr.Ref.Items) == 0 {
			g.pf("%s      # empty arm — presence is the payload (SPEC §4.6)\n", ind)
		} else {
			g.withOwner(vr.Type, func() {
				g.emitWriteItems(vr.Ref.Items, expr+"."+elixirName(vr.Name), ind+"      ")
			})
		}
		g.pf("%s      {data, scratch, scratch_bits}\n", ind)
		g.pf("\n")
	}
	g.pf("%s    _ ->\n", ind)
	g.pf("%s      # None — the tag is the whole wire (SPEC §4.8)\n", ind)
	g.pf("%s      {data, scratch, scratch_bits}\n", ind)
	g.pf("%s  end\n", ind)
	g.pf("\n")
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
			for ; i < j; i++ {
				g.emitReadItem(items[i], pre, ind, true)
			}
			continue
		}
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
	g.emitReadItems(item.Then, pre, ind+"    ", bounded)
	g.emitZeroLocals(item.Else, pre, ind+"    ")
	g.pf("%s    %s\n", ind, tuple)
	g.pf("%s  else\n", ind)
	if item.Else != nil {
		g.emitReadItems(item.Else, pre, ind+"    ", bounded)
	}
	g.emitZeroLocals(item.Then, pre, ind+"    ")
	g.pf("%s    %s\n", ind, tuple)
	g.pf("%s  end\n", ind)
	g.pf("\n")
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
		g.callAssign(fmt.Sprintf("{bits_read, %s}", lv),
			fmt.Sprintf("%s(n, [], data, num_bits, bits_read)", helper), ind)
		return
	}
	helper := g.readHelper(f, false)
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
		return name
	}
	g.helpers[name] = ""
	saved := g.fn
	g.fn = strings.Builder{}
	// the counter is `remaining`, never `n` — a nested counted array's count
	// local would shadow it
	base := fmt.Sprintf("  defp %s(0, acc, _data, _num_bits, bits_read), do: {bits_read, Enum.reverse(acc)}", name)
	if len(base) <= formatWidth {
		g.pf("%s\n\n", base)
	} else {
		g.pf("  defp %s(0, acc, _data, _num_bits, bits_read),\n    do: {bits_read, Enum.reverse(acc)}\n\n", name)
	}
	g.pf("  defp %s(remaining, acc, data, num_bits, bits_read) do\n", name)
	g.emitReadElem(f, "    ", bounded)
	g.pf("    %s(remaining - 1, [e | acc], data, num_bits, bits_read)\n", name)
	g.pf("  end\n\n")
	g.helpers[name] = g.fn.String()
	g.fn = saved
	g.helperOrder = append(g.helperOrder, name)
	return name
}

// emitReadElem reads one array element into local e.
func (g *gen) emitReadElem(f *ir.Field, ind string, bounded bool) {
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			g.withOwner(ref.Name, func() {
				g.emitReadItems(ref.Items, "e", ind, bounded)
				g.emitBuildStruct(ref, "e", "e", ind)
			})
			return
		case *ir.Union:
			g.emitReadUnion(ref, "e", ind, bounded)
			return
		}
	}
	g.emitReadScalar(f, "e", ind, bounded)
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
	g.needCf = true
	maxInt, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	minF := float32(f.FMin)
	deltaF := float32(f.FMax) - minF
	g.readR(bits, ind)
	if maxInt != (uint64(1)<<bits)-1 {
		g.throwIf(fmt.Sprintf("v > %s", intLitU64(maxInt)), "headroom above the quantum count is refused", ind)
	}
	g.pf("%s%s = cf_decode(v, %s, %s, %s)\n", ind, lv, intLitU64(maxInt), f32lit(float64(deltaF)), f32lit(float64(minF)))
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
	if f.Type.Kind == ir.TString {
		g.throwIf(fmt.Sprintf(":binary.match(%s, <<0>>) != :nomatch", lv),
			"an interior null is content the read refuses (SPEC §4.7)", ind)
		g.throwIf(fmt.Sprintf("not String.valid?(%s)", lv),
			"strings are UTF-8 by contract — a read validates it (SPEC §4.7)", ind)
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
	if g.needCf {
		g.bpf("  # A number rounded to its nearest float32 — one emulated float32 step\n")
		g.bpf("  # (exact: the double result carries 2x24+2 significant bits, so the second\n")
		g.bpf("  # rounding is innocuous). Overflow reports :pos_inf / :neg_inf so the\n")
		g.bpf("  # compressed-float clamps can resolve it the way the reference's float\n")
		g.bpf("  # arithmetic does.\n")
		g.bpf("  defp fr(value) do\n")
		g.bpf("    <<bits::little-32>> = <<value::float-32-little>>\n\n")
		g.bpf("    if (bits >>> 23 &&& 0xFF) != 0xFF do\n")
		g.bpf("      <<rounded::float-32-little>> = <<bits::little-32>>\n")
		g.bpf("      rounded\n")
		g.bpf("    else\n")
		g.bpf("      if bits >>> 31 == 1, do: :neg_inf, else: :pos_inf\n")
		g.bpf("    end\n")
		g.bpf("  end\n\n")
		g.bpf("  defp cf_clamp01(:pos_inf), do: 1.0\n")
		g.bpf("  defp cf_clamp01(:neg_inf), do: 0.0\n")
		g.bpf("  defp cf_clamp01(n) when n < 0.0, do: 0.0\n")
		g.bpf("  defp cf_clamp01(n) when n > 1.0, do: 1.0\n")
		g.bpf("  defp cf_clamp01(n), do: n\n\n")
		g.bpf("  # The family's two-rounding float32 quantization (SPEC §4.3), the port's\n")
		g.bpf("  # own steps: normalize with every step rounding, clamp to [0, 1] (which\n")
		g.bpf("  # also grounds an overflowed value), scale, round BEFORE the +0.5, floor,\n")
		g.bpf("  # and the normative integer clamp to the step count.\n")
		g.bpf("  defp cf_quantize(value, min32, delta, miv) do\n")
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
		g.bpf("    scaled = fr(normalized * fr(miv * 1.0))\n")
		g.bpf("    integer = trunc(Float.floor(fr(scaled + 0.5)))\n")
		g.bpf("    min(integer, miv)\n")
		g.bpf("  end\n\n")
		g.bpf("  # The reader's arithmetic, pinned the same way: the quotient rounds, the\n")
		g.bpf("  # product rounds BEFORE min is added, and the sum rounds — float32\n")
		g.bpf("  # throughout, never fused, never widened. The final add cannot overflow\n")
		g.bpf("  # for a conforming declaration; the non-finite mapping keeps the\n")
		g.bpf("  # never-raise reader obligation airtight.\n")
		g.bpf("  defp cf_decode(integer, miv, delta, min32) do\n")
		g.bpf("    quotient = fr(fr(integer * 1.0) / fr(miv * 1.0))\n")
		g.bpf("    scaled = fr(quotient * delta)\n\n")
		g.bpf("    case fr(scaled + min32) do\n")
		g.bpf("      :pos_inf -> {:nonfinite, 0x7F800000}\n")
		g.bpf("      :neg_inf -> {:nonfinite, 0xFF800000}\n")
		g.bpf("      value -> value\n")
		g.bpf("    end\n")
		g.bpf("  end\n\n")
	}
}
