// The BLOCK FORM for Elixir (docs/SPEC-TABLES.md §19): the read side and the
// build side. A block is POINTED AT and, on this leg, also BUILT: block_begin
// writes the prologue and the triples (docs/SPEC-TABLES.md §19.1) into a
// zeroed extent the caller fills. The bytes are the Go builder's and the
// pinned golden's.
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
// THE READING AND BUILDING TIER. A BEAM binary has no address, so the caller
// states `lead` beside the bytes when opening (emitBlockOpen) and the builder
// returns a fresh zeroed binary rather than a placed allocation. The layout
// is still §19.1's: prologue, triples, rows at a 64-byte pitch.
package elixirtable

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// BlockRuntimeModule is the block form's shared runtime, emitted once per unit
// that has a block-form table. It is its own module because a VARIABLE unit
// gets no table runtime at all (§11) and still has both accelerators.
const BlockRuntimeModule = "BlockRuntime"

func generateBlocks(u *ir.Unit, ns string, blocks *ir.BlockUnit) (map[string][]byte, error) {
	if len(blocks.Tables) == 0 {
		return nil, nil
	}
	out := map[string][]byte{}
	out[BlockRuntimeModule+".ex"] = []byte(header(BlockRuntimeModule, u.Package, "the BLOCK FORM's shared runtime (docs/SPEC-TABLES.md §19)") +
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

		var text strings.Builder
		text.WriteString(header(base, u.Package, "the BLOCK FORM (docs/SPEC-TABLES.md §19)"))
		text.WriteString("\n")
		// a block's last section ends in a blank line, and none stands
		// before the module's end
		text.WriteString(strings.TrimRight(b.body.String(), "\n") + "\n")
		text.WriteString("end\n")
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

  READING AND BUILDING. block_begin writes the prologue and the triples into
  a zeroed extent (docs/SPEC-TABLES.md §19.1). The caller fills rows at the
  offsets the triples name. A row the builder returns is a SUB-BINARY over
  that extent.
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

  # a 64-bit word composed from two 32-bit halves, so no bignum is formed on
  # the hot path before the bound that refuses it runs: a high half of
  # 0x08000000 or more would compose to 2^59 or above, which is not a BEAM
  # immediate.
  def u64halves(data, at) do
    lo = u32(data, at)
    hi = u32(data, at + 4)
    if hi >= 0x08000000, do: nil, else: hi * 0x100000000 + lo
  end

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
    if is_integer(at) and is_integer(size) and at >= 0 and size >= 0 and
         at + size <= byte_size(data) do
      binary_part(data, at, size)
    else
      nil
    end
  end

  # An array is ITERATED, not indexed by hand: the accessor yields each row
  # where it lies, at the pitch the INSTANCE gives, for count rows. A call site
  # never spells the pitch arithmetic itself (§19.2).
  #
  # THE WALK IS LAZY (docs/PORTING.md M7): a Stream over the range builds no
  # list, so reaching the array costs the cursor and reaching row i costs the
  # row — never a cons cell per row. A caller that wants one row by index uses
  # the count/at pair the block accessors expose beside this.
  def rows(data, offset_of, count, stride) do
    Stream.map(0..(count - 1)//1, fn i -> slice(data, offset_of + i * stride, stride) end)
  end

  # ONE ROW at the pitch the INSTANCE gives, bounds-checked against the count
  # the instance carries: the read half of the count/at pair, and what makes
  # reaching a row cost the row rather than the whole array.
  def row(data, offset_of, count, stride, index) do
    if index >= 0 and index < count do
      slice(data, offset_of + index * stride, stride)
    else
      nil
    end
  end

  # block_begin's shared half (docs/SPEC-TABLES.md §19.1). Counts are checked
  # before any byte of the extent changes. The prologue and the triples are
  # written; rows and padding stay the zeros the extent was allocated with.
  # A count past the declared maximum, or a negative count, names the array
  # and writes nothing.
  def begin(counts, arrays, projection, build) when is_map(counts) and is_integer(projection) do
    case Enum.reduce_while(arrays, :ok, fn a, _ ->
           c = count_of(counts, a.name)

           if c < 0 or c > a.max do
             {:halt, {:error, %{array: a.name, count: c, maximum: a.max}}}
           else
             {:cont, :ok}
           end
         end) do
      {:error, _} = err -> err
      :ok -> lay(counts, arrays, projection, build)
    end
  end

  def begin(_, _, _, _), do: false

  defp count_of(counts, name) do
    cond do
      Map.has_key?(counts, name) -> counts[name]
      Map.has_key?(counts, String.to_atom(name)) -> counts[String.to_atom(name)]
      true -> 0
    end
  end

  defp lay(counts, arrays, projection, build) do
    {used, placed} =
      Enum.reduce(arrays, {projection, []}, fn a, {offset, acc} ->
        c = count_of(counts, a.name)
        start = align_up(offset, a.start_align)
        {start + c * a.stride, [{a, start, c} | acc]}
      end)

    bytes = align_up(used, align())
    data = :binary.copy(<<0>>, bytes)
    data = put64(data, 0, magic())
    data = put64(data, 8, build)
    data = put64(data, 16, byte_order())

    data =
      Enum.reduce(placed, data, fn {a, start, c}, data ->
        data
        |> put64(a.offset_of_offset, start)
        |> put32(a.count_offset, c)
        |> put32(a.stride_offset, a.stride)
      end)

    {:ok, data}
  end

  defp align_up(v, a), do: div(v + a - 1, a) * a

  def put64(data, at, v), do: put(data, at, <<v::little-unsigned-64>>)
  def put32(data, at, v), do: put(data, at, <<v::little-unsigned-32>>)

  defp put(data, at, bytes) do
    n = byte_size(bytes)
    binary_part(data, 0, at) <> bytes <> binary_part(data, at + n, byte_size(data) - at - n)
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
	b.pf("  def %s_block_max_bytes, do: %s\n", lo, exInt(bl.MaxBytes))
	b.pf("  def %s_block_projection_bytes, do: %s\n\n", lo, exInt(proj.Size))

	for _, a := range bl.Arrays {
		b.pf("  # %s: the constants this build asserts against. A consumer INDEXES with\n", a.Field.Name)
		b.pf("  # what it read from the instance, never with these (§19.2).\n")
		b.pf("  def %s_%s_stride, do: %s\n", lo, a.Field.Name, exInt(a.Stride))
		b.pf("  def %s_%s_max, do: %s\n", lo, a.Field.Name, exInt(a.Max))
		b.pf("  def %s_%s_projection_offset, do: %s\n\n", lo, a.Field.Name, exInt(a.TripleOffset))
	}

	b.emitBlockArrays(bl)
	b.emitBlockOpen(bl)
	b.emitBlockBuild(bl)
	b.emitBlockRows(bl)
	b.emitBlockInfo(bl)
}

// emitBlockBuild is the producer half (docs/SPEC-TABLES.md §19.1): one function
// per table, block_begin_<name>, which checks counts before it writes and then
// writes only the prologue and the triples into a zeroed extent.
func (b *blockGen) emitBlockBuild(bl *ir.BlockLayout) {
	lo := ir.RustSnake(bl.Table.Name)
	arrays := lo + "_block_arrays()"
	if len(bl.Arrays) == 0 {
		arrays = "[]"
	}
	b.pf("  # block_begin_%s builds one block (docs/SPEC-TABLES.md §19.1).\n", lo)
	b.pf("  # Measure is the count check; a failing count writes nothing. The\n")
	b.pf("  # returned binary is the used extent, 64-aligned, prologue and triples\n")
	b.pf("  # written, rows left zero for the caller to fill.\n")
	b.pf("  def block_begin_%s(counts) do\n", lo)
	b.pf("    B.begin(counts, %s, %s, @build_version)\n", arrays, exInt(bl.Projection.Size))
	b.pf("  end\n\n")
}

// blockStartAlign is where one out-of-line array begins: aligned to
// max(64, alignof(element)) (docs/SPEC-TABLES.md §19.1).
func blockStartAlign(a ir.BlockArray) int64 {
	if a.ElemAlign() > int64(ir.BlockAlign) {
		return a.ElemAlign()
	}
	return int64(ir.BlockAlign)
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
	b.pf("  # EVERY OTHER NUMBER COMES FROM THE INSTANCE, and every 64-bit word the\n")
	b.pf("  # read side INDEXES WITH is composed from two 32-bit halves (B.u64halves),\n")
	b.pf("  # so no bignum is formed before the bound that refuses it runs: a forged\n")
	b.pf("  # offset_of or delta whose high half is 0x08000000 or more (2^59, past a\n")
	b.pf("  # BEAM immediate) is refused from the high half alone. A legitimate 64-bit\n")
	b.pf("  # FIELD value of 2^59 or more is a bignum on the BEAM whatever a reader\n")
	b.pf("  # does; the header's magic, build and byte-order words are compared against\n")
	b.pf("  # constants and are out of the composed path.\n")
	b.pf("  def block_open_%s(data), do: block_open_%s(data, 0)\n\n", lo, lo)
	b.pf("  def block_open_%s(data, lead) when is_binary(data) and is_integer(lead) do\n", lo)
	b.pf("    bytes = byte_size(data)\n\n")
	// One `with` and no nested if/case/if: credo's default
	// Credo.Check.Refactor.Nesting reports a body nested past depth 2
	// (issue #547), and the three refusals read in order as its clauses.
	b.pf("    # the prologue's size and the base's alignment, then the magic, the\n")
	b.pf("    # build and the byte order: a byte-swapped magic is a FOREIGN BYTE\n")
	b.pf("    # ORDER, and anything else is not a block at all. All refuse.\n")
	b.pf("    with true <- bytes >= %s and rem(lead, B.align()) == 0,\n", exInt(proj.Size))
	b.pf("         <<magic::little-unsigned-64, build::little-unsigned-64, order::little-unsigned-64,\n")
	b.pf("           _::binary>> <- data,\n")
	b.pf("         true <- magic == B.magic() and build == @build_version and order == B.byte_order() do\n")
	b.pf("      block_extent_%s(data, bytes)\n", lo)
	b.pf("    else\n")
	b.pf("      _ -> :error\n")
	b.pf("    end\n")
	b.pf("  end\n\n")
	b.pf("  def block_open_%s(_data, _lead), do: :error\n\n", lo)

	b.pf("  defp block_extent_%s(data, bytes) do\n", lo)
	if len(bl.Arrays) == 0 {
		b.pf("    # this table declares no out-of-line array, so the prologue and the\n")
		b.pf("    # projection's own extent are the whole check\n")
		b.pf("    block_used_%s(data, bytes, %s)\n", lo, exInt(proj.Size))
	} else {
		b.pf("    used =\n")
		b.pf("      Enum.reduce_while(%s_block_arrays(), %s, fn a, used ->\n", lo, exInt(proj.Size))
		b.pf("        offset_of = B.u64halves(data, a.offset_of_offset)\n")
		b.pf("        count = B.u32(data, a.count_offset)\n")
		b.pf("        stride = B.u32(data, a.stride_offset)\n")
		b.pf("        rows = count * stride\n\n")
		b.pf("        cond do\n")
		b.pf("          # a high half of 0x08000000 or more would compose to a non-immediate\n")
		b.pf("          # bignum, so the refusal is decided here, before any 64-bit integer is\n")
		b.pf("          # formed, and no real image is that large\n")
		b.pf("          offset_of == nil -> {:halt, :error}\n")
		b.pf("          stride != a.stride -> {:halt, :error}\n")
		b.pf("          # past the DECLARED MAXIMUM: a consumer that sized anything by\n")
		b.pf("          # the maximum would overflow on a count the maximum does not bound\n")
		b.pf("          count > a.max -> {:halt, :error}\n")
		b.pf("          offset_of < %s -> {:halt, :error}\n", exInt(proj.Size))
		b.pf("          rem(offset_of, B.align()) != 0 -> {:halt, :error}\n")
		b.pf("          offset_of > bytes -> {:halt, :error}\n")
		b.pf("          rows > bytes - offset_of -> {:halt, :error}\n")
		b.pf("          true -> {:cont, max(used, offset_of + rows)}\n")
		b.pf("        end\n")
		b.pf("      end)\n\n")
		b.pf("    if used == :error, do: :error, else: block_used_%s(data, bytes, used)\n", lo)
	}
	b.pf("  end\n\n")

	b.pf("  # the used extent, rounded to 64 out of the slack that is LEFT rather than\n")
	b.pf("  # added and compared after\n")
	b.pf("  defp block_used_%s(data, bytes, used) do\n", lo)
	b.pf("    padding = rem(B.align() - rem(used, B.align()), B.align())\n\n")
	b.pf("    if padding > bytes - used do\n")
	b.pf("      :error\n")
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
		for i, a := range bl.Arrays {
			comma := exComma(i, len(bl.Arrays))
			pairs := []string{
				fmt.Sprintf("name: %q", a.Field.Name),
				"offset_of_offset: " + exInt(a.OffsetOfOffset),
				"count_offset: " + exInt(a.CountOffset),
				"stride_offset: " + exInt(a.StrideOffset),
				"stride: " + exInt(a.Stride),
				"max: " + exInt(a.Max),
				"start_align: " + exInt(blockStartAlign(a)),
			}
			// one line when it fits the formatter's limit, one pair a line when not
			if one := "    %{" + strings.Join(pairs, ", ") + "}" + comma; exWidth(one) <= exLineLimit {
				b.pf("%s\n", one)
			} else {
				b.pf("    %%{\n      %s\n    }%s\n", strings.Join(pairs, ",\n      "), comma)
			}
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
		b.pf("  #\n")
		b.pf("  # THE ALLOCATION-FREE PAIR (docs/PORTING.md M7): the row count and ONE row\n")
		b.pf("  # by index, both read from the INSTANCE, so reaching row i costs the row\n")
		b.pf("  # and not the array. %s/1 beside them is the lazy convenience — a\n", a.Field.Name)
		b.pf("  # Stream, so a walk builds no list.\n")
		b.pf("  def %s_%s_count(block) do\n", lo, a.Field.Name)
		b.pf("    data = block.base\n")
		b.pf("    B.u32(data, %s)\n", exInt(a.CountOffset))
		b.pf("  end\n\n")
		b.pf("  def %s_%s_at(block, index) do\n", lo, a.Field.Name)
		b.pf("    data = block.base\n")
		b.pf("    B.row(data, B.u64halves(data, %s), B.u32(data, %s), B.u32(data, %s), index)\n",
			exInt(a.OffsetOfOffset), exInt(a.CountOffset), exInt(a.StrideOffset))
		b.pf("  end\n\n")
		b.pf("  def %s_%s(block) do\n", lo, a.Field.Name)
		b.pf("    data = block.base\n")
		b.pf("    B.rows(data, B.u64halves(data, %s), B.u32(data, %s), B.u32(data, %s))\n",
			exInt(a.OffsetOfOffset), exInt(a.CountOffset), exInt(a.StrideOffset))
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
	b.pf("    size: %s,\n", exInt(bl.Projection.Size))
	b.pf("    align: %d,\n", bl.Projection.Align)
	b.pf("    fields: [\n")
	for i, fl := range bl.Projection.Fields {
		b.pf("      %s%s\n", b.blockFieldRow(bl, fl, true), exComma(i, len(bl.Projection.Fields)))
	}
	b.pf("    ]\n")
	b.pf("  }\n")
	b.pf("  def block_info_%s, do: @block_info_%s\n\n", lo, lo)
}

// emitRecordInfos emits one descriptor per RECORD the block reaches — the row
// types, and anything they nest by value — plus the typed row accessors beside
// them, which is the fast path a per-frame job uses (§19.2).
func (b *blockGen) emitRecordInfos(tables []*ir.BlockLayout) {
	records := b.recordsOf(tables)
	for ri, name := range records {
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
		b.pf("    size: %s,\n", exInt(layout.Size))
		b.pf("    align: %d,\n", layout.Align)
		b.pf("    fields: [\n")
		for i, fl := range layout.Fields {
			b.pf("      %s%s\n", b.blockFieldRow(nil, fl, false), exComma(i, len(layout.Fields)))
		}
		b.pf("    ]\n")
		b.pf("  }\n")
		b.pf("  def block_record_%s, do: @block_record_%s\n\n", lo, lo)

		b.pf("  # the TYPED fast path beside the descriptors: one accessor per field,\n")
		b.pf("  # reading it at its own offset out of the row's sub-binary. Both come\n")
		b.pf("  # from one declaration and a consumer picks by what it is doing —\n")
		b.pf("  # reflection to walk anything, these to read one thing fast (§19.2).\n")
		var items []string
		for _, fl := range layout.Fields {
			items = append(items, b.rowAccessors(name, fl)...)
		}
		b.pf("%s", exBlock(items))
		if ri < len(records)-1 {
			b.pf("\n")
		}
	}
}

// rowAccessors renders one field's typed row accessors, one expression each
// for exBlock to separate the formatter's way.
func (b *blockGen) rowAccessors(record string, fl ir.FieldLayout) []string {
	var out []string
	b.rowAccessor(record, fl, func(format string, args ...any) {
		out = append(out, fmt.Sprintf(format, args...))
	})
	return out
}

func (b *blockGen) rowAccessor(record string, fl ir.FieldLayout, emit func(string, ...any)) {
	f := fl.Field
	lo := ir.RustSnake(record)
	pieces := ir.BlockFieldPieceOffsets(b.unit, f, fl.Offset, false)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		used := fl.Offset
		if len(pieces) > 1 {
			used = pieces[1].Offset
		}
		emit("%s", exDefDo("  ", fmt.Sprintf("def %s_row_%s(row)", lo, f.Name),
			fmt.Sprintf("B.slice(row, %s, max(B.i32(row, %s), 0))", exInt(fl.Offset), exInt(used))))
	case f.KeyEnum != "" || f.Array != ir.ArrayNone:
		size := elementSizeOf(b.unit, f)
		count := f.ArrayBound
		if f.Array == ir.ArrayCounted {
			// a counted array in a RECORD keeps every slot, with the used
			// count beside it (§7.2)
			emit("%s", exDefDo("  ", fmt.Sprintf("def %s_row_%s_count(row)", lo, f.Name),
				fmt.Sprintf("B.i32(row, %s)", exInt(pieces[len(pieces)-1].Offset))))
		}
		emit("  def %s_row_%s(row) do\n    Enum.map(0..%s//1, fn i -> %s end)\n  end\n", lo, f.Name, exInt(count-1),
			b.readAt(f, fmt.Sprintf("%s + %s", exInt(fl.Offset), exTimes("i", int64(size))), size))
	default:
		size := elementSizeOf(b.unit, f)
		if f.Type.Optional {
			emit("%s", exDefDo("  ", fmt.Sprintf("def %s_row_%s_present?(row)", lo, f.Name),
				fmt.Sprintf("B.uint(row, %s, 1) != 0", exInt(pieces[len(pieces)-1].Offset))))
		}
		emit("%s", exDefDo("  ", fmt.Sprintf("def %s_row_%s(row)", lo, f.Name), b.readAt(f, fmt.Sprint(fl.Offset), size)))
	}
}

// readAt renders the expression that reads ONE slot of a field at `at`.
func (b *blockGen) readAt(f *ir.Field, at string, size int64) string {
	// a bare offset is spelled the formatter's way
	if n, err := strconv.ParseInt(at, 10, 64); err == nil {
		at = exInt(n)
	}
	if isStruct(f) {
		// a nested record is a SUB-BINARY, which the record's own accessors read
		return fmt.Sprintf("B.slice(row, %s, %s)", at, exInt(size))
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
		offsetOfOffset = exInt(a.OffsetOfOffset)
		countOffset = exInt(a.CountOffset)
		strideOffset = exInt(a.StrideOffset)
		stride = a.Stride
		element = b.recordRef(a.ElemName)
		isArray, counted, arrayBound, elemSize = true, true, f.ArrayBound, 0
	case f.Type.Kind == ir.TString:
		counted, arrayBound, elemSize = true, f.Type.Size, 1
		if len(pieces) > 1 {
			countOffset = exInt(pieces[1].Offset)
		}
	case f.Type.Kind == ir.TBytes:
		isArray, counted, arrayBound, elemSize = true, true, f.Type.Size, 1
		if len(pieces) > 1 {
			countOffset = exInt(pieces[1].Offset)
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
		presentOffset = exInt(pieces[len(pieces)-1].Offset)
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
	fmt.Fprintf(&out, "        offset: %s,\n", exInt(fl.Offset))
	fmt.Fprintf(&out, "        size: %s,\n", exInt(fl.Size))
	fmt.Fprintf(&out, "        kind: %d,\n", kind)
	fmt.Fprintf(&out, "        out_of_line: %v,\n", outOfLine)
	fmt.Fprintf(&out, "        offset_of_offset: %s,\n", offsetOfOffset)
	fmt.Fprintf(&out, "        count_offset: %s,\n", countOffset)
	fmt.Fprintf(&out, "        stride_offset: %s,\n", strideOffset)
	fmt.Fprintf(&out, "        stride: %s,\n", exInt(stride))
	fmt.Fprintf(&out, "        is_array: %v,\n", isArray)
	fmt.Fprintf(&out, "        counted: %v,\n", counted)
	fmt.Fprintf(&out, "        optional: %v,\n", f.Type.Optional)
	fmt.Fprintf(&out, "        array_bound: %s,\n", exInt(arrayBound))
	fmt.Fprintf(&out, "        elem_size: %s,\n", exInt(elemSize))
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
