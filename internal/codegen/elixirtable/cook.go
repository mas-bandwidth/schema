// The COOKED FORM for Elixir (docs/SPEC-TABLES.md §7): the header match, the
// region where it lies, and the descriptors a region is read through, plus the
// write side cook_measure, cook_measure_reason and cook_from (§7.6) on the
// unit's Table module. PREVIEW, exactly as §7 says — a cook is a
// load-trusted-data format and not a wire.
//
// Tooling writes a region for one BUILD VERSION and that build points at it:
// Open matches the header and returns the region. There is no walk, no fix-up
// and no copy, which is what makes Open O(1) in the file's size — on the BEAM
// the region is a SUB-BINARY over the caller's own bytes, which the runtime
// shares rather than copies.
//
// THE READ AND WRITE TIER. Open matches the header and points. cook_from writes
// the same bytes `schema cook` writes, in the order the caller names (§7.6).
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
	var handles []*ir.Struct
	for _, st := range roots {
		if g.cookable(st.Name) {
			handles = append(handles, st)
		}
	}
	for i, st := range handles {
		g.emitCookHandle(st)
		if i < len(handles)-1 {
			g.pf("\n")
		}
	}
	g.pf("end\n")

	var b strings.Builder
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

  The write side is cook_measure, cook_measure_reason and cook_from on the
  unit's Table module (docs/SPEC-TABLES.md §7.6).
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

  # The magic as a cook of the OTHER byte order spells it: the one wrong magic
  # the reason names foreign_order rather than not_a_cook (§7.1; the reference
  # leg's TableRefuseForeignOrder).
  @foreign_magic 0x5343484D434F4F4B

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
  def open(data, lead, root_size, root_align, build_version) do
    case check(data, lead, root_size, root_align, build_version) do
      {:ok, region, region_length} -> {:ok, region, region_length}
      {:error, _reason} -> :error
    end
  end

  @doc """
  open_reason is Open's FIRST FAILING CLAUSE, the reason beside the refusal
  (docs/SPEC-TABLES.md §7): :ok when the cook opens, and otherwise the
  reference leg's TableRefuseReason value name (internal/codegen/gotable/
  refusal.go) as an atom — :unaligned_base, :truncated, :foreign_order,
  :not_a_cook, :wrong_build_version, :reserved_not_zero or :bad_alignment —
  checked in the reference's order (internal/codegen/gotable/cook.go, Open).
  nil is the null buffer, which the reference refuses as an unaligned base.
  """
  def open_reason(data, lead, root_size, root_align, build_version) do
    case check(data, lead, root_size, root_align, build_version) do
      {:ok, _region, _region_length} -> :ok
      {:error, reason} -> reason
    end
  end

  defp check(data, lead, root_size, root_align, build_version) when is_binary(data) do
    length = byte_size(data)

    if length < @header_bytes do
      {:error, :truncated}
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
        magic == @foreign_magic ->
          {:error, :foreign_order}

        magic != @magic ->
          {:error, :not_a_cook}

        order != @byte_order ->
          {:error, :not_a_cook}

        build != build_version ->
          {:error, :wrong_build_version}

        # a non-zero RESERVED word means a writer used a form this build does
        # not understand, and Open refuses rather than ignoring it
        reserved0 != 0 or reserved1 != 0 ->
          {:error, :reserved_not_zero}

        true ->
          extent(
            data,
            length,
            lead,
            data_length,
            attribution_length,
            alignment,
            root_size,
            root_align
          )
      end
    end
  end

  # no buffer at all: the reference's null base, refused as an unaligned base
  defp check(_data, _lead, _root_size, _root_align, _build_version),
    do: {:error, :unaligned_base}

  defp extent(
         data,
         length,
         lead,
         data_length,
         attribution_length,
         alignment,
         root_size,
         root_align
       ) do
    # THE ALIGNMENT WORD IS DATA, and it is the one header field the rest of the
    # check does arithmetic WITH rather than only compares against — so it is
    # checked BEFORE anything divides by it.
    if alignment < 8 or alignment > @max_align or (alignment &&& alignment - 1) != 0 or
         rem(alignment, root_align) != 0 do
      {:error, :bad_alignment}
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
      length < data_offset -> {:error, :truncated}
      # the whole file is data_offset + data_length + attribution_length, and a
      # length that is not EXACTLY that refuses — truncation and trailing bytes
      # are one refusal
      data_length > length - data_offset -> {:error, :truncated}
      attribution_length != length - data_offset - data_length -> {:error, :truncated}
      # the ROOT sits at the region's base, so the region has to hold it
      data_length < root_size -> {:error, :truncated}
      # the alignment of the BASE, which the caller states (see the doc above)
      rem(lead + data_offset, alignment) != 0 -> {:error, :unaligned_base}
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
  escapes. The delta is COMPOSED from a u32 low half and an i32 high half, the
  way the JavaScript leg composes it, so no bignum is formed before the bound
  that refuses it runs: a high half whose magnitude is 0x08000000 or more
  (2^59, past a BEAM immediate) is refused from the high half alone, and that
  is the same answer exact arithmetic gives for a delta that far outside a
  region.
  """
  def deref(region, at) do
    low = uint(region, at, 4)
    high = int(region, at + 4, 4)

    cond do
      low == 0 and high == 0 -> :null
      high >= 0x08000000 or high <= -0x08000000 -> :error
      true -> inside(region, at + high * 0x100000000 + low)
    end
  end

  defp inside(region, target) do
    if target < 0 or target >= byte_size(region), do: :error, else: {:ok, target}
  end

  @doc """
  A LIST's or a MAP's SPAN (docs/SPEC-TABLES.md §2.8, §2.9): the sixteen-byte
  holder at at, the self-relative delta to the element array and the int32
  count beside it, resolved IN PLACE to {:ok, start, count}: where the array
  lies in the region and how many elements of stride bytes it holds. Nothing
  is copied. An empty holder is {:ok, nil, 0}; a negative count, or an array
  that runs past the region, is :error.
  """
  def span(region, at, stride) do
    count = i32(region, at + 8)

    cond do
      count < 0 ->
        :error

      count == 0 ->
        {:ok, nil, 0}

      true ->
        case deref(region, at) do
          {:ok, start} when start + count * stride <= byte_size(region) -> {:ok, start, count}
          _ -> :error
        end
    end
  end

  @doc """
  FIND in a cooked map (docs/SPEC-TABLES.md §2.8, docs/PORTING.md M19): the
  holder at at resolved to its span, then a binary search IN PLACE over the
  entries, which lie ascending by key, reading each probed entry's key with
  key_at: floor(log2 n) + 1 key compares and no copy. Integers compare by
  value and string keys by their bytes, unsigned, a prefix first, which is the
  BEAM's own term order on both. {:ok, entry} is the entry's offset in the
  region; :error a key the map does not hold, or a holder span/3 refuses.
  """
  def find(region, at, stride, key_at, key) do
    case span(region, at, stride) do
      {:ok, start, count} -> search(region, start, stride, key_at, key, 0, count)
      :error -> :error
    end
  end

  defp search(_region, _start, _stride, _key_at, _key, low, high) when low >= high, do: :error

  defp search(region, start, stride, key_at, key, low, high) do
    mid = div(low + high, 2)
    entry = start + mid * stride
    k = key_at.(region, entry)

    cond do
      k == key -> {:ok, entry}
      k < key -> search(region, start, stride, key_at, key, mid + 1, high)
      true -> search(region, start, stride, key_at, key, low, mid)
    end
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
		g.pf("    size: %s,\n", exInt(layout.Size))
		g.pf("    align: %s,\n", exInt(layout.Align))
		g.pf("    fields: [\n")
		for i, fl := range layout.Fields {
			// the formatter drops a trailing comma, so the last row has none
			comma := ","
			if i == len(layout.Fields)-1 {
				comma = ""
			}
			g.pf("      %s%s\n", g.cookFieldRow(fl), comma)
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
	case f.IsMap():
		// a MAP's sixteen-byte holder (§2.8): its count companion at eight,
		// and its entries, each a record of the generated entry table
		storage, elemSize = ":map", ir.RecordLayout(g.unit, f.MapEntry).Size
		record = g.cookRecordRef(f.MapEntry.Name)
		countOffset = fl.Offset + 8
	case f.IsList():
		// a LIST's sixteen-byte holder (§2.9): its count companion at eight,
		// and its elements at their own size
		storage = ":list"
		elemSize, _ = ir.ListElementLayout(g.unit, f)
		if isStruct(f) && !f.Type.Pointer {
			record = g.cookRecordRef(f.Type.Name)
		}
		countOffset = fl.Offset + 8
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
	fmt.Fprintf(&b, "        offset: %s,\n", exInt(fl.Offset))
	fmt.Fprintf(&b, "        size: %s,\n", exInt(fl.Size))
	fmt.Fprintf(&b, "        elem_size: %s,\n", exInt(elemSize))
	fmt.Fprintf(&b, "        is_array: %v,\n", isArray)
	fmt.Fprintf(&b, "        array_bound: %s,\n", exInt(arrayBound))
	fmt.Fprintf(&b, "        is_pointer: %v,\n", f.Type.Pointer)
	fmt.Fprintf(&b, "        count_offset: %s,\n", exInt(countOffset))
	fmt.Fprintf(&b, "        present_offset: %s,\n", exInt(presentOffset))
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
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes:
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
	g.pf("  def %s_cook_region_alignment, do: %s\n", lo, exInt(align))
	g.pf("  def %s_cook_root_size, do: %s\n", lo, exInt(layout.Size))
	g.pf("  def %s_cook_root_align, do: %s\n\n", lo, exInt(layout.Align))

	g.pf("%s\n", exDefDo("  ", "def cook_open_"+lo+"(data)", "cook_open_"+lo+"(data, 0)"))
	g.pf("  # lead is how many bytes past an aligned base the caller's buffer\n")
	g.pf("  # begins — the one pointer fact a BEAM binary cannot carry, so the caller\n")
	g.pf("  # states it (see CookRuntime.open/5).\n")
	g.pf("  def cook_open_%s(data, lead) do\n", lo)
	g.pf("    case C.open(data, lead, %s, %s, @build_version) do\n", exInt(layout.Size), exInt(layout.Align))
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

	g.pf("  # the reason beside the refusal: :ok, or Open's first failing clause as\n")
	g.pf("  # the reference leg's TableRefuseReason value name (see\n")
	g.pf("  # CookRuntime.open_reason/5; docs/SPEC-TABLES.md §7).\n")
	g.pf("  def cook_open_reason_%s(data, lead) do\n", lo)
	g.pf("    C.open_reason(data, lead, %s, %s, @build_version)\n", exInt(layout.Size), exInt(layout.Align))
	g.pf("  end\n\n")

	g.pf("  # the TYPED slot accessors: each field of a %s node, read at its own\n", st.Name)
	g.pf("  # offset from the node's base within the region.\n")
	var items []string
	for _, fl := range ir.RecordLayout(g.unit, st).Fields {
		items = append(items, g.cookAccessors(st, fl)...)
	}
	g.pf("%s", exBlock(items))
}

// cookAccessors renders one field's accessors, each one expression (with the
// comment that leads it) for exBlock to separate the formatter's way.
func (g *gen) cookAccessors(st *ir.Struct, fl ir.FieldLayout) []string {
	f := fl.Field
	lo := ir.RustSnake(st.Name)
	pieces := ir.BlockFieldPieceOffsets(g.unit, f, fl.Offset, false)
	switch {
	case f.IsMap() || f.IsList():
		return g.cookHolderAccessors(st, f, fl)
	case f.Type.Pointer:
		return []string{"  # a REFERENCE: the eight-byte signed self-relative delta of §6.3, and\n" +
			"  # NULL IS ZERO. :error is a delta that resolves past the region.\n" +
			exDefDo("  ", fmt.Sprintf("def %s_node_%s(region, node)", lo, f.Name),
				fmt.Sprintf("C.deref(region, node + %s)", exInt(fl.Offset)))}
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		used := pieces[1].Offset
		return []string{exDefDo("  ", fmt.Sprintf("def %s_node_%s(region, node)", lo, f.Name),
			fmt.Sprintf("C.text(region, node + %s, C.i32(region, node + %s))", exInt(fl.Offset), exInt(used)))}
	case f.KeyEnum != "" || f.Array != ir.ArrayNone:
		var out []string
		size := elementSizeOf(g.unit, f)
		if f.Array == ir.ArrayCounted && f.KeyEnum == "" {
			out = append(out, exDefDo("  ", fmt.Sprintf("def %s_node_%s_count(region, node)", lo, f.Name),
				fmt.Sprintf("C.i32(region, node + %s)", exInt(pieces[1].Offset))))
		}
		out = append(out, fmt.Sprintf("  def %s_node_%s(%s, node) do\n", lo, f.Name, regionParam(f))+
			fmt.Sprintf("    Enum.map(0..%s//1, fn i -> %s end)\n", exInt(f.ArrayBound-1),
				g.cookReadAt(f, fmt.Sprintf("node + %s + i * %s", exInt(fl.Offset), exInt(size)), size))+
			"  end\n")
		return out
	default:
		var out []string
		size := elementSizeOf(g.unit, f)
		if f.Type.Optional {
			out = append(out, exDefDo("  ", fmt.Sprintf("def %s_node_%s_present?(region, node)", lo, f.Name),
				fmt.Sprintf("C.bool(region, node + %s)", exInt(pieces[len(pieces)-1].Offset))))
		}
		out = append(out, exDefDo("  ", fmt.Sprintf("def %s_node_%s(%s, node)", lo, f.Name, regionParam(f)),
			g.cookReadAt(f, fmt.Sprintf("node + %s", exInt(fl.Offset)), size)))
		return out
	}
}

// cookHolderAccessors renders a LIST's or a MAP's accessors (docs/SPEC-TABLES.md
// §2.8, §2.9, docs/PORTING.md M19): the holder's span, resolved in place to
// where the element array lies and how many it holds, the stride an element
// takes, and for a map FIND, the binary search over its ascending entries
// where they lie. Each element is read with its own record's accessors (a
// map's entry with the entry table's) at start + i * stride.
func (g *gen) cookHolderAccessors(st *ir.Struct, f *ir.Field, fl ir.FieldLayout) []string {
	lo := ir.RustSnake(st.Name)
	var stride int64
	if f.IsMap() {
		stride = ir.RecordLayout(g.unit, f.MapEntry).Size
	} else {
		stride, _ = ir.ListElementLayout(g.unit, f)
	}
	what := "LIST"
	if f.IsMap() {
		what = "MAP"
	}
	out := []string{
		fmt.Sprintf("  # a %s's holder, in place: {:ok, start, count} over elements of\n", what) +
			fmt.Sprintf("  # %s_node_%s_stride() bytes from start, {:ok, nil, 0} when empty\n", lo, f.Name) +
			exDefDo("  ", fmt.Sprintf("def %s_node_%s(region, node)", lo, f.Name),
				fmt.Sprintf("C.span(region, node + %s, %s)", exInt(fl.Offset), exInt(stride))),
		exDefDo("  ", fmt.Sprintf("def %s_node_%s_stride", lo, f.Name), exInt(stride)),
	}
	if f.IsMap() && g.cookable(f.MapEntry.Name) {
		entry := f.MapEntry
		home := g.unit.DeclFile[entry.Name]
		if home == "" {
			home = g.file.Base
		}
		keyAt := fmt.Sprintf("&%s_node_%s/2", ir.RustSnake(entry.Name), ir.MapKeyField(f).Name)
		if home != g.file.Base {
			keyAt = fmt.Sprintf("&%s.%sCook.%s_node_%s/2", g.ns, moduleBase(home), ir.RustSnake(entry.Name), ir.MapKeyField(f).Name)
		}
		out = append(out, "  # FIND: the entry whose key is key, {:ok, entry} or :error, by a binary\n"+
			"  # search over the entries where they lie (CookRuntime.find/5)\n"+
			fmt.Sprintf("  def %s_node_%s_find(region, node, key) do\n", lo, f.Name)+
			fmt.Sprintf("    key_at = %s\n", keyAt)+
			fmt.Sprintf("    C.find(region, node + %s, %s, key_at, key)\n", exInt(fl.Offset), exInt(stride))+
			"  end\n")
	}
	return out
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
// the root's node's own and every node reachable from it, a map's entries
// included.
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
		// a node with an extent aligns to every array that extent can hold
		// too (ir.CookNodeAlign, docs/SPEC-TABLES.md §2.8)
		aligns = append(aligns, cookNodeAlign(g.unit, rec))
		for _, f := range rec.Fields {
			if f.IsMap() {
				walk(f.MapEntry.Name)
			}
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
