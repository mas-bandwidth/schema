package elixirtable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedRuntimeModule is the FIXED FORM's shared runtime, emitted ONCE PER UNIT
// beside the two accelerators' runtimes and for the same reason they are their
// own modules: a unit that carries no fixed root gets none of it.
const FixedRuntimeModule = "FixedRuntime"

// fixedRuntimeModule is the fixed form's shared Elixir runtime
// (docs/SPEC-TABLES.md §3.4): the sixty-four-bit hash, the LAYOUT's own
// validation rule by named rule, the PLAN COMPILER, THE ONE READ LOOP, and the
// per-peer plan cache.
//
// THE READER'S OWN STORAGE IN THIS LANGUAGE IS THE RECORD IMAGE, and that is
// this port's one real departure from the reference. C++ lands a plan entry
// straight into a struct at an `offsetof`; Elixir has no such thing — a
// %Struct{} is a map and its fields are TERMS, not bytes — so the plan's
// DESTINATION is a binary holding THIS BUILD's declared order at THIS BUILD's
// own widths. That image IS the wire's layout, so:
//
//   - the identity plan's source and destination are the same number, and
//     §3.4's coalescing rule takes a record of plain scalars to a SINGLE run;
//   - a record whose plan is that one run needs no image built at all — the
//     body sub-binary IS the image, which on the BEAM is a reference and not a
//     copy;
//   - a generated straight-line decode then projects the image into the
//     language's own terms in ONE BINARY PATTERN MATCH, which is the step C++
//     gets free because there a struct IS its bytes.
//
// NOTHING IS MUTATED, because nothing on the BEAM can be. Where the reference
// and the JavaScript port write into a byte buffer at each entry's destination,
// this one COLLECTS the writes and assembles the image once, splicing the
// PREFILL's bytes into every gap the plan left — which is the same answer to
// "absent field" by a different mechanism, and it is one pass over one list.
func fixedRuntimeModule(u *ir.Unit, ns string) []byte {
	var b strings.Builder
	b.WriteString(header(FixedRuntimeModule, u.Package, "the FIXED FORM's shared runtime (docs/SPEC-TABLES.md §3.4)"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "defmodule %s.FixedRuntime do\n", ns)
	b.WriteString(fixedRuntimeBody)
	b.WriteString("end\n")
	return []byte(b.String())
}

const fixedRuntimeBody = `  @moduledoc """
  The FIXED FORM's shared runtime (docs/SPEC-TABLES.md §3.4), form byte 3,
  emitted once per unit.

  A fixed-table record is an EIGHT-BYTE HASH of the writer's LAYOUT and then
  the values in declared order, every field at its declared storage width,
  nothing padded between fields. Everything else the reader needs is in the
  layout, and the layout is sent once per carrier.

  THE ONE READER PATH IS A PLAN. A plan is a flat list of entries and a read is
  one loop over it: the IDENTITY PLAN when the record's hash is this build's
  own, a plan compiled once from the writer's own layout and cached by hash
  otherwise. The same loop runs either way and the only thing that differs is
  which plan it was handed — the owner's ruling, which §3.4 quotes him on.

  THE PLAN IS DATA. On the BEAM there is no caller-owned array to declare a
  capacity in, so §3.4's "the plan's storage is the caller's" is honoured the
  way this runtime can honour it: a compiled plan is a TERM, a caller may hold
  one and hand it back through ` + "`" + `plan:` + "`" + `, and the entry capacity is a bound this
  runtime refuses past by name (` + "`" + `:plan_too_large` + "`" + `).

  A COMPILED PLAN IS CACHED BY LAYOUT HASH, AND THE CACHE IS BOUNDED. A layout
  arrives from a PEER, so an unbounded cache is a peer's lever on this node's
  memory and on every process in the VM — see ` + "`" + `cache/3` + "`" + ` and
  ` + "`" + `plan_cache_max/0` + "`" + `. A service reading files from untrusted peers should hand
  its own plan in through ` + "`" + `plan:` + "`" + ` and never mint one from a peer's layout.

  ## What a loaded value HOLDS

  A read is a REFERENCE and not a copy wherever it can be, which is where this
  form's speed comes from — and on the BEAM a reference into a binary keeps the
  WHOLE binary alive. A text field lifted out of a big file is a small term
  holding a big one, so a caller that keeps a handful of fields and drops the
  file has not dropped the file. Pass ` + "`" + `copy: true` + "`" + ` to ` + "`" + `<root>_fixed_load/2` + "`" + `
  to detach each record from the file as it is read; ` + "`" + `detach/2` + "`" + ` says exactly
  what that buys and what it does not.

  ## The options ` + "`" + `<root>_fixed_load/2` + "`" + ` takes

    * ` + "`" + `:plan` + "`" + ` — a plan this caller owns, used instead of the cache.
    * ` + "`" + `:plan_capacity` + "`" + ` — this caller's entry bound; past it,
      ` + "`" + `:plan_too_large` + "`" + `.
    * ` + "`" + `:copy` + "`" + ` — detach each record body from the file (above). Default false.
  """

  import Bitwise

  # ---------------------------------------------------------------------------
  # THE WIRE'S OWN CONSTANTS
  # ---------------------------------------------------------------------------

  # form byte 3 is the FIXED FORM. Bytes 1 and 2 do not move.
  @form 3

  # AN ENTRY IS SEVENTEEN BYTES AND THE LAYOUT IS A COUNT AND A RUN OF THEM,
  # forever. There is no version byte inside a layout: the FORM BYTE versions
  # everything behind it, the layout's own format included.
  @entry_bytes 17
  @header_bytes 4

  # THE FILE HEADER, ONE RULE FOR ALL FIVE FORMS (docs/SPEC-TABLES.md §3): the
  # form byte, seven reserved zero bytes, the form's eight-byte hash at 8, and
  # the body at 16, so a memory-mapped body keeps its alignment. The fixed form
  # does not need the alignment and pads anyway, so its bytes do not move a
  # second time when the cook and the block form join the registry.
  @file_header_bytes 16
  @file_hash_at 8

  # §3.4's RECORD CEILING, which a reader holds an UNTRUSTED PEER's layout to
  # for the same reason the compiler holds a declaration to it: a record past it
  # is one this build will not decode.
  @record_max 65536

  # A BOUND ON THE WALK, not on the wire: the validation below is recursive, so
  # a hostile layout of three thousand entries each claiming one child would
  # otherwise spend a reader's stack before any rule fired. Nothing in §3.4
  # fixes the number; a reader states its own.
  @max_depth 64

  # THE PLAN'S DEFAULT CAPACITY. A layout whose compiled plan does not fit is a
  # refusal BY NAME and never a growth (§3.3's rule for a resolved vocabulary,
  # holding here unchanged).
  @plan_capacity 4096

  def form, do: @form
  def entry_bytes, do: @entry_bytes
  def record_max, do: @record_max
  def plan_capacity, do: @plan_capacity

  # ---------------------------------------------------------------------------
  # THE FILE HEADER, WRITTEN AND READ IN ONE PLACE
  # ---------------------------------------------------------------------------
  #
  # It is a pair of functions in the shared runtime and not a shape spelled at
  # each end, because a header written in one place and matched in another is a
  # header two edits can disagree about — and this one has moved once already.

  @doc """
  The twenty bytes in front of a form-3 FILE: the form byte, seven reserved
  zeros, the LAYOUT HASH at offset 8, and the layout's length at 16.
  """
  def file_header(hash, layout_bytes) do
    <<@form, 0::size(@file_hash_at - 1)-unit(8), hash::little-unsigned-64,
      layout_bytes::little-unsigned-32>>
  end

  def file_header_bytes, do: @file_header_bytes + 4

  @doc """
  Split a form-3 FILE into its header hash, its layout and its records.

  THE FORM BYTE IS READ FIRST, AND IT SAYS WHICH DIRECTION. The registry is
  ordered, so a byte this reader does not carry is named by where it sits
  relative to this form and never by one word for both: ` + "`" + `:previous_form` + "`" + ` for
  the VARIABLE form, which is older, ` + "`" + `:message_form_as_file` + "`" + ` for a batch
  handed to a file reader, and ` + "`" + `:newer_form` + "`" + ` only for a byte no form
  defines.
  """
  def read_file_header(data)

  def read_file_header(
        <<@form, _reserved::binary-size(@file_hash_at - 1), hash::little-unsigned-64,
          len::little-unsigned-32, rest::binary>>
      )
      when byte_size(rest) >= len do
    <<layout::binary-size(^len), records::binary>> = rest
    {:ok, hash, layout, records}
  end

  # STEP 1 BEFORE STEP 2 (§5.3): a file with no header in it at all is the
  # RESIDUE — malformed, with no name — and the FORM BYTE is read only once there
  # are bytes enough to carry one.
  def read_file_header(data) when is_binary(data) and byte_size(data) < @file_header_bytes + 4,
    do: {:error, :malformed}

  def read_file_header(<<1, _::binary>>), do: {:error, :previous_form}
  def read_file_header(<<2, _::binary>>), do: {:error, :message_form_as_file}
  def read_file_header(<<@form, _::binary>>), do: {:error, :layout_malformed}
  def read_file_header(<<_form, _::binary>>), do: {:error, :newer_form}
  def read_file_header(_), do: {:error, :layout_malformed}

  # ---------------------------------------------------------------------------
  # THE REPORT: §4's SIX COUNTERS, and the refusals beside them
  # ---------------------------------------------------------------------------

  @doc """
  A fresh read report: §4's six counters and nothing set.

  A REFUSAL IS NOT ONE OF THE SIX. ` + "`" + `:newer_form` + "`" + `, ` + "`" + `:no_layout` + "`" + `, the seven
  named layout rules and ` + "`" + `:plan_too_large` + "`" + ` each say nothing was decoded, so
  none of them moves a counter and none of them fires ` + "`" + `malformed` + "`" + `.
  """
  def report do
    %{
      malformed: false,
      unknown: 0,
      kind_mismatch: 0,
      clamped: 0,
      widened: 0,
      duplicate: 0,
      layout_hash: 0
    }
  end

  defp bump(report, key), do: Map.update!(report, key, &(&1 + 1))

  @doc """
  Add ` + "`" + `n` + "`" + ` ` + "`" + `clamped` + "`" + ` to a report at once, and the same report back when there were
  none — which is what a record whose every ranged value was already in range
  hands back, so the common read allocates no new map at all.

  THE COUNTING IS THE GENERATED DECODE'S, for every plan (docs/SPEC-TABLES.md
  §3.4). Neither plan clamps a ranged integer: a bound is held by the
  projection that runs after the loop, the same pass for either plan. The
  IDENTITY plan is one copy run; a compiled plan copies a same-size ranged
  integer the same way. This is where the projection's number joins §4's six.
  """
  def clamped(report, 0), do: report
  def clamped(report, n), do: Map.update!(report, :clamped, &(&1 + n))

  @doc """
  Set ` + "`" + `malformed` + "`" + ` when the projection's damage flag rode true, and the same
  report back when it did not — so a clean read allocates no new map.
  """
  @doc """
  The PLAN'S OWN CENSUS onto the report, ONCE, after the record loop (§5.9 #6).

  unknown and kind_mismatch were fixed when the plan was built — §5.4 says once
  per peer and never per record — so they ride in the lane and land here, on a
  read that RETURNS and on no other. On a LAWFUL lineage they are both zero,
  always (§5.9 #30): a newer reader names every field of every writer its lineage
  can select, so the census is the report of an UNLAWFUL or HOSTILE entry only.
  """
  def census(report, 0, 0), do: report

  def census(report, unknown, kind_mismatch) do
    %{
      report
      | unknown: report.unknown + unknown,
        kind_mismatch: report.kind_mismatch + kind_mismatch
    }
  end

  def damaged(report, false), do: report
  def damaged(report, true), do: %{report | malformed: true}

  @doc """
  ONE VALUE AGAINST ONE PAIR OF BOUNDS: ` + "`" + `acc` + "`" + ` and one more when the value lies
  outside them, ` + "`" + `acc` + "`" + ` unchanged when it does not.

  The accumulator leads so that a generated counting pass is a column of
  ` + "`" + `c = R.clamps(c, ...)` + "`" + ` and nothing else — one shape the emitter has to know
  the formatter's mind about, rather than a ` + "`" + `+` + "`" + ` chain that breaks differently at
  every width.
  """
  def clamps(acc, v, lo, hi) when v < lo or v > hi, do: acc + 1
  def clamps(acc, _v, _lo, _hi), do: acc

  @doc """
  The count moved by an ORDINAL past the last variant the reader declares: an
  enum value, or a union tag, that names nothing lands None and counts one.
  """
  def past(acc, v, variants) when v > variants, do: acc + 1
  def past(acc, _v, _variants), do: acc

  @doc """
  ` + "`" + `fun.(element, acc)` + "`" + ` over every element in order, the binary threaded through:
  the one loop a writer appends its arrays with, so every element lands on
  the same binary in place and no list of small binaries is built to flatten.
  """
  def each([], acc, _fun), do: acc
  def each([e | rest], acc, fun), do: each(rest, fun.(e, acc), fun)

  @doc """
  The bytes of slack behind a text value, out to its declared bound, or
  ArgumentError when the value is past it — the writer's contract, which this
  port raises on for the packet port's own reason.
  """
  def text_slack(bytes, n) when is_binary(bytes) and byte_size(bytes) <= n,
    do: n - byte_size(bytes)

  def text_slack(bytes, n) when is_binary(bytes) do
    raise ArgumentError, "value is #{byte_size(bytes)} bytes, past the declared bound of #{n}"
  end

  # ---------------------------------------------------------------------------
  # THE HASH: fnv1a64 over the layout's bytes, exactly as written
  # ---------------------------------------------------------------------------

  @doc """
  fnv1a64 over a layout's bytes exactly as written — the eight bytes every
  record carries. It is a WIRE IDENTITY and not a security claim.
  """
  def hash(bytes) when is_binary(bytes), do: fnv(bytes, 0xCBF29CE484222325)

  defp fnv(<<>>, h), do: h

  defp fnv(<<b, rest::binary>>, h) do
    fnv(rest, bxor(h, b) * 0x100000001B3 &&& 0xFFFFFFFFFFFFFFFF)
  end

  # ---------------------------------------------------------------------------
  # FLOATS ARE BIT-TRANSPARENT, the serialize.elixir convention
  # ---------------------------------------------------------------------------
  #
  # A non-finite IEEE-754 pattern is one no BEAM float term can hold, so it
  # reads back as {:nonfinite, bits} and writes from the same form — exactly as
  # the packet codec does, because a fixed record and a packet body carry the
  # same value in the same language.

  def f32_value(bits) do
    if (bits >>> 23 &&& 0xFF) != 0xFF do
      <<value::float-32-little>> = <<bits::little-32>>
      value
    else
      {:nonfinite, bits}
    end
  end

  def f32_bits(value) when is_float(value) do
    <<bits::little-32>> = <<value::float-32-little>>

    if (bits >>> 23 &&& 0xFF) == 0xFF do
      raise ArgumentError, "float overflows float32; write {:nonfinite, bits} instead"
    end

    bits
  end

  # AND THE OTHER HALF OF THE PACKET CODEC'S CONTRACT: a ` + "`" + `{:nonfinite, bits}` + "`" + `
  # carrying a FINITE pattern is a caller who tagged an ordinary number, and it
  # raises rather than riding. The tag is not a spelling of "a float" — it is
  # the ONLY way to spell a pattern no BEAM float term holds — so a finite
  # pattern under it means the value surface and the wire disagree about what
  # the field contains, and letting it ride would put a number on the wire that
  # reads back as a float the writer never wrote.
  def f32_bits({:nonfinite, bits}) when bits >= 0 and bits <= 0xFFFFFFFF do
    if (bits >>> 23 &&& 0xFF) != 0xFF do
      raise ArgumentError, "{:nonfinite, bits} carries a finite float32 pattern; write the float"
    end

    bits
  end

  def f64_value(bits) do
    if (bits >>> 52 &&& 0x7FF) != 0x7FF do
      <<value::float-64-little>> = <<bits::little-64>>
      value
    else
      {:nonfinite, bits}
    end
  end

  def f64_bits(value) when is_float(value) do
    <<bits::little-64>> = <<value::float-64-little>>
    bits
  end

  def f64_bits({:nonfinite, bits}) when bits >= 0 and bits <= 0xFFFFFFFFFFFFFFFF do
    if (bits >>> 52 &&& 0x7FF) != 0x7FF do
      raise ArgumentError, "{:nonfinite, bits} carries a finite float64 pattern; write the float"
    end

    bits
  end

  @doc """
  A ` + "`" + `string(N)` + "`" + `'s used bytes: well-formed UTF-8 with no zero among them, or
  the field's declared default and ` + "`" + `malformed` + "`" + `. An ABSENT optional does not
  run the rule — the payload is unspecified and nobody wrote it (§3.4).
  """
  def text(used, default, m), do: text(used, default, m, true)

  def text(used, default, m, present) do
    if present and not text_ok?(1, used), do: {default, true}, else: {used, m}
  end

  @doc """
  A ` + "`" + `wstring(N)` + "`" + `'s used units: paired UTF-16 with no zero unit, or the
  field's declared default and ` + "`" + `malformed` + "`" + `. The same present-flag rule.
  """
  def wtext(used, default, m), do: wtext(used, default, m, true)

  def wtext(used, default, m, present) do
    if present and not text_ok?(2, used), do: {default, true}, else: {used, m}
  end

  # ---------------------------------------------------------------------------
  # THE WRITE's ONE HELPER: a value's bytes onto the zeroed template
  # ---------------------------------------------------------------------------

  @doc """
  One record's body, DETACHED from the file it was read out of when ` + "`" + `copy` + "`" + `.

  THE READ HANDS BACK SUB-BINARIES, AND A SUB-BINARY KEEPS ITS WHOLE PARENT
  ALIVE. A record whose plan is ONE run covering the whole image needs no image
  built — the body IS the image, which is a reference and not a copy, and that
  is where this form's read speed comes from. The projection then binds each text field
  as a sub-binary of THAT, so an eight-byte name lifted out of an eighty-kilobyte
  file is an eight-byte term that holds eighty kilobytes off the collector for
  as long as anything keeps it. Hold a few strings out of a big file and the
  file never leaves memory.

  IT IS THE RIGHT DEFAULT ANYWAY: a caller that reads a file, uses the values
  and drops them all pays nothing for the sharing and saves the whole copy.
  ` + "`" + `copy: true` + "`" + ` is for the other shape — values kept while the file is not —
  and it copies ONE RECORD BODY per record, so what a field can retain falls
  from the whole file to that record. A caller keeping ONE small field out of a
  large record can go further itself with ` + "`" + `:binary.copy/1` + "`" + ` on the field; this
  runtime will not do that for it, because a copy per field is a cost every
  caller would pay for a problem few of them have.
  """
  def detach(body, false), do: body
  def detach(body, true), do: :binary.copy(body)

  @doc """
  ` + "`" + `value` + "`" + `, or ArgumentError when it is outside ` + "`" + `lo..hi` + "`" + `.

  THE WRITE-SIDE CONTRACT, AND WHY IT IS ALWAYS ON HERE. The C++ reference
  spends ` + "`" + `schema_assert` + "`" + ` on the same question and NDEBUG compiles it out,
  which is this project's rule for a write-side check: the caller's own bug is
  caught at the call site in a debug build and the READ side is what keeps the
  wire safe in every build. The BEAM has no compile-out assert, so this port
  does what its packet codec already does — an O(1) check that raises — and
  that is the debug-assert equivalent here rather than a second safety layer.

  IT IS NOT A CLAMP. A reader CLAMPS a value outside ITS OWN declared range and
  counts it (§3.4's op table), because the value came from a peer. A value
  handed to a writer came from this program, and silently bending it would hide
  the bug; worse, an unbounded integer past the field's STORAGE WIDTH is
  TRUNCATED by a BEAM binary construction without a word, which is a wrong value
  on the wire and no way to know.
  """
  def ranged(value, {lo, hi}) when is_integer(value) and value >= lo and value <= hi, do: value

  def ranged(value, {lo, hi}) do
    raise ArgumentError, "#{inspect(value)} is outside the declared range #{lo}..#{hi}"
  end

  @doc """
  ` + "`" + `value` + "`" + `, or ArgumentError when it does not fit ` + "`" + `bits` + "`" + ` of storage.

  THE SAME CONTRACT AT THE FIELD'S OWN WIDTH, for a field that declares no
  range: then the storage width IS the range. This is the half that cannot be
  left to the reader, because a value past it never reaches a reader as itself
  — a BEAM binary construction TRUNCATES it in silence, and the peer decodes a
  different number with no counter and no refusal to say so.
  """
  def fits(value, bits, false) when is_integer(value) and value >= 0 and value < 1 <<< bits do
    value
  end

  def fits(value, bits, true)
      when is_integer(value) and value >= -(1 <<< (bits - 1)) and value < 1 <<< (bits - 1) do
    value
  end

  def fits(value, bits, signed) do
    what = if signed, do: "int", else: "uint"
    raise ArgumentError, "#{inspect(value)} does not fit the field's #{what}#{bits} storage"
  end

  @doc """
  The length of ` + "`" + `list` + "`" + `, or ArgumentError when it is outside ` + "`" + `min..max` + "`" + `.

  A ` + "`" + `[Min..Max]T` + "`" + ` COUNT IS THE ONE NUMBER A COUNTED ARRAY PUTS ON THE WIRE,
  and both ends of its declared range are the writer's contract — the same one
  the packet codec raises on. ` + "`" + `pad/2` + "`" + ` would catch a run past the bound in
  BYTES a moment later; it would say nothing about a count below ` + "`" + `Min` + "`" + `, and
  its complaint would name a byte count rather than the array.
  """
  def count(list, min, max) do
    n = length(list)

    if n < min or n > max do
      raise ArgumentError, "an array of #{n} elements is outside the declared #{min}..#{max}"
    end

    n
  end

  # ---------------------------------------------------------------------------
  # THE LAYOUT'S OWN VALIDATION, RULE BY NAMED RULE
  # ---------------------------------------------------------------------------
  #
  # A LAYOUT ARRIVES FROM AN UNTRUSTED PEER and it is the one structure a reader
  # must parse before it knows anything at all, so every rule below runs BEFORE
  # a single record byte is touched and each refuses under its OWN NAME rather
  # than under one word for all of them.
  #
  # A KIND THIS BUILD DOES NOT KNOW IS A REFUSAL and not a leaf to step over: a
  # FIXED FORM HAS A CLOSED KIND SET, the form byte versions everything behind
  # it, kinds included, and a kind outside the set means a NEWER FORM BYTE — a
  # different form, which is not this reader's to guess at.
  #
  # NOR IS THERE A CYCLE RULE, because a pre-order walk cannot express a cycle:
  # an entry's children ARE the entries that follow it, so a child's index is
  # always higher than its parent's and there is no back reference for a cycle
  # to be made of.

  @doc """
  Parse and validate a layout. ` + "`" + `{:ok, layout}` + "`" + ` or ` + "`" + `{:error, reason}` + "`" + `, the
  reason BY NAME.

  A layout is refused WHOLE or not at all, and a refusal sets nothing on its
  way out.
  """
  def parse_layout(bytes) when is_binary(bytes) do
    with {:ok, count} <- layout_count(bytes),
         entries = layout_entries(bytes, count),
         {:ok, _} <- layout_root(elem(entries, 0)),
         {:ok, used} <- check_entry(entries, count, 0, 0),
         :ok <- layout_closes(used, count) do
      {:ok, %{count: count, entries: entries, subs: subtrees(entries, count)}}
    end
  end

  def parse_layout(_), do: {:error, :layout_malformed}

  # 1. THE ENTRY COUNT FITS THE LAYOUT'S LENGTH EXACTLY. The count is the first
  #    thing read and every other rule indexes off it: a count that overruns is
  #    a read past the buffer and a count that undershoots is bytes nobody
  #    accounts for.
  defp layout_count(bytes) do
    case bytes do
      <<count::little-unsigned-32, _::binary>>
      when count > 0 and byte_size(bytes) == @header_bytes + @entry_bytes * count ->
        {:ok, count}

      <<_::little-unsigned-32, _::binary>> ->
        {:error, :layout_count_mismatch}

      _ ->
        {:error, :layout_malformed}
    end
  end

  # 2. THE ROOT IS A TABLE, and its size is the record's body.
  defp layout_root({_id, 13, size, _children}) when size > 0 and size <= @record_max,
    do: {:ok, size}

  defp layout_root({_id, 13, _size, _children}), do: {:error, :layout_record_too_large}
  defp layout_root(_), do: {:error, :layout_kind_invalid}

  defp layout_closes(used, count) when used == count, do: :ok
  defp layout_closes(_, _), do: {:error, :layout_tree_unclosed}

  defp layout_entries(bytes, count) do
    <<_::binary-size(@header_bytes), rest::binary>> = bytes
    List.to_tuple(entry_list(rest, count, []))
  end

  defp entry_list(_, 0, acc), do: :lists.reverse(acc)

  defp entry_list(
         <<id::little-unsigned-64, kind, size::little-unsigned-32, children::little-unsigned-32,
           rest::binary>>,
         n,
         acc
       ) do
    entry_list(rest, n - 1, [{id, kind, size, children} | acc])
  end

  # HOW MANY ENTRIES THE SUBTREE ROOTED AT i OCCUPIES, so a walk steps over a
  # child it does not want without knowing what is in it. THIS IS WHAT MAKES
  # SKIPPING FREE: an unknown field, and an unknown NESTED TYPE, are stepped
  # over by the size their entry states, and skipping is not an act.
  #
  # It is computed BOTTOM-UP after validation, in one pass, because a child's
  # index is always higher than its parent's.
  defp subtrees(entries, count) do
    List.to_tuple(subtree_acc(entries, count - 1, []))
  end

  defp subtree_acc(_entries, i, acc) when i < 0, do: acc

  defp subtree_acc(entries, i, acc) do
    {_, _, _, children} = elem(entries, i)
    subtree_acc(entries, i - 1, [1 + subtree_span(acc, children, 0) | acc])
  end

  # acc holds the subtree sizes of i+1, i+2, ... in order, so the k children of
  # i are found by stepping through it.
  defp subtree_span(_acc, 0, n), do: n

  defp subtree_span(acc, children, n) do
    case Enum.drop(acc, n) do
      [sub | _] -> subtree_span(acc, children - 1, n + sub)
      [] -> n
    end
  end

  def sub(layout, i), do: elem(layout.subs, i)
  def at(layout, i), do: elem(layout.entries, i)
  def kind_at(layout, i), do: elem(elem(layout.entries, i), 1)
  def size_at(layout, i), do: elem(elem(layout.entries, i), 2)
  def children_at(layout, i), do: elem(elem(layout.entries, i), 3)
  def id_at(layout, i), do: elem(elem(layout.entries, i), 0)

  # THE CLOSED KIND SET (docs/SPEC-TABLES.md §3, §3.4): §3's own kinds 1..30,
  # the no-payload variant 32, wstring 33, and the ONE kind this form's layout
  # adds, the optional wrapper 35. 31 is §3's body framing escape and 34 is
  # reserved: neither is a kind a declaration spells.
  defp known_kind?(kind) when kind >= 1 and kind <= 30, do: true
  defp known_kind?(kind), do: kind == 32 or kind == 33 or kind == 35

  # A LEAF KIND'S ADMITTED SIZES. Kind and size fix each other on this wire with
  # ONE exception, stated here rather than left to be found: a bits(N) field
  # rides at its DECLARED STORAGE WIDTH — four bytes for N <= 32 and eight above
  # — under the unsigned integer kind its BIT COUNT picks, so kinds 6 and 7
  # admit four as well as their own width.
  defp leaf_size(1, size), do: {true, size == 1}
  defp leaf_size(2, size), do: {true, size == 1}
  defp leaf_size(3, size), do: {true, size == 2}
  defp leaf_size(4, size), do: {true, size == 4}
  defp leaf_size(5, size), do: {true, size == 8}
  defp leaf_size(6, size), do: {true, size == 1 or size == 4}
  defp leaf_size(7, size), do: {true, size == 2 or size == 4}
  defp leaf_size(8, size), do: {true, size == 4}
  defp leaf_size(9, size), do: {true, size == 8}
  defp leaf_size(10, size), do: {true, size == 4}
  defp leaf_size(11, size), do: {true, size == 8}
  defp leaf_size(17, size), do: {true, size == 4}
  defp leaf_size(18, size), do: {true, size == 16}
  defp leaf_size(19, size), do: {true, size == 16}
  defp leaf_size(20, size), do: {true, size == 1}
  defp leaf_size(25, size), do: {true, size == 1}
  defp leaf_size(21, size), do: {true, size == 2}
  defp leaf_size(26, size), do: {true, size == 2}
  defp leaf_size(22, size), do: {true, size == 4}
  defp leaf_size(27, size), do: {true, size == 4}
  defp leaf_size(23, size), do: {true, size == 8}
  defp leaf_size(28, size), do: {true, size == 8}
  defp leaf_size(24, size), do: {true, size == 16}
  defp leaf_size(29, size), do: {true, size == 16}
  defp leaf_size(32, size), do: {true, size == 0}
  defp leaf_size(_, _), do: {false, true}

  defp ordinal_width?(n), do: n == 1 or n == 2 or n == 4 or n == 8

  # check_entry validates the subtree rooted at i and answers how many entries
  # it occupies — the same arithmetic subtrees/2 does, which is why that walk
  # is safe to run afterwards and only afterwards.
  defp check_entry(_entries, count, i, _depth) when i < 0 or i >= count,
    do: {:error, :layout_tree_unclosed}

  defp check_entry(_entries, _count, _i, depth) when depth > @max_depth,
    do: {:error, :layout_too_deep}

  defp check_entry(entries, count, i, depth) do
    {_id, kind, size, children} = elem(entries, i)

    cond do
      not known_kind?(kind) -> {:error, :layout_kind_unknown}
      size > @record_max -> {:error, :layout_record_too_large}
      true -> check_children(entries, count, i, depth, kind, size, children)
    end
  end

  defp check_children(entries, count, i, depth, kind, size, children) do
    case walk_children(entries, count, i + 1, depth, children, 0, %{
           sum: 0,
           widest: 0,
           first_size: 0,
           first_kind: 0,
           first_children: 0,
           second_size: 0,
           variants: true
         }) do
      {:error, why} ->
        {:error, why}

      {:ok, at, facts} ->
        case check_shape(kind, size, children, facts) do
          :ok -> {:ok, at - i}
          {:error, why} -> {:error, why}
        end
    end
  end

  defp walk_children(_entries, _count, at, _depth, 0, _k, facts), do: {:ok, at, facts}

  defp walk_children(entries, count, at, depth, children, k, facts) do
    case check_entry(entries, count, at, depth + 1) do
      {:error, why} ->
        {:error, why}

      {:ok, used} ->
        {_, ck, cs, cc} = elem(entries, at)
        sum = facts.sum + cs

        if sum > @record_max do
          {:error, :layout_record_too_large}
        else
          facts = %{
            facts
            | sum: sum,
              widest: max(facts.widest, cs),
              variants: facts.variants and ck == 32,
              first_size: if(k == 0, do: cs, else: facts.first_size),
              first_kind: if(k == 0, do: ck, else: facts.first_kind),
              first_children: if(k == 0, do: cc, else: facts.first_children),
              second_size: if(k == 1, do: cs, else: facts.second_size)
          }

          walk_children(entries, count, at + used, depth, children - 1, k + 1, facts)
        end
    end
  end

  # A CONSTANT SIZE MATCHES ITS KIND: the size is what every offset in the plan
  # is computed from, so a size its own kind does not admit is a lie the plan
  # would then be built on.
  defp check_shape(kind, size, children, facts) do
    case leaf_size(kind, size) do
      {true, false} -> {:error, :layout_size_mismatch}
      {true, true} when children != 0 -> {:error, :layout_kind_invalid}
      {true, true} -> :ok
      {false, _} -> check_composite(kind, size, children, facts)
    end
  end

  # a TABLE: its size is the SUM of its fields'
  defp check_composite(13, size, _children, facts) do
    if size == facts.sum, do: :ok, else: {:error, :layout_size_mismatch}
  end

  # the OPTIONAL WRAPPER: one child, one present byte in front of it
  defp check_composite(35, size, children, facts) do
    cond do
      children != 1 -> {:error, :layout_kind_invalid}
      size != facts.sum + 1 -> {:error, :layout_size_mismatch}
      true -> :ok
    end
  end

  # an ARRAY: a whole number of elements, behind a count or not
  defp check_composite(14, size, children, facts) do
    cond do
      children != 1 ->
        {:error, :layout_kind_invalid}

      facts.first_size == 0 ->
        {:error, :layout_size_mismatch}

      rem(size, facts.first_size) == 0 ->
        :ok

      size >= 4 and rem(size - 4, facts.first_size) == 0 ->
        :ok

      true ->
        {:error, :layout_size_mismatch}
    end
  end

  # an ENUM-KEYED array: the KEY ENUM then the ELEMENT, every slot written. AT
  # LEAST one slot per variant, not exactly one: an enum widened by an explicit
  # max has more slots than it has names, and the layout carries only the names.
  defp check_composite(16, size, children, facts) do
    cond do
      children != 2 -> {:error, :layout_kind_invalid}
      facts.first_kind != 30 -> {:error, :layout_kind_invalid}
      facts.second_size == 0 -> {:error, :layout_size_mismatch}
      rem(size, facts.second_size) != 0 -> {:error, :layout_size_mismatch}
      div(size, facts.second_size) < facts.first_children -> {:error, :layout_size_mismatch}
      true -> :ok
    end
  end

  # a UNION: the TAG at its own width, then the WIDEST ARM
  defp check_composite(15, size, children, facts) do
    cond do
      children == 0 -> {:error, :layout_kind_invalid}
      size <= facts.widest -> {:error, :layout_size_mismatch}
      not ordinal_width?(size - facts.widest) -> {:error, :layout_size_mismatch}
      true -> :ok
    end
  end

  # an ENUM: the ordinal's storage width, and its children are VARIANTS
  defp check_composite(30, size, children, facts) do
    cond do
      not ordinal_width?(size) -> {:error, :layout_size_mismatch}
      children != 0 and not facts.variants -> {:error, :layout_kind_invalid}
      true -> :ok
    end
  end

  # string(N): the length, then N bytes
  defp check_composite(12, size, children, _facts) do
    cond do
      children != 0 -> {:error, :layout_kind_invalid}
      size < 4 -> {:error, :layout_size_mismatch}
      true -> :ok
    end
  end

  # wstring(N): the length in CODE UNITS, then 2N bytes
  defp check_composite(33, size, children, _facts) do
    cond do
      children != 0 -> {:error, :layout_kind_invalid}
      size < 4 or rem(size - 4, 2) != 0 -> {:error, :layout_size_mismatch}
      true -> :ok
    end
  end

  # Every kind of the closed set is either a leaf above or a case here, so this
  # is unreachable — and it REFUSES rather than admits, because a kind that
  # reached it is a kind the two lists disagree about.
  defp check_composite(_kind, _size, _children, _facts), do: {:error, :layout_kind_unknown}

  # ---------------------------------------------------------------------------
  # THE PLAN COMPILER — once per peer, never once per record
  # ---------------------------------------------------------------------------
  #
  # The SAME LOOP runs over this plan as over the identity plan. What the
  # compiler does, ONCE PER PEER, is what a read would otherwise do once per
  # record: map the writer's ids onto this reader's fields; leave a field it
  # cannot name OUT of the plan, which is what skips it, by arithmetic that was
  # going to step past it anyway; and leave a field the writer does not carry
  # out too, which is what defaults it, because the prefill already put the
  # declared default there.
  #
  # A RENAMED FIELD NEEDS NO RULE. A ` + "`" + `was =` + "`" + ` field's id is the id of the name it
  # was (§5), so it matches on the id like any other and the rename is a fact
  # the compiler never learns.

  @doc """
  Compile a plan for ANOTHER writer's layout against my own.

  ` + "`" + `{:ok, entries, made, report}` + "`" + ` or ` + "`" + `{:error, :plan_too_large, report}` + "`" + `.
  ` + "`" + `mine` + "`" + ` is this build's own parsed layout and ` + "`" + `dst` + "`" + ` is MY SIDE of it, one
  row per entry; ` + "`" + `made` + "`" + ` is the entry count the capacity was measured against,
  BEFORE coalescing, which is the number a cache has to remember.
  """
  def compile(theirs, mine, dst, capacity, report) do
    {acc, report} = match_children(theirs, 0, 0, mine, 0, dst, 0, nil, 0, {[], 0}, report)
    {entries, n} = acc

    if n > capacity do
      {:error, :plan_too_large, report}
    else
      # THE ENTRY COUNT THE CAPACITY WAS MEASURED AGAINST RIDES BACK, so the
      # CACHE can be checked against a later caller's capacity by the SAME
      # NUMBER. Coalescing runs after it and can only shrink the list, so the
      # length of the returned plan is a SMALLER number — and a cached plan
      # admitted under that is one a caller compiling fresh would be refused.
      {:ok, coalesce(:lists.reverse(entries)), n, report}
    end
  end

  defp push({entries, n}, entry), do: {[entry | entries], n + 1}

  defp guarded(acc, nil, _tag, entry), do: push(acc, entry)
  defp guarded(acc, guard, tag, entry), do: push(acc, {:guard, guard, tag, entry})

  # COUNTER-MOVING OPS ADDED FOR ONE SLOT wrap so slack and an absent
  # optional's payload never count. A copy is unspecified on read and stays.
  defp wrap_new({entries, n}, old_n, fun) do
    added = n - old_n
    {new, rest} = Enum.split(entries, added)
    {Enum.map(new, fun) ++ rest, n}
  end

  defp live_wrap_entry({:copy, _, _, _} = e, _dst, _i), do: e
  defp live_wrap_entry(e, dst, i), do: {:live, dst, i, e}

  defp present_wrap_entry({:copy, _, _, _} = e, _src), do: e
  defp present_wrap_entry(e, src), do: {:present, src, e}

  # match_children walks a TABLE's children on both sides, BY ID.
  defp match_children(theirs, ti, their_at, mine, mi, dst, my_at, guard, tag, acc, report) do
    their_children = children_at(theirs, ti)
    my_children = children_at(mine, mi)

    {acc, report} =
      Enum.reduce(offsets(mine, mi, my_children), {acc, report}, fn mc, {acc, report} ->
        case find_id(theirs, ti, their_children, their_at, id_at(mine, mc)) do
          nil ->
            {acc, report}

          {tc, toff} ->
            compile_entry(theirs, tc, toff, mine, mc, dst, my_at, guard, tag, acc, report)
        end
      end)

    # EVERY FIELD OF THEIRS I COULD NOT NAME IS ONE ` + "`" + `unknown` + "`" + `, and the bytes it
    # occupies are stepped over because no entry ever names them.
    report =
      Enum.reduce(offsets(theirs, ti, their_children), report, fn tc, report ->
        case find_id(mine, mi, my_children, 0, id_at(theirs, tc)) do
          nil -> bump(report, :unknown)
          _ -> report
        end
      end)

    {acc, report}
  end

  # the child indices of an entry, in declared order
  defp offsets(layout, i, children), do: child_indices(layout, i + 1, children, [])

  defp child_indices(_layout, _at, 0, acc), do: :lists.reverse(acc)

  defp child_indices(layout, at, children, acc) do
    child_indices(layout, at + sub(layout, at), children - 1, [at | acc])
  end

  # the child of ` + "`" + `i` + "`" + ` carrying ` + "`" + `id` + "`" + `, and its byte offset in the parent's body
  defp find_id(layout, i, children, base, id), do: find_id_at(layout, i + 1, children, base, id)

  defp find_id_at(_layout, _at, 0, _off, _id), do: nil

  defp find_id_at(layout, at, children, off, id) do
    if id_at(layout, at) == id do
      {at, off}
    else
      find_id_at(layout, at + sub(layout, at), children - 1, off + size_at(layout, at), id)
    end
  end

  # §4'S WIDENING RUNGS, and the fixed form spends no rule of its own on them: a
  # kind that GREW since the writer decodes at the writer's width and lands
  # exactly, counting one ` + "`" + `widened` + "`" + `. Coming back DOWN the ladder, or across two
  # of them, is a kind that MOVED and is reported rather than reinterpreted.
  defp widens?(from, to) when from >= 6 and from <= 9 and to >= 6 and to <= 9, do: to > from
  defp widens?(from, to) when from >= 2 and from <= 5 and to >= 2 and to <= 5, do: to > from
  # THE FIXED-POINT RUNGS, the SIGNED run 20..24 and the UNSIGNED run 25..29
  # (docs/FIXED-FORM-ALGORITHM.md §1, §5.2 EMIT): a fixed-point whose I grew at
  # equal F is a widen like any other, INSIDE its family and upward only.
  defp widens?(from, to) when from >= 20 and from <= 24 and to >= 20 and to <= 24, do: to > from
  defp widens?(from, to) when from >= 25 and from <= 29 and to >= 25 and to <= 29, do: to > from
  defp widens?(from, to), do: from == 10 and to == 11

  # A LADDER WIDEN'S SIGN IS THE WRITER'S KIND'S (§5.2): the signed integers
  # 2..5 and the SIGNED fixed-point run 20..24 sign-extend and everything else
  # zero-extends. The SAME-KIND widen has no sign at all and is not this
  # function's question.
  defp signed_kind?(kind), do: (kind >= 2 and kind <= 5) or (kind >= 20 and kind <= 24)

  defp compile_entry(theirs, ti, their_at, mine, mi, dst, my_at, guard, tag, acc, report) do
    their_kind = kind_at(theirs, ti)
    my_kind = kind_at(mine, mi)
    row = elem(dst, mi)
    at = my_at + elem(row, 0)
    aux_at = my_at + elem(row, 2)

    cond do
      # T INTO ?T (bill §12.8): the reader wrapped a field in an OPTIONAL the
      # writer did not have. The present byte is a CONSTANT 1 — constant in its
      # VALUE and still carrying the row's own guard and ordinal (§5.9 #13), so
      # a present byte is never set for an arm the tag did not name — and then
      # the PAYLOAD is emitted under that same guard, against the wrapper's one
      # child rather than against the wrapper.
      my_kind == 35 and their_kind != 35 ->
        acc = guarded(acc, guard, tag, {:const, aux_at, 1, 1})

        compile_entry(
          theirs,
          ti,
          their_at,
          mine,
          mi + 1,
          dst,
          my_at,
          guard,
          tag,
          acc,
          report
        )

      their_kind != my_kind ->
        widen_entry(theirs, ti, their_at, mine, mi, at, guard, tag, acc, report)

      true ->
        same_kind(
          theirs,
          ti,
          their_at,
          mine,
          mi,
          dst,
          my_at,
          at,
          aux_at,
          row,
          guard,
          tag,
          acc,
          report
        )
    end
  end

  defp widen_entry(theirs, ti, their_at, mine, mi, at, guard, tag, acc, report) do
    their_kind = kind_at(theirs, ti)
    my_kind = kind_at(mine, mi)

    if widens?(their_kind, my_kind) do
      their_size = size_at(theirs, ti)
      my_size = size_at(mine, mi)

      entry =
        if their_kind == 10 do
          {:widenf, their_at, at}
        else
          {:widen, their_at, at, their_size, my_size, signed_kind?(their_kind)}
        end

      {guarded(acc, guard, tag, entry), report}
    else
      # A KIND THAT MOVED IS REPORTED AND NEVER REINTERPRETED (§4).
      {acc, bump(report, :kind_mismatch)}
    end
  end

  defp same_kind(
         theirs,
         ti,
         their_at,
         mine,
         mi,
         dst,
         my_at,
         at,
         aux_at,
         row,
         guard,
         tag,
         acc,
         report
       ) do
    case kind_at(mine, mi) do
      # the OPTIONAL wrapper: the present byte, then the payload WHOLE, present
      # or not. §2.3 makes an optional and a plain nesting wire-identical in
      # form 1; ON THIS FORM THEY ARE ONE BYTE APART, and the wrapper kind is
      # what lets a reader SEE the edit rather than have every byte after it
      # slide by one.
      35 ->
        acc = guarded(acc, guard, tag, {:copy, their_at, aux_at, 1})
        {_, old_n} = acc

        {acc, report} =
          compile_entry(
            theirs,
            ti + 1,
            their_at + 1,
            mine,
            mi + 1,
            dst,
            my_at,
            guard,
            tag,
            acc,
            report
          )

        {wrap_new(acc, old_n, &present_wrap_entry(&1, their_at)), report}

      # a nested TABLE: match its fields
      13 ->
        match_children(theirs, ti, their_at, mine, mi, dst, at, guard, tag, acc, report)

      # an ARRAY: the count, then min( their bound, my bound ) elements
      14 ->
        compile_array(
          theirs,
          ti,
          their_at,
          mine,
          mi,
          dst,
          at,
          aux_at,
          row,
          guard,
          tag,
          acc,
          report
        )

      # an ENUM-KEYED array: every slot, matched by the KEY's id
      16 ->
        compile_keyed(theirs, ti, their_at, mine, mi, dst, at, row, guard, tag, acc, report)

      # a UNION: the tag, remapped, then each arm matched by id
      15 ->
        compile_union(theirs, ti, their_at, mine, mi, dst, at, aux_at, guard, tag, acc, report)

      # an ENUM: the ordinal is the layout's POSITION, so it remaps
      30 ->
        compile_enum(theirs, ti, their_at, mine, mi, at, guard, tag, acc, report)

      k when k == 12 or k == 33 ->
        units = min(size_at(mine, mi) - 4, size_at(theirs, ti) - 4)
        entry = {:text, their_at, at, aux_at, units, elem(row, 4)}
        {guarded(acc, guard, tag, entry), report}

      _ ->
        # SAME-SIZE IS A COPY. A ranged integer's bound is held after the loop
        # by the generated projection, the same pass for either plan — there is
        # no clamp op (docs/SPEC-TABLES.md §3.4). A narrower source still widens.
        their_size = size_at(theirs, ti)
        my_size = size_at(mine, mi)

        cond do
          their_size == my_size ->
            {guarded(acc, guard, tag, {:copy, their_at, at, my_size}), report}

          their_size < my_size and my_size <= 8 ->
            entry = {:widen, their_at, at, their_size, my_size, false}
            {guarded(acc, guard, tag, entry), report}

          true ->
            {acc, bump(report, :kind_mismatch)}
        end
    end
  end

  defp compile_array(
         theirs,
         ti,
         their_at,
         mine,
         mi,
         dst,
         at,
         aux_at,
         row,
         guard,
         tag,
         acc,
         report
       ) do
    their_elem = size_at(theirs, ti + 1)
    my_elem = size_at(mine, mi + 1)
    head = if elem(row, 3) == 1, do: 4, else: 0
    their_n = if their_elem == 0, do: 0, else: div(size_at(theirs, ti) - head, their_elem)
    my_n = if my_elem == 0, do: 0, else: div(size_at(mine, mi) - head, my_elem)

    acc =
      if elem(row, 3) == 1 do
        guarded(acc, guard, tag, {:count, their_at, aux_at, my_n})
      else
        acc
      end

    their_base = their_at + head
    stride = elem(row, 1)
    counted = elem(row, 3) == 1
    n = min(their_n, my_n)

    Enum.reduce(0..(n - 1)//1, {acc, report}, fn i, {acc, report} ->
      {_, old_n} = acc

      {acc, report} =
        compile_entry(
          theirs,
          ti + 1,
          their_base + i * their_elem,
          mine,
          mi + 1,
          dst,
          at + i * stride,
          guard,
          tag,
          acc,
          report
        )

      acc =
        if counted do
          wrap_new(acc, old_n, &live_wrap_entry(&1, aux_at, i))
        else
          acc
        end

      {acc, report}
    end)
  end

  defp compile_keyed(theirs, ti, their_at, mine, mi, dst, at, row, guard, tag, acc, report) do
    their_keys = children_at(theirs, ti + 1)
    my_keys = children_at(mine, mi + 1)
    their_elem_at = ti + 1 + sub(theirs, ti + 1)
    my_elem_at = mi + 1 + sub(mine, mi + 1)
    their_elem = size_at(theirs, their_elem_at)
    stride = elem(row, 1)

    Enum.reduce(0..(my_keys - 1)//1, {acc, report}, fn k, {acc, report} ->
      case key_slot(theirs, ti, their_keys, id_at(mine, mi + 2 + k)) do
        nil ->
          {acc, report}

        j ->
          compile_entry(
            theirs,
            their_elem_at,
            their_at + j * their_elem,
            mine,
            my_elem_at,
            dst,
            at + k * stride,
            guard,
            tag,
            acc,
            report
          )
      end
    end)
  end

  defp key_slot(theirs, ti, their_keys, id) do
    Enum.find(0..(their_keys - 1)//1, fn j -> id_at(theirs, ti + 2 + j) == id end)
  end

  defp compile_union(theirs, ti, their_at, mine, mi, dst, at, aux_at, _guard, _tag, acc, report) do
    their_tag = tag_bytes(theirs, ti)
    my_tag = tag_bytes(mine, mi)
    their_arms = children_at(theirs, ti)
    my_arms = children_at(mine, mi)

    Enum.reduce(
      Enum.with_index(offsets(mine, mi, my_arms)),
      {acc, report},
      fn {my_arm, k}, {acc, report} ->
        case arm_index(theirs, ti, their_arms, id_at(mine, my_arm)) do
          nil ->
            {acc, report}

          {their_arm, j} ->
            # MY tag value, written under THEIR tag's guard
            acc = push(acc, {:guard, their_at, j + 1, {:const, aux_at, my_tag, k + 1}})

            compile_entry(
              theirs,
              their_arm,
              their_at + their_tag,
              mine,
              my_arm,
              dst,
              at,
              their_at,
              j + 1,
              acc,
              report
            )
        end
      end
    )
  end

  defp arm_index(theirs, ti, their_arms, id) do
    theirs
    |> offsets(ti, their_arms)
    |> Enum.with_index()
    |> Enum.find(fn {arm, _j} -> id_at(theirs, arm) == id end)
  end

  defp tag_bytes(layout, i) do
    size_at(layout, i) - union_arm_bytes(layout, i)
  end

  defp union_arm_bytes(layout, i) do
    layout
    |> offsets(i, children_at(layout, i))
    |> Enum.reduce(0, fn arm, widest -> max(widest, size_at(layout, arm)) end)
  end

  # A GROWN ORDINAL WIDTH IS A WIDEN, UNSIGNED (§5.2 EMIT kind 30, bill §12.7):
  # the writer's enum rode in one byte and this reader's rides in two, so the
  # ordinal decodes at the writer's width and lands exactly, counting one
  # widened. ITS SIGN IS ZERO and not the kind's (§5.2: the same-kind widen and
  # the enum widen both ZERO-extend).
  defp compile_enum(theirs, ti, their_at, mine, mi, at, guard, tag, acc, report)
       when is_integer(ti) do
    their_size = size_at(theirs, ti)
    my_size = size_at(mine, mi)

    if their_size < my_size do
      entry = {:widen, their_at, at, their_size, my_size, false}
      {guarded(acc, guard, tag, entry), report}
    else
      compile_enum_remap(theirs, ti, their_at, mine, mi, at, guard, tag, acc, report)
    end
  end

  defp compile_enum_remap(theirs, ti, their_at, mine, mi, at, guard, tag, acc, report) do
    their_variants = children_at(theirs, ti)
    my_variants = children_at(mine, mi)

    remap =
      Enum.map(0..(their_variants - 1)//1, fn j ->
        their_id = id_at(theirs, ti + 1 + j)

        case Enum.find(0..(my_variants - 1)//1, fn k -> id_at(mine, mi + 1 + k) == their_id end) do
          nil -> 0
          k -> k + 1
        end
      end)

    entry =
      {:ordinal, their_at, at, size_at(theirs, ti), size_at(mine, mi), List.to_tuple(remap)}

    {guarded(acc, guard, tag, entry), report}
  end

  # THE COALESCER, and it is the only optimization a plan compiler performs: two
  # neighbouring COPY entries whose source and destination both advance together
  # are one entry. It is performed IDENTICALLY on both sides — this is the same
  # function the emitter runs over the identity plan at generation time.
  def coalesce(entries), do: coalesce(entries, [])

  defp coalesce([], acc), do: :lists.reverse(acc)

  defp coalesce([{:copy, s, d, n} | rest], [{:copy, ps, pd, pn} | acc])
       when ps + pn == s and pd + pn == d do
    coalesce(rest, [{:copy, ps, pd, pn + n} | acc])
  end

  defp coalesce([e | rest], acc), do: coalesce(rest, [e | acc])

  # ---------------------------------------------------------------------------
  # THE ONE READ LOOP
  # ---------------------------------------------------------------------------
  #
  # A READ IS A PREFILL AND THIS LOOP, AND NOTHING ELSE. Which plan it is handed
  # is the only thing that differs between reading this build's own record and
  # reading anybody else's.
  #
  # THE PREFILL IS WHAT ANSWERS "ABSENT FIELD": a field this record does not
  # carry has NO PLAN ENTRY at all, so the assembler splices the prefill's bytes
  # over the gap and the loop never learns it existed. THE ABSENCE OF AN ENTRY
  # IS ALSO WHAT ANSWERS "UNKNOWN FIELD" — a field of the writer's this reader
  # cannot name is simply never a source.

  @doc """
  Run one plan over one record body and answer ` + "`" + `{image, report}` + "`" + `.

  The image is this build's own record image: the declared order at this
  build's own widths, which a generated decode then projects into terms in one
  binary pattern match.
  """
  def run(entries, body, prefill, report) do
    {writes, report} = step(entries, body, [], report)
    {assemble(writes, prefill), report}
  end

  defp step([], _body, writes, report), do: {writes, report}

  defp step([{:live, count_dst, index, inner} | rest], body, writes, report) do
    # A COUNTED ARRAY'S SLACK MOVES NO COUNTER (§3.4). The live count already
    # landed through the ` + "`" + `count` + "`" + ` op; slots at and past it are storage nobody
    # wrote.
    if index < live_at(writes, count_dst) do
      step([inner | rest], body, writes, report)
    else
      step(rest, body, writes, report)
    end
  end

  defp step([{:present, src, inner} | rest], body, writes, report) do
    # AN ABSENT OPTIONAL'S PAYLOAD IS IGNORED ON READ (§3.4), so it is not
    # held to a bound either.
    case body do
      <<_::binary-size(^src), p, _::binary>> when p != 0 ->
        step([inner | rest], body, writes, report)

      _ ->
        step(rest, body, writes, report)
    end
  end

  defp step([{:guard, gsrc, tag, inner} | rest], body, writes, report) do
    # AN ENTRY BELONGING TO AN ARM RUNS ONLY UNDER ITS OWN TAG.
    case body do
      <<_::binary-size(^gsrc), ^tag, _::binary>> -> step([inner | rest], body, writes, report)
      _ -> step(rest, body, writes, report)
    end
  end

  defp step([{:copy, src, dst, n} | rest], body, writes, report) do
    # A RUN OF BYTES MOVES AS A SUB-BINARY: the BEAM shares them rather than
    # copying, which is what makes the identity plan's single run free.
    case body do
      <<_::binary-size(^src), take::binary-size(^n), _::binary>> ->
        step(rest, body, [{dst, take} | writes], report)

      _ ->
        step(rest, body, writes, report)
    end
  end

  defp step([{:count, src, dst, max} | rest], body, writes, report) do
    case body do
      <<_::binary-size(^src), raw::little-signed-32, _::binary>> ->
        {v, report} = clamp_count(raw, max, report)
        step(rest, body, [{dst, <<v::little-signed-32>>} | writes], report)

      _ ->
        step(rest, body, writes, report)
    end
  end

  defp step([{:text, src, dst, aux, size, flavour} | rest], body, writes, report) do
    unit = if flavour == 2, do: 2, else: 1
    cap = div(size, unit)

    case body do
      <<_::binary-size(^src), raw::little-signed-32, take::binary-size(^size), _::binary>> ->
        {v, report} = clamp_count(raw, cap, report)

        # THE ONE CONTENT RULE THE WIRE HAS (§3, §4), over the USED UNITS and
        # over nothing else: the slack carries no meaning, and reading a whole
        # declared bound to check bytes that mean nothing is the cost this form
        # exists to avoid.
        if text_ok?(flavour, binary_part(take, 0, v * unit)) do
          step(rest, body, [{aux, take}, {dst, <<v::little-signed-32>>} | writes], report)
        else
          # A PAYLOAD THAT IS NOT THE TEXT ITS KIND SAYS IT IS is DAMAGE and not
          # data, and the verdict is the one every form reaches: THE FIELD READS
          # ITS DECLARED DEFAULT, one damage flag fires, and the rest of the
          # record stands. SPEC.md §4.7 refuses the whole read on the packet
          # wire because a packet has no position after a field that did not
          # decode; a FIXED RECORD IS POSITIONAL, so the damage is one field's.
          #
          # LANDING THE DEFAULT COSTS NOTHING HERE. A field with no write in the
          # plan's output is a GAP, and the assembler fills every gap from the
          # PREFILL — which is this type's declared defaults, the string's
          # length in front of its bytes. So the answer is the absence of an
          # answer, which is the same mechanism that answers "absent field".
          step(rest, body, writes, %{report | malformed: true})
        end

      _ ->
        step(rest, body, writes, report)
    end
  end

  defp step([{:ordinal, src, dst, size, width, remap} | rest], body, writes, report) do
    # A VARIANT ORDINAL IS ITS POSITION IN THE LAYOUT, so a writer whose enum
    # gained a variant IN THE MIDDLE is remapped here and never reinterpreted.
    case body do
      <<_::binary-size(^src), raw::little-unsigned-size(^size)-unit(8), _::binary>> ->
        {v, report} = ordinal(raw, remap, report)
        step(rest, body, [{dst, <<v::little-unsigned-size(width)-unit(8)>>} | writes], report)

      _ ->
        step(rest, body, writes, report)
    end
  end

  defp step([{:widen, src, dst, size, width, signed} | rest], body, writes, report) do
    # TWO'S COMPLEMENT WIDENS BY ITS SIGN BIT, which is the whole reason a widen
    # is an op and not a short copy. The bound, if any, is held after the loop.
    case leaf(body, src, size, signed) do
      nil ->
        step(rest, body, writes, report)

      raw ->
        step(
          rest,
          body,
          [{dst, image_bytes(raw, width, signed)} | writes],
          bump(report, :widened)
        )
    end
  end

  defp step([{:widenf, src, dst} | rest], body, writes, report) do
    # every f32 value is exactly representable in an f64, infinities and NaN
    # payloads included, so there is nothing to round and nothing to lose
    case body do
      <<_::binary-size(^src), bits::little-unsigned-32, _::binary>> ->
        wide = f64_bits(widen_float(f32_value(bits)))
        writes = [{dst, <<wide::little-unsigned-64>>} | writes]
        step(rest, body, writes, bump(report, :widened))

      _ ->
        step(rest, body, writes, report)
    end
  end

  defp step([{:const, dst, size, value} | rest], body, writes, report) do
    step(rest, body, [{dst, <<value::little-unsigned-size(size)-unit(8)>>} | writes], report)
  end

  # ---------------------------------------------------------------------------
  # THE CONTENT RULE: what a string(N) and a wstring(N) are allowed to be
  # ---------------------------------------------------------------------------
  #
  # A bytes(N) IS UNTOUCHED. It is bytes, and there is nothing for it to be
  # ill-formed as. These sit AFTER every ` + "`" + `step/4` + "`" + ` clause so the compiler sees
  # one function, not a helper splitting the clauses.
  #
  # UTF-8 IS ONE WALK, the same shape as the UTF-16 walk below: eight ASCII
  # bytes per clause when they sit in 1..127, then four, then one, then a
  # codepoint, and a zero is not a character. Hostile falls through. The
  # previous check was String.valid?/1 (one codepoint per call) plus a second
  # BIF for the NUL; this does both in one match.

  defp text_ok?(1, <<>>), do: true

  defp text_ok?(1, <<a, b, c, d, e, f, g, h, rest::binary>>)
       when a >= 1 and a <= 127 and b >= 1 and b <= 127 and c >= 1 and c <= 127 and d >= 1 and
              d <= 127 and e >= 1 and e <= 127 and f >= 1 and f <= 127 and g >= 1 and g <= 127 and
              h >= 1 and h <= 127,
       do: text_ok?(1, rest)

  defp text_ok?(1, <<a, b, c, d, rest::binary>>)
       when a >= 1 and a <= 127 and b >= 1 and b <= 127 and c >= 1 and c <= 127 and d >= 1 and
              d <= 127,
       do: text_ok?(1, rest)

  defp text_ok?(1, <<c, rest::binary>>) when c >= 1 and c <= 127, do: text_ok?(1, rest)
  defp text_ok?(1, <<c::utf8, rest::binary>>) when c > 0, do: text_ok?(1, rest)
  defp text_ok?(1, _), do: false
  defp text_ok?(2, used), do: rem(byte_size(used), 2) == 0 and utf16_ok?(used)
  defp text_ok?(_, _), do: true

  # PAIRED UTF-16 WITH NO ZERO UNIT, read as the wire it came from: the units
  # ride little-endian and land as a raw copy, so this walks them as they lie.
  defp utf16_ok?(<<>>), do: true
  defp utf16_ok?(<<0::little-unsigned-16, _::binary>>), do: false

  defp utf16_ok?(<<u::little-unsigned-16, rest::binary>>) when u >= 0xD800 and u <= 0xDBFF do
    case rest do
      <<low::little-unsigned-16, more::binary>> when low >= 0xDC00 and low <= 0xDFFF ->
        utf16_ok?(more)

      # A HIGH SURROGATE WITH NOTHING BEHIND IT, or with something that is not a
      # low surrogate, is half a character and not a character.
      _ ->
        false
    end
  end

  defp utf16_ok?(<<u::little-unsigned-16, _::binary>>) when u >= 0xDC00 and u <= 0xDFFF, do: false
  defp utf16_ok?(<<_::little-unsigned-16, rest::binary>>), do: utf16_ok?(rest)

  defp leaf(body, src, size, true) do
    case body do
      <<_::binary-size(^src), raw::little-signed-size(^size)-unit(8), _::binary>> -> raw
      _ -> nil
    end
  end

  defp leaf(body, src, size, false) do
    case body do
      <<_::binary-size(^src), raw::little-unsigned-size(^size)-unit(8), _::binary>> -> raw
      _ -> nil
    end
  end

  defp image_bytes(v, width, true), do: <<v::little-signed-size(width)-unit(8)>>
  defp image_bytes(v, width, false), do: <<v::little-unsigned-size(width)-unit(8)>>

  defp widen_float({:nonfinite, bits}) do
    # THE f32 PATTERN'S SIGN AND PAYLOAD, CARRIED INTO f64 — AND A SIGNALLING
    # NaN IS QUIETED, because that is what the reference does. Its rung is the
    # C++ ` + "`" + `(double) f` + "`" + `, and every hardware f32→f64 convert (cvtss2sd, fcvt)
    # turns an sNaN into the corresponding qNaN: the payload is carried and the
    # QUIET BIT — the destination mantissa's top bit — is set. Preserving the
    # signalling state instead would put a pattern on this side of the rung
    # that the reference cannot produce, and the byte oracle compares patterns.
    #
    # AN INFINITY IS NOT A NaN and keeps its zero payload: setting the quiet bit
    # on ±inf would hand back a NaN nobody wrote.
    sign = bits >>> 31 &&& 1
    mantissa = bits &&& 0x7FFFFF
    wide = sign <<< 63 ||| 0x7FF0000000000000 ||| mantissa <<< 29
    {:nonfinite, if(mantissa == 0, do: wide, else: wide ||| 0x8000000000000)}
  end

  defp widen_float(value), do: value

  # AN ORDINAL PAST THE LAST VARIANT THE WRITER DECLARED IS NOT A VARIANT AT
  # ALL: it lands None and counts one ` + "`" + `clamped` + "`" + `, which is the answer §3 gives
  # every other value outside its range and the answer a union tag beyond the
  # declared arm count gets too. TAG 0 IS None and is not a clamp.
  #
  # ` + "`" + `remap` + "`" + ` is the WRITER's table where the two sides can disagree about
  # what a number means, and the plain VARIANT COUNT where they cannot — which
  # is what the identity plan carries, because a plan compiled against my own
  # layout remaps every ordinal to itself.
  defp ordinal(0, _remap, report), do: {0, report}

  defp ordinal(raw, variants, report) when is_integer(variants) do
    if raw <= variants, do: {raw, report}, else: {0, bump(report, :clamped)}
  end

  defp ordinal(raw, remap, report) do
    if raw <= tuple_size(remap),
      do: {elem(remap, raw - 1), report},
      else: {0, bump(report, :clamped)}
  end

  defp clamp_count(raw, max, report) do
    cond do
      raw < 0 -> {0, bump(report, :clamped)}
      raw > max -> {max, bump(report, :clamped)}
      true -> {raw, report}
    end
  end

  defp live_at(writes, dst) do
    case List.keyfind(writes, dst, 0) do
      {^dst, <<v::little-signed-32>>} -> v
      _ -> 0
    end
  end

  # THE IMAGE IS ASSEMBLED, NOT MUTATED. The plan's writes are collected in plan
  # order and spliced onto the PREFILL, whose bytes fill every gap the plan
  # left. The sort is what makes the assembler independent of the order the plan
  # happens to be in; ` + "`" + `:lists.keysort/2` + "`" + ` is a merge sort that detects an already
  # ascending run, which is what every plan this compiler emits is, so it costs
  # a walk and nothing more.
  defp assemble([{0, image}], prefill) when byte_size(image) == byte_size(prefill) do
    # THE IDENTITY PLAN'S SINGLE RUN: the body sub-binary IS the image, and the
    # BEAM shares it rather than copying.
    image
  end

  defp assemble(writes, prefill) do
    writes
    |> :lists.reverse()
    |> then(&:lists.keysort(1, &1))
    |> splice(0, prefill, [])
  end

  defp splice([], pos, prefill, acc) do
    IO.iodata_to_binary(
      :lists.reverse([binary_part(prefill, pos, byte_size(prefill) - pos) | acc])
    )
  end

  defp splice([{dst, bytes} | rest], pos, prefill, acc) when dst >= pos do
    gap = binary_part(prefill, pos, dst - pos)
    splice(rest, dst + byte_size(bytes), prefill, [bytes, gap | acc])
  end

  # A DESTINATION BEHIND THE CURSOR IS ONE THIS COMPILER NEVER EMITS: it walks
  # MY OWN structure in MY OWN declared order, so every destination ascends, and
  # a union's arms overlap only under guards of which exactly one can fire.
  defp splice([_ | rest], pos, prefill, acc), do: splice(rest, pos, prefill, acc)

  # ---------------------------------------------------------------------------
  # THE PLAN CACHE: once per peer, and never once per record
  # ---------------------------------------------------------------------------

  # THE CACHE IS BOUNDED, AND THE BOUND IS THE POINT. A plan is cached under the
  # hash of the LAYOUT THAT MINTED IT, and a layout comes from a PEER: anyone
  # who can hand this reader a file can hand it a layout it has never seen, and
  # every one of those is a :persistent_term.put/2 — which makes EVERY PROCESS
  # IN THE VM scan its heap for the old term. An unbounded cache turns "send me
  # files with fresh layouts" into "stop this node", at no cost to the sender.
  #
  # SO THE CACHE FILLS ONCE AND THEN STOPS. Past the bound a new peer's plan is
  # compiled per load and not cached: compiling is bounded work already held to
  # the plan capacity and it writes NOTHING, so an attacker past the bound buys
  # exactly one plan compile per file and no global scan at all.
  #
  # IT IS NOT AN LRU, deliberately. An LRU would go on writing :persistent_term
  # forever — one put and one erase per eviction — which is the cost being
  # defended against, dressed up as a policy. A service that faces untrusted
  # peers should not be minting plans from their layouts in the first place: it
  # hands its own plan in through the plan: option and never reaches this.
  @plan_cache_max 64

  def plan_cache_max, do: @plan_cache_max

  @doc """
  The cached plan for ` + "`" + `{module, hash}` + "`" + `, or nil.

  IT IS ` + "`" + `:persistent_term` + "`" + `, and the cost is the reason. A ` + "`" + `get/2` + "`" + ` is a constant
  time lookup that COPIES NOTHING — the term is read where it lies, so a read
  pays one lookup and no allocation, which is the only per-record cost §3.4
  allows a plan to have. A ` + "`" + `put/2` + "`" + ` is the expensive half: it makes every
  process scan for the old term, which is why it happens ONCE PER PEER, why it
  happens at most ` + "`" + `plan_cache_max/0` + "`" + ` times per module, and why a caller that
  would rather own the plan itself hands one in through ` + "`" + `plan:` + "`" + ` and never
  touches this.
  """
  def cached(module, hash), do: :persistent_term.get({module, :fixed_plan, hash}, nil)

  @doc """
  THIS BUILD's OWN parsed layout, parsed once and held beside the plans.

  The plan compiler walks the peer's layout against mine, so mine has to be a
  parsed layout too — and parsing it is the one thing about the slow path that
  is the same for every peer, so it happens once for the module rather than
  once per peer.
  """
  # IT NEEDS NO BOUND: the only bytes ever handed to it are THIS BUILD's own
  # layouts, one per fixed root of the unit, so the number of them is a fact
  # about the schema and not about who is calling. The index beside them is
  # what lets forget/1 name them again.
  def my_layout(module, bytes) do
    case :persistent_term.get({module, :fixed_mine, bytes}, nil) do
      nil ->
        {:ok, mine} = parse_layout(bytes)
        :persistent_term.put({module, :fixed_mine, bytes}, mine)

        :persistent_term.put(
          {module, :fixed_mines},
          [bytes | :persistent_term.get({module, :fixed_mines}, [])]
        )

        mine

      mine ->
        mine
    end
  end

  @doc """
  Hold ` + "`" + `entry` + "`" + ` for ` + "`" + `{module, hash}` + "`" + ` while the module is under its bound, and
  hand it back either way.

  THE CALLER GETS THE SAME PLAN WHETHER IT WAS CACHED OR NOT, so nothing above
  has to know which happened: a full cache changes what the NEXT read costs and
  never what THIS read returns.
  """
  def cache(module, hash, entry) do
    held = :persistent_term.get({module, :fixed_plans}, [])

    if length(held) < @plan_cache_max do
      :persistent_term.put({module, :fixed_plan, hash}, entry)
      :persistent_term.put({module, :fixed_plans}, [hash | held])
    end

    entry
  end

  # ---------------------------------------------------------------------------
  # THE LINEAGE (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.3)
  # ---------------------------------------------------------------------------
  #
  # A FIXED TABLE READS BACKWARD AND NEVER FORWARD. A file is matched on the
  # eight bytes of its header's hash against a lineage THE BUILD laid down —
  # the lock's entries, OLDEST FIRST, the current layout last — and NOTHING
  # PARSES A STRANGER'S LAYOUT, ON ANY PATH. A hash the lineage does not hold is
  # :layout_newer (ship the reader); a hash below the FLOOR is
  # :layout_unsupported (upgrade the client); a hash the lineage holds whose
  # layout BYTES differ from the ones the lock recorded is ONE name,
  # :layout_malformed — a lie about a known version, which is where §1.1's seven
  # rules land once they are told about a layout this reader already knows.

  @doc """
  Select the lineage index a file's header hash resolves to (§5.3 steps 5 to 7).

  {:ok, i}, or {:error, reason, the file's hash}. THE HEADER'S HASH IS TAKEN AS
  GIVEN and nothing is recomputed from the wire: the definitions digest is not on
  the wire, so the hash cannot be re-derived from the bytes behind it. BOTH
  layout refusals report the file's hash (§5.9 #7).
  """
  def select(known, floor, hash, layout) do
    case index_of(known, hash, 0) do
      nil ->
        {:error, :layout_newer, hash}

      i when i < floor ->
        {:error, :layout_unsupported, hash}

      i ->
        k = elem(known, i)

        if byte_size(layout) == k.layout_bytes and layout == k.layout do
          {:ok, i}
        else
          {:error, :layout_malformed, 0}
        end
    end
  end

  defp index_of(known, _hash, i) when i >= tuple_size(known), do: nil

  defp index_of(known, hash, i) do
    if elem(known, i).hash == hash, do: i, else: index_of(known, hash, i + 1)
  end

  @doc """
  Build ONE PLAN PER LINEAGE ENTRY, at MODULE LOAD, from THE LOCK'S OWN BYTES.

  §5.9 #3: plans built at package initialization from the lock's bytes ARE build
  time for this page. That is not a run-time walk of a stranger's layout — the
  bytes came from the lock, the walk happens ONCE PER PROCESS, off every load
  path, and nothing on the load path compiles or parses a layout a file carried.
  This leg's storage is :persistent_term, written from the module's @on_load,
  which is the BEAM's one place that runs before any caller can reach the module.

  ITS INIT CANNOT THROW. Every entry is a LANE WITH ITS OWN REFUSAL REASON: an
  entry that is not a parseable layout, or whose plan does not fit the declared
  capacity, carries {:error, name} and LOAD refuses by that name if a file ever
  selects it (§5.9 #8, #26 for the run-time half; at BUILD the same fact is a
  lock bug and the build fails). A lane is never an exception, so @on_load always
  returns :ok and a module always loads.
  """
  def lineage_init(module, root, known, my_bytes, dst, capacity) do
    mine = my_layout(module, my_bytes)
    own = hash(my_bytes)

    lanes =
      for i <- 0..(tuple_size(known) - 1) do
        lineage_lane_of(elem(known, i), own, mine, dst, capacity)
      end

    :persistent_term.put({module, :fixed_lineage, root}, List.to_tuple(lanes))
    :ok
  end

  defp lineage_lane_of(%{hash: h}, own, _mine, _dst, _capacity) when h == own, do: :identity

  defp lineage_lane_of(k, _own, mine, dst, capacity) do
    case parse_layout(k.layout) do
      {:ok, theirs} ->
        # THE CENSUS IS THE PLAN'S OWN NUMBER, fixed when the plan was built
        # (§5.9 #6): it rides in the lane, and LOAD carries it onto the report
        # ONCE, after the record loop, on a read that RETURNS.
        case compile(theirs, mine, dst, capacity, report()) do
          {:ok, plan, made, census} ->
            {:ok, plan, k.record_bytes - 8, made, census.unknown, census.kind_mismatch}

          {:error, why, _census} ->
            {:error, why}
        end

      {:error, _why} ->
        # A LINEAGE ENTRY THAT IS NOT A LAYOUT IS A BUG IN THE LOCK and never a
        # wire event (§5.9 #8): where it arrives at run time the entry still owes
        # a NAME rather than a throw.
        {:error, :layout_malformed}
    end
  rescue
    _ -> {:error, :layout_malformed}
  end

  @doc """
  The lane the build laid down for lineage index i of root.
  """
  def lineage_lane(module, root, i) do
    case :persistent_term.get({module, :fixed_lineage, root}, nil) do
      nil -> {:error, :layout_malformed}
      lanes -> elem(lanes, i)
    end
  end

  @doc """
  Erase every plan this module has cached, and its parsed own layout.

  A CACHE THAT ONLY EVER FILLS NEEDS A WAY TO BE EMPTIED. It is not on the read
  path and it is not automatic: erasing is a ` + "`" + `:persistent_term` + "`" + ` write like any
  other, so a caller that does it per file has rebuilt the problem the bound
  exists to prevent. It is for a node that has genuinely rolled its peers over
  — a deploy, a long-lived service reconfigured — and for tests.
  """
  def forget(module) do
    for hash <- :persistent_term.get({module, :fixed_plans}, []) do
      :persistent_term.erase({module, :fixed_plan, hash})
    end

    :persistent_term.erase({module, :fixed_plans})

    for bytes <- :persistent_term.get({module, :fixed_mines}, []) do
      :persistent_term.erase({module, :fixed_mine, bytes})
    end

    :persistent_term.erase({module, :fixed_mines})
    :ok
  end
`
