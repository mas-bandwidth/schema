package elixirtable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TableRuntimeModule is the shared module a unit with tables grows for the
// id-table wire's framing, beside the block and cook runtimes (docs/SPEC-
// TABLES.md §3). It is a file basename as well as a module name, so it is
// claimed the way every other generated spelling is (docs/SPEC-TABLES.md §11).
const TableRuntimeModule = "TableRuntime"

// tableRuntimeModule emits <Package>.TableRuntime: the primitives the wire's
// codecs call, emitted ONCE per unit rather than once per file. It carries no
// codec for any table — a codec is the per-file emitter's, this is the framing
// all of them share.
func tableRuntimeModule(u *ir.Unit, ns string) []byte {
	var b strings.Builder
	b.WriteString(header(TableRuntimeModule, u.Package,
		"the id-table wire's shared runtime (docs/SPEC-TABLES.md §3, §5)"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "defmodule %s.TableRuntime do\n", ns)
	b.WriteString(tableRuntimeSource)
	b.WriteString("end\n")
	return []byte(b.String())
}

// tableRuntimeSource is the module body. It is INDENTED for the module it lands
// in and carries no banner: `mix format --check-formatted` is the leg's gate,
// so the shapes here are the formatter's own.
const tableRuntimeSource = `  @moduledoc """
  The id-table wire's shared runtime (docs/SPEC-TABLES.md §3, §5): the form
  byte, the canonical LEB128 every length, count, index and reference rides as,
  the sixty-four-bit identity, and the id table a reader locates from the END.

  The file is a FORM BYTE, then a body of ` + "`id reference, kind, payload`" + ` runs
  terminated by the zero reference, then the id table — every id the body used,
  once each, in FIRST-USE order over the whole wire — and its entry count as the
  final eight bytes, a fixed little-endian u64. Nothing here is a codec for one
  table: a codec calls these primitives, and the primitives are emitted once.
  """

  import Bitwise

  # ---- the form byte (docs/SPEC-TABLES.md §3) ----
  #
  # Form 1 is the VARIABLE form, and it is read FIRST. A byte this reader does
  # not know is a REFUSAL by name, never framing damage.
  @form 1

  def form, do: @form

  def known_form?(@form), do: true
  def known_form?(_form), do: false

  # ---- canonical LEB128 (docs/SPEC-TABLES.md §3) ----
  #
  # Every length, count, index and id reference is an unsigned LEB128 written
  # with the FEWEST bytes that carry it, and a non-minimal spelling is
  # malformed. The encoder is canonical BY CONSTRUCTION; the decoder refuses the
  # trailing zero group, which is the shape a longer spelling of a shorter value
  # always ends in, and refuses a tenth byte that overflows the word.
  def leb_encode(n) when is_integer(n) and n >= 0, do: do_leb_encode(n, [])

  defp do_leb_encode(n, acc) when n < 0x80 do
    :erlang.list_to_binary(Enum.reverse([n | acc]))
  end

  defp do_leb_encode(n, acc) do
    do_leb_encode(n >>> 7, [((n &&& 0x7F) ||| 0x80) | acc])
  end

  def leb_size(n) when is_integer(n) and n >= 0, do: do_leb_size(n, 1)

  defp do_leb_size(n, size) when n < 0x80, do: size
  defp do_leb_size(n, size), do: do_leb_size(n >>> 7, size + 1)

  # {value, rest} on a canonical spelling and :malformed otherwise. Ten bytes is
  # the ceiling for a sixty-four-bit value, so a continuation past the tenth
  # byte, or a tenth byte above one, is overflow and not a number.
  def leb_decode(bytes), do: do_leb_decode(bytes, 0, 0)

  defp do_leb_decode(<<b, rest::binary>>, shift, acc) when shift < 64 do
    value = acc ||| ((b &&& 0x7F) <<< shift)

    cond do
      shift == 63 and b > 0x01 ->
        :malformed

      (b &&& 0x80) != 0 ->
        do_leb_decode(rest, shift + 7, value)

      b == 0 and shift > 0 ->
        # a trailing zero group: the value was spelled with more bytes than it
        # needs, which the wire refuses rather than reading a second way
        :malformed

      true ->
        {value, rest}
    end
  end

  defp do_leb_decode(_binary, _shift, _acc), do: :malformed

  # ---- the sixty-four-bit identity (docs/SPEC-TABLES.md §5) ----
  #
  # A field's id, an enum variant's, a union arm's and a table's own name id
  # are all fnv1a64(name), with NO fold and NO rebound. It rides in the id table
  # and a body names it by reference.
  @fnv_offset 0xCBF29CE484222325
  @fnv_prime 0x100000001B3
  @u64_mask 0xFFFFFFFFFFFFFFFF

  def fnv1a64(name) when is_binary(name), do: do_fnv1a64(name, @fnv_offset)

  defp do_fnv1a64(<<>>, hash), do: hash

  defp do_fnv1a64(<<b, rest::binary>>, hash) do
    do_fnv1a64(rest, ((hash ^^^ b) * @fnv_prime) &&& @u64_mask)
  end

  # ---- the id table, located from the END (docs/SPEC-TABLES.md §3) ----
  #
  # It is the last thing in the file: the FINAL EIGHT BYTES are the entry count,
  # a fixed little-endian u64, and that many little-endian u64 entries precede
  # it. id_table/1 answers them in wire order, which is first-use order.
  def id_table_count(file) when byte_size(file) >= 8 do
    n = byte_size(file) - 8
    <<_::binary-size(n), count::little-unsigned-64>> = file
    count
  end

  def id_table_count(_file), do: :malformed

  def id_table(file) when byte_size(file) >= 8 do
    count = id_table_count(file)
    entries = count * 8
    start = byte_size(file) - 8 - entries

    if start < 0 do
      :malformed
    else
      <<_::binary-size(start), table::binary-size(entries), _::binary-size(8)>> = file

      for <<id::little-unsigned-64 <- table>>, do: id
    end
  end

  def id_table(_file), do: :malformed
`
