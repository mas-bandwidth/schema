// THE MESSAGE FORM ON THE ELIXIR LEG (docs/SPEC-TABLES.md §3.3). The message
// form is a batch of bitpacked bodies under one announced vocabulary, and the
// announcement is an ordinary form-`1` file the compiler settles byte for byte.
// When the unit has a fixed root, this file emits both surfaces:
//
//	announce / announce_measure — the constant and its length, not a walk;
//	announce_read — reads the announcement file and fills a TableVocabulary;
//	for_root — binds the resolved vocabulary to one fixed root's record;
//	measure_messages / save_messages / load_messages — the bitpacked batch,
//	form byte 2, over a vocabulary bound to its root, and the per-root
//	<root>_measure_messages / <root>_save_messages / <root>_load_messages.
//
// The batch runtime below reads and writes that form-`2` body. The
// announcement's form-`1` reader is emitted once per unit that declares a
// fixed table, beside BuildVersion. The announcement is a compile-time constant
// of the unit and its vocabulary is a pure function of the build version
// (docs/SPEC-TABLES.md §3.3). A batch is always read and written against one
// root's record: the module's batch verbs act on a vocabulary for_root bound,
// answer :no_root on one no root is bound to, and each fixed root also gets its
// own verbs that bind and act in one call.
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
	"math"
	"math/big"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
	b.WriteString("  MESSAGE VOCABULARY, and whose trailer is those two reserved ids. It is a\n")
	b.WriteString("  compile-time constant of the unit, so `announce/0` and `announce_measure/0`\n")
	b.WriteString("  are the constant and its length and not a walk.\n\n")
	b.WriteString("  `announce_read/2` resolves the vocabulary ONCE into the caller's\n")
	b.WriteString("  `TableMessage` struct — the `TableVocabulary` of §3.3's surface — keeping\n")
	b.WriteString("  the endpoint's build version. A second announcement is `:second_announcement`,\n")
	b.WriteString("  a vocabulary past either bound is `:vocabulary_too_large`, and the two\n")
	b.WriteString("  strict checks — the build version present exactly once at kind 9 eight\n")
	b.WriteString("  bytes wide, the vocabulary present exactly once at kind 14 element kind 6\n")
	b.WriteString("  — answer `:malformed`, never a refusal.\n\n")
	b.WriteString("  A batch is read and written against ONE ROOT'S RECORD. `for_root/2` binds\n")
	b.WriteString("  the resolved vocabulary to a fixed root (`:unknown_root` for any other\n")
	b.WriteString("  name); `measure_messages/2`, `save_messages/2` and `load_messages/2,3`\n")
	b.WriteString("  measure, write and read a form-`2` batch of that root's bodies, and answer\n")
	b.WriteString("  `:no_root` on a vocabulary no root is bound to. Each fixed root also has\n")
	b.WriteString("  `<root>_measure_messages/2`, `<root>_save_messages/2` and\n")
	b.WriteString("  `<root>_load_messages/2,3`, which bind and act in one call. Under this\n")
	b.WriteString("  unit's own build version a field the root does not declare is refused,\n")
	b.WriteString("  and where the announced entry is this build's own, so are a field out of\n")
	b.WriteString("  declaration order and a field at the default the writer elides; a union\n")
	b.WriteString("  field at arm 0 is refused at every build. A read refusal discards the\n")
	b.WriteString("  batch, and a write refusal is `:invalid_value`.\n")
	b.WriteString("  \"\"\"\n\n")
	b.WriteString("  import Bitwise\n\n")
	b.WriteString(exAttrBinary("  @announcement ", elixirBytes(tablewire.Announce(u))))
	fmt.Fprintf(&b, "  @entries_here %d\n", len(tablewire.MessageVocabulary(u)))
	records, unions := messageRecords(u)
	b.WriteString(records)
	b.WriteString("\n")
	// THE ANNOUNCEMENT IS A FILE (§3.3), held to the id table's first-use rule
	// like every other (docs/SPEC-TABLES.md §3; the engine's checkAnnouncement)
	runtime := insertOnce(messageRuntimeBody, "  # A table carrying one id twice is the whole wire malformed (§3).\n", firstUseSource())
	batch := messageBatchRuntimeBody
	if !unions {
		batch = strings.Replace(batch, messageArmSubUnion, "", 1)
		batch = strings.Replace(batch, messageArmRawUnion, "", 1)
		batch = strings.Replace(batch, messageArmKnownUnion, "", 1)
	}
	b.WriteString(messageIntegrity(u, runtime+batch))
	b.WriteString(messageBinding(u))
	b.WriteString(messageWriteRuleRows(u))
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
  def measure_messages(%__MODULE__{announced: true, root: nil}, _values),
    do: {:error, :no_root}

  def measure_messages(%__MODULE__{announced: true} = vocab, values) when is_list(values) do
    n = length(values)

    if n < 1 or n > @batch_max do
      {:error, :batch_too_large}
    else
      case encode_bodies(vocab, values, {0, 0}, own_record(vocab)) do
        {:ok, {_acc, nbits}} -> {:ok, 2 + div(nbits + 7, 8)}
        :error -> {:error, :malformed}
      end
    end
  catch
    # every writer refuses a float that is not finite (docs/SPEC-TABLES.md §3.3)
    :non_finite_float -> {:error, :non_finite_float}
    :invalid_value -> {:error, :invalid_value}
  end

  def measure_messages(_vocab, _values), do: {:error, :no_vocabulary}

  @doc """
  Write ` + "`" + `values` + "`" + ` as one batch (docs/SPEC-TABLES.md §3.3).
  """
  def save_messages(%__MODULE__{announced: true, root: nil}, _values),
    do: {:error, :no_root}

  def save_messages(%__MODULE__{announced: true} = vocab, values) when is_list(values) do
    n = length(values)

    if n < 1 or n > @batch_max do
      {:error, :batch_too_large}
    else
      case encode_bodies(vocab, values, {0, 0}, own_record(vocab)) do
        {:ok, bits} -> {:ok, frame_batch(n, bits)}
        :error -> {:error, :malformed}
      end
    end
  catch
    :non_finite_float -> {:error, :non_finite_float}
    :invalid_value -> {:error, :invalid_value}
  end

  def save_messages(_vocab, _values), do: {:error, :no_vocabulary}

  @doc false
  def rebind_messages(from_vocab, %__MODULE__{announced: true, root: root} = to_vocab, bodies)
      when is_binary(root) and is_list(bodies) do
    indexed = Enum.with_index(to_vocab.entries, 1)
    to_by_raw = Map.new(indexed, fn {e, idx} -> {e.raw, {idx, e}} end)

    to_name_by_id =
      indexed
      |> Enum.filter(fn {e, _idx} -> e.kind == 0 end)
      |> Map.new(fn {e, idx} -> {e.id, {idx, e}} end)

    to_lookup = %{by_raw: to_by_raw, name_by_id: to_name_by_id}
    rules = write_rules()
    rec = own_record(to_vocab)
    {:ok, Enum.map(bodies, &rebind_body(from_vocab, to_vocab, to_lookup, rules, &1, rec))}
  end

  defp rebind_body(_from_vocab, _to_vocab, _to_lookup, _rules, fields, nil), do: fields

  defp rebind_body(from_vocab, to_vocab, to_lookup, rules, fields, rec) do
    present =
      Enum.reduce(fields, %{}, fn {peer_ref, val}, acc ->
        case entry_at(from_vocab, peer_ref) do
          {:ok, pe} -> Map.put(acc, pe.id, val)
          _ -> acc
        end
      end)

    rec
    |> Enum.flat_map(fn {fid, {decl_idx, raw, rule, sub}} ->
      case Map.get(to_lookup.by_raw, raw) do
        {to_ref, te} ->
          case Map.fetch(present, fid) do
            {:ok, val} ->
              val2 =
                rebind_val(
                  from_vocab,
                  to_vocab,
                  to_lookup,
                  rules,
                  te.id,
                  te.kind,
                  Map.get(te, :element),
                  val,
                  rule,
                  sub
                )
                |> pad_rebound_array(from_vocab, to_vocab, to_lookup, rules, te, rule, sub)

              if (te.kind == 15 and val2 == :none) or
                   written_at_default?(to_vocab, {decl_idx, true, rule, sub}, te, val2) do
                []
              else
                [{decl_idx, {to_ref, val2}}]
              end

            :error ->
              case default_omitted_field(from_vocab, to_vocab, to_lookup, rules, te, rule, sub) do
                :elide -> []
                {:ride, val2} -> [{decl_idx, {to_ref, val2}}]
              end
          end

        _ ->
          []
      end
    end)
    |> Enum.sort_by(&elem(&1, 0))
    |> Enum.map(&elem(&1, 1))
  end

  defp pad_rebound_array(items, from_vocab, to_vocab, to_lookup, rules, %{kind: 14, shape: shape, element: elem}, rule, sub)
       when is_list(items) do
    if length(items) < shape.min do
      elem_rule =
        case rule do
          {:counted, _born, er} -> er
          {:fixed, er} -> er
          :fixed_table -> :empty
          _ -> :never
        end

      d = default_elem_val(from_vocab, to_vocab, to_lookup, rules, elem, elem_rule, sub)
      items ++ List.duplicate(d, shape.min - length(items))
    else
      items
    end
  end

  defp pad_rebound_array(val, _from_vocab, _to_vocab, _to_lookup, _rules, _te, _rule, _sub), do: val

  defp default_elem_val(from_vocab, to_vocab, to_lookup, rules, %{kind: 13}, _er, sub),
    do: rebind_body(from_vocab, to_vocab, to_lookup, rules, [], record(sub))

  defp default_elem_val(_from_vocab, _to_vocab, _to_lookup, _rules, %{kind: 15}, _er, _sub),
    do: :none

  defp default_elem_val(_from_vocab, _to_vocab, _to_lookup, _rules, _elem, :zero_ref, _sub), do: 0
  defp default_elem_val(_from_vocab, _to_vocab, _to_lookup, _rules, _elem, {:enum, :none}, _sub), do: 0

  defp default_elem_val(_from_vocab, _to_vocab, to_lookup, _rules, _elem, {:enum, def_id}, _sub) do
    case Map.get(to_lookup.name_by_id, def_id) do
      {def_ref, _} -> def_ref
      _ -> 0
    end
  end

  defp default_elem_val(_from_vocab, _to_vocab, _to_lookup, _rules, _elem, {:same, d}, _sub), do: d
  defp default_elem_val(_from_vocab, _to_vocab, _to_lookup, _rules, _elem, {ftag, bits}, _sub)
       when ftag in [:f32, :f32eq],
       do: f32(bits)

  defp default_elem_val(_from_vocab, _to_vocab, _to_lookup, _rules, _elem, {ftag, bits}, _sub)
       when ftag in [:f64, :f64eq],
       do: f64_of(bits)

  defp default_elem_val(_from_vocab, _to_vocab, _to_lookup, _rules, _elem, _er, _sub), do: 0

  defp default_omitted_field(from_vocab, to_vocab, to_lookup, rules, %{kind: 13}, :empty, sub) do
    case rebind_body(from_vocab, to_vocab, to_lookup, rules, [], record(sub)) do
      [] -> :elide
      body -> {:ride, body}
    end
  end

  defp default_omitted_field(from_vocab, to_vocab, to_lookup, rules, %{kind: 14, shape: shape}, :fixed_table, sub) do
    elem_body = rebind_body(from_vocab, to_vocab, to_lookup, rules, [], record(sub))
    {:ride, List.duplicate(elem_body, shape.max)}
  end

  defp default_omitted_field(from_vocab, to_vocab, to_lookup, rules, %{kind: 14}, {:counted, born, :empty}, sub)
       when born > 0 do
    case rebind_body(from_vocab, to_vocab, to_lookup, rules, [], record(sub)) do
      [] -> :elide
      elem_body -> {:ride, List.duplicate(elem_body, born)}
    end
  end

  defp default_omitted_field(from_vocab, to_vocab, to_lookup, rules, %{kind: 16}, {:keyed, keys, :empty}, sub) do
    case rebind_body(from_vocab, to_vocab, to_lookup, rules, [], record(sub)) do
      [] ->
        :elide

      elem_body ->
        pairs =
          Enum.flat_map(keys, fn kid ->
            case Map.get(to_lookup.name_by_id, kid) do
              {kref, _} -> [{kref, elem_body}]
              _ -> []
            end
          end)

        if pairs == [], do: :elide, else: {:ride, pairs}
    end
  end

  defp default_omitted_field(_from_vocab, _to_vocab, _to_lookup, _rules, _te, _rule, _sub),
    do: :elide

  defp rebind_val(from_vocab, to_vocab, to_lookup, rules, _fid, 13, _elem, val, _rule, sub),
    do: rebind_body(from_vocab, to_vocab, to_lookup, rules, val, record(sub))

  defp rebind_val(from_vocab, to_vocab, to_lookup, rules, fid, 14, elem, val, rule, sub) do
    elem_rule =
      case rule do
        {:counted, _born, er} -> er
        {:fixed, er} -> er
        :fixed_table -> :empty
        _ -> :never
      end

    Enum.map(val, fn item ->
      rebind_val(
        from_vocab,
        to_vocab,
        to_lookup,
        rules,
        fid,
        elem.kind,
        Map.get(elem, :element),
        item,
        elem_rule,
        sub
      )
    end)
  end

  defp rebind_val(_from_vocab, _to_vocab, _to_lookup, _rules, _fid, 15, _elem, x, _rule, _sub) when x in [:none, :unknown_arm],
    do: :none

  defp rebind_val(
         from_vocab,
         to_vocab,
         to_lookup,
         rules,
         _fid,
         15,
         _elem,
         {arm_peer_ref, arm_val},
         _rule,
         sub
       ) do
    with {:ok, ape} <- entry_at(from_vocab, arm_peer_ref),
         true <- arm_known?(sub, ape.id),
         arm_raw when arm_raw != nil <- arm_raw(sub, ape.id),
         {to_arm_ref, ate} <- Map.get(to_lookup.by_raw, arm_raw) do
      asub = arm_sub(sub, ape.id)

      {to_arm_ref,
       rebind_val(
         from_vocab,
         to_vocab,
         to_lookup,
         rules,
         ate.id,
         ate.kind,
         Map.get(ate, :element),
         arm_val,
         :never,
         asub
       )}
    else
      _ -> :none
    end
  end

  defp rebind_val(from_vocab, to_vocab, to_lookup, rules, fid, 16, elem, val, rule, sub) do
    {key_list, elem_rule} =
      case rule do
        {:keyed, ks, er} -> {ks, er}
        _ -> {nil, :never}
      end

    allowed_keys = rules |> Map.get({fid, 16}, %{}) |> Map.get(:member)

    val
    |> Enum.flat_map(fn {key_peer_ref, kv} ->
      with {:ok, kpe} <- entry_at(from_vocab, key_peer_ref),
           true <-
             (if key_list != nil,
                do: kpe.id in key_list,
                else: allowed_keys == nil or MapSet.member?(allowed_keys, kpe.id)),
           {to_key_ref, _} <- Map.get(to_lookup.name_by_id, kpe.id) do
        v2 =
          rebind_val(
            from_vocab,
            to_vocab,
            to_lookup,
            rules,
            fid,
            elem.kind,
            Map.get(elem, :element),
            kv,
            elem_rule,
            sub
          )

        if at_default?(to_vocab, elem_rule, v2) or
             at_default?(to_vocab, elem_rule, as_read(elem, v2)) do
          []
        else
          ord =
            if key_list != nil do
              Enum.find_index(key_list, &(&1 == kpe.id)) || 0
            else
              to_key_ref
            end

          [{ord, {to_key_ref, v2}}]
        end
      else
        _ -> []
      end
    end)
    |> Enum.sort_by(&elem(&1, 0))
    |> Enum.map(&elem(&1, 1))
  end

  defp rebind_val(_from_vocab, _to_vocab, _to_lookup, _rules, _fid, 30, _elem, 0, _rule, _sub),
    do: 0

  defp rebind_val(from_vocab, _to_vocab, to_lookup, rules, fid, 30, _elem, peer_vref, rule, _sub) do
    {allowed, retired} =
      case Map.get(rules, {fid, 30}) do
        %{member: m} = rmap -> {m, Map.get(rmap, :retired)}
        _ ->
          case Map.get(rules, {fid, 14}) do
            %{each_member: m} = rmap -> {m, Map.get(rmap, :each_retired)}
            _ ->
              case Map.get(rules, {fid, 16}) do
                %{keyed_member: m} = rmap -> {m, Map.get(rmap, :keyed_retired)}
                _ -> {nil, nil}
              end
          end
      end

    case entry_at(from_vocab, peer_vref) do
      {:ok, vpe} ->
        cond do
          (allowed == nil or MapSet.member?(allowed, vpe.id)) and
              Map.has_key?(to_lookup.name_by_id, vpe.id) ->
            {to_vref, _} = Map.get(to_lookup.name_by_id, vpe.id)
            to_vref

          retired != nil and MapSet.member?(retired, vpe.id) ->
            case rule do
              {tag, def_id} when tag in [:enum, :never_enum] and is_integer(def_id) ->
                case Map.get(to_lookup.name_by_id, def_id) do
                  {def_ref, _} -> def_ref
                  _ -> 0
                end

              _ ->
                0
            end

          true ->
            0
        end

      _ ->
        0
    end
  end

  defp rebind_val(
         _from_vocab,
         _to_vocab,
         _to_by_id,
         _rules,
         _fid,
         _kind,
         _elem,
         val,
         _rule,
         _sub
       ),
       do: val

  @doc """
  Read one batch into at most ` + "`" + `capacity` + "`" + ` bodies (docs/SPEC-TABLES.md §3.3), against
  the root record ` + "`" + `for_root/2` + "`" + ` bound: a vocabulary with no bound root is
  ` + "`" + `:no_root` + "`" + `.
  """
  def load_messages(vocab, data, capacity \\ @batch_max)

  def load_messages(%__MODULE__{announced: true, root: nil}, _data, _capacity),
    do: {:error, :no_root}

  def load_messages(vocab, data, capacity), do: load_messages_raw(vocab, data, capacity)

  defp load_messages_raw(%__MODULE__{announced: true} = vocab, <<2, m, rest::binary>>, capacity) do
    count = m + 1

    if count > capacity do
      # "M above the caller's capacity on the read side is a refusal by name"
      # and "the reader sets the returned count to the wire's M before it
      # returns the refusal" (docs/SPEC-TABLES.md §3.3).
      {:error, :batch_too_large, count}
    else
      # DAMAGE ANYWHERE IN THE BATCH DISCARDS THE WHOLE BATCH
      # (docs/SPEC-TABLES.md §3.3, §4): no body is delivered and the count is
      # zero, for damage inside a body and for "BYTES AFTER THE PAD" alike.
      r = {bytes_to_int(rest), 0, bit_size(rest)}

      case read_bodies(vocab, r, count, [], own_record(vocab)) do
        {:ok, bodies, r} ->
          if batch_ends_clean?(r), do: {:ok, bodies}, else: {:malformed, 0, []}

        {:malformed, _k, _bodies} ->
          {:malformed, 0, []}
      end
    end
  catch
    # A NaN OR AN INFINITY REFUSES THE BATCH (docs/SPEC-TABLES.md §3.3), the
    # first one the walk meets, and outranks any damage after it: no body is
    # delivered.
    :non_finite_float -> {:error, :non_finite_float}
  end

  defp load_messages_raw(%__MODULE__{announced: true}, <<2>>, _capacity),
    do: {:error, :malformed}

  defp load_messages_raw(%__MODULE__{announced: true}, <<_form, _::binary>>, _capacity),
    do: {:error, :newer_form}

  defp load_messages_raw(%__MODULE__{announced: true}, _data, _capacity),
    do: {:error, :malformed}

  defp load_messages_raw(_vocab, _data, _capacity), do: {:error, :no_vocabulary}

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

  defp encode_bodies(_vocab, [], acc, _rec), do: {:ok, acc}

  defp encode_bodies(vocab, [body | rest], acc, rec) do
    case encode_body(vocab, body, acc, rec) do
      {:ok, acc2} -> encode_bodies(vocab, rest, acc2, rec)
      :error -> :error
    end
  end

  defp encode_body(vocab, body, acc, rec) do
    case encode_fields(vocab, body, acc, rec, -1) do
      {:ok, acc2} -> {:ok, put_bits(acc2, 0, vocab.ref_bits)}
      :error -> :error
    end
  end

  # THE BOUND WRITE HOLDS THE BOUND READ'S RULES (docs/SPEC-TABLES.md §3.3), so
  # no batch this writer emits is one its own reader refuses: a union field at
  # arm 0 is refused at every build, and under this unit's own build version a
  # field the record does not declare, a field out of its declaration order and
  # a field stored at the default the writer elides are refused, each by
  # :invalid_value in every build. Nothing is reordered or dropped.
  defp encode_fields(_vocab, [], acc, _rec, _last), do: {:ok, acc}

  defp encode_fields(vocab, [{ref, value} | rest], acc, rec, last) do
    case entry_at(vocab, ref) do
      {:ok, entry} ->
        check_field(vocab, entry, value)
        check(not (entry.kind == 15 and value == :none))

        decl =
          case write_decl(rec, entry, last, vocab) do
            {:ok, decl} -> decl
            _ -> throw(:invalid_value)
          end

        acc2 = put_bits(acc, ref, vocab.ref_bits)
        elem = Map.get(entry, :element)

        case encode_value(vocab, entry.kind, entry.shape, elem, value, acc2, sub_of(decl)) do
          {:ok, acc3} ->
            check(not written_at_default?(vocab, decl, entry, value))
            encode_fields(vocab, rest, acc3, rec, next_last(decl, last))

          :error ->
            :error
        end

      :error ->
        :error
    end
  end

  defp encode_value(_vocab, 0, _shape, _elem, _value, acc, _sub), do: {:ok, acc}
  defp encode_value(_vocab, 32, _shape, _elem, _value, acc, _sub), do: {:ok, acc}

  defp encode_value(_vocab, 1, _shape, _elem, value, acc, _sub) when value in [0, 1],
    do: {:ok, put_bits(acc, value, 1)}

  defp encode_value(_vocab, kind, shape, _elem, value, acc, _sub)
       when kind in [2, 3, 4, 5, 6, 7, 8, 9, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 36],
       do: encode_scalar(kind, shape, value, acc)

  defp encode_value(_vocab, 10, shape, _elem, value, acc, _sub),
    do: encode_float(shape, value, acc)

  defp encode_value(_vocab, 11, _shape, _elem, value, acc, _sub),
    do: {:ok, put_bits(acc, f64_to_bits(value), 64)}

  defp encode_value(_vocab, 12, shape, _elem, value, acc, _sub),
    do: encode_string(shape, value, acc)

  defp encode_value(vocab, 13, _shape, _elem, value, acc, sub),
    do: encode_body(vocab, value, acc, record(sub))

  defp encode_value(vocab, 14, shape, elem, value, acc, sub),
    do: encode_array(vocab, shape, elem, value, acc, sub)

  defp encode_value(vocab, 15, _shape, _elem, value, acc, sub),
    do: encode_union(vocab, value, acc, sub)

  defp encode_value(vocab, 16, shape, elem, value, acc, sub),
    do: encode_keyed(vocab, shape, elem, value, acc, sub)

  defp encode_value(vocab, 30, _shape, _elem, value, acc, _sub) do
    if enum_ref?(vocab, value), do: {:ok, put_bits(acc, value, vocab.ref_bits)}, else: :error
  end

  defp encode_value(_vocab, _kind, _shape, _elem, _value, _acc, _sub), do: :error

  # A SCALAR THE SHAPE CANNOT CARRY IS REFUSED, NEVER MASKED (docs/SPEC.md §5.1,
  # docs/SPEC-TABLES.md §3.3): a ranged value rides as value - base in its
  # bits, so it must lie in [base, base + 2^bits - 1]; an unranged integer rides
  # at its storage width, so it must lie in that width's signed or unsigned
  # range, the same domain read_scalar returns.
  defp encode_scalar(kind, shape, value, acc) when is_integer(value) do
    bits = scalar_bits(kind, shape)

    raw =
      case shape do
        %{packing: 1, base: base} -> value - base
        _ when kind in [2, 3, 4, 5, 18, 20, 21, 22, 23, 24, 36] -> value + (1 <<< (bits - 1))
        _ -> value
      end

    if raw >= 0 and raw <= bit_mask(bits) do
      {:ok, put_bits(acc, scalar_raw(shape, value, raw, bits), bits)}
    else
      :error
    end
  end

  defp encode_scalar(_kind, _shape, _value, _acc), do: :error

  defp scalar_raw(%{packing: 1}, _value, raw, _bits), do: raw
  defp scalar_raw(_shape, value, _raw, bits), do: mask_bits(value, bits)

  # An enum value is the zero reference or a reference to a name entry: the
  # references read_enum accepts and no other.
  defp enum_ref?(_vocab, 0), do: true

  defp enum_ref?(vocab, ref) when is_integer(ref) do
    case entry_at(vocab, ref) do
      {:ok, entry} -> entry.kind == 0 and entry.id < 0xFFFFFFFFFFFFFFFD
      :error -> false
    end
  end

  defp enum_ref?(_vocab, _ref), do: false

  defp encode_float(%{packing: 0}, value, acc),
    do: {:ok, put_bits(acc, f32_to_bits(value), 32)}

  # A QUANTIZED FLOAT RIDES ITS STEP INDEX (docs/SPEC-TABLES.md §3.3,
  # SPEC.md §4.3): narrow to float32, refuse a value that overflows the width
  # and a finite value outside [min, max] (invalid_value, refused whole), then
  # quantize by the packet wire's rule, in float32 at every step.
  defp encode_float(%{packing: 2} = shape, value, acc) do
    case round_f32(value) do
      :nonfinite -> throw(:non_finite_float)
      v32 when v32 < shape.qmin or v32 > shape.qmax -> throw(:invalid_value)
      v32 -> {:ok, put_bits(acc, quantize(shape, v32), shape.bits)}
    end
  end

  defp encode_float(_shape, _value, _acc), do: :error

  # A text writes only what its entry announces, in every build (docs/SPEC.md
  # §5.1 rows 4 and 8): well-formed UTF-8, no zero byte, and at most the
  # entry's max bytes, so its length fits the count and frames what follows.
  # Anything else refuses the batch and nothing is written.
  defp encode_string(shape, value, acc) do
    if is_binary(value) and byte_size(value) <= shape.max and String.valid?(value) and
         :binary.match(value, <<0>>) == :nomatch do
      acc2 = put_bits(acc, byte_size(value), bit_length(shape.max))
      {:ok, put_bytes(align_write(acc2), value)}
    else
      :error
    end
  end

  defp encode_array(vocab, shape, elem, value, acc, sub) do
    # a count off the announced shape's min..max refuses and is never masked
    # into the count's width (docs/SPEC.md §5.1, row 3)
    count = length(value)
    check(count >= shape.min and count <= shape.max)
    acc2 = put_bits(acc, count - shape.min, bit_length(shape.max - shape.min))
    acc3 = if elem.kind == 6, do: align_write(acc2), else: acc2
    encode_elements(vocab, elem, value, acc3, sub)
  end

  defp encode_elements(_vocab, _elem, [], acc, _sub), do: {:ok, acc}

  defp encode_elements(vocab, elem, [v | rest], acc, sub) do
    case encode_value(vocab, elem.kind, elem.shape, Map.get(elem, :element), v, acc, sub) do
      {:ok, acc2} -> encode_elements(vocab, elem, rest, acc2, sub)
      :error -> :error
    end
  end

  # An enum-keyed array (docs/SPEC-TABLES.md §3.2, §3.3): the present-slot
  # count at bits_required(0, slots), then per slot a KEY REFERENCE and the
  # element. A count past the announced slots, a key that names no kind-0 entry
  # (a variant name), and a key that names one twice each refuse the batch in
  # every build (docs/SPEC-TABLES.md §3.2, §3.3, §5.1). No align: a keyed body
  # never aligns, whatever its element kind.
  defp encode_keyed(vocab, shape, elem, value, acc, sub) do
    count = length(value)
    check(count <= shape.max)
    acc2 = put_bits(acc, count, bit_length(shape.max))
    encode_keyed_slots(vocab, elem, value, acc2, MapSet.new(), sub)
  end

  defp encode_keyed_slots(_vocab, _elem, [], acc, _seen, _sub), do: {:ok, acc}

  defp encode_keyed_slots(vocab, elem, [{key_ref, value} | rest], acc, seen, sub) do
    case entry_at(vocab, key_ref) do
      {:ok, entry} ->
        check(
          entry.kind == 0 and entry.id < 0xFFFFFFFFFFFFFFFD and not MapSet.member?(seen, entry.id)
        )

        acc2 = put_bits(acc, key_ref, vocab.ref_bits)
        inner = Map.get(elem, :element)

        case encode_value(vocab, elem.kind, elem.shape, inner, value, acc2, sub) do
          {:ok, acc3} ->
            encode_keyed_slots(vocab, elem, rest, acc3, MapSet.put(seen, entry.id), sub)

          :error ->
            :error
        end

      :error ->
        :error
    end
  end

  defp encode_union(vocab, :none, acc, _sub), do: {:ok, put_bits(acc, 0, vocab.ref_bits)}

  defp encode_union(vocab, {ref, value}, acc, sub) do
    acc2 = put_bits(acc, ref, vocab.ref_bits)

    case entry_at(vocab, ref) do
      {:ok, entry} ->
        check_field(vocab, entry, value)
        elem = Map.get(entry, :element)
        encode_value(vocab, entry.kind, entry.shape, elem, value, acc2, arm_sub(sub, entry.id))

      :error ->
        :error
    end
  end

  # THE WRITE CONTRACT (docs/SPEC.md §5.1). Elixir has no compile-out assert,
  # so save_messages and measure_messages refuse, in every build, a value its
  # own declaration does not admit: an integer off its declared range or past
  # its storage (an integer array's elements too), a count off its shape, a
  # text past its bound, ill-formed or holding a zero byte, an enum reference
  # that names no member and a union reference that names no arm. Nothing is
  # masked into the width. The rules are this unit's own declarations, emitted
  # by the compiler as write_rule_rows/0 and keyed by the {id, kind} a
  # vocabulary entry carries; a field no declaration of this unit names
  # carries no rule.
  defp check_field(vocab, entry, value) do
    case Map.get(write_rules(), {entry.id, entry.kind}) do
      %{min: lo, max: hi} ->
        check(is_integer(value) and value >= lo and value <= hi)

      %{each_min: lo, each_max: hi} ->
        check(is_list(value) and Enum.all?(value, &(is_integer(&1) and &1 >= lo and &1 <= hi)))

      %{keyed_each_min: lo, keyed_each_max: hi} ->
        check(
          is_list(value) and
            Enum.all?(
              value,
              &(is_tuple(&1) and is_integer(elem(&1, 1)) and elem(&1, 1) >= lo and
                  elem(&1, 1) <= hi)
            )
        )

      %{text: max} ->
        check(is_binary(value) and byte_size(value) <= max and text_ok?(value))

      %{member: members} ->
        check(named?(vocab, members, value))

      %{each_member: members} ->
        check(is_list(value) and Enum.all?(value, &named?(vocab, members, &1)))

      %{keyed_member: members} ->
        check(
          is_list(value) and
            Enum.all?(value, &(is_tuple(&1) and named?(vocab, members, elem(&1, 1))))
        )

      %{arm: arms} ->
        check(value == :none or (is_tuple(value) and named?(vocab, arms, elem(value, 0))))

      _ ->
        :ok
    end
  end

  defp check(true), do: :ok
  defp check(false), do: throw(:invalid_value)

  defp text_ok?(text), do: String.valid?(text) and :binary.match(text, <<0>>) == :nomatch

  # an enum rides as the reference of its member's name, zero being None; a
  # union as the reference of its arm
  defp named?(_vocab, _ids, 0), do: true

  defp named?(vocab, ids, ref) when is_integer(ref) do
    case entry_at(vocab, ref) do
      {:ok, entry} -> MapSet.member?(ids, entry.id)
      :error -> false
    end
  end

  defp named?(_vocab, _ids, _ref), do: false

  defp write_rules do
    case :persistent_term.get({__MODULE__, :write_rules}, nil) do
      nil ->
        rules =
          Enum.reduce(write_rule_rows(), %{}, fn {id, kind, what, v}, acc ->
            Map.update(acc, {id, kind}, add_rule(%{}, what, v), &add_rule(&1, what, v))
          end)

        :persistent_term.put({__MODULE__, :write_rules}, rules)
        rules

      rules ->
        rules
    end
  end

  defp add_rule(rule, what, v)
       when what in [:member, :retired, :each_member, :each_retired, :keyed_member, :keyed_retired, :arm],
       do: Map.update(rule, what, MapSet.new([v]), &MapSet.put(&1, v))

  defp add_rule(rule, what, v), do: Map.put(rule, what, v)

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

  defp read_bodies(_vocab, r, 0, acc, _rec), do: {:ok, Enum.reverse(acc), r}

  defp read_bodies(vocab, r, n, acc, rec) do
    case read_body(vocab, r, [], MapSet.new(), 0, rec, -1) do
      {:ok, body, rest} -> read_bodies(vocab, rest, n - 1, [body | acc], rec)
      :malformed -> {:malformed, length(acc), Enum.reverse(acc)}
    end
  end

  # A BODY HAS EXACTLY ONE ENCODING FOR ITS VALUE (§3.3), judged where the walk
  # first meets each spelling, against the record the bound root reaches (held
  # at every build version, because the reader still needs to know which
  # fields its record declares; under another build version the sender may have
  # lawfully reordered, added or removed fields: §3.3, §4):
  #
  #   * a field id twice in one body is damage, named or not;
  #   * a union field written with arm reference 0 is damage at every build
  #     (§3.3 (c)); a None element of an array of unions and a nested arm's
  #     None are positional and read;
  #   * under this reader's own build version, a field the record does not
  #     declare is damage, and where the field's announced entry is this
  #     reader's own byte for byte, a field out of the record's declaration
  #     order (§3.3 (b)) and a field stored at the default the writer elides
  #     (§3.3 (a)) are damage;
  #   * under another build version an undeclared field is stepped over by its
  #     announced shape and omitted, never appended to the body.
  defp read_body(_vocab, _r, _acc, _seen, depth, _rec, _last) when depth > 128, do: :malformed

  defp read_body(vocab, r, acc, seen, depth, rec, last) do
    case get_bits(r, vocab.ref_bits) do
      {:ok, 0, rest} ->
        {:ok, Enum.reverse(acc), rest}

      {:ok, ref, rest} ->
        # the ENTRY's id, not the position that named it, is the field's identity
        case entry_at(vocab, ref) do
          {:ok, entry} when entry.id < 0xFFFFFFFFFFFFFFFD ->
            elem = Map.get(entry, :element)

            with false <- MapSet.member?(seen, entry.id) do
              case field_decl(rec, entry, last, vocab) do
                :skip ->
                  case skip_value(vocab, entry.kind, entry.shape, elem, rest, depth) do
                    {:ok, rest2} ->
                      read_body(vocab, rest2, acc, MapSet.put(seen, entry.id), depth, rec, last)

                    :malformed ->
                      :malformed
                  end

                {:ok, decl} ->
                  with {:ok, value, rest2} <-
                         read_value(
                           vocab,
                           entry.kind,
                           entry.shape,
                           elem,
                           rest,
                           depth,
                           sub_of(decl)
                         ),
                       false <- entry.kind == 15 and value == :none,
                       false <- stored_at_default?(vocab, decl, value) do
                    seen = MapSet.put(seen, entry.id)
                    last = next_last(decl, last)

                    acc2 =
                      if entry.kind == 15 and value == :unknown_arm,
                        do: acc,
                        else: [{ref, value} | acc]

                    read_body(vocab, rest2, acc2, seen, depth, rec, last)
                  else
                    _ -> :malformed
                  end

                _ ->
                  :malformed
              end
            else
              _ -> :malformed
            end

          _ ->
            :malformed
        end

      :error ->
        :malformed
    end
  end

  # One field's declaration in the bound record: {:ok, {index, exact, rule,
  # sub}} for a declared field, :skip where the reader has no record or the
  # record does not declare the field under another build version, and a
  # refusal for a field the record does not declare at this reader's own build
  # version or, where the entry is exact, one out of order.
  defp field_decl(nil, _entry, _last, _vocab), do: :skip

  defp field_decl(rec, entry, last, vocab) do
    case Map.get(rec, entry.id) do
      nil ->
        if own_build?(vocab), do: :undeclared, else: :skip

      {index, raw, rule, sub} ->
        if own_build?(vocab) do
          exact = raw == Map.get(entry, :raw)

          if exact and index <= last,
            do: :out_of_order,
            else: {:ok, {index, exact, rule, sub}}
        else
          {:ok, {index, false, rule, sub}}
        end
    end
  end

  # The writer holds the record at every build version too, but judges order
  # and defaults only under its own; a field its record does not declare under
  # another build version writes with no record rules, as before.
  defp write_decl(rec, entry, last, vocab) do
    case field_decl(rec, entry, last, vocab) do
      :skip -> {:ok, nil}
      other -> other
    end
  end

  defp next_last({index, true, _rule, _sub}, _last), do: index
  defp next_last(_decl, last), do: last

  defp sub_of({_index, _exact, _rule, sub}), do: sub
  defp sub_of(nil), do: nil

  defp stored_at_default?(_vocab, {_index, true, {:keyed, _keys, _elem_rule}, _sub}, []),
    do: true

  defp stored_at_default?(vocab, {_index, true, {:keyed, keys, elem_rule}, _sub}, value),
    do: keyed_non_canonical?(vocab, keys, elem_rule, value, -1)

  defp stored_at_default?(vocab, {_index, true, rule, _sub}, value),
    do: at_default?(vocab, rule, value)

  defp stored_at_default?(_vocab, _decl, _value), do: false

  defp keyed_non_canonical?(_vocab, _keys, _elem_rule, [], _last), do: false

  defp keyed_non_canonical?(vocab, keys, elem_rule, [{kref, v} | rest], last) do
    case entry_at(vocab, kref) do
      {:ok, ke} ->
        case Enum.find_index(keys, &(&1 == ke.id)) do
          nil ->
            keyed_non_canonical?(vocab, keys, elem_rule, rest, last)

          idx when is_integer(idx) and idx > last ->
            at_default?(vocab, elem_rule, v) or
              keyed_non_canonical?(vocab, keys, elem_rule, rest, idx)

          _ ->
            true
        end

      :error ->
        true
    end
  end

  # The writer checks the value it was handed and the value its reader will
  # see: a quantized float reads back as its step's value.
  defp written_at_default?(vocab, {_index, true, rule, _sub}, entry, value),
    do: at_default?(vocab, rule, value) or at_default?(vocab, rule, as_read(entry, value))

  defp written_at_default?(_vocab, _decl, _entry, _value), do: false

  defp as_read(%{kind: 10, shape: %{packing: 2} = shape}, value), do: dequantized(shape, value)

  defp as_read(%{kind: 14, element: %{kind: 10, shape: %{packing: 2} = shape}}, value),
    do: Enum.map(value, &dequantized(shape, &1))

  defp as_read(_entry, value), do: value

  defp dequantized(shape, value) do
    {delta, count} = quant_facts(shape)
    bits = dequantize_bits(quantize(shape, round_f32(value)), shape.qmin, delta, count)
    <<v::little-float-32>> = <<bits::little-32>>
    v
  end

  # THE WRITER'S ELISION RULE, one rule per declared field, emitted by the
  # compiler beside the record (internal/tablewire messageElides): the value a
  # field holds when a conforming writer leaves it out.
  defp at_default?(_vocab, :empty, value), do: value == []
  defp at_default?(_vocab, {:keyed, _keys, _elem}, value), do: value == []
  defp at_default?(_vocab, :zero_ref, value), do: value == 0 or value == :none
  defp at_default?(_vocab, {:same, d}, value), do: value === d

  defp at_default?(_vocab, {:f32, bits}, value),
    do: is_number(value) and f32_raw(value) == bits

  defp at_default?(_vocab, {:f32eq, bits}, value),
    do: is_number(value) and round_f32(value) == f32(bits)

  defp at_default?(_vocab, {:f64, bits}, value),
    do: is_number(value) and f64_raw(value) == bits

  defp at_default?(_vocab, {:f64eq, bits}, value),
    do: is_number(value) and value == f64_of(bits)

  defp at_default?(_vocab, {:enum, :none}, value), do: value == 0

  defp at_default?(vocab, {:enum, id}, value) when is_integer(value) and value > 0 do
    case entry_at(vocab, value) do
      {:ok, entry} -> entry.id == id
      :error -> false
    end
  end

  defp at_default?(vocab, {:counted, born, elem}, value) when is_list(value) do
    value == [] or
      (born > 0 and length(value) == born and Enum.all?(value, &at_default?(vocab, elem, &1)))
  end

  defp at_default?(vocab, {:fixed, elem}, value) when is_list(value),
    do: Enum.all?(value, &at_default?(vocab, elem, &1))

  defp at_default?(_vocab, _rule, _value), do: false

  defp f32_raw(value) do
    <<bits::little-32>> = <<value::little-float-32>>
    bits
  end

  defp f64_raw(value) do
    <<bits::little-64>> = <<value::little-float-64>>
    bits
  end

  defp f64_of(bits) do
    <<v::little-float-64>> = <<bits::little-64>>
    v
  end

  # the record the bound root reaches, held at every build version so the walk
  # can tell a declared field from one it must step over
  defp own_record(%{root: root}) when is_binary(root), do: record(root)

  defp own_record(_vocab), do: nil

  defp own_build?(%{build_version: @build_version}), do: true
  defp own_build?(_vocab), do: false

  defp record(name) when is_binary(name), do: Map.get(@message_records, name)
  defp record(_sub), do: nil

  defp arm_sub({:union, name}, id) do
    case @message_unions |> Map.get(name, %{}) |> Map.get(id) do
      {_raw, sub} -> sub
      _ -> nil
    end
  end
  defp arm_sub(_sub, _id), do: nil

  defp arm_raw({:union, name}, id) do
    case @message_unions |> Map.get(name, %{}) |> Map.get(id) do
      {raw, _sub} -> raw
      _ -> nil
    end
  end
  defp arm_raw(_sub, id), do: if(id < 0, do: 0, else: nil)

  defp arm_known?({:union, name}, id),
    do: @message_unions |> Map.get(name, %{}) |> Map.has_key?(id)

  defp arm_known?(_sub, id), do: id >= 0

  # An undeclared field under another build version is stepped over by its
  # announced shape and omitted (docs/SPEC-TABLES.md §3.3, §4). Its payload is
  # read exactly as a kept value would be, so damage stays damage, but nothing
  # lands in the body; a union field at arm 0 is still refused where the walk
  # meets it, whatever the build.
  defp skip_value(vocab, kind, shape, elem, r, depth) do
    case read_value(vocab, kind, shape, elem, r, depth, nil) do
      {:ok, :none, _rest} when kind == 15 -> :malformed
      {:ok, _value, rest} -> {:ok, rest}
      :malformed -> :malformed
    end
  end

  defp read_value(_vocab, _kind, _shape, _elem, _r, depth, _sub) when depth > 128,
    do: :malformed

  defp read_value(vocab, 13, _shape, _elem, r, depth, sub),
    do: read_body(vocab, r, [], MapSet.new(), depth + 1, record(sub), -1)

  defp read_value(vocab, 14, shape, elem, r, depth, sub),
    do: read_array(vocab, shape, elem, r, depth, sub)

  defp read_value(vocab, 15, _shape, _elem, r, depth, sub), do: read_union(vocab, r, depth, sub)

  defp read_value(vocab, 16, shape, elem, r, depth, sub),
    do: read_keyed(vocab, shape, elem, r, depth, sub)

  defp read_value(vocab, kind, shape, elem, r, _depth, _sub),
    do: read_value(vocab, kind, shape, elem, r)

  defp read_value(_vocab, 0, _shape, _elem, r), do: {:ok, :name, r}
  defp read_value(_vocab, 32, _shape, _elem, r), do: {:ok, :none, r}

  defp read_value(_vocab, 1, _shape, _elem, r), do: get_bits(r, 1)

  defp read_value(_vocab, kind, shape, _elem, r)
       when kind in [2, 3, 4, 5, 6, 7, 8, 9, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 36],
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
      {:ok, raw, rest} ->
        case scalar_value(kind, shape, raw) do
          :malformed -> :malformed
          value -> {:ok, value, rest}
        end

      :error ->
        :malformed
    end
  end

  defp read_float(%{packing: 0}, r) do
    case get_bits(r, 32) do
      {:ok, v, rest} -> {:ok, f32_from_bits(v), rest}
      :error -> :malformed
    end
  end

  # A QUANTIZED INDEX ABOVE ITS COUNT IS DAMAGE (§3.3, SPEC.md §4.3), a
  # skipped field's too: the width spells such an index whenever the count is
  # not one less than a power of two, and the value it names is held to the
  # finite rule a raw payload is held to.
  defp read_float(%{packing: 2} = shape, r) do
    {delta, count} = quant_facts(shape)

    case get_bits(r, bit_length(count)) do
      {:ok, index, rest} when index <= count ->
        bits = dequantize_bits(index, shape.qmin, delta, count)

        if (bits >>> 23 &&& 0xFF) == 0xFF do
          :malformed
        else
          <<v::little-float-32>> = <<bits::little-32>>
          {:ok, v, rest}
        end

      _ ->
        :malformed
    end
  end

  defp read_float(_shape, _r), do: :malformed

  # The step count and delta derive from the announced triple exactly as
  # ir.TableMessageQuantization derives them, in float32.
  defp quant_facts(%{qmin: lo, qmax: hi, qres: res}) do
    delta = round_f32(hi - lo)
    values = round_f32(delta / res)

    values =
      cond do
        values == :nonfinite -> 1.0
        not (values >= 1) -> 1.0
        values > 4_294_967_040.0 -> 4_294_967_040.0
        true -> values
      end

    {delta, trunc(Float.ceil(values))}
  end

  # ILL-FORMED TEXT IS DAMAGE, a skipped field's too (§3.3, §4): the bytes
  # number at most the declared max and are well-formed UTF-8 with no NUL, or
  # the batch refuses
  defp read_string(shape, r) do
    with {:ok, n, rest} <- get_bits(r, bit_length(shape.max)),
         {:ok, rest2} <- align(rest),
         {:ok, bytes, rest3} <- take_bytes(rest2, n),
         true <- n <= shape.max,
         true <- text_ok?(bytes) do
      {:ok, bytes, rest3}
    else
      _ -> :malformed
    end
  end

  # THE COUNT'S BITS CAN SPELL MORE THAN max - min (§3.3), so a count above
  # the announced maximum is damage, a skipped field's too.
  defp read_array(vocab, shape, elem, r, depth, sub) do
    with {:ok, offset, rest} <- get_bits(r, bit_length(shape.max - shape.min)),
         n = shape.min + offset,
         true <- n <= shape.max,
         {:ok, rest2} <- maybe_align(elem, rest),
         next_depth = elem_depth(elem, depth),
         {:ok, list, rest3} <- read_elements(vocab, shape, elem, n, rest2, [], next_depth, sub) do
      {:ok, Enum.map(list, &if(&1 == :unknown_arm, do: :none, else: &1)), rest3}
    else
      _ -> :malformed
    end
  end

  # AN ENUM-KEYED ARRAY (§3.2): the number of present slots, then one
  # (key reference, element) pair per slot. A key that names no declared
  # no-payload entry, and one that names the same entry twice, is damage
  # (§3.3), a skipped field's too.
  defp read_keyed(vocab, shape, elem, r, depth, sub) do
    case get_bits(r, bit_length(shape.max)) do
      {:ok, n, rest} when n <= shape.max ->
        read_keyed_pairs(vocab, elem, n, rest, [], MapSet.new(), elem_depth(elem, depth), sub)

      _ ->
        :malformed
    end
  end

  defp read_keyed_pairs(_vocab, _elem, 0, r, acc, _seen, _depth, _sub),
    do: {:ok, Enum.reverse(acc), r}

  defp read_keyed_pairs(vocab, elem, n, r, acc, seen, depth, sub) do
    with {:ok, ref, rest} <- get_bits(r, vocab.ref_bits),
         {:ok, entry} <- entry_at(vocab, ref),
         true <- entry.kind == 0,
         true <- entry.id < 0xFFFFFFFFFFFFFFFD,
         false <- MapSet.member?(seen, entry.id) do
      seen = MapSet.put(seen, entry.id)
      inner = Map.get(elem, :element)

      case read_value(vocab, elem.kind, elem.shape, inner, rest, depth, sub) do
        {:ok, value, rest2} ->
          v2 = if value == :unknown_arm, do: :none, else: value
          read_keyed_pairs(vocab, elem, n - 1, rest2, [{ref, v2} | acc], seen, depth, sub)

        :malformed ->
          :malformed
      end
    else
      _ -> :malformed
    end
  end

  defp elem_depth(%{kind: kind}, depth) when kind in [12, 13, 14, 15, 16, 17, 30], do: depth + 1
  defp elem_depth(_elem, depth), do: depth

  defp maybe_align(%{kind: 6}, r), do: align(r)
  defp maybe_align(_elem, r), do: {:ok, r}

  defp read_elements(_vocab, _shape, _elem, 0, r, acc, _depth, _sub),
    do: {:ok, Enum.reverse(acc), r}

  # A zero-width element leaves the cursor where it stood: kind 0, kind 32, a
  # ranged scalar whose bit width is 0, or a nested run of those. Looping on
  # the count would allocate a list in proportion to the announcement and
  # consume no input. This reader returns the list, so zero_width_run holds
  # every fixed-width zero run to the bits the batch still holds
  # (internal/tablewire/messagedecode.go) before any cell is allocated. A
  # WALKED element that consumes no bits is damage, even where the remaining
  # count could fit in the batch's padding.
  defp read_elements(vocab, shape, elem, n, r, acc, depth, sub) when is_map(elem) and n > 0 do
    case read_value(vocab, elem.kind, elem.shape, Map.get(elem, :element), r, depth, sub) do
      {:ok, v, ^r} ->
        if element_run_bits(elem) < 0 do
          :malformed
        else
          zero_width_run(vocab, shape, n, r, acc, v)
        end

      {:ok, v, rest} ->
        read_elements(vocab, shape, elem, n - 1, rest, [v | acc], depth, sub)

      :malformed ->
        :malformed
    end
  end

  defp read_elements(_vocab, _shape, _elem, n, _r, _acc, _depth, _sub) when n > 0,
    do: :malformed

  defp zero_width_run(_vocab, _shape, n, r, acc, v) do
    # The shape is the writer's announcement, whatever its minimum and maximum:
    # a run the batch's bits cannot back is damage
    # (internal/tablewire/messagedecode.go). A run the bits back is
    # materialized, and the uint32 ceiling on the announced shape is untouched.
    if n > bits_left(r) do
      :malformed
    else
      {:ok, Enum.reverse(acc, List.duplicate(v, n)), r}
    end
  end

  # THE ELEMENT'S FIXED WIDTH WHERE IT IS ONE THIS READER CAN MULTIPLY, and -1
  # where it must be walked (§3.3): a table, union, enum, pointer or nested
  # array resolves something, a string finds a length, and a float or a
  # ranged/quantized scalar carries a value condition the wire decides. A fixed
  # width is stepped by its width, including zero, and is not walked; a walked
  # element must move the cursor on every step.
  defp element_run_bits(%{kind: kind, shape: shape}) do
    cond do
      kind in [0, 32] ->
        0

      kind in [10, 11, 12, 13, 14, 15, 16, 17, 30] ->
        -1

      kind in [1, 2, 3, 4, 5, 6, 7, 8, 9, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 36] ->
        width = scalar_bits(kind, shape)
        if width > 0 and shape.packing in [1, 2], do: -1, else: width

      true ->
        -1
    end
  end

  defp bits_left({_int, off, total}) when total > off, do: total - off
  defp bits_left({_int, _off, _total}), do: 0

  defp read_union(vocab, r, depth, sub) do
    case get_bits(r, vocab.ref_bits) do
      {:ok, 0, rest} ->
        {:ok, :none, rest}

      {:ok, ref, rest} ->
        case entry_at(vocab, ref) do
          {:ok, entry} when entry.kind != 0 and entry.id < 0xFFFFFFFFFFFFFFFD ->
            elem = Map.get(entry, :element)
            known = arm_known?(sub, entry.id)
            arm = if known, do: arm_sub(sub, entry.id), else: nil

            case read_value(vocab, entry.kind, entry.shape, elem, rest, depth + 1, arm) do
              {:ok, value, rest2} ->
                if not known,
                  do: {:ok, :unknown_arm, rest2},
                  else: {:ok, {ref, if(entry.kind == 15 and value == :unknown_arm, do: :none, else: value)}, rest2}

              :malformed ->
                :malformed
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

  # A RANGED VALUE'S OFFSET CAN SPELL A VALUE PAST ITS KIND (§3.3), a skipped
  # field's too: base + offset must land in the domain the kind declares.
  defp scalar_value(kind, %{packing: 1, base: base}, raw) do
    value = base + raw
    if kind_holds?(kind, value), do: value, else: :malformed
  end

  defp scalar_value(kind, _shape, raw) when kind in [2, 3, 4, 5, 18, 20, 21, 22, 23, 24, 36],
    do: sign_extend(raw, kind_bits(kind))

  defp scalar_value(_kind, _shape, raw), do: raw

  defp kind_holds?(kind, value) when kind in [2, 3, 4, 5, 18, 20, 21, 22, 23, 24, 36] do
    bits = kind_bits(kind)
    value >= -(1 <<< (bits - 1)) and value <= (1 <<< (bits - 1)) - 1
  end

  defp kind_holds?(kind, value) do
    bits = kind_bits(kind)
    bits > 0 and value >= 0 and value <= (1 <<< bits) - 1
  end

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
            integrity: false,
            root: nil,
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
      {:ok, body, ids} ->
        # an id table out of first-use order is the whole wire malformed (§3),
        # judged before the build version is read, as the engine does
        if first_use_order?(body, ids),
          do: walk_body(body, ids, vocab),
          else: {:error, :malformed, %{vocab | refused: true}}

      {:error, reason} ->
        {:error, reason, %{vocab | refused: true}}
    end
  end

  defp walk_body(body, ids, vocab) do
    case walk(body, ids, 0, 0, 0, nil, vocab.max_bytes, MapSet.new()) do
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

    # §3, THE DISTINCTNESS CHECK: an entry count above 4096 is malformed
    # whatever the buffer would hold
    cond do
      count > 4096 or count > div(size - 9, 8) ->
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
  defp walk(bin, ids, versions, words_count, build_version, words, max_bytes, seen) do
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
            if MapSet.member?(seen, id) do
              {:malformed, build_version}
            else
              seen = MapSet.put(seen, id)
              case rest do
                <<kind, rest2::binary>> ->
                  cond do
                    id == 0xFFFFFFFFFFFFFFFE ->
                      case rest2 do
                        <<bv::little-unsigned-64, rest3::binary>> when kind == 9 ->
                          walk(rest3, ids, versions + 1, words_count, bv, words, max_bytes, seen)

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
                              max_bytes,
                              seen
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
                          walk(rest3, ids, versions, words_count, build_version, words, max_bytes, seen)

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

      kind in [5, 9, 11, 23, 28, 36] ->
        take(8, bin)

      kind in [18, 19, 24, 29] ->
        take(16, bin)

      kind in [17, 30] ->
        leb_rest(bin)

      kind in [12, 13, 14, 16, 31, 32] ->
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
  # one entry twice, by everything the entry decodes to
  defp duplicate?(e, [x | rest]), do: same_entry?(e, x) or duplicate?(e, rest)

  defp same_entry?(e, x) do
    e.id == x.id and e.kind == x.kind and e.shape == x.shape and
      Map.get(e, :element) == Map.get(x, :element)
  end

  defp ref_bits(0), do: 1
  defp ref_bits(n), do: ref_bits(n, 1)
  defp ref_bits(n, b) when n >= 2, do: ref_bits(n >>> 1, b + 1)
  defp ref_bits(_n, b), do: b

  # ---- one entry -----------------------------------------------------------------

  # An entry keeps its own bytes as :raw, behind a leading 1 as one integer:
  # the bound read judges a field's order and default only where its announced
  # entry is this reader's own, byte for byte (§3.3).
  defp entry(bin) do
    case entry_shape(bin) do
      {:ok, e, rest} ->
        bytes = binary_part(bin, 0, byte_size(bin) - byte_size(rest))
        {:ok, Map.put(e, :raw, :binary.decode_unsigned(<<1, bytes::binary>>)), rest}

      :error ->
        :error
    end
  end

  defp entry_shape(<<id::little-unsigned-64, kind, rest::binary>>) do
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

  defp entry_shape(_), do: :error

  defp read_element(kind, id, s, <<elem, rest::binary>>) do
    if not known_kind?(elem) or elem in [14, 16] do
      :error
    else
      case shape(elem, rest) do
        {:ok, es, rest2} ->
          {:ok, %{id: id, kind: kind, shape: s, element: %{kind: elem, shape: es}}, rest2}

        :error ->
          :error
      end
    end
  end

  defp read_element(_kind, _id, _s, _), do: :error

  defp known_kind?(kind), do: (kind >= 0 and kind <= 32) or kind == 36

  defp base_inside_kind?(kind, base) do
    dom = kind_bits(kind)
    if kind in [2, 3, 4, 5, 18, 20, 21, 22, 23, 24] do
      mn = -(1 <<< (dom - 1))
      mx = (1 <<< (dom - 1)) - 1
      base >= mn and base <= mx
    else
      base >= 0 and base <= (1 <<< dom) - 1
    end
  end

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
      36 -> 64
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
                        # A signed kind's sixteen-byte base is two's complement
                        # (docs/SPEC-TABLES.md §3.3): int128 and the signed
                        # fixed-point widths announce a base below zero.
                        base = hi <<< 64 ||| lo

                        base =
                          if kind in [18, 20, 21, 22, 23, 24],
                            do: sign_extend(base, 128),
                            else: base

                        if base_inside_kind?(kind, base) do
                          {:ok, %{kind: kind, packing: 1, bits: n, base: base}, rest3}
                        else
                          :error
                        end

                      _ ->
                        :error
                    end
                  else
                    case leb(rest2) do
                      {:ok, raw, rest3} ->
                        base = if kind >= 2 and kind <= 5, do: zigzag(raw), else: raw
                        if base_inside_kind?(kind, base) do
                          {:ok, %{kind: kind, packing: 1, bits: n, base: base}, rest3}
                        else
                          :error
                        end

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

                  if delta != :nonfinite do
                    values = round_f32(delta / resf)

                    if values != :nonfinite do
                      # the step count and the index width derive from the
                      # triple exactly as the packet wire derives them (SPEC.md
                      # §4.3): the count clamps to [1, 2^32 - 256]
                      values = if values < 1.0, do: 1.0, else: values
                      values = if values > 4_294_967_040.0, do: 4_294_967_040.0, else: values
                      count = ceil(values)

                      {:ok,
                       %{
                         kind: kind,
                         packing: 2,
                         qmin: lof,
                         qmax: hif,
                         qres: resf,
                         count: count,
                         bits: bit_length(count)
                       }, rest2}
                    else
                      :error
                    end
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

  defp shape(kind, bin) when kind == 12 do
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

  # The compressed float's step-index arithmetic (docs/SPEC-TABLES.md §3.3,
  # SPEC.md §4.3): the index a value takes, in float32 at every step, never
  # fused, never widened. The announcement already held the triple to the
  # packet wire's own rule, so an in-range value quantizes without overflow;
  # the clamp to [0, 1] absorbs only the float32 rounding at the ends.
  defp quantize(%{qmin: qmin, qmax: qmax, count: count}, value) do
    delta = round_f32(qmax - qmin)
    count32 = round_f32(count * 1.0)
    normalized = round_f32(round_f32(value - qmin) / delta)
    normalized = if normalized < 0.0, do: 0.0, else: normalized
    normalized = if normalized > 1.0, do: 1.0, else: normalized
    min(floor(round_f32(round_f32(normalized * count32) + 0.5)), count)
  end

`

// messageRecords emits THE RECORDS THE BOUND BATCH HOLDS (docs/SPEC-TABLES.md
// §3.3 (a), (b)): this unit's build version, and per record of the message
// closure its declared fields by id, each {declaration index, the field's
// encoded vocabulary entry, the writer's elision rule, what a body under it
// is read against}; and per union the same last fact for each arm. The rule
// is internal/tablewire's messageElides spelled over the batch's own values:
//
//	:never                the field is never judged at a default (a `?T`, a
//	                      `| required`, a union, whose None is refused where it
//	                      is read, a fixed array of tables)
//	:empty                a nested table or an enum-keyed array: elides empty
//	{:same, v}            an integer, a bool, a text or a bytes at its default
//	{:f32, bits} {:f64, bits}      a plain float, compared by bit pattern
//	{:f32eq, bits} {:f64eq, bits}  a ranged float, compared by value
//	{:enum, id | :none}   an enum naming its default variant
//	{:counted, born, e}   no element, or the born count all at e
//	{:fixed, e}           every element at e
//	:zero_ref             an enum or a union element at reference 0
func messageRecords(u *ir.Unit) (string, bool) {
	closure := tablewire.MessageClosure(u)
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	unions := map[string]*ir.Union{}
	var records []string
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		var fields []string
		for i, f := range st.Fields {
			e := ir.TableFieldEntry(f)
			// the entry's bytes behind a leading 1, as one integer (entry/1's :raw)
			fields = append(fields, fmt.Sprintf("0x%016X => {%d, 0x01%X, %s, %s}", e.Id, i, e.Encode(nil), messageFieldRule(f), messageSub(f, unions)))
		}
		records = append(records, fmt.Sprintf("%q => %%{%s}", name, strings.Join(fields, ", ")))
	}
	// every union a record or an arm reaches, its arms' bodies too
	done := map[string]bool{}
	var unionRows []string
	for len(done) < len(unions) {
		var pending []string
		for name := range unions {
			if !done[name] {
				pending = append(pending, name)
			}
		}
		sort.Strings(pending)
		for _, name := range pending {
			done[name] = true
			var arms []string
			for _, v := range unions[name].Variants {
				sub := "nil"
				if v.F != nil {
					sub = messageSub(v.F, unions)
				}
				ae := ir.TableArmEntry(v)
				arms = append(arms, fmt.Sprintf("0x%016X => {0x01%X, %s}", ae.Id, ae.Encode(nil), sub))
			}
			unionRows = append(unionRows, fmt.Sprintf("%q => %%{%s}", name, strings.Join(arms, ", ")))
		}
	}
	sort.Strings(unionRows)
	var b strings.Builder
	b.WriteString("\n  # this unit's own build version and records, as its message writer lays\n")
	b.WriteString("  # them out (docs/SPEC-TABLES.md §3.3 (a), (b)): the bound batch's rules\n")
	fmt.Fprintf(&b, "  @build_version 0x%016X\n", ir.BuildVersion(u))
	b.WriteString(exAttribute("message_records", "%{"+strings.Join(records, ", ")+"}"))
	// a unit no union reaches carries no union table and no union clause of
	// arm_sub/2: an empty map literal there is a type warning on the BEAM
	if len(unionRows) > 0 {
		b.WriteString(exAttribute("message_unions", "%{"+strings.Join(unionRows, ", ")+"}"))
	}
	return b.String(), len(unionRows) > 0
}

// messageArmSubUnion is arm_sub/2's union clause in messageBatchRuntimeBody,
// dropped from a unit no union reaches (messageRecords).
const messageArmSubUnion = "  defp arm_sub({:union, name}, id) do\n    case @message_unions |> Map.get(name, %{}) |> Map.get(id) do\n      {_raw, sub} -> sub\n      _ -> nil\n    end\n  end\n"
const messageArmRawUnion = "  defp arm_raw({:union, name}, id) do\n    case @message_unions |> Map.get(name, %{}) |> Map.get(id) do\n      {raw, _sub} -> raw\n      _ -> nil\n    end\n  end\n"
const messageArmKnownUnion = "  defp arm_known?({:union, name}, id),\n    do: @message_unions |> Map.get(name, %{}) |> Map.has_key?(id)\n\n"

// messageSub is what a body under field f is read against: a record's name, a
// union's arms by name, or nil.
func messageSub(f *ir.Field, unions map[string]*ir.Union) string {
	if f.Type.Kind != ir.TNamed || f.Type.Pointer || f.IsMap() {
		return "nil"
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return fmt.Sprintf("%q", ref.Name)
	case *ir.Union:
		unions[ref.Name] = ref
		return fmt.Sprintf("{:union, %q}", ref.Name)
	}
	return "nil"
}

// messageFieldRule is one field's elision rule, in internal/tablewire
// messageElides's order (see messageRecords for the spellings).
func messageFieldRule(f *ir.Field) string {
	if f.Required || f.Type.Optional || f.Type.Pointer || f.IsMap() {
		if en, ok := f.Type.Ref.(*ir.Enum); ok && f.Type.Kind == ir.TNamed && f.Array == ir.ArrayNone && f.KeyEnum == "" && f.HasDefault && f.DefVariant != "" && f.DefVariant != "None" && slices.Contains(en.Variants, f.DefVariant) {
			return fmt.Sprintf("{:never_enum, 0x%016X}", ir.TableWireId(en.VariantWireNameOf(f.DefVariant)))
		}
		return ":never"
	}
	kind := ir.TableWireScalarKind(f)
	switch {
	case f.KeyEnum != "":
		var keyIDs []string
		for ki := range f.KeyEnumRef.Variants {
			keyIDs = append(keyIDs, fmt.Sprintf("0x%016X", ir.TableWireId(f.KeyEnumRef.VariantWireName(ki))))
		}
		elemRule := messageScalarRule(f)
		switch kind {
		case ir.TableKindTable:
			elemRule = ":empty"
		case ir.TableKindEnum, ir.TableKindUnion:
			elemRule = ":zero_ref"
		}
		return fmt.Sprintf("{:keyed, [%s], %s}", strings.Join(keyIDs, ", "), elemRule)
	case f.Type.Kind == ir.TString:
		return "{:same, <<" + strings.Join(elixirBytes(f.DefBytes), ", ") + ">>}"
	case f.Type.Kind == ir.TBytes:
		return "{:same, [" + strings.Join(elixirBytes(f.DefBytes), ", ") + "]}"
	case f.CountedOnWire():
		// the born slots all at their defaults elide too, except a compressed
		// float's (internal/tablewire array's allBirth)
		born := f.BornCount()
		if f.HasFloatRange {
			born = 0
		}
		elem := messageScalarRule(f)
		switch kind {
		case ir.TableKindTable:
			elem = ":empty"
		case ir.TableKindEnum, ir.TableKindUnion:
			elem = ":zero_ref"
		}
		return fmt.Sprintf("{:counted, %d, %s}", born, elem)
	case f.Array == ir.ArrayFixed && kind == ir.TableKindTable:
		return ":fixed_table"
	case f.Array == ir.ArrayFixed:
		if kind == ir.TableKindUnion {
			return "{:fixed, :zero_ref}"
		}
		return "{:fixed, " + messageScalarRule(f) + "}"
	case kind == ir.TableKindUnion:
		return ":never"
	case kind == ir.TableKindTable:
		return ":empty"
	}
	return messageScalarRule(f)
}

// messageScalarRule is internal/tablewire cellIsDefault over one value of f,
// against the declared default the engine's model would place in the field
// (internal/tabletext fieldDefault), read from the declaration itself.
func messageScalarRule(f *ir.Field) string {
	if en, ok := f.Type.Ref.(*ir.Enum); ok && f.Type.Kind == ir.TNamed {
		if !f.HasDefault || f.DefVariant == "" || f.DefVariant == "None" || !slices.Contains(en.Variants, f.DefVariant) {
			return "{:enum, :none}"
		}
		return fmt.Sprintf("{:enum, 0x%016X}", ir.TableWireId(en.VariantWireNameOf(f.DefVariant)))
	}
	var defFloat float64
	if f.HasDefault {
		defFloat = f.DefFloat
	}
	switch {
	case f.Type.Kind == ir.TBool:
		if f.HasDefault && f.DefBool {
			return "{:same, 1}"
		}
		return "{:same, 0}"
	case f.Type.Kind == ir.TFloat32 && f.HasFloatRange:
		return fmt.Sprintf("{:f32eq, 0x%08X}", math.Float32bits(float32(defFloat)))
	case f.Type.Kind == ir.TFloat32:
		return fmt.Sprintf("{:f32, 0x%08X}", math.Float32bits(float32(defFloat)))
	case f.Type.Kind == ir.TFloat64 && f.HasFloatRange:
		return fmt.Sprintf("{:f64eq, 0x%016X}", math.Float64bits(defFloat))
	case f.Type.Kind == ir.TFloat64:
		return fmt.Sprintf("{:f64, 0x%016X}", math.Float64bits(defFloat))
	}
	i, u := messageDefaultInt(f)
	switch {
	case ir.TableKindWide(ir.TableScalarKind(f)):
		wide := new(big.Int)
		if (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.HasDefault && f.DefInt != nil {
			wide.Set(f.DefInt)
		}
		return "{:same, " + wide.String() + "}"
	case f.Type.Kind == ir.TInt && f.Type.Signed:
		return fmt.Sprintf("{:same, %d}", i)
	}
	return fmt.Sprintf("{:same, %d}", u)
}

// messageDefaultInt is an integer-like field's declared default as the
// engine's model holds it: a signed reading for a signed int or fixed-point
// field or a negative value, the flags mask, and zero where nothing is
// declared (internal/tabletext fieldDefault and intCell).
func messageDefaultInt(f *ir.Field) (int64, uint64) {
	if !f.HasDefault || f.DefInt == nil {
		return 0, 0
	}
	v := f.DefInt
	switch f.Type.Kind {
	case ir.TInt, ir.TFixed, ir.TBits:
		if (f.Type.Kind != ir.TBits && f.Type.Signed) || v.Sign() < 0 {
			return v.Int64(), uint64(v.Int64())
		}
		return int64(v.Uint64()), v.Uint64()
	case ir.TNamed:
		if _, ok := f.Type.Ref.(*ir.Flags); ok {
			return 0, v.Uint64()
		}
	}
	return 0, 0
}

// messageWriteRuleRows emits write_rule_rows/0: the write contract
// (docs/SPEC.md §5.1) of every declaration the message closure reaches, one
// row per {id, kind, rule, value}, which save_messages folds into one rule per
// vocabulary entry. An integer leaf is held to its declared range, else its
// bits(N), else its storage; an integer array's elements the same; a bounded
// string to its N; an enum to its members' ids and a union to its arms' ids.
// Declarations sharing an id and a kind share one entry on the wire, so their
// rules merge to the looser, and a pair whose rules disagree in shape carries
// none rather than refusing a lawful value of either.
func messageWriteRuleRows(u *ir.Unit) string {
	type key struct {
		id   uint64
		kind uint8
	}
	type rule struct {
		what     string // int, each, text, member, each_member, keyed_member, arm
		whatRet  string
		lo, hi   *big.Int
		text     int64
		ids      map[uint64]bool
		retired  map[uint64]bool
		conflict bool
	}
	rules := map[key]*rule{}
	add := func(k key, r *rule) {
		old := rules[k]
		if old == nil {
			rules[k] = r
			return
		}
		if old.conflict || old.what != r.what {
			old.conflict = true
			return
		}
		switch r.what {
		case "int", "each", "keyed_each":
			if r.lo.Cmp(old.lo) < 0 {
				old.lo = r.lo
			}
			if r.hi.Cmp(old.hi) > 0 {
				old.hi = r.hi
			}
		case "text":
			old.text = max(old.text, r.text)
		default:
			for id := range r.ids {
				old.ids[id] = true
			}
			for id := range r.retired {
				if old.retired == nil {
					old.retired = map[uint64]bool{}
				}
				old.retired[id] = true
			}
		}
	}
	fieldRule := func(f *ir.Field) *rule {
		if f.IsMap() || f.Type.Pointer {
			return nil
		}
		if lo, hi, ok := messageIntBounds(f); ok {
			if f.Array != ir.ArrayNone {
				if f.KeyEnum != "" {
					return &rule{what: "keyed_each", lo: lo, hi: hi}
				}
				return &rule{what: "each", lo: lo, hi: hi}
			}
			return &rule{what: "int", lo: lo, hi: hi}
		}
		if en, ok := f.Type.Ref.(*ir.Enum); ok && f.Type.Kind == ir.TNamed {
			ids := map[uint64]bool{}
			retired := map[uint64]bool{}
			for i := range en.Variants {
				wid := ir.TableWireId(en.VariantWireName(i))
				if i < len(en.VariantDeprecated) && en.VariantDeprecated[i] {
					retired[wid] = true
				} else {
					ids[wid] = true
				}
			}
			if f.KeyEnum != "" {
				return &rule{what: "keyed_member", whatRet: "keyed_retired", ids: ids, retired: retired}
			}
			if f.Array != ir.ArrayNone {
				return &rule{what: "each_member", whatRet: "each_retired", ids: ids, retired: retired}
			}
			return &rule{what: "member", whatRet: "retired", ids: ids, retired: retired}
		}
		if f.KeyEnum != "" || f.Array != ir.ArrayNone {
			return nil
		}
		if f.Type.Kind == ir.TString && !f.Type.Blob() && !f.Type.Unbounded && f.Type.Size > 0 {
			return &rule{what: "text", text: f.Type.Size}
		}
		if f.Type.Kind != ir.TNamed {
			return nil
		}
		if ref, ok := f.Type.Ref.(*ir.Union); ok {
			ids := map[uint64]bool{}
			for _, v := range ref.Variants {
				ids[ir.TableArmEntry(v).Id] = true
			}
			return &rule{what: "arm", ids: ids}
		}
		return nil
	}
	seenUnion := map[*ir.Union]bool{}
	fields := func(fs []*ir.Field) {
		for _, f := range fs {
			e := ir.TableFieldEntry(f)
			if r := fieldRule(f); r != nil {
				add(key{e.Id, e.Kind}, r)
			}
			un, ok := f.Type.Ref.(*ir.Union)
			if f.Type.Kind != ir.TNamed || !ok || seenUnion[un] {
				continue
			}
			seenUnion[un] = true
			for _, v := range un.Variants {
				if v.F == nil {
					continue
				}
				arm := ir.TableArmEntry(v)
				if r := fieldRule(v.F); r != nil {
					add(key{arm.Id, arm.Kind}, r)
				}
			}
		}
	}
	closure := tablewire.MessageClosure(u)
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st != nil {
			fields(st.Fields)
		}
	}

	keys := make([]key, 0, len(rules))
	for k, r := range rules {
		if !r.conflict {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].id != keys[j].id {
			return keys[i].id < keys[j].id
		}
		return keys[i].kind < keys[j].kind
	})
	var rows []string
	for _, k := range keys {
		r := rules[k]
		head := fmt.Sprintf("{0x%016X, %d, ", k.id, k.kind)
		switch r.what {
		case "int":
			rows = append(rows, head+":min, "+fixedIntLiteral(r.lo)+"}", head+":max, "+fixedIntLiteral(r.hi)+"}")
		case "each":
			rows = append(rows, head+":each_min, "+fixedIntLiteral(r.lo)+"}", head+":each_max, "+fixedIntLiteral(r.hi)+"}")
		case "keyed_each":
			rows = append(rows, head+":keyed_each_min, "+fixedIntLiteral(r.lo)+"}", head+":keyed_each_max, "+fixedIntLiteral(r.hi)+"}")
		case "text":
			rows = append(rows, fmt.Sprintf("%s:text, %d}", head, r.text))
		default:
			ids := make([]uint64, 0, len(r.ids))
			for id := range r.ids {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			for _, id := range ids {
				rows = append(rows, fmt.Sprintf("%s:%s, 0x%016X}", head, r.what, id))
			}
			if r.whatRet != "" && len(r.retired) > 0 {
				rids := make([]uint64, 0, len(r.retired))
				for id := range r.retired {
					rids = append(rids, id)
				}
				slices.Sort(rids)
				for _, id := range rids {
					rows = append(rows, fmt.Sprintf("%s:%s, 0x%016X}", head, r.whatRet, id))
				}
			}
		}
	}

	var b strings.Builder
	b.WriteString("\n  # this unit's own write rules, one {id, kind, rule, value} row each\n")
	if len(rows) == 0 {
		b.WriteString("  defp write_rule_rows, do: []\n")
		return b.String()
	}
	b.WriteString("  defp write_rule_rows do\n    [\n")
	for i, row := range rows {
		b.WriteString("      " + row)
		if i < len(rows)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("    ]\n  end\n")
	return b.String()
}

// messageIntBounds is an integer leaf's write domain: its declared range,
// else its bits(N), else its storage width. ok is false for anything else.
func messageIntBounds(f *ir.Field) (*big.Int, *big.Int, bool) {
	switch f.Type.Kind {
	case ir.TInt, ir.TBits, ir.TFixed:
	default:
		return nil, nil, false
	}
	if f.HasIntRange {
		if rawLo, rawHi, ok := ir.TableRawRange(f); ok {
			return rawLo, rawHi, true
		}
	}
	one := big.NewInt(1)
	w := uint(f.Type.Width)
	if f.Type.Kind == ir.TFixed {
		w = uint(f.Type.IntBits + f.Type.FracBits)
	}
	if (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed {
		hi := new(big.Int).Sub(new(big.Int).Lsh(one, w-1), one)
		return new(big.Int).Neg(new(big.Int).Lsh(one, w-1)), hi, true
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, w), one), true
}
