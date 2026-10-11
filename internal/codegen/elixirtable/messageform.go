// THE MESSAGE FORM'S ANNOUNCEMENT ON THE ELIXIR LEG (docs/SPEC-TABLES.md
// §3.3). The message form is a batch of bitpacked bodies under one announced
// vocabulary, and the announcement is an ordinary form-`1` file the compiler
// settles byte for byte. This card emits the ANNOUNCEMENT SURFACE only:
//
//	Announce / AnnounceMeasure — the constant and its length, not a walk;
//	AnnounceRead — reads the announcement file and fills a TableVocabulary;
//	the refusals the page names, and the two strict checks answered malformed.
//
// The batch verbs (MeasureMessages / SaveMessages / LoadMessages) are the NEXT
// card; nothing here reads a form-`2` body. The announcement's form-`1` reader
// below is the whole of the wire this surface owes, and it is emitted once per
// unit that declares a fixed table, beside BuildVersion — the announcement is a
// compile-time constant of the unit and its vocabulary is a pure function of
// the build version (docs/SPEC-TABLES.md §3.3).
//
// THE BEAM HAS NO CALLER-OWNED ARRAY, so §3.3's "the resolved vocabulary's
// storage is the caller's, declared by capacity" is honoured the way this leg's
// fixed form honours §3.4's own capacity rules: the entry bound and the byte
// bound are FIELDS of the TableVocabulary the caller may state, defaulted to
// 4096 entries and 64 KiB, and a peer past either is refused by name
// `:vocabulary_too_large`.
package elixirtable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TableMessageModule is the message form's announcement surface module, one per
// unit that declares a fixed table, claimed beside BuildVersion and the two
// accelerators' runtimes (docs/SPEC-TABLES.md §3.3, §11).
const TableMessageModule = "TableMessage"

// messageformModule emits <ns>.TableMessage: the announcement constant, its
// measure, and the reader that resolves it into a TableVocabulary.
func messageformModule(u *ir.Unit, ns string) []byte {
	var b strings.Builder
	b.WriteString(header(TableMessageModule, u.Package, "the MESSAGE FORM's announcement surface (docs/SPEC-TABLES.md §3.3)"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "defmodule %s.%s do\n", ns, TableMessageModule)
	b.WriteString("  @moduledoc \"\"\"\n")
	b.WriteString("  The MESSAGE FORM's announcement (docs/SPEC-TABLES.md §3.3), form byte 2.\n\n")
	b.WriteString("  A message is a bitpacked body under a vocabulary the peer announces once,\n")
	b.WriteString("  before the first body and never again. The announcement is an ordinary\n")
	b.WriteString("  form-`1` FILE whose body carries the unit's BUILD VERSION and the unit's\n")
	b.WriteString("  whole VOCABULARY, and whose trailer is those two reserved ids. It is a\n")
	b.WriteString("  compile-time constant of the unit, so `announce/0` and `announce_measure/0`\n")
	b.WriteString("  are the constant and its length and not a walk.\n\n")
	b.WriteString("  `announce_read/2` resolves the vocabulary ONCE into the caller's\n")
	b.WriteString("  `TableMessage` struct — the `TableVocabulary` of §3.3's surface — keeping\n")
	b.WriteString("  the peer's build version. A second announcement is `:second_announcement`,\n")
	b.WriteString("  a vocabulary past either bound is `:vocabulary_too_large`, and the two\n")
	b.WriteString("  strict checks — the build version present exactly once at kind 9 eight\n")
	b.WriteString("  bytes wide, the vocabulary present exactly once at kind 14 element kind 6\n")
	b.WriteString("  — answer `:malformed`, never a refusal.\n")
	b.WriteString("  \"\"\"\n\n")
	b.WriteString("  import Bitwise\n\n")
	fmt.Fprintf(&b, "  @announcement <<%s>>\n", elixirBytes(ir.TableAnnouncement(u)))
	fmt.Fprintf(&b, "  @entries_here %d\n\n", len(ir.TableVocabulary(u)))
	b.WriteString(messageRuntimeBody)
	b.WriteString("end\n")
	return []byte(b.String())
}

// elixirBytes spells a byte run as an Elixir binary literal, low byte first.
func elixirBytes(bytes []byte) string {
	var s strings.Builder
	for i, v := range bytes {
		if i > 0 {
			s.WriteByte(',')
		}
		fmt.Fprintf(&s, "0x%02X", v)
	}
	return s.String()
}

const messageRuntimeBody = `  defstruct [
    announced: false,
    refused: false,
    build_version: 0,
    entries: [],
    count: 0,
    ref_bits: 0,
    max_bytes: 65536,
    max_entries: 4096
  ]

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
                          walk(rest3, ids, versions, words_count + 1, build_version, words, max_bytes)

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
      kind in [1, 2, 6, 20, 25] -> take(1, bin)
      kind in [3, 7, 21, 26] -> take(2, bin)
      kind in [4, 8, 10, 22, 27] -> take(4, bin)
      kind in [5, 9, 11, 23, 28] -> take(8, bin)
      kind in [18, 19, 24, 29] -> take(16, bin)
      kind in [17, 30] -> leb_rest(bin)
      kind in [12, 13, 14, 16, 31, 32, 33] -> body(bin)
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
          e.id == 0xFFFFFFFFFFFFFFFE or e.id == 0xFFFFFFFFFFFFFFFD -> :error
          e.id == 0xFFFFFFFFFFFFFFFF and node -> :error
          duplicate?(e, acc) -> :error
          true -> parse_entries(rest, max, [e | acc], count + 1, node or e.id == 0xFFFFFFFFFFFFFFFF)
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
            # A nested array carries one more kind byte, which nothing decodes.
            case rest2 do
              <<inner, rest3::binary>> ->
                if known_kind?(inner) and inner not in [12, 33] do
                  {:ok, %{id: id, kind: kind, shape: s, element: %{kind: elem, shape: es}}, rest3}
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

                if lof < hif and resf > 0.0 do
                  {:ok, %{kind: kind, packing: 2, qmin: lof, qmax: hif, qres: resf}, rest2}
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

  defp shape(kind, bin) when kind == 14 or kind == 16 do
    case leb(bin) do
      {:ok, min, rest} ->
        case leb(rest) do
          {:ok, max, rest2} ->
            cond do
              kind == 14 and (min > 0xFFFFFFFF or max > 0xFFFFFFFF or max < min) -> :error
              kind == 16 and max > 0xFFFFFFFF -> :error
              kind == 14 -> {:ok, %{kind: kind, min: min, max: max}, rest2}
              true -> {:ok, %{kind: kind, max: max}, rest2}
            end

          :error ->
            :error
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

  defp leb(_bin, 9, _acc), do: :error

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
  defp f32(bits) do
    <<f::little-float-32>> = <<bits::little-32>>
    f
  end
`
