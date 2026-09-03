// The COOKED FORM's read side for Elixir (docs/SPEC-TABLES.md §7): the header
// match, the region where it lies, and the descriptors a region is read
// through. PREVIEW, exactly as §7 says — a cook is a load-trusted-data format
// and not a wire.
//
// Tooling writes a region for one BUILD VERSION and that build points at it:
// Open matches the header and returns the region. There is no walk, no fix-up
// and no copy, which is what makes Open O(1) in the file's size — on the BEAM
// the region is a SUB-BINARY over the caller's own bytes, which the runtime
// shares rather than copies.
//
// THE READING TIER. Elixir cannot cook: a region is a memory image of a
// storage type, and a BEAM term has none. What it can do is open one and read
// every slot at its offset, with a reference resolved through the eight-byte
// SIGNED SELF-RELATIVE delta (§6.3) and a delta past the region refused.
package elixirtable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// CookRuntimeModule is the cooked form's shared runtime, emitted once per unit
// that has a cooked form. Its own module for the block runtime's reason: a
// VARIABLE unit gets no table runtime at all (§11) and still has both
// accelerators.
const CookRuntimeModule = "CookRuntime"

// cookModule assembles <Base>Cook.ex: the cook descriptors of every cookable
// record the file declares, one Open per table, and the typed slot accessors
// beside them.
func (g *gen) cookModule() []byte {
	roots := g.cookRoots()
	if len(roots) == 0 {
		return nil
	}
	any := false
	for _, st := range roots {
		if g.cookable(st.Name) {
			any = true
			break
		}
	}
	if !any {
		return nil
	}

	g.body.Reset()
	g.pf("defmodule %s.%sCook do\n", g.ns, moduleBase(g.file.Base))
	g.pf("%s", cookModuleBanner)
	g.pf("  alias %s.CookRuntime, as: C\n\n", g.ns)
	g.pf("  @build_version %s.BuildVersion.build_version()\n\n", g.ns)
	g.emitCookDescriptors(roots)
	for _, st := range roots {
		g.emitCookHandle(st)
	}
	g.pf("end\n")

	var b strings.Builder
	b.WriteString(g.banner)
	b.WriteString(header(g.file.Base, g.unit.Package, "the COOKED FORM (docs/SPEC-TABLES.md §7): the READ half"))
	b.WriteString("\n")
	b.WriteString(g.body.String())
	g.body.Reset()
	return []byte(b.String())
}

const cookModuleBanner = `  @moduledoc """
  The COOKED FORM's read half (docs/SPEC-TABLES.md §7), which §7 calls a
  PREVIEW. A cook is a load-trusted-data-from-tools format and not a wire:
  tooling writes a region for one BUILD VERSION and this build points at it.

  Open matches the header and returns the region — no walk, no fix-up, no copy.
  The region is a SUB-BINARY over the caller's own bytes.

  READING ONLY. A region is a memory image of a storage type and a BEAM term
  has none, so a cook arrives from a C++, C# or Rust build.
  """

`

func cookRuntimeModule(u *ir.Unit, ns string) []byte {
	var b strings.Builder
	b.WriteString(header(CookRuntimeModule, u.Package,
		"the COOKED FORM's shared runtime (docs/SPEC-TABLES.md §7)"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "defmodule %s.CookRuntime do\n", ns)
	b.WriteString(cookRuntimeSource)
	b.WriteString("end\n")
	return []byte(b.String())
}

const cookRuntimeSource = `  @moduledoc """
  The cooked form's read runtime (docs/SPEC-TABLES.md §7), emitted once per
  unit.
  """

  import Bitwise

  # The cook's MAGIC (§7.1), read BYTEWISE before anything else: it is what
  # establishes the byte order every other header word is written in, and it is
  # also what separates a COOK from a BLOCK — the two accelerators carry the
  # same build version and different magics.
  #
  # The value is "SCHMCOOK" read as ASCII in the byte order a little-endian
  # store produces, so a hex dump of a little-endian cook is legible.
  @magic 0x4B4F4F434D484353

  # The order this leg reads in; a cook of the other one is refused, and the
  # fix-up path is a named obligation rather than something a consumer
  # improvises.
  @byte_order 1

  # §7.1's header is 64 bytes of u64 words, and the DATA part begins at
  # align_up(64, alignment) — DERIVED and not a header field, because a fact a
  # reader computes is a fact two writers cannot disagree about.
  @header_bytes 64

  # The ceiling on the header's alignment word: the same sixty-four a block's
  # base takes (§19.1).
  @max_align 64

  def magic, do: @magic
  def header_bytes, do: @header_bytes

  @doc """
  Open checks the header and POINTS, and this is the WHOLE check (§7): the
  magic read bytewise, the byte order it establishes, the build version, every
  RESERVED word zero, the region ALIGNMENT the header names, the two part
  lengths against the length the caller passed — a truncated file refuses — the
  ROOT's own storage inside the data part, and the alignment of the base.
  Nothing per node, ever: that is what makes this O(1) in the file's size.

  THE lead ARGUMENT IS THE READING TIER'S ONE ADDITION, and it is named here
  rather than discovered. §7 checks the alignment of the BASE, which is a fact
  about the pointer a caller holds; a BEAM binary has no address a caller can
  observe or place, so the caller states it — lead is how many bytes past an
  aligned base its buffer begins, and 0 (the default every generated Open
  takes) is the aligned case a file read into a fresh binary always is. The
  check is then a real one rather than a skipped one, which is the whole point:
  the alternative is a leg that cannot refuse an unaligned base at all.
  """
  def open(data, lead, root_size, root_align, build_version) when is_binary(data) do
    length = byte_size(data)

    if length < @header_bytes do
      :error
    else
      # the MAGIC, bytewise and first: it is what establishes the byte order
      # every other header word is read in, so nothing else may be read before
      # it. A byte-reversed constant is a cook of the other order and refuses
      # here, which is why the order never reaches a fix-up pass.
      <<magic::little-unsigned-64, build::little-unsigned-64, order::little-unsigned-64,
        data_length::little-unsigned-64, attribution_length::little-unsigned-64,
        alignment::little-unsigned-64, reserved0::little-unsigned-64,
        reserved1::little-unsigned-64, _::binary>> = data

      cond do
        magic != @magic -> :error
        order != @byte_order -> :error
        build != build_version -> :error
        # a non-zero RESERVED word means a writer used a form this build does
        # not understand, and Open refuses rather than ignoring it
        reserved0 != 0 -> :error
        reserved1 != 0 -> :error
        true -> extent(data, length, lead, data_length, attribution_length, alignment, root_size, root_align)
      end
    end
  end

  def open(_data, _lead, _root_size, _root_align, _build_version), do: :error

  defp extent(data, length, lead, data_length, attribution_length, alignment, root_size, root_align) do
    # THE ALIGNMENT WORD IS DATA, and it is the one header field the rest of the
    # check does arithmetic WITH rather than only compares against — so it is
    # checked BEFORE anything divides by it.
    if alignment < 8 or alignment > @max_align or (alignment &&& (alignment - 1)) != 0 or
         rem(alignment, root_align) != 0 do
      :error
    else
      region(data, length, lead, data_length, attribution_length, alignment, root_size)
    end
  end

  defp region(data, length, lead, data_length, attribution_length, alignment, root_size) do
    # The DATA part begins at align_up(64, alignment). It is DERIVED and not a
    # header field, because a fact a reader computes is a fact two writers
    # cannot disagree about.
    data_offset = align_up(@header_bytes, alignment)

    cond do
      length < data_offset -> :error
      # the whole file is data_offset + data_length + attribution_length, and a
      # length that is not EXACTLY that refuses — truncation and trailing bytes
      # are one refusal
      data_length > length - data_offset -> :error
      attribution_length != length - data_offset - data_length -> :error
      # the ROOT sits at the region's base, so the region has to hold it
      data_length < root_size -> :error
      # the alignment of the BASE, which the caller states (see the doc above)
      rem(lead + data_offset, alignment) != 0 -> :error
      true -> {:ok, binary_part(data, data_offset, data_length), data_length}
    end
  end

  defp align_up(v, a), do: div(v + a - 1, a) * a

  # ---- reading one slot out of a region ----

  def uint(region, at, width) do
    <<_::binary-size(^at), v::little-unsigned-size(^width)-unit(8), _::binary>> = region
    v
  end

  def int(region, at, width) do
    <<_::binary-size(^at), v::little-signed-size(^width)-unit(8), _::binary>> = region
    v
  end

  def i32(region, at), do: int(region, at, 4)

  def bool(region, at), do: uint(region, at, 1) != 0

  # a string's or a bytes' USED bytes, without the zero tail (§7.2)
  def text(region, at, used), do: binary_part(region, at, used)

  @doc """
  A REFERENCE, resolved: the slot holds the eight-byte SIGNED SELF-RELATIVE
  delta of §6.3, and NULL IS ZERO. A delta that resolves outside the region is
  refused — an index out of bounds is a refusal here, never an exception that
  escapes.
  """
  def deref(region, at) do
    delta = int(region, at, 8)

    cond do
      delta == 0 -> :null
      true -> inside(region, at + delta)
    end
  end

  defp inside(region, target) do
    if target < 0 or target >= byte_size(region), do: :error, else: {:ok, target}
  end
`

// cookRoots is every closure member declared in this file that a cooked region
// can hold: a root is ANY table, and a record is anything one reaches by value.
func (g *gen) cookRoots() []*ir.Struct {
	out := append([]*ir.Struct(nil), orderTables(g.file.Tables)...)
	for _, d := range g.file.Decls {
		if st, ok := d.(*ir.Struct); ok && g.closure[st.Name] {
			out = append(out, st)
		}
	}
	return out
}

// cookable reports whether a record has an Elixir cooked form. A record that
// reaches a UNION by value does not: the cook descriptor family has no union
// row — a tag beside overlaid arms is one more storage shape than a slot walk
// has — and it is a NAMED FOLLOW-ON rather than something to improvise inside
// a record. The rule is the Rust backend's, for a different reason and with the
// same effect.
func (g *gen) cookable(name string) bool {
	seen := map[string]bool{}
	var walk func(n string) bool
	walk = func(n string) bool {
		if seen[n] {
			return true
		}
		seen[n] = true
		st := g.unit.Tables[n]
		if st == nil {
			st = g.unit.Structs[n]
		}
		if st == nil {
			return false
		}
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Union:
				return false
			case *ir.Struct:
				if !walk(ref.Name) {
					return false
				}
			}
		}
		return true
	}
	return walk(name)
}

// ---- the cooked records as DATA (docs/SPEC-TABLES.md §7.5) ----

func (g *gen) emitCookDescriptors(members []*ir.Struct) {
	g.pf("  # ---- the cooked records as DATA (docs/SPEC-TABLES.md §7.5) ----\n")
	g.pf("  #\n")
	g.pf("  # One record's layout, as a module attribute: where each field sits, how\n")
	g.pf("  # big it is, whether it is a POINTER EDGE, the bound its COUNT COMPANION\n")
	g.pf("  # is checked against, and the record it names. A walker DESCENDS through\n")
	g.pf("  # the record column — a pointer's target, a by-value nesting and an\n")
	g.pf("  # array's element are all reached through that one column.\n\n")
	for _, st := range members {
		if !g.cookable(st.Name) {
			continue
		}
		layout := ir.RecordLayout(g.unit, st)
		lo := ir.RustSnake(st.Name)
		g.pf("  @cook_info_%s %%{\n", lo)
		g.pf("    name: %q,\n", st.Name)
		g.pf("    size: %d,\n", layout.Size)
		g.pf("    align: %d,\n", layout.Align)
		g.pf("    fields: [\n")
		for _, fl := range layout.Fields {
			g.pf("      %s,\n", g.cookFieldRow(fl))
		}
		g.pf("    ]\n")
		g.pf("  }\n")
		g.pf("  def cook_info_%s, do: @cook_info_%s\n\n", lo, lo)
	}
}

func (g *gen) cookFieldRow(fl ir.FieldLayout) string {
	f := fl.Field
	pieces := ir.BlockFieldPieceOffsets(g.unit, f, fl.Offset, false)
	countOffset, presentOffset := int64(-1), int64(-1)
	for i, name := range cookRowMembers(f) {
		if i >= len(pieces) {
			break
		}
		if strings.HasSuffix(name, "_length") || strings.HasSuffix(name, "_count") {
			countOffset = pieces[i].Offset
		}
		if strings.HasSuffix(name, "_present") {
			presentOffset = pieces[i].Offset
		}
	}

	var storage string
	var elemSize int64
	record := "nil"
	isArray := false
	arrayBound := int64(0)
	switch {
	case f.Type.Pointer:
		storage, elemSize = ":reference", 8
		record = g.cookRecordRef(f.Type.Name)
	case f.Type.Kind == ir.TString:
		storage, elemSize, arrayBound = ":string", f.Type.Size+1, f.Type.Size
	case f.Type.Kind == ir.TBytes:
		storage, elemSize, arrayBound, isArray = ":bytes", 1, f.Type.Size, true
	default:
		storage, elemSize = cookStorageOf(g.unit, f)
		if isStruct(f) {
			record = g.cookRecordRef(f.Type.Name)
		}
		if f.KeyEnum != "" || f.Array != ir.ArrayNone {
			isArray = true
			arrayBound = f.ArrayBound
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%%{\n")
	fmt.Fprintf(&b, "        name: %q,\n", f.Name)
	fmt.Fprintf(&b, "        offset: %d,\n", fl.Offset)
	fmt.Fprintf(&b, "        size: %d,\n", fl.Size)
	fmt.Fprintf(&b, "        elem_size: %d,\n", elemSize)
	fmt.Fprintf(&b, "        is_array: %v,\n", isArray)
	fmt.Fprintf(&b, "        array_bound: %d,\n", arrayBound)
	fmt.Fprintf(&b, "        is_pointer: %v,\n", f.Type.Pointer)
	fmt.Fprintf(&b, "        count_offset: %d,\n", countOffset)
	fmt.Fprintf(&b, "        present_offset: %d,\n", presentOffset)
	fmt.Fprintf(&b, "        storage: %s,\n", storage)
	fmt.Fprintf(&b, "        record: %s\n", record)
	fmt.Fprintf(&b, "      }")
	return b.String()
}

// cookRecordRef is the {module, function} pair a descriptor descends through.
func (g *gen) cookRecordRef(name string) string {
	home := g.unit.DeclFile[name]
	if home == "" {
		home = g.file.Base
	}
	return fmt.Sprintf("{%s.%sCook, :cook_info_%s}", g.ns, moduleBase(home), ir.RustSnake(name))
}

// cookStorageOf classifies one slot and gives its width. An ENUM slot holds the
// ORDINAL at the enum's own derived storage width (§7.2), where the wire rides
// the variant-name hash — the width comes from here and not from the kind.
func cookStorageOf(u *ir.Unit, f *ir.Field) (string, int64) {
	switch f.Type.Kind {
	case ir.TBool:
		return ":bool", 1
	case ir.TFloat32:
		return ":float", 4
	case ir.TFloat64:
		return ":float", 8
	case ir.TInt:
		if f.Type.Signed {
			return ":signed", int64(f.Type.Width) / 8
		}
		return ":unsigned", int64(f.Type.Width) / 8
	case ir.TBits:
		if f.Type.Width <= 32 {
			return ":unsigned", 4
		}
		return ":unsigned", 8
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			return ":unsigned", int64(ir.StorageBitsFor(ref.Max)) / 8
		case *ir.Flags:
			return ":unsigned", 8
		case *ir.Struct:
			return ":record", ir.RecordLayout(u, ref).Size
		}
	}
	return ":unsigned", 1
}

// cookRowMembers names a field's contiguous storage members in order, which is
// what the piece offsets are indexed by.
func cookRowMembers(f *ir.Field) []string {
	names := []string{f.Name}
	switch {
	case f.Type.Pointer:
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		names = append(names, f.Name+"_length")
	case f.Array == ir.ArrayCounted && f.KeyEnum == "":
		names = append(names, f.Name+"_count")
	}
	if f.Type.Optional {
		names = append(names, f.Name+"_present")
	}
	return names
}

// ---- the cook handles ----

func (g *gen) emitCookHandle(st *ir.Struct) {
	if !g.cookable(st.Name) {
		return
	}
	layout := ir.RecordLayout(g.unit, st)
	align := ir.RegionAlignOf(g.cookRegionAligns(st)...)
	lo := ir.RustSnake(st.Name)

	g.pf("  # %s's cook: a region and a length, and then the root where it lies.\n", st.Name)
	g.pf("  # Opening one is a HEADER MATCH and no copy (docs/SPEC-TABLES.md §7).\n")
	g.pf("  #\n")
	g.pf("  # §7.1's constants, so a consumer reading this file has the facts and not\n")
	g.pf("  # a description of them.\n")
	g.pf("  def %s_cook_region_alignment, do: %d\n", lo, align)
	g.pf("  def %s_cook_root_size, do: %d\n", lo, layout.Size)
	g.pf("  def %s_cook_root_align, do: %d\n\n", lo, layout.Align)

	g.pf("  def cook_open_%s(data), do: cook_open_%s(data, 0)\n\n", lo, lo)
	g.pf("  # lead is how many bytes past an aligned base the caller's buffer\n")
	g.pf("  # begins — the one pointer fact a BEAM binary cannot carry, so the caller\n")
	g.pf("  # states it (see CookRuntime.open/5).\n")
	g.pf("  def cook_open_%s(data, lead) do\n", lo)
	g.pf("    case C.open(data, lead, %d, %d, @build_version) do\n", layout.Size, layout.Align)
	g.pf("      {:ok, region, region_length} ->\n")
	g.pf("        {:ok,\n")
	g.pf("         %%{\n")
	g.pf("           name: %q,\n", st.Name)
	g.pf("           region: region,\n")
	g.pf("           region_length: region_length,\n")
	g.pf("           info: {__MODULE__, :cook_info_%s}\n", lo)
	g.pf("         }}\n\n")
	g.pf("      :error ->\n")
	g.pf("        :error\n")
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  # the TYPED slot accessors: each field of a %s node, read at its own\n", st.Name)
	g.pf("  # offset from the node's base within the region.\n")
	for _, fl := range ir.RecordLayout(g.unit, st).Fields {
		g.emitCookAccessor(st, fl)
	}
	g.pf("\n")
}

func (g *gen) emitCookAccessor(st *ir.Struct, fl ir.FieldLayout) {
	f := fl.Field
	lo := ir.RustSnake(st.Name)
	pieces := ir.BlockFieldPieceOffsets(g.unit, f, fl.Offset, false)
	switch {
	case f.Type.Pointer:
		g.pf("  # a REFERENCE: the eight-byte signed self-relative delta of §6.3, and\n")
		g.pf("  # NULL IS ZERO. :error is a delta that resolves past the region.\n")
		g.pf("  def %s_node_%s(region, node), do: C.deref(region, node + %d)\n", lo, f.Name, fl.Offset)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		used := pieces[len(pieces)-1].Offset
		if !f.Type.Optional {
			used = pieces[1].Offset
		}
		g.pf("  def %s_node_%s(region, node), do: C.text(region, node + %d, C.i32(region, node + %d))\n",
			lo, f.Name, fl.Offset, used)
	case f.KeyEnum != "" || f.Array != ir.ArrayNone:
		size := elementSizeOf(g.unit, f)
		if f.Array == ir.ArrayCounted && f.KeyEnum == "" {
			g.pf("  def %s_node_%s_count(region, node), do: C.i32(region, node + %d)\n",
				lo, f.Name, pieces[1].Offset)
		}
		g.pf("  def %s_node_%s(%s, node) do\n", lo, f.Name, regionParam(f))
		g.pf("    Enum.map(0..%d//1, fn i -> %s end)\n", f.ArrayBound-1,
			g.cookReadAt(f, fmt.Sprintf("node + %d + i * %d", fl.Offset, size), size))
		g.pf("  end\n")
	default:
		size := elementSizeOf(g.unit, f)
		if f.Type.Optional {
			g.pf("  def %s_node_%s_present?(region, node), do: C.bool(region, node + %d)\n",
				lo, f.Name, pieces[len(pieces)-1].Offset)
		}
		g.pf("  def %s_node_%s(%s, node), do: %s\n", lo, f.Name, regionParam(f),
			g.cookReadAt(f, fmt.Sprintf("node + %d", fl.Offset), size))
	}
}

// regionParam names the region argument, underscored where a nested record's
// accessor answers with an OFFSET and never reads a byte itself.
func regionParam(f *ir.Field) string {
	if isStruct(f) && !f.Type.Pointer {
		return "_region"
	}
	return "region"
}

// cookReadAt renders the expression that reads ONE slot at `at`.
func (g *gen) cookReadAt(f *ir.Field, at string, size int64) string {
	if isStruct(f) {
		// a nested record is reached by its OFFSET: the record's own accessors
		// take the region and that offset, so nothing is copied out of it
		return at
	}
	switch f.Type.Kind {
	case ir.TBool:
		return fmt.Sprintf("C.bool(region, %s)", at)
	case ir.TFloat32, ir.TFloat64:
		// a float's PATTERN, because §7.5's dump refuses to invent a spelling
		// and a consumer that wants the value decodes the bits it holds
		return fmt.Sprintf("C.uint(region, %s, %d)", at, size)
	}
	if f.Type.Kind == ir.TInt && f.Type.Signed {
		return fmt.Sprintf("C.int(region, %s, %d)", at, size)
	}
	return fmt.Sprintf("C.uint(region, %s, %d)", at, size)
}

// cookRegionAligns is every alignment a region rooted at this record can hold:
// the root's own and every record reachable from it.
func (g *gen) cookRegionAligns(st *ir.Struct) []int64 {
	seen := map[string]bool{}
	var aligns []int64
	var walk func(name string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		rec := g.unit.Tables[name]
		if rec == nil {
			rec = g.unit.Structs[name]
		}
		if rec == nil {
			return
		}
		aligns = append(aligns, ir.RecordLayout(g.unit, rec).Align)
		for _, f := range rec.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				walk(ref.Name)
			}
		}
	}
	walk(st.Name)
	return aligns
}

// anyCookable reports whether the unit has a cooked form at all: a record whose
// by-value closure reaches a union has none, and a unit of only those gets no
// cook runtime rather than a runtime nothing calls.
func anyCookable(u *ir.Unit, closure map[string]bool) bool {
	g := &gen{unit: u, closure: closure}
	for name := range closure {
		if g.cookable(name) {
			return true
		}
	}
	return false
}
