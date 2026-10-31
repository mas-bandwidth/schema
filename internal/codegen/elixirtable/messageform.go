// THE MESSAGE FORM ON THE ELIXIR LEG (docs/SPEC-TABLES.md §3.3). The message
// form is a batch of bitpacked bodies under one announced vocabulary, and the
// announcement is an ordinary form-`1` file the compiler settles byte for byte.
// When the unit has a fixed root, this file emits both surfaces:
//
//	announce / announce_measure — the constant and its length, not a walk;
//	announce_read — reads the announcement file and fills a TableVocabulary;
//	measure_messages / save_messages / load_messages — the bitpacked batch,
//	form byte 2, over the vocabulary announce_read resolved.
//
// The batch runtime below reads and writes that form-`2` body. The
// announcement's form-`1` reader is emitted once per unit that declares a
// fixed table, beside BuildVersion. The announcement is a compile-time constant
// of the unit and its vocabulary is a pure function of the build version
// (docs/SPEC-TABLES.md §3.3). The batch verbs are the module's, not a generated
// verb per root.
//
// THE BEAM HAS NO CALLER-OWNED ARRAY, so §3.3's "the resolved vocabulary's
// storage is the caller's, declared by capacity" is honoured the way this leg's
// fixed form honours §3.4's own capacity rules: the entry bound and the byte
// bound are FIELDS of the TableVocabulary the caller may state, defaulted to
// 4096 entries and 64 KiB, and an endpoint past either is refused by name
// `:vocabulary_too_large`.
package elixirtable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TableMessageModule is the message form's module, one per unit that declares
// a fixed table, claimed beside BuildVersion and the two accelerators'
// runtimes (docs/SPEC-TABLES.md §3.3, §11). It carries the announcement
// surface and the batch verbs that read and write a form-`2` body.
const TableMessageModule = "TableMessage"

// messageformModule emits <ns>.TableMessage: the announcement constant, its
// measure, the reader that resolves it into a TableVocabulary, and the batch
// verbs that measure, save and load a form-`2` body.
func messageformModule(u *ir.Unit, ns string) []byte {
	var b strings.Builder
	b.WriteString(header(TableMessageModule, u.Package, "the MESSAGE FORM, announcement and batch (docs/SPEC-TABLES.md §3.3)"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "defmodule %s.%s do\n", ns, TableMessageModule)
	b.WriteString("  @moduledoc \"\"\"\n")
	b.WriteString("  The MESSAGE FORM's announcement and batch runtime (docs/SPEC-TABLES.md §3.3), form byte 2.\n\n")
	b.WriteString("  A message is a bitpacked body under a vocabulary the endpoint announces once,\n")
	b.WriteString("  before the first body and never again. The announcement is an ordinary\n")
	b.WriteString("  form-`1` FILE whose body carries the unit's BUILD VERSION and the unit's\n")
	b.WriteString("  whole VOCABULARY, and whose trailer is those two reserved ids. It is a\n")
	b.WriteString("  compile-time constant of the unit, so `announce/0` and `announce_measure/0`\n")
	b.WriteString("  are the constant and its length and not a walk.\n\n")
	b.WriteString("  `announce_read/2` resolves the vocabulary ONCE into the caller's\n")
	b.WriteString("  `TableMessage` struct — the `TableVocabulary` of §3.3's surface — keeping\n")
	b.WriteString("  the endpoint's build version. A second announcement is `:second_announcement`,\n")
	b.WriteString("  a vocabulary past either bound is `:vocabulary_too_large`, and the two\n")
	b.WriteString("  strict checks — the build version present exactly once at kind 9 eight\n")
	b.WriteString("  bytes wide, the vocabulary present exactly once at kind 14 element kind 6\n")
	b.WriteString("  — answer `:malformed`, never a refusal. `measure_messages/2`,\n")
	b.WriteString("  `save_messages/2` and `load_messages/2,3` measure, write and read a\n")
	b.WriteString("  form-`2` batch of bodies against that resolved vocabulary.\n")
	b.WriteString("  \"\"\"\n\n")
	b.WriteString("  import Bitwise\n\n")
	b.WriteString(exAttrBinary("  @announcement ", elixirBytes(ir.TableAnnouncement(u))))
	fmt.Fprintf(&b, "  @entries_here %d\n\n", len(ir.TableVocabulary(u)))
	b.WriteString(messageRuntimeBody)
	b.WriteString(messageBatchRuntimeBody)
	b.WriteString("end\n")
	return []byte(b.String())
}

// messageBatchRuntimeBody is the batch surface of docs/SPEC-TABLES.md §3.3: a
// batch is the form byte 2, a one-byte count of bodies minus one, then the
// bodies as ONE CONTINUOUS BIT STREAM ending at a zero reference each, then the
// zero pad to the next byte boundary (§3.3's "THE WIRE, EXACTLY"). The verbs
// are plural because the primitive is a batch and a single message is the batch
// of one; the five answers are the two write-side refusals and the read-side
// capacity refusal, damage inside a body delivering the bodies before it, and
// bytes after the pad (§3.3's "THE BATCH SURFACE'S FIVE ANSWERS").
//
// THE BODY IS BITPACKED and reads through the ANNOUNCED VOCABULARY, which
// announce_read already resolved into entries of {id, kind, shape}: a body is a
// sequence of fields, each a REFERENCE (ref_bits wide, counting from one) and a
// PAYLOAD at the width the announced shape states (§3.3's "A BODY IS A SEQUENCE
// OF FIELDS"). No kind byte, no length and no alignment rides inside a body, so
// a reader that has lost its position has lost it for the rest of the batch:
// damage is terminal for the batch (§3.3's "DAMAGE IS TERMINAL FOR THE BATCH").
const messageBatchRuntimeBody = `  @batch_max 256

  @doc """
  The batch's byte size for ` + "`" + `values` + "`" + `, or the refusal by name (docs/SPEC-TABLES.md §3.3).
  """
  def measure_messages(%__MODULE__{announced: true} = vocab, values) when is_list(values) do
    n = length(values)

    if n < 1 or n > @batch_max do
      {:error, :batch_too_large}
    else
      case encode_bodies(vocab, values, {0, 0}) do
        {:ok, {_acc, nbits}} -> {:ok, 2 + div(nbits + 7, 8)}
        :error -> {:error, :malformed}
      end
    end
  catch
    # every writer refuses a float that is not finite (docs/SPEC-TABLES.md §3.3)
    :non_finite_float -> {:error, :non_finite_float}
  end

  def measure_messages(_vocab, _values), do: {:error, :no_vocabulary}

  @doc """
  Write ` + "`" + `values` + "`" + ` as one batch (docs/SPEC-TABLES.md §3.3).
  """
  def save_messages(%__MODULE__{announced: true} = vocab, values) when is_list(values) do
    n = length(values)

    if n < 1 or n > @batch_max do
      {:error, :batch_too_large}
    else
      case encode_bodies(vocab, values, {0, 0}) do
        {:ok, bits} -> {:ok, frame_batch(n, bits)}
        :error -> {:error, :malformed}
      end
    end
  catch
    :non_finite_float -> {:error, :non_finite_float}
  end

  def save_messages(_vocab, _values), do: {:error, :no_vocabulary}

  @doc """
  Read one batch into at most ` + "`" + `capacity` + "`" + ` bodies (docs/SPEC-TABLES.md §3.3).
  """
  def load_messages(vocab, data, capacity \\ @batch_max)

  def load_messages(%__MODULE__{announced: true} = vocab, <<2, m, rest::binary>>, capacity) do
    count = m + 1

    if count > capacity do
      # "M above the caller's capacity on the read side is a refusal by name"
      # and "the reader sets the returned count to the wire's M before it
      # returns the refusal" (docs/SPEC-TABLES.md §3.3).
      {:error, :batch_too_large, count}
    else
      case read_bodies(vocab, {bytes_to_int(rest), 0, bit_size(rest)}, count, []) do
        {:ok, bodies, r} ->
          if batch_ends_clean?(r) do
            {:ok, bodies}
          else
            # "BYTES AFTER THE PAD ARE MALFORMED" (docs/SPEC-TABLES.md §3.3).
            {:malformed, count, bodies}
          end

        # "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1" (§3.3).
        {:malformed, k, bodies} ->
          {:malformed, k, bodies}
      end
    end
  catch
    # A NaN OR AN INFINITY REFUSES THE BATCH (docs/SPEC-TABLES.md §3.3), the
    # first one the walk meets, and outranks any damage after it: no body is
    # delivered.
    :non_finite_float -> {:error, :non_finite_float}
  end

  def load_messages(%__MODULE__{announced: true}, <<2>>, _capacity), do: {:error, :malformed}

  def load_messages(%__MODULE__{announced: true}, <<_form, _::binary>>, _capacity),
    do: {:error, :newer_form}

  def load_messages(%__MODULE__{announced: true}, _data, _capacity), do: {:error, :malformed}

  def load_messages(_vocab, _data, _capacity), do: {:error, :no_vocabulary}

  # ---------------------------------------------------------------------------
  # THE BATCH: form byte 2, a count of bodies minus one, then one bit stream
  # ---------------------------------------------------------------------------

  defp frame_batch(n, {acc, nbits}) do
    <<2, n - 1, int_to_bytes(acc, div(nbits + 7, 8))::binary>>
  end

  # ---------------------------------------------------------------------------
  # THE WRITER: one bit stream, low bit first (docs/SPEC-TABLES.md §3.3)
  #
  # The accumulator is ONE INTEGER and a bit count. A value's bit 0 lands at the
  # stream's current bit and its bit i at current + i, which is the packet wire's
  # own low-bit-first order; int_to_bytes spills it into bytes at the end.
  # ---------------------------------------------------------------------------

  defp put_bits({acc, nbits}, value, n),
    do: {acc ||| (value &&& bit_mask(n)) <<< nbits, nbits + n}

  defp align_write({acc, nbits}), do: {acc, nbits + rem(8 - rem(nbits, 8), 8)}

  defp encode_bodies(_vocab, [], acc), do: {:ok, acc}

  defp encode_bodies(vocab, [body | rest], acc) do
    case encode_body(vocab, body, acc) do
      {:ok, acc2} -> encode_bodies(vocab, rest, acc2)
      :error -> :error
    end
  end

  defp encode_body(vocab, body, acc) do
    case encode_fields(vocab, body, acc) do
      {:ok, acc2} -> {:ok, put_bits(acc2, 0, vocab.ref_bits)}
      :error -> :error
    end
  end

  defp encode_fields(_vocab, [], acc), do: {:ok, acc}

  defp encode_fields(vocab, [{ref, value} | rest], acc) do
    case entry_at(vocab, ref) do
      {:ok, entry} ->
        acc2 = put_bits(acc, ref, vocab.ref_bits)

        case encode_value(vocab, entry.kind, entry.shape, Map.get(entry, :element), value, acc2) do
          {:ok, acc3} -> encode_fields(vocab, rest, acc3)
          :error -> :error
        end

      :error ->
        :error
    end
  end

  defp encode_value(_vocab, 0, _shape, _elem, _value, acc), do: {:ok, acc}
  defp encode_value(_vocab, 32, _shape, _elem, _value, acc), do: {:ok, acc}

  defp encode_value(_vocab, 1, _shape, _elem, value, acc), do: {:ok, put_bits(acc, value, 1)}

  defp encode_value(_vocab, kind, shape, _elem, value, acc)
       when kind in [2, 3, 4, 5, 6, 7, 8, 9, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29],
       do: {:ok, encode_scalar(kind, shape, value, acc)}

  defp encode_value(_vocab, 10, shape, _elem, value, acc), do: encode_float(shape, value, acc)

  defp encode_value(_vocab, 11, _shape, _elem, value, acc),
    do: {:ok, put_bits(acc, f64_to_bits(value), 64)}

  defp encode_value(_vocab, 12, shape, _elem, value, acc), do: encode_string(shape, value, acc)

  defp encode_value(vocab, 13, _shape, _elem, value, acc), do: encode_body(vocab, value, acc)

  defp encode_value(vocab, 14, shape, elem, value, acc),
    do: encode_array(vocab, shape, elem, value, acc)

  defp encode_value(vocab, 15, _shape, _elem, value, acc), do: encode_union(vocab, value, acc)

  defp encode_value(vocab, 30, _shape, _elem, value, acc),
    do: {:ok, put_bits(acc, value, vocab.ref_bits)}

  defp encode_value(_vocab, _kind, _shape, _elem, _value, _acc), do: :error

  defp encode_scalar(kind, shape, value, acc) do
    bits = scalar_bits(kind, shape)

    raw =
      case shape do
        %{packing: 1, base: base} -> value - base
        _ -> mask_bits(value, bits)
      end

    put_bits(acc, raw, bits)
  end

  defp encode_float(%{packing: 0}, value, acc),
    do: {:ok, put_bits(acc, f32_to_bits(value), 32)}

  defp encode_float(_shape, _value, _acc), do: :error

  defp encode_string(shape, value, acc) do
    n = byte_size(value)
    acc2 = put_bits(acc, n, bit_length(shape.max))
    {:ok, put_bytes(align_write(acc2), value)}
  end

  defp encode_array(vocab, shape, elem, value, acc) do
    count = length(value)
    acc2 = put_bits(acc, count - shape.min, bit_length(shape.max - shape.min))
    acc3 = if elem.kind == 6, do: align_write(acc2), else: acc2
    encode_elements(vocab, elem, value, acc3)
  end

  defp encode_elements(_vocab, _elem, [], acc), do: {:ok, acc}

  defp encode_elements(vocab, elem, [v | rest], acc) do
    case encode_value(vocab, elem.kind, elem.shape, Map.get(elem, :element), v, acc) do
      {:ok, acc2} -> encode_elements(vocab, elem, rest, acc2)
      :error -> :error
    end
  end

  defp encode_union(vocab, :none, acc), do: {:ok, put_bits(acc, 0, vocab.ref_bits)}

  defp encode_union(vocab, {ref, value}, acc) do
    acc2 = put_bits(acc, ref, vocab.ref_bits)

    case entry_at(vocab, ref) do
      {:ok, entry} ->
        encode_value(vocab, entry.kind, entry.shape, Map.get(entry, :element), value, acc2)

      :error ->
        :error
    end
  end

  # A byte run appended whole: the writer must be at a byte boundary, which the
  # align that precedes a string or a bytes array guarantees.
  defp put_bytes({acc, nbits}, bytes) do
    n = byte_size(bytes)
    {acc ||| bytes_to_int(bytes) <<< nbits, nbits + n * 8}
  end

  # ---------------------------------------------------------------------------
  # THE READER: one bit stream, low bit first, terminal damage (§3.3)
  #
  # The reader is the body as ONE LITTLE-ENDIAN INTEGER and a bit offset: value
  # bit i sits at integer bit (offset + i), so reading n bits is a shift and a
  # mask and never a byte-order question.
  # ---------------------------------------------------------------------------

  defp read_bodies(_vocab, r, 0, acc), do: {:ok, Enum.reverse(acc), r}

  defp read_bodies(vocab, r, n, acc) do
    case read_body(vocab, r, [], 0) do
      {:ok, body, rest} -> read_bodies(vocab, rest, n - 1, [body | acc])
      :malformed -> {:malformed, length(acc), Enum.reverse(acc)}
    end
  end

  defp read_body(_vocab, _r, _acc, depth) when depth > 128, do: :malformed

  defp read_body(vocab, r, acc, depth) do
    case get_bits(r, vocab.ref_bits) do
      {:ok, 0, rest} ->
        {:ok, Enum.reverse(acc), rest}

      {:ok, ref, rest} ->
        case entry_at(vocab, ref) do
          {:ok, entry} when entry.id < 0xFFFFFFFFFFFFFFFD ->
            elem = Map.get(entry, :element)

            case read_value(vocab, entry.kind, entry.shape, elem, rest, depth) do
              {:ok, value, rest2} -> read_body(vocab, rest2, [{ref, value} | acc], depth)
              :malformed -> :malformed
            end

          _ ->
            :malformed
        end

      :error ->
        :malformed
    end
  end

  defp read_value(_vocab, _kind, _shape, _elem, _r, depth) when depth > 128, do: :malformed

  defp read_value(vocab, 13, _shape, _elem, r, depth), do: read_body(vocab, r, [], depth + 1)

  defp read_value(vocab, 14, shape, elem, r, depth),
    do: read_array(vocab, shape, elem, r, depth)

  defp read_value(vocab, 15, _shape, _elem, r, depth), do: read_union(vocab, r, depth)

  defp read_value(vocab, kind, shape, elem, r, _depth),
    do: read_value(vocab, kind, shape, elem, r)

  defp read_value(_vocab, 0, _shape, _elem, r), do: {:ok, :name, r}
  defp read_value(_vocab, 32, _shape, _elem, r), do: {:ok, :none, r}

  defp read_value(_vocab, 1, _shape, _elem, r), do: get_bits(r, 1)

  defp read_value(_vocab, kind, shape, _elem, r)
       when kind in [2, 3, 4, 5, 6, 7, 8, 9, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29],
       do: read_scalar(kind, shape, r)

  defp read_value(_vocab, 10, shape, _elem, r), do: read_float(shape, r)

  defp read_value(_vocab, 11, _shape, _elem, r) do
    case get_bits(r, 64) do
      {:ok, v, rest} -> {:ok, f64_from_bits(v), rest}
      :error -> :malformed
    end
  end

  defp read_value(_vocab, 12, shape, _elem, r), do: read_string(shape, r)

  defp read_value(vocab, 30, _shape, _elem, r), do: read_enum(vocab, r)

  defp read_value(_vocab, _kind, _shape, _elem, _r), do: :malformed

  defp read_scalar(kind, shape, r) do
    n = scalar_bits(kind, shape)

    case get_bits(r, n) do
      {:ok, raw, rest} -> {:ok, scalar_value(kind, shape, raw), rest}
      :error -> :malformed
    end
  end

  defp read_float(%{packing: 0}, r) do
    case get_bits(r, 32) do
      {:ok, v, rest} -> {:ok, f32_from_bits(v), rest}
      :error -> :malformed
    end
  end

  defp read_float(_shape, _r), do: :malformed

  defp read_string(shape, r) do
    with {:ok, n, rest} <- get_bits(r, bit_length(shape.max)),
         {:ok, rest2} <- align(rest),
         {:ok, bytes, rest3} <- take_bytes(rest2, n) do
      {:ok, bytes, rest3}
    else
      _ -> :malformed
    end
  end

  defp read_array(vocab, shape, elem, r, depth) do
    with {:ok, offset, rest} <- get_bits(r, bit_length(shape.max - shape.min)),
         {:ok, rest2} <- maybe_align(elem, rest) do
      next_depth = elem_depth(elem, depth)
      read_elements(vocab, shape, elem, shape.min + offset, rest2, [], next_depth)
    else
      _ -> :malformed
    end
  end

  defp elem_depth(%{kind: kind}, depth) when kind in [12, 13, 14, 15, 16, 17, 30], do: depth + 1
  defp elem_depth(_elem, depth), do: depth

  defp maybe_align(%{kind: 6}, r), do: align(r)
  defp maybe_align(_elem, r), do: {:ok, r}

  defp read_elements(_vocab, _shape, _elem, 0, r, acc, _depth), do: {:ok, Enum.reverse(acc), r}

  # A zero-width element leaves the cursor where it stood: kind 0, kind 32, a
  # ranged scalar whose bit width is 0, or a nested run of those. Looping on
  # the count would allocate a list in proportion to the announcement and
  # consume no input. This reader returns the list, so zero_width_run holds an
  # unbounded list (shape.max == 0xFFFFFFFF, ir.TableMessageListMax) to the
  # bits the batch still holds (internal/tablewire/messagedecode.go) before any
  # cell is allocated. A fixed or bounded array's count is a declared capacity,
  # not a wire claim, so it is materialized without that limit.
  defp read_elements(vocab, shape, elem, n, r, acc, depth) when is_map(elem) and n > 0 do
    case read_value(vocab, elem.kind, elem.shape, Map.get(elem, :element), r, depth) do
      {:ok, v, ^r} ->
        zero_width_run(vocab, shape, n, r, acc, v)

      {:ok, v, rest} ->
        read_elements(vocab, shape, elem, n - 1, rest, [v | acc], depth)

      :malformed ->
        :malformed
    end
  end

  defp read_elements(_vocab, _shape, _elem, n, _r, _acc, _depth) when n > 0, do: :malformed

  defp zero_width_run(_vocab, shape, n, r, acc, v) do
    # A fixed or bounded array (shape.max < 0xFFFFFFFF) declares its count in
    # the schema, so its elements are legitimate required capacity regardless
    # of the vocabulary's byte budget (vocab.max_bytes bounds the announcement,
    # not an element count) or the bits the batch still holds; a valid
    # last-field arrangement must not depend on following payload or padding.
    # An unbounded array (shape.max == 0xFFFFFFFF, ir.TableMessageListMax) has
    # no declared bound and is held to the bits left in the batch
    # (internal/tablewire/messagedecode.go): an element costs at least one bit
    # (floor 1), so a count the batch cannot back is refused before any cell is
    # allocated. The uint32 ceiling on the announced shape is untouched.
    if shape.max == 0xFFFFFFFF and n > bits_left(r) do
      :malformed
    else
      {:ok, Enum.reverse(acc, List.duplicate(v, n)), r}
    end
  end

  defp bits_left({_int, off, total}) when total > off, do: total - off
  defp bits_left({_int, _off, _total}), do: 0

  defp read_union(vocab, r, depth) do
    case get_bits(r, vocab.ref_bits) do
      {:ok, 0, rest} ->
        {:ok, :none, rest}

      {:ok, ref, rest} ->
        case entry_at(vocab, ref) do
          {:ok, entry} when entry.kind != 0 and entry.id < 0xFFFFFFFFFFFFFFFD ->
            elem = Map.get(entry, :element)

            case read_value(vocab, entry.kind, entry.shape, elem, rest, depth + 1) do
              {:ok, value, rest2} -> {:ok, {ref, value}, rest2}
              :malformed -> :malformed
            end

          _ ->
            :malformed
        end

      :error ->
        :malformed
    end
  end

  defp read_enum(vocab, r) do
    case get_bits(r, vocab.ref_bits) do
      {:ok, 0, rest} ->
        {:ok, 0, rest}

      {:ok, ref, rest} ->
        case entry_at(vocab, ref) do
          {:ok, entry} when entry.kind == 0 and entry.id < 0xFFFFFFFFFFFFFFFD ->
            {:ok, ref, rest}

          _ ->
            :malformed
        end

      :error ->
        :malformed
    end
  end

  # ---- the shared bit helpers --------------------------------------------------

  defp scalar_bits(kind, %{packing: 1, bits: b})
       when kind in [2, 3, 4, 5, 6, 7, 8, 9, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29],
       do: b

  defp scalar_bits(kind, _shape), do: kind_bits(kind)

  defp scalar_value(_kind, %{packing: 1, base: base}, raw), do: base + raw

  defp scalar_value(kind, _shape, raw) when kind in [2, 3, 4, 5, 18],
    do: sign_extend(raw, kind_bits(kind))

  defp scalar_value(_kind, _shape, raw), do: raw

  defp sign_extend(raw, bits) do
    if (raw >>> (bits - 1) &&& 1) == 1 do
      raw - (1 <<< bits)
    else
      raw
    end
  end

  defp mask_bits(value, bits), do: value &&& bit_mask(bits)

  defp bit_mask(0), do: 0
  defp bit_mask(n), do: (1 <<< n) - 1

  defp get_bits({int, off, total}, n) do
    if off + n > total do
      :error
    else
      {:ok, int >>> off &&& bit_mask(n), {int, off + n, total}}
    end
  end

  defp align({int, off, total}) do
    pad = rem(8 - rem(off, 8), 8)

    if off + pad > total or (int >>> off &&& bit_mask(pad)) != 0 do
      :error
    else
      {:ok, {int, off + pad, total}}
    end
  end

  # r is byte-aligned here (align precedes the call), so read n bytes whole.
  # A legal zero-length text is an exact empty binary and the cursor unchanged.
  # 0..(n - 1) with no step is the descending range 0..-1 when n is 0, and that
  # range walks two bytes. Positive lengths step //1. A run past the batch
  # stays :error.
  defp take_bytes({_int, _off, _total} = r, 0), do: {:ok, <<>>, r}

  defp take_bytes({int, off, total}, n) when is_integer(n) and n > 0 do
    need = n * 8

    if off + need > total do
      :error
    else
      bytes = for i <- 0..(n - 1)//1, into: <<>>, do: <<int >>> (off + 8 * i) &&& 0xFF>>
      {:ok, bytes, {int, off + need, total}}
    end
  end

  defp entry_at(vocab, ref) do
    if ref >= 1 and ref <= length(vocab.entries) do
      {:ok, Enum.at(vocab.entries, ref - 1)}
    else
      :error
    end
  end

  # The batch ends at the pad to the byte boundary: no more than seven bits left,
  # and all of them zero (docs/SPEC-TABLES.md §3.3).
  defp batch_ends_clean?({int, off, total}) do
    rem_bits = total - off
    rem_bits < 8 and (rem_bits == 0 or (int >>> off &&& bit_mask(rem_bits)) == 0)
  end

  defp int_to_bytes(_int, 0), do: <<>>

  defp int_to_bytes(int, nbytes) do
    for i <- 0..(nbytes - 1), into: <<>>, do: <<int >>> (8 * i) &&& 0xFF>>
  end

  defp bytes_to_int(bin), do: bytes_to_int(bin, 0, 0)
  defp bytes_to_int(<<>>, _i, acc), do: acc

  defp bytes_to_int(<<b, rest::binary>>, i, acc),
    do: bytes_to_int(rest, i + 1, acc ||| b <<< (8 * i))

  defp bit_length(0), do: 0
  defp bit_length(n), do: bit_length(n, 0)
  defp bit_length(n, b) when n > 0, do: bit_length(n >>> 1, b + 1)
  defp bit_length(_n, b), do: b

  # A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): a read tests the raw bits for
  # an all-ones exponent before they become a float and refuses the batch, and
  # a write refuses a float64 that narrows past float32's range to an infinity
  # (a BEAM float holds no NaN and no infinity of its own).
  defp f64_from_bits(v) when (v >>> 52 &&& 0x7FF) == 0x7FF, do: throw(:non_finite_float)

  defp f64_from_bits(v) do
    <<f::little-float-64>> = <<v::little-64>>
    f
  end

  defp f64_to_bits(f) do
    <<v::little-64>> = <<f::little-float-64>>
    v
  end

  defp f32_from_bits(v) when (v &&& 0x7F800000) == 0x7F800000, do: throw(:non_finite_float)

  defp f32_from_bits(v) do
    <<f::little-float-32>> = <<v::little-32>>
    f
  end

  defp f32_to_bits(f) do
    <<v::little-32>> = <<f::little-float-32>>
    if (v &&& 0x7F800000) == 0x7F800000, do: throw(:non_finite_float), else: v
  end
`

// elixirBytes spells a byte run as the segments of an Elixir binary literal,
// low byte first.
func elixirBytes(bytes []byte) []string {
	segs := make([]string, len(bytes))
	for i, v := range bytes {
		segs[i] = fmt.Sprintf("0x%02X", v)
	}
	return segs
}

// exAttrBinary writes `<prefix><<a, b, ...>>` the way `mix format` writes a
// module attribute's binary: one line when it fits, and otherwise FILLED to
// the line limit, every continuation aligned under the first segment.
func exAttrBinary(prefix string, segs []string) string {
	if one := prefix + "<<" + strings.Join(segs, ", ") + ">>"; exWidth(one) <= exLineLimit {
		return one + "\n"
	}
	cont := strings.Repeat(" ", exWidth(prefix)+2)
	var b strings.Builder
	b.WriteString(prefix + "<<" + segs[0])
	col := exWidth(prefix) + 2 + exWidth(segs[0])
	for i := 1; i < len(segs); i++ {
		after := 1 // the comma that follows it
		if i == len(segs)-1 {
			after = 2 // the closing >>
		}
		if col+2+exWidth(segs[i])+after > exLineLimit {
			b.WriteString(",\n" + cont + segs[i])
			col = exWidth(cont) + exWidth(segs[i])
			continue
		}
		b.WriteString(", " + segs[i])
		col += 2 + exWidth(segs[i])
	}
	return b.String() + ">>\n"
}

const messageRuntimeBody = `  defstruct announced: false,
            refused: false,
            build_version: 0,
            entries: [],
            count: 0,
            ref_bits: 0,
            max_bytes: 65536,
            max_entries: 4096

  # ---------------------------------------------------------------------------
  # THE CONSTANT, and nothing but the constant
  # ---------------------------------------------------------------------------

  @doc """
  The unit's announcement, byte for byte the compiler settled (docs/SPEC-TABLES.md
  §3.3): a form-` + "`" + `1` + "`" + ` file whose body carries the build version and the vocabulary.
  """
  def announce, do: @announcement

  @doc "The announcement's byte count, a constant of the unit."
  def announce_measure, do: byte_size(@announcement)

  @doc "This unit's own entry count: the capacity a receiver that talks only to its own build declares."
  def entries_here, do: @entries_here

  def new, do: %__MODULE__{}

  # ---------------------------------------------------------------------------
  # THE READER
  # ---------------------------------------------------------------------------

  @doc """
  Read an announcement into the caller's vocabulary.

  ` + "`" + `{:ok, vocab}` + "`" + ` on a clean read; ` + "`" + `{:error, reason, vocab}` + "`" + ` otherwise, the
  reason by name. The build version is kept the moment it is read, refusal or
  not. A REFUSAL IS TERMINAL (docs/SPEC-TABLES.md §3.3): once refused — or once
  malformed — every later announcement is ` + "`" + `:second_announcement` + "`" + `.
  """
  def announce_read(data), do: announce_read(data, new())

  def announce_read(_data, %__MODULE__{announced: true} = vocab),
    do: {:error, :second_announcement, vocab}

  def announce_read(_data, %__MODULE__{refused: true} = vocab),
    do: {:error, :second_announcement, vocab}

  def announce_read(data, vocab) do
    case open_file(data) do
      {:ok, body, ids} -> walk_body(body, ids, vocab)
      {:error, reason} -> {:error, reason, %{vocab | refused: true}}
    end
  end

  defp walk_body(body, ids, vocab) do
    case walk(body, ids, 0, 0, 0, nil, vocab.max_bytes) do
      {:ok, build_version, words} ->
        parse_words(words, %{vocab | build_version: build_version})

      {:refuse, reason} ->
        {:error, reason, %{vocab | refused: true}}

      {:malformed, build_version} ->
        {:error, :malformed, %{vocab | refused: true, build_version: build_version}}
    end
  end

  # ---------------------------------------------------------------------------
  # THE FORM-` + "`" + `1` + "`" + ` FILE, read whole
  # ---------------------------------------------------------------------------

  # THE FORM BYTE IS READ FIRST (§3.3): a byte this reader does not carry is a
  # refusal by name and never damage.
  defp open_file(<<>>), do: {:error, :malformed}
  defp open_file(<<2, _::binary>>), do: {:error, :message_form_as_file}
  defp open_file(<<form, _::binary>>) when form != 1, do: {:error, :newer_form}
  defp open_file(data) when byte_size(data) < 9, do: {:error, :malformed}

  defp open_file(data) do
    size = byte_size(data)
    count = :binary.decode_unsigned(binary_part(data, size - 8, 8), :little)

    cond do
      count > div(size - 9, 8) ->
        {:error, :malformed}

      true ->
        start = size - 8 - count * 8
        body = binary_part(data, 1, start - 1)
        ids = binary_part(data, start, count * 8)

        if unique_ids?(ids) do
          {:ok, body, ids}
        else
          {:error, :malformed}
        end
    end
  end

  # A table carrying one id twice is the whole wire malformed (§3).
  defp unique_ids?(<<>>), do: true

  defp unique_ids?(<<id::little-unsigned-64, rest::binary>>) do
    not member_id?(rest, id) and unique_ids?(rest)
  end

  defp member_id?(<<>>, _id), do: false
  defp member_id?(<<x::little-unsigned-64, _::binary>>, id) when x == id, do: true
  defp member_id?(<<_::little-unsigned-64, rest::binary>>, id), do: member_id?(rest, id)

  # ---------------------------------------------------------------------------
  # THE BODY WALK: the two strict checks, and tolerance for everything else
  # ---------------------------------------------------------------------------

  # THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS (§3.3):
  # the build version present exactly once at kind 9 eight bytes wide, and the
  # vocabulary present exactly once at kind 14 element kind 6. A failed strict
  # check is malformed; an unknown field is skipped and counted, never fatal.
  defp walk(bin, ids, versions, words_count, build_version, words, max_bytes) do
    case leb(bin) do
      :error ->
        {:malformed, build_version}

      {:ok, 0, rest} ->
        # BYTES BETWEEN THE BODY'S TERMINATOR AND THE FIRST ENTRY ARE MALFORMED (§3).
        if rest == <<>> and versions == 1 and words_count == 1 do
          {:ok, build_version, words}
        else
          {:malformed, build_version}
        end

      {:ok, ref, rest} ->
        case resolve(ids, ref) do
          :error ->
            {:malformed, build_version}

          {:ok, id} ->
            case rest do
              <<kind, rest2::binary>> ->
                cond do
                  id == 0xFFFFFFFFFFFFFFFE ->
                    case rest2 do
                      <<bv::little-unsigned-64, rest3::binary>> when kind == 9 ->
                        walk(rest3, ids, versions + 1, words_count, bv, words, max_bytes)

                      _ ->
                        {:malformed, build_version}
                    end

                  id == 0xFFFFFFFFFFFFFFFD ->
                    if kind != 14 do
                      {:malformed, build_version}
                    else
                      case read_vocab(rest2, max_bytes) do
                        {:ok, words, rest3} ->
                          walk(
                            rest3,
                            ids,
                            versions,
                            words_count + 1,
                            build_version,
                            words,
                            max_bytes
                          )

                        {:refuse, reason} ->
                          {:refuse, reason}

                        :error ->
                          {:malformed, build_version}
                      end
                    end

                  true ->
                    case skip(kind, rest2) do
                      {:ok, rest3} ->
                        walk(rest3, ids, versions, words_count, build_version, words, max_bytes)

                      :error ->
                        {:malformed, build_version}
                    end
                end

              _ ->
                {:malformed, build_version}
            end
        end
    end
  end

  # The vocabulary field: kind 14, element kind 6, a length and the bytes. The
  # BYTE BOUND is checked from the field's own length before an entry is touched
  # (docs/SPEC-TABLES.md §3.3).
  defp read_vocab(bin, max_bytes) do
    case leb(bin) do
      {:ok, l, sub} when byte_size(sub) >= l ->
        body = binary_part(sub, 0, l)
        rest = binary_part(sub, l, byte_size(sub) - l)

        case body do
          <<6, count_rest::binary>> ->
            case leb(count_rest) do
              {:ok, n, words} when n == byte_size(words) ->
                if n > max_bytes do
                  {:refuse, :vocabulary_too_large}
                else
                  {:ok, words, rest}
                end

              _ ->
                :error
            end

          _ ->
            :error
        end

      _ ->
        :error
    end
  end

  # Skip an unknown field by its declared kind (§3's closed set).
  defp skip(kind, bin) do
    cond do
      kind in [1, 2, 6, 20, 25] ->
        take(1, bin)

      kind in [3, 7, 21, 26] ->
        take(2, bin)

      kind in [4, 8, 10, 22, 27] ->
        take(4, bin)

      kind in [5, 9, 11, 23, 28] ->
        take(8, bin)

      kind in [18, 19, 24, 29] ->
        take(16, bin)

      kind in [17, 30] ->
        leb_rest(bin)

      kind in [12, 13, 14, 16, 31, 32, 33] ->
        body(bin)

      kind == 15 ->
        case leb(bin) do
          {:ok, 0, rest} -> {:ok, rest}
          {:ok, _ref, <<_k, rest::binary>>} -> body(rest)
          _ -> :error
        end

      true ->
        :error
    end
  end

  defp take(n, bin) when byte_size(bin) >= n,
    do: {:ok, binary_part(bin, n, byte_size(bin) - n)}

  defp take(_n, _bin), do: :error

  defp leb_rest(bin) do
    case leb(bin) do
      {:ok, _v, rest} -> {:ok, rest}
      :error -> :error
    end
  end

  defp body(bin) do
    case leb(bin) do
      {:ok, l, rest} when byte_size(rest) >= l ->
        {:ok, binary_part(rest, l, byte_size(rest) - l)}

      _ ->
        :error
    end
  end

  # ---------------------------------------------------------------------------
  # THE VOCABULARY: one entry is an id, a kind and a shape
  # ---------------------------------------------------------------------------

  defp parse_words(words, vocab) do
    case parse_entries(words, vocab.max_entries, [], 0, false) do
      {:ok, entries, count} ->
        {:ok,
         %{
           vocab
           | announced: true,
             entries: Enum.reverse(entries),
             count: count,
             ref_bits: ref_bits(count)
         }}

      {:error, :vocabulary_too_large} ->
        {:error, :vocabulary_too_large, %{vocab | refused: true}}

      :error ->
        {:error, :malformed, %{vocab | refused: true}}
    end
  end

  # THE ENTRY BOUND IS THE CAPACITY THE CALLER DECLARED (§3.3), refused by name
  # before an entry is touched.
  defp parse_entries(<<>>, _max, acc, count, _node), do: {:ok, acc, count}

  defp parse_entries(_bin, max, _acc, count, _node) when count >= max,
    do: {:error, :vocabulary_too_large}

  defp parse_entries(bin, max, acc, count, node) do
    case entry(bin) do
      {:ok, e, rest} ->
        cond do
          e.id == 0xFFFFFFFFFFFFFFFE or e.id == 0xFFFFFFFFFFFFFFFD ->
            :error

          e.id == 0xFFFFFFFFFFFFFFFF and node ->
            :error

          duplicate?(e, acc) ->
            :error

          true ->
            parse_entries(rest, max, [e | acc], count + 1, node or e.id == 0xFFFFFFFFFFFFFFFF)
        end

      :error ->
        :error
    end
  end

  defp duplicate?(_e, []), do: false
  defp duplicate?(e, [x | rest]), do: e == x or duplicate?(e, rest)

  defp ref_bits(0), do: 1
  defp ref_bits(n), do: ref_bits(n, 1)
  defp ref_bits(n, b) when n >= 2, do: ref_bits(n >>> 1, b + 1)
  defp ref_bits(_n, b), do: b

  # ---- one entry -----------------------------------------------------------------

  defp entry(<<id::little-unsigned-64, kind, rest::binary>>) do
    if not known_kind?(kind) do
      :error
    else
      case shape(kind, rest) do
        {:ok, s, rest2} ->
          if kind in [14, 16] do
            read_element(kind, id, s, rest2)
          else
            {:ok, %{id: id, kind: kind, shape: s}, rest2}
          end

        :error ->
          :error
      end
    end
  end

  defp entry(_), do: :error

  defp read_element(kind, id, s, <<elem, rest::binary>>) do
    if not known_kind?(elem) or elem in [12, 33] do
      :error
    else
      case shape(elem, rest) do
        {:ok, es, rest2} ->
          if elem in [14, 16] do
            # A nested array carries one more kind byte. That kind is kept so a
            # zero-width nested run is the same bounded read as a flat one.
            # The shape bytes after that kind byte are still not decoded.
            case rest2 do
              <<inner, rest3::binary>> ->
                if known_kind?(inner) and inner not in [12, 33] do
                  inner_elem = %{kind: inner, shape: %{kind: inner}}

                  {:ok,
                   %{
                     id: id,
                     kind: kind,
                     shape: s,
                     element: %{kind: elem, shape: es, element: inner_elem}
                   }, rest3}
                else
                  :error
                end

              _ ->
                :error
            end
          else
            {:ok, %{id: id, kind: kind, shape: s, element: %{kind: elem, shape: es}}, rest2}
          end

        :error ->
          :error
      end
    end
  end

  defp read_element(_kind, _id, _s, _), do: :error

  defp known_kind?(kind), do: kind >= 0 and kind <= 33

  defp kind_bits(kind) do
    case kind do
      1 -> 1
      2 -> 8
      3 -> 16
      4 -> 32
      5 -> 64
      6 -> 8
      7 -> 16
      8 -> 32
      9 -> 64
      10 -> 32
      11 -> 64
      18 -> 128
      19 -> 128
      20 -> 8
      21 -> 16
      22 -> 32
      23 -> 64
      24 -> 128
      25 -> 8
      26 -> 16
      27 -> 32
      28 -> 64
      29 -> 128
      _ -> 0
    end
  end

  # ---- one shape -----------------------------------------------------------------

  defp shape(kind, bin) when (kind >= 2 and kind <= 10) or (kind >= 18 and kind <= 29) do
    case bin do
      <<packing, rest::binary>> ->
        cond do
          packing == 0 ->
            {:ok, %{kind: kind, packing: 0}, rest}

          packing == 1 and kind != 10 ->
            case leb(rest) do
              {:ok, n, rest2} ->
                if n <= kind_bits(kind) do
                  if kind >= 18 do
                    case rest2 do
                      <<lo::little-unsigned-64, hi::little-unsigned-64, rest3::binary>> ->
                        {:ok, %{kind: kind, packing: 1, bits: n, base: hi <<< 64 ||| lo}, rest3}

                      _ ->
                        :error
                    end
                  else
                    case leb(rest2) do
                      {:ok, raw, rest3} ->
                        base = if kind >= 2 and kind <= 5, do: zigzag(raw), else: raw
                        {:ok, %{kind: kind, packing: 1, bits: n, base: base}, rest3}

                      :error ->
                        :error
                    end
                  end
                else
                  :error
                end

              :error ->
                :error
            end

          packing == 2 and kind == 10 ->
            case rest do
              <<lo::little-unsigned-32, hi::little-unsigned-32, res::little-unsigned-32,
                rest2::binary>> ->
                lof = f32(lo)
                hif = f32(hi)
                resf = f32(res)

                # A quantized triple whose delta or delta/res is not finite in
                # float32 is a hostile width (docs/SPEC-TABLES.md §3.3).
                if is_float(lof) and is_float(hif) and is_float(resf) and lof < hif and resf > 0.0 do
                  delta = round_f32(hif - lof)

                  if delta != :nonfinite and round_f32(delta / resf) != :nonfinite do
                    {:ok, %{kind: kind, packing: 2, qmin: lof, qmax: hif, qres: resf}, rest2}
                  else
                    :error
                  end
                else
                  :error
                end

              _ ->
                :error
            end

          true ->
            :error
        end

      _ ->
        :error
    end
  end

  defp shape(kind, bin) when kind == 12 or kind == 33 do
    case leb(bin) do
      {:ok, n, rest} when n <= 0x7FFFFFFF -> {:ok, %{kind: kind, max: n}, rest}
      _ -> :error
    end
  end

  # An array's shape is min, max, then the element kind (u8) after them, two
  # numbers (§3.3's shape table).
  defp shape(kind, bin) when kind == 14 do
    case leb(bin) do
      {:ok, min, rest} ->
        case leb(rest) do
          {:ok, max, rest2} ->
            if min > 0xFFFFFFFF or max > 0xFFFFFFFF or max < min do
              :error
            else
              {:ok, %{kind: kind, min: min, max: max}, rest2}
            end

          :error ->
            :error
        end

      :error ->
        :error
    end
  end

  # An enum-keyed array's shape is slots, then the element kind (u8) after it,
  # ONE number and no min (§3.3's shape table): the row spells slots and the
  # element byte follows it.
  defp shape(kind, bin) when kind == 16 do
    case leb(bin) do
      {:ok, slots, rest} ->
        if slots > 0xFFFFFFFF do
          :error
        else
          {:ok, %{kind: kind, max: slots}, rest}
        end

      :error ->
        :error
    end
  end

  defp shape(kind, bin), do: {:ok, %{kind: kind}, bin}

  # ---- LEB128 and friends -------------------------------------------------------

  # Canonical unsigned LEB128: minimal, at most ten bytes, one value bit in the
  # tenth. Returns {:ok, value, rest} or :error.
  defp leb(<<b, rest::binary>>) when b < 0x80, do: {:ok, b, rest}

  defp leb(bin), do: leb(bin, 0, 0)

  defp leb(<<b, rest::binary>>, i, acc) do
    cond do
      i == 9 and b > 1 ->
        :error

      (b &&& 0x80) == 0 ->
        if i > 0 and b == 0, do: :error, else: {:ok, acc + ((b &&& 0x7F) <<< (7 * i)), rest}

      true ->
        leb(rest, i + 1, acc + ((b &&& 0x7F) <<< (7 * i)))
    end
  end

  defp leb(_bin, _i, _acc), do: :error

  defp resolve(ids, ref) do
    if ref == 0 or ref > div(byte_size(ids), 8) do
      :error
    else
      {:ok, :binary.decode_unsigned(binary_part(ids, (ref - 1) * 8, 8), :little)}
    end
  end

  defp zigzag(raw), do: bxor(raw >>> 1, -band(raw, 1))

  # A float32, read bit-transparent (docs/SPEC-TABLES.md §3.3's float32 facts).
  # A non-finite pattern — an infinity or a NaN — is a hostile width on the
  # announcement and reads back as :nonfinite, because the BEAM's arithmetic
  # refuses to hold one as a float term (fixedRuntime's f32_value does the same).
  defp f32(bits) do
    if (bits >>> 23 &&& 0xFF) == 0xFF do
      :nonfinite
    else
      <<f::little-float-32>> = <<bits::little-32>>
      f
    end
  end

  # docs/SPEC.md Floating-point discipline and docs/FIXED-FORM-ALGORITHM.md §5.1.
  # The product rounds to float32 before the add. Elixir does not contract.
  def dequantize_bits(index, qmin, delta, count)
      when is_integer(index) and is_integer(count) and count > 0 do
    index = if index < 0, do: 0, else: index
    index = if index > count, do: count, else: index
    normalized = round_f32(index / count)
    scaled = round_f32(normalized * delta)
    <<bits::little-unsigned-32>> = <<round_f32(scaled + qmin)::little-float-32>>
    bits
  end

  # A finite float rounded to float32, or :nonfinite where it overflows the
  # width: the page's "not finite in float32" (docs/SPEC-TABLES.md §3.3), which
  # a ranged float's delta or delta/res reaches by overflow alone.
  defp round_f32(x) do
    <<bits::little-32>> = <<x::little-float-32>>

    if (bits >>> 23 &&& 0xFF) == 0xFF do
      :nonfinite
    else
      <<f::little-float-32>> = <<bits::little-32>>
      f
    end
  end

`
