// The BLOCK FORM for Elixir (docs/SPEC-TABLES.md §19): the READ side, and only
// the read side.
//
// NOTHING DECLARES THIS FORM. Every fixed table has one, and it is emitted on
// the side, in <Base>Block.ex, which a consumer calls only if it uses the form.
// The unit's <Base>Table.ex carries not one symbol of it.
//
// A block is one flat extent: the table's own instance at the front — the
// PROJECTION, carrying per bounded array of structs where its rows start, how
// many there are and how far apart they sit — and then those rows, each array
// at a fixed pitch. This side reads those three facts out of the INSTANCE and
// points at rows with them, never at its own constants.
//
// THE READING TIER, and what that costs here: Elixir cannot produce a block,
// because a BEAM term has no layout a producer could write. What it CAN do is
// read one, and the whole of §19.2's check list is expressible — the magic
// bytewise, the build version, the byte order, each array's pitch against this
// build's own, its count against the declared maximum, and its extent inside
// the block. The ONE check that is not is the BASE'S ALIGNMENT: a BEAM binary
// has no address a caller places, so there is no unaligned base to refuse. The
// forgery battery's pointer column says the same thing from the other side.
package elixirtable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// BlockRuntimeModule is the block form's shared runtime, emitted once per unit
// that has a block-form table. It is its own module because a VARIABLE unit
// gets no table runtime at all (§11) and still has both accelerators.
const BlockRuntimeModule = "BlockRuntime"

func generateBlocks(u *ir.Unit, ns string, blocks *ir.BlockUnit, banner string) (map[string][]byte, error) {
	if len(blocks.Tables) == 0 {
		return nil, nil
	}
	out := map[string][]byte{}
	out[BlockRuntimeModule+".ex"] = []byte(banner +
		header(BlockRuntimeModule, u.Package, "the BLOCK FORM's shared runtime (docs/SPEC-TABLES.md §19)") +
		"\n" + fmt.Sprintf("defmodule %s.BlockRuntime do\n", ns) + blockRuntimeSource + "end\n")

	byFile := map[string][]*ir.BlockLayout{}
	for _, bl := range blocks.Tables {
		base := u.DeclFile[bl.Table.Name]
		byFile[base] = append(byFile[base], bl)
	}
	bases := make([]string, 0, len(byFile))
	for base := range byFile {
		bases = append(bases, base)
	}
	sort.Strings(bases)

	for _, base := range bases {
		tables := byFile[base]
		sort.Slice(tables, func(i, j int) bool { return tables[i].Table.Name < tables[j].Table.Name })
		b := &blockGen{unit: u, ns: ns, base: base}
		b.pf("defmodule %s.%sBlock do\n", ns, moduleBase(base))
		b.pf("%s", blockModuleBanner)
		b.pf("  alias %s.BlockRuntime, as: B\n\n", ns)
		b.pf("  @build_version %s.BuildVersion.build_version()\n\n", ns)
		for _, bl := range tables {
			b.emitBlock(bl)
		}
		b.emitRecordInfos(tables)
		b.pf("end\n")

		var text strings.Builder
		text.WriteString(banner)
		text.WriteString(header(base, u.Package, "the BLOCK FORM (docs/SPEC-TABLES.md §19)"))
		text.WriteString("\n")
		text.WriteString(b.body.String())
		out[base+"Block.ex"] = []byte(text.String())
	}
	return out, nil
}

const blockModuleBanner = `  @moduledoc """
  NOTHING DECLARES THIS FORM. Every fixed table has one, and it is emitted on
  the side: call into this module only if you use the block form. The unit's
  table module carries not one symbol of it.

  A block is one flat extent: the table's own instance at the front — the
  PROJECTION, carrying per bounded array of structs where its rows start, how
  many there are and how far apart they sit — and then those rows, each array
  at a fixed pitch. This side reads those three facts and points.

  READING ONLY. A BEAM term has no layout a producer could write, so a block
  arrives from a C++, C# or Rust build and this module opens it. The bytes are
  never copied: a row is a SUB-BINARY over the caller's own block.
  """

`

const blockRuntimeSource = `  @moduledoc """
  The block form's shared runtime (docs/SPEC-TABLES.md §19), emitted once per
  unit. It is its own module because a VARIABLE unit gets no table runtime at
  all (§11) and still has both accelerators.
  """

  # The block's magic, and the byte-order check with it (§19.1). It is stored in
  # the producer's NATIVE order; a consumer that reads back the byte-swapped
  # value has found a foreign byte order, and one that reads back anything else
  # has not found a block at all.
  @magic 0x4B4C42414D484353

  # The order a BEAM reader reads in. The generated codecs spell every word
  # little-endian explicitly, so this is the order this leg understands and a
  # block of the other one is REFUSED: a big-endian fix-up path is a named
  # obligation, not something a consumer improvises row by row.
  @byte_order 1

  # Every block base and every out-of-line array start takes this alignment: a
  # cache line, so two workers filling different arrays never share one.
  @align 64

  def magic, do: @magic
  def byte_order, do: @byte_order
  def align, do: @align

  # the prologue, read at explicit widths out of the caller's own binary
  def u64(data, at), do: read(data, at, 8)
  def u32(data, at), do: read(data, at, 4)

  def i32(data, at) do
    case data do
      <<_::binary-size(^at), v::little-signed-32, _::binary>> -> v
      _ -> nil
    end
  end

  defp read(data, at, width) do
    case data do
      <<_::binary-size(^at), v::little-unsigned-size(^width)-unit(8), _::binary>> -> v
      _ -> nil
    end
  end

  def uint(data, at, width), do: read(data, at, width)

  def int(data, at, width) do
    case data do
      <<_::binary-size(^at), v::little-signed-size(^width)-unit(8), _::binary>> -> v
      _ -> nil
    end
  end

  # a float is its IEEE-754 pattern here: the bits are what a row carries, and
  # the block dump spells them as bits for exactly that reason (§19.2)
  def f32(data, at), do: read(data, at, 4)
  def f64(data, at), do: read(data, at, 8)

  # one row, as a SUB-BINARY over the block: the BEAM shares the bytes rather
  # than copying them, which is what makes an iteration over 4096 rows cost the
  # references and nothing else
  def slice(data, at, size) do
    if at >= 0 and size >= 0 and at + size <= byte_size(data) do
      binary_part(data, at, size)
    else
      nil
    end
  end

  # An array is ITERATED, not indexed by hand: the accessor yields each row
  # where it lies, at the pitch the INSTANCE gives, for count rows. A call site
  # never spells the pitch arithmetic itself (§19.2).
  def rows(data, offset_of, count, stride) do
    Enum.map(0..(count - 1)//1, fn i -> slice(data, offset_of + i * stride, stride) end)
  end
`

type blockGen struct {
	unit *ir.Unit
	ns   string
	base string
	body strings.Builder
}

func (b *blockGen) pf(format string, args ...any) {
	fmt.Fprintf(&b.body, format, args...)
}

func (b *blockGen) emitBlock(bl *ir.BlockLayout) {
	name := bl.Table.Name
	lo := ir.RustSnake(name)
	proj := bl.Projection

	b.pf("  # ---- the block form of table %s (docs/SPEC-TABLES.md §19) ----\n\n", name)

	b.pf("  # The block's STORAGE is sized from the declared maxima: one extent,\n")
	b.pf("  # allocated once, never grown, never pooled (§19.1).\n")
	b.pf("  def %s_block_max_bytes, do: %d\n", lo, bl.MaxBytes)
	b.pf("  def %s_block_projection_bytes, do: %d\n\n", lo, proj.Size)

	for _, a := range bl.Arrays {
		b.pf("  # %s: the constants this build asserts against. A consumer INDEXES with\n", a.Field.Name)
		b.pf("  # what it read from the instance, never with these (§19.2).\n")
		b.pf("  def %s_%s_stride, do: %d\n", lo, a.Field.Name, a.Stride)
		b.pf("  def %s_%s_max, do: %d\n", lo, a.Field.Name, a.Max)
		b.pf("  def %s_%s_projection_offset, do: %d\n\n", lo, a.Field.Name, a.TripleOffset)
	}

	b.emitBlockArrays(bl)
	b.emitBlockOpen(bl)
	b.emitBlockRows(bl)
	b.emitBlockInfo(bl)
}

func (b *blockGen) emitBlockOpen(bl *ir.BlockLayout) {
	name := bl.Table.Name
	lo := ir.RustSnake(name)
	proj := bl.Projection

	b.pf("  # Open checks once and POINTS, and this is the WHOLE check (§19.2): the\n")
	b.pf("  # BASE'S ALIGNMENT, the magic, the build version, the byte order, and then,\n")
	b.pf("  # per out-of-line array, the pitch against this build's, the count against\n")
	b.pf("  # the declared maximum, and the rows inside the extent the caller passed.\n")
	b.pf("  #\n")
	b.pf("  # lead IS THE BASE'S ALIGNMENT, and it is the same argument the cook's Open\n")
	b.pf("  # takes for the same reason: §19.1 lays a block at a 64-byte aligned base\n")
	b.pf("  # and §19.2 checks it, and a BEAM binary has no address a caller can\n")
	b.pf("  # observe or place — so the caller states how many bytes past an aligned\n")
	b.pf("  # base its buffer begins. 0 is the aligned case a file read into a fresh\n")
	b.pf("  # binary always is. Stating it makes the check a real one; the alternative\n")
	b.pf("  # is a leg that cannot refuse an unaligned base at all, which is a check\n")
	b.pf("  # four other backends make.\n")
	b.pf("  #\n")
	b.pf("  # EVERY OTHER NUMBER COMES FROM THE INSTANCE. BEAM integers are\n")
	b.pf("  # arbitrary-precision, so no term of this arithmetic can carry past the\n")
	b.pf("  # top of a type — a forged offset_of near 2^63 is simply a large number\n")
	b.pf("  # that fails its bound.\n")
	b.pf("  def block_open_%s(data), do: block_open_%s(data, 0)\n\n", lo, lo)
	b.pf("  def block_open_%s(data, lead) when is_binary(data) and is_integer(lead) do\n", lo)
	b.pf("    bytes = byte_size(data)\n\n")
	b.pf("    if bytes < %d or rem(lead, B.align()) != 0 do\n", proj.Size)
	b.pf("      :refuse\n")
	b.pf("    else\n")
	b.pf("      case data do\n")
	b.pf("        <<magic::little-unsigned-64, build::little-unsigned-64,\n")
	b.pf("          order::little-unsigned-64, _::binary>> ->\n")
	b.pf("          # a byte-swapped magic is a FOREIGN BYTE ORDER, and anything else\n")
	b.pf("          # is not a block at all. Both refuse.\n")
	b.pf("          if magic != B.magic() or build != @build_version or order != B.byte_order() do\n")
	b.pf("            :refuse\n")
	b.pf("          else\n")
	b.pf("            block_extent_%s(data, bytes)\n", lo)
	b.pf("          end\n\n")
	b.pf("        _ ->\n")
	b.pf("          :refuse\n")
	b.pf("      end\n")
	b.pf("    end\n")
	b.pf("  end\n\n")
	b.pf("  def block_open_%s(_data, _lead), do: :refuse\n\n", lo)

	b.pf("  defp block_extent_%s(data, bytes) do\n", lo)
	if len(bl.Arrays) == 0 {
		b.pf("    # this table declares no out-of-line array, so the prologue and the\n")
		b.pf("    # projection's own extent are the whole check\n")
		b.pf("    block_used_%s(data, bytes, %d)\n", lo, proj.Size)
	} else {
		b.pf("    used =\n")
		b.pf("      Enum.reduce_while(%s_block_arrays(), %d, fn a, used ->\n", lo, proj.Size)
		b.pf("        offset_of = B.u64(data, a.offset_of_offset)\n")
		b.pf("        count = B.u32(data, a.count_offset)\n")
		b.pf("        stride = B.u32(data, a.stride_offset)\n")
		b.pf("        rows = count * stride\n\n")
		b.pf("        cond do\n")
		b.pf("          stride != a.stride -> {:halt, :refuse}\n")
		b.pf("          # past the DECLARED MAXIMUM: a consumer that sized anything by\n")
		b.pf("          # the maximum would overflow on a count the maximum does not bound\n")
		b.pf("          count > a.max -> {:halt, :refuse}\n")
		b.pf("          offset_of < %d -> {:halt, :refuse}\n", proj.Size)
		b.pf("          rem(offset_of, B.align()) != 0 -> {:halt, :refuse}\n")
		b.pf("          offset_of > bytes -> {:halt, :refuse}\n")
		b.pf("          rows > bytes - offset_of -> {:halt, :refuse}\n")
		b.pf("          true -> {:cont, max(used, offset_of + rows)}\n")
		b.pf("        end\n")
		b.pf("      end)\n\n")
		b.pf("    if used == :refuse, do: :refuse, else: block_used_%s(data, bytes, used)\n", lo)
	}
	b.pf("  end\n\n")

	b.pf("  # the used extent, rounded to 64 out of the slack that is LEFT rather than\n")
	b.pf("  # added and compared after\n")
	b.pf("  defp block_used_%s(data, bytes, used) do\n", lo)
	b.pf("    padding = rem(B.align() - rem(used, B.align()), B.align())\n\n")
	b.pf("    if padding > bytes - used do\n")
	b.pf("      :refuse\n")
	b.pf("    else\n")
	b.pf("      {:ok, %%{name: %q, base: data, bytes: used + padding}}\n", bl.Table.Name)
	b.pf("    end\n")
	b.pf("  end\n\n")
}

func (b *blockGen) emitBlockArrays(bl *ir.BlockLayout) {
	lo := ir.RustSnake(bl.Table.Name)
	if len(bl.Arrays) > 0 {
		b.pf("  # the out-of-line arrays of %s, in declaration order: the TRIPLE's own\n", bl.Table.Name)
		b.pf("  # positions in the projection, this build's pitch and the declared\n")
		b.pf("  # maximum beside them.\n")
		b.pf("  @%s_block_arrays [\n", lo)
		for _, a := range bl.Arrays {
			b.pf("    %%{name: %q, offset_of_offset: %d, count_offset: %d, stride_offset: %d, stride: %d, max: %d},\n",
				a.Field.Name, a.OffsetOfOffset, a.CountOffset, a.StrideOffset, a.Stride, a.Max)
		}
		b.pf("  ]\n")
		b.pf("  def %s_block_arrays, do: @%s_block_arrays\n\n", lo, lo)
	}
}

func (b *blockGen) emitBlockRows(bl *ir.BlockLayout) {
	lo := ir.RustSnake(bl.Table.Name)
	for _, a := range bl.Arrays {
		b.pf("  # %s's rows, at the pitch the INSTANCE gives. The pitch IS the element's\n", a.Field.Name)
		b.pf("  # size rounded to its alignment — derived, always (§2.7) — so the rows are\n")
		b.pf("  # contiguous and each is a sub-binary over the block, copied by nothing.\n")
		b.pf("  def %s_%s(block) do\n", lo, a.Field.Name)
		b.pf("    data = block.base\n")
		b.pf("    B.rows(data, B.u64(data, %d), B.u32(data, %d), B.u32(data, %d))\n",
			a.OffsetOfOffset, a.CountOffset, a.StrideOffset)
		b.pf("  end\n\n")
	}
}

// emitBlockInfo is the block's own descriptor: the PROJECTION as data
// (docs/SPEC-TABLES.md §8, §19.2). It is what retires a hand-kept mirror — a
// consumer holding it reads the triples out of an instance and points at rows,
// with no hand-written record per table.
func (b *blockGen) emitBlockInfo(bl *ir.BlockLayout) {
	name := bl.Table.Name
	lo := ir.RustSnake(name)
	b.pf("  # %s's block descriptor (docs/SPEC-TABLES.md §8, §19.2): a module\n", name)
	b.pf("  # attribute, so a read of it copies no term — describing the PROJECTION.\n")
	b.pf("  @block_info_%s %%{\n", lo)
	b.pf("    name: %q,\n", name)
	b.pf("    build_version: %s.BuildVersion.build_version(),\n", b.ns)
	b.pf("    size: %d,\n", bl.Projection.Size)
	b.pf("    align: %d,\n", bl.Projection.Align)
	b.pf("    fields: [\n")
	for _, fl := range bl.Projection.Fields {
		b.pf("      %s,\n", b.blockFieldRow(bl, fl, true))
	}
	b.pf("    ]\n")
	b.pf("  }\n")
	b.pf("  def block_info_%s, do: @block_info_%s\n\n", lo, lo)
}

// emitRecordInfos emits one descriptor per RECORD the block reaches — the row
// types, and anything they nest by value — plus the typed row accessors beside
// them, which is the fast path a per-frame job uses (§19.2).
func (b *blockGen) emitRecordInfos(tables []*ir.BlockLayout) {
	for _, name := range b.recordsOf(tables) {
		st := b.unit.Tables[name]
		if st == nil {
			st = b.unit.Structs[name]
		}
		if st == nil {
			continue
		}
		layout := ir.RecordLayout(b.unit, st)
		lo := ir.RustSnake(name)
		b.pf("  # %s as a block RECORD: the row an out-of-line array holds, or a\n", name)
		b.pf("  # record one nests by value (docs/SPEC-TABLES.md §19.2).\n")
		b.pf("  @block_record_%s %%{\n", lo)
		b.pf("    name: %q,\n", name)
		b.pf("    build_version: %s.BuildVersion.build_version(),\n", b.ns)
		b.pf("    size: %d,\n", layout.Size)
		b.pf("    align: %d,\n", layout.Align)
		b.pf("    fields: [\n")
		for _, fl := range layout.Fields {
			b.pf("      %s,\n", b.blockFieldRow(nil, fl, false))
		}
		b.pf("    ]\n")
		b.pf("  }\n")
		b.pf("  def block_record_%s, do: @block_record_%s\n\n", lo, lo)

		b.pf("  # the TYPED fast path beside the descriptors: one accessor per field,\n")
		b.pf("  # reading it at its own offset out of the row's sub-binary. Both come\n")
		b.pf("  # from one declaration and a consumer picks by what it is doing —\n")
		b.pf("  # reflection to walk anything, these to read one thing fast (§19.2).\n")
		for _, fl := range layout.Fields {
			b.emitRowAccessor(name, fl)
		}
		b.pf("\n")
	}
}

func (b *blockGen) emitRowAccessor(record string, fl ir.FieldLayout) {
	f := fl.Field
	lo := ir.RustSnake(record)
	pieces := ir.BlockFieldPieceOffsets(b.unit, f, fl.Offset, false)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		used := fl.Offset
		if len(pieces) > 1 {
			used = pieces[1].Offset
		}
		b.pf("  def %s_row_%s(row), do: B.slice(row, %d, max(B.i32(row, %d), 0))\n", lo, f.Name, fl.Offset, used)
	case f.KeyEnum != "" || f.Array != ir.ArrayNone:
		size := elementSizeOf(b.unit, f)
		count := f.ArrayBound
		if f.Array == ir.ArrayCounted {
			// a counted array in a RECORD keeps every slot, with the used
			// count beside it (§7.2)
			b.pf("  def %s_row_%s_count(row), do: B.i32(row, %d)\n", lo, f.Name, pieces[len(pieces)-1].Offset)
		}
		b.pf("  def %s_row_%s(row) do\n", lo, f.Name)
		b.pf("    Enum.map(0..%d//1, fn i -> %s end)\n", count-1, b.readAt(f, fmt.Sprintf("%d + i * %d", fl.Offset, size), size))
		b.pf("  end\n")
	default:
		size := elementSizeOf(b.unit, f)
		if f.Type.Optional {
			b.pf("  def %s_row_%s_present?(row), do: B.uint(row, %d, 1) != 0\n", lo, f.Name, pieces[len(pieces)-1].Offset)
		}
		b.pf("  def %s_row_%s(row), do: %s\n", lo, f.Name, b.readAt(f, fmt.Sprint(fl.Offset), size))
	}
}

// readAt renders the expression that reads ONE slot of a field at `at`.
func (b *blockGen) readAt(f *ir.Field, at string, size int64) string {
	if isStruct(f) {
		// a nested record is a SUB-BINARY, which the record's own accessors read
		return fmt.Sprintf("B.slice(row, %s, %d)", at, size)
	}
	switch f.Type.Kind {
	case ir.TBool:
		return fmt.Sprintf("B.uint(row, %s, 1) != 0", at)
	case ir.TFloat32:
		return fmt.Sprintf("B.f32(row, %s)", at)
	case ir.TFloat64:
		return fmt.Sprintf("B.f64(row, %s)", at)
	}
	if f.Type.Kind == ir.TInt && f.Type.Signed {
		return fmt.Sprintf("B.int(row, %s, %d)", at, size)
	}
	return fmt.Sprintf("B.uint(row, %s, %d)", at, size)
}

// blockFieldRow renders one field descriptor of a projection or a record.
func (b *blockGen) blockFieldRow(bl *ir.BlockLayout, fl ir.FieldLayout, projection bool) string {
	f := fl.Field
	outOfLine := projection && ir.BlockOutOfLine(f)
	offsetOfOffset, countOffset, strideOffset, stride := "nil", "nil", "nil", int64(0)
	presentOffset := "nil"
	element := "nil"
	isArray, counted := false, false
	arrayBound, elemSize := int64(0), fl.Size

	pieces := ir.BlockFieldPieceOffsets(b.unit, f, fl.Offset, projection)
	switch {
	case outOfLine:
		a := bl.ArrayByName(f.Name)
		offsetOfOffset = fmt.Sprint(a.OffsetOfOffset)
		countOffset = fmt.Sprint(a.CountOffset)
		strideOffset = fmt.Sprint(a.StrideOffset)
		stride = a.Stride
		element = b.recordRef(a.ElemName)
		isArray, counted, arrayBound, elemSize = true, true, f.ArrayBound, 0
	case f.Type.Kind == ir.TString:
		counted, arrayBound, elemSize = true, f.Type.Size, 1
		if len(pieces) > 1 {
			countOffset = fmt.Sprint(pieces[1].Offset)
		}
	case f.Type.Kind == ir.TBytes:
		isArray, counted, arrayBound, elemSize = true, true, f.Type.Size, 1
		if len(pieces) > 1 {
			countOffset = fmt.Sprint(pieces[1].Offset)
		}
	default:
		if f.KeyEnum != "" || f.Array != ir.ArrayNone {
			isArray, arrayBound = true, f.ArrayBound
			elemSize = elementSizeOf(b.unit, f)
		}
		if f.Type.Kind == ir.TNamed {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				element = b.recordRef(ref.Name)
			}
		}
	}
	if f.Type.Optional {
		presentOffset = fmt.Sprint(pieces[len(pieces)-1].Offset)
		elemSize = elementSizeOf(b.unit, f)
	}
	if !isArray && !counted && !f.Type.Optional {
		elemSize = elementSizeOf(b.unit, f)
	}

	kind := ir.TableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = ir.TableElemKind(f)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "%%{\n")
	fmt.Fprintf(&out, "        name: %q,\n", f.Name)
	fmt.Fprintf(&out, "        offset: %d,\n", fl.Offset)
	fmt.Fprintf(&out, "        size: %d,\n", fl.Size)
	fmt.Fprintf(&out, "        kind: %d,\n", kind)
	fmt.Fprintf(&out, "        out_of_line: %v,\n", outOfLine)
	fmt.Fprintf(&out, "        offset_of_offset: %s,\n", offsetOfOffset)
	fmt.Fprintf(&out, "        count_offset: %s,\n", countOffset)
	fmt.Fprintf(&out, "        stride_offset: %s,\n", strideOffset)
	fmt.Fprintf(&out, "        stride: %d,\n", stride)
	fmt.Fprintf(&out, "        is_array: %v,\n", isArray)
	fmt.Fprintf(&out, "        counted: %v,\n", counted)
	fmt.Fprintf(&out, "        optional: %v,\n", f.Type.Optional)
	fmt.Fprintf(&out, "        array_bound: %d,\n", arrayBound)
	fmt.Fprintf(&out, "        elem_size: %d,\n", elemSize)
	fmt.Fprintf(&out, "        present_offset: %s,\n", presentOffset)
	fmt.Fprintf(&out, "        element: %s\n", element)
	out.WriteString("      }")
	return out.String()
}

// recordRef is the {module, function} pair a descriptor DESCENDS through: an
// out-of-line array's rows and a nested record's fields are both reached
// through this one column.
func (b *blockGen) recordRef(name string) string {
	home := b.unit.DeclFile[name]
	if home == "" {
		home = b.base
	}
	return fmt.Sprintf("{%s.%sBlock, :block_record_%s}", b.ns, moduleBase(home), ir.RustSnake(name))
}

// recordsOf is every record the file's block forms reach, sorted: each
// out-of-line array's element, each by-value nesting, and everything those
// reach transitively — restricted to the records this FILE declares, so a unit
// with several files defines each once.
func (b *blockGen) recordsOf(tables []*ir.BlockLayout) []string {
	seen := map[string]bool{}
	var walk func(st *ir.Struct)
	walk = func(st *ir.Struct) {
		if st == nil || seen[st.Name] {
			return
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				walk(ref)
			}
		}
	}
	for _, bl := range tables {
		for _, a := range bl.Arrays {
			walk(a.Elem)
		}
		for _, f := range bl.Table.Fields {
			if ir.BlockOutOfLine(f) || f.Type.Kind != ir.TNamed {
				continue
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				walk(ref)
			}
		}
	}
	// every record the unit's blocks reach, filed under its DECLARING file
	var out []string
	for name := range seen {
		if b.unit.DeclFile[name] == b.base {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// elementSizeOf is ONE slot's size: an array element's, or the field's own when
// it holds one value. It is the layout model's answer, never a BEAM term's —
// the descriptors describe the bytes a producer wrote.
func elementSizeOf(u *ir.Unit, f *ir.Field) int64 {
	switch f.Type.Kind {
	case ir.TBool:
		return 1
	case ir.TFloat32:
		return 4
	case ir.TFloat64:
		return 8
	case ir.TInt:
		return int64(f.Type.Width) / 8
	case ir.TBits:
		if f.Type.Width <= 32 {
			return 4
		}
		return 8
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			return int64(ir.StorageBitsFor(ref.Max)) / 8
		case *ir.Flags:
			return 8
		case *ir.Struct:
			return ir.RecordLayout(u, ref).Size
		}
	}
	return 1
}
