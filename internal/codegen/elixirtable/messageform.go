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
	// R8: the writer refuses a vocabulary past the readers' default entry maximum.
	fmt.Fprintf(&b, "  @announcement_too_large %t\n", len(tablewire.MessageVocabulary(u)) > ir.TableVocabularyMaxEntries)
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
		batch = strings.Replace(batch, messageUnionOwner, "", 1)
	}
	if len(vocab) > 4096 {
		runtime = strings.Replace(runtime, "  def announce_measure, do: byte_size(@announcement)\n", "  def announce_measure, do: -1\n", 1)
	}
	b.WriteString(messageIntegrity(u, runtime+batch))
	b.WriteString(messageBinding(u))
	b.WriteString(messageWriteRuleRows(u))
	b.WriteString(messageScopedRuleRows(u))
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
      case encode_bodies(vocab, values, {0, 0}, own_record(vocab), own_scope(vocab)) do
        {:ok, {_acc, nbits}} -> {:ok, 2 + div(nbits + 7, 8)}
        :error -> {:error, :malformed}
      end
    end
  catch
    # every writer refuses a float that is not finite (docs/SPEC-TABLES.md §3.3)
    :non_finite_float -> {:error, :non_finite_float}
    :invalid_value -> {:error, :invalid_value}
  end

  def measure_messages(%__MODULE__{announced: false, refused: false, root: root}, values)
      when is_binary(root) and is_list(values) do
    with {:ok, own} <- own_vocab(root), do: measure_messages(own, values)
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
      case encode_bodies(vocab, values, {0, 0}, own_record(vocab), own_scope(vocab)) do
        {:ok, bits} -> {:ok, frame_batch(n, bits)}
        :error -> {:error, :malformed}
      end
    end
  catch
    :non_finite_float -> {:error, :non_finite_float}
    :invalid_value -> {:error, :invalid_value}
  end

  def save_messages(%__MODULE__{announced: false, refused: false, root: root}, values)
      when is_binary(root) and is_list(values) do
    with {:ok, own} <- own_vocab(root), do: save_messages(own, values)
  end

  def save_messages(_vocab, _values), do: {:error, :no_vocabulary}

  defp own_vocab(root) do
    case :persistent_term.get({__MODULE__, :own_vocab}, nil) do
      nil ->
        case announce_read(@announcement) do
          {:ok, vocab} ->
            :persistent_term.put({__MODULE__, :own_vocab}, vocab)
            for_root(vocab, root)

          _ ->
            {:error, :no_vocabulary}
        end

      vocab ->
        for_root(vocab, root)
    end
  end

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
      Enum.reduce(fields, %{}, fn {src_ref, val}, acc ->
        case entry_at(from_vocab, src_ref) do
          {:ok, pe} -> Map.put(acc, pe.id, val)
          _ -> acc
        end
      end)

    rec
    |> Enum.flat_map(fn {fid, {decl_idx, raw, rule, sub, _range}} ->
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

  defp pad_rebound_array(
         items,
         from_vocab,
         to_vocab,
         to_lookup,
         rules,
         %{kind: 14, shape: shape, element: elem},
         rule,
         sub
       )
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

  defp pad_rebound_array(val, _from_vocab, _to_vocab, _to_lookup, _rules, _te, _rule, _sub),
    do: val

  defp default_elem_val(from_vocab, to_vocab, to_lookup, rules, %{kind: 13}, _er, sub),
    do: rebind_body(from_vocab, to_vocab, to_lookup, rules, [], record(sub))

  defp default_elem_val(_from_vocab, _to_vocab, _to_lookup, _rules, %{kind: 15}, _er, _sub),
    do: :none

  defp default_elem_val(_from_vocab, _to_vocab, _to_lookup, _rules, _elem, :zero_ref, _sub), do: 0

  defp default_elem_val(_from_vocab, _to_vocab, _to_lookup, _rules, _elem, {:enum, :none}, _sub),
    do: 0

  defp default_elem_val(_from_vocab, _to_vocab, to_lookup, _rules, _elem, {:enum, def_id}, _sub) do
    case Map.get(to_lookup.name_by_id, def_id) do
      {def_ref, _} -> def_ref
      _ -> 0
    end
  end

  defp default_elem_val(_from_vocab, _to_vocab, _to_lookup, _rules, _elem, {:same, d}, _sub),
    do: d

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

  defp default_omitted_field(
         from_vocab,
         to_vocab,
         to_lookup,
         rules,
         %{kind: 14, shape: shape},
         :fixed_table,
         sub
       ) do
    elem_body = rebind_body(from_vocab, to_vocab, to_lookup, rules, [], record(sub))
    {:ride, List.duplicate(elem_body, shape.max)}
  end

  defp default_omitted_field(
         from_vocab,
         to_vocab,
         to_lookup,
         rules,
         %{kind: 14},
         {:counted, born, :empty},
         sub
       )
       when born > 0 do
    case rebind_body(from_vocab, to_vocab, to_lookup, rules, [], record(sub)) do
      [] -> :elide
      elem_body -> {:ride, List.duplicate(elem_body, born)}
    end
  end

  defp default_omitted_field(
         from_vocab,
         to_vocab,
         to_lookup,
         rules,
         %{kind: 16},
         {:keyed, keys, :empty},
         sub
       ) do
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

  defp rebind_val(_from_vocab, _to_vocab, _to_lookup, _rules, _fid, 15, _elem, x, _rule, _sub)
       when x in [:none, :unknown_arm],
       do: :none

  defp rebind_val(
         from_vocab,
         to_vocab,
         to_lookup,
         rules,
         _fid,
         15,
         _elem,
         {arm_src_ref, arm_val},
         _rule,
         sub
       ) do
    with {:ok, ape} <- entry_at(from_vocab, arm_src_ref),
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
    |> Enum.flat_map(fn {key_src_ref, kv} ->
      with {:ok, kpe} <- entry_at(from_vocab, key_src_ref),
           true <-
             if(key_list != nil,
               do: kpe.id in key_list,
               else: allowed_keys == nil or MapSet.member?(allowed_keys, kpe.id)
             ),
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

  defp rebind_val(from_vocab, _to_vocab, to_lookup, rules, fid, 30, _elem, src_vref, rule, _sub) do
    {allowed, retired} =
      case Map.get(rules, {fid, 30}) do
        %{member: m} = rmap ->
          {m, Map.get(rmap, :retired)}

        _ ->
          case Map.get(rules, {fid, 14}) do
            %{each_member: m} = rmap ->
              {m, Map.get(rmap, :each_retired)}

            _ ->
              case Map.get(rules, {fid, 16}) do
                %{keyed_member: m} = rmap -> {m, Map.get(rmap, :keyed_retired)}
                _ -> {nil, nil}
              end
          end
      end

    case entry_at(from_vocab, src_vref) do
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
      # and "A refused batch returns a count of ` + "`0`" + `, as every refused batch
      # does; a caller that needs room reads the wire's ` + "`M`" + ` itself"
      # (docs/SPEC-TABLES.md §3.3).
      {:error, :batch_too_large, 0}
    else
      # DAMAGE ANYWHERE IN THE BATCH DISCARDS THE WHOLE BATCH
      # (docs/SPEC-TABLES.md §3.3, §4): no body is delivered and the count is
      # zero, for damage inside a body and for "BYTES AFTER THE PAD" alike.
      # THE READER READS THE BINARY AT A MOVING BIT OFFSET: it never turns the
      # batch into one integer, so a read costs the bits it reads and a
      # hostile batch's trailing bytes cost nothing (ELIXIR-5).
      r = {rest, 0, bit_size(rest)}

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

  defp encode_bodies(_vocab, [], acc, _rec, _scope), do: {:ok, acc}

  defp encode_bodies(vocab, [body | rest], acc, rec, scope) do
    case encode_body(vocab, body, acc, rec, scope) do
      {:ok, acc2} -> encode_bodies(vocab, rest, acc2, rec, scope)
      :error -> :error
    end
  end

  defp encode_body(vocab, body, acc, rec, scope) do
    case encode_fields(vocab, body, acc, rec, scope, -1) do
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
  defp encode_fields(_vocab, [], acc, _rec, _scope, _last), do: {:ok, acc}

  defp encode_fields(vocab, [{ref, value} | rest], acc, rec, scope, last) do
    case entry_at(vocab, ref) do
      {:ok, entry} ->
        decl =
          case write_decl(rec, entry, last, vocab) do
            {:ok, decl} -> decl
            _ -> throw(:invalid_value)
          end

        # THE RULE IS THE DECLARATION'S OWN: the record being written holds this
        # field, so the rule of THIS table's declaration judges it, never the
        # merge of every declaration that shares its name and kind on the wire
        check_field(vocab, entry, value, scope, decl != nil)
        check(not (entry.kind == 15 and value == :none))

        acc2 = put_bits(acc, ref, vocab.ref_bits)
        elem = Map.get(entry, :element)

        case encode_value(vocab, entry.kind, entry.shape, elem, value, acc2, sub_of(decl)) do
          {:ok, acc3} ->
            check(not written_at_default?(vocab, decl, entry, value))
            encode_fields(vocab, rest, acc3, rec, scope, next_last(decl, last))

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
    do: encode_body(vocab, value, acc, record(sub), sub)

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
        check_field(vocab, entry, value, sub, arm_known?(sub, entry.id))
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
  defp check_field(vocab, entry, value, scope, declared) do
    case field_rule(entry, scope, declared) do
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

  # a declaration of the scope (a table name, or {:union, name}) judges its own
  # field; a field the scope does not declare (a peer at another build, an
  # unbound root) falls back to the merge over every declaration of its id and kind
  defp field_rule(entry, scope, true) when scope != nil,
    do: Map.get(scoped_rules(), {scope, entry.id, entry.kind})

  defp field_rule(entry, _scope, _declared), do: Map.get(write_rules(), {entry.id, entry.kind})

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

  defp scoped_rules do
    case :persistent_term.get({__MODULE__, :scoped_rules}, nil) do
      nil ->
        rules =
          Enum.reduce(scoped_write_rule_rows(), %{}, fn {scope, id, kind, what, v}, acc ->
            Map.update(acc, {scope, id, kind}, add_rule(%{}, what, v), &add_rule(&1, what, v))
          end)

        :persistent_term.put({__MODULE__, :scoped_rules}, rules)
        rules

      rules ->
        rules
    end
  end

  defp write_rules do
    case :persistent_term.get({__MODULE__, :write_rules}, nil) do
      nil ->
        rules =
          Enum.reduce(write_rule_rows(), %{}, fn {id, kind, what, v}, acc ->
            Map.update(acc, {id, kind}, add_rule(%{}, what, v), &add_rule(&1, what, v))
          end)

        rules =
          Enum.reduce(write_owner_rule_rows(), rules, fn {owner, id, kind, what, v}, acc ->
            Map.update(acc, {owner, id, kind}, add_rule(%{}, what, v), &add_rule(&1, what, v))
          end)

        :persistent_term.put({__MODULE__, :write_rules}, rules)
        rules

      rules ->
        rules
    end
  end

  defp add_rule(rule, what, v)
       when what in [
              :member,
              :retired,
              :each_member,
              :each_retired,
              :keyed_member,
              :keyed_retired,
              :arm
            ],
       do: Map.update(rule, what, MapSet.new([v]), &MapSet.put(&1, v))

  defp add_rule(rule, what, v), do: Map.put(rule, what, v)

  # A byte run appended whole: the writer must be at a byte boundary, which the
  # align that precedes a string or a bytes array guarantees.
  defp put_bytes({acc, nbits}, bytes) do
    n = byte_size(bytes)
    {acc ||| :binary.decode_unsigned(bytes, :little) <<< nbits, nbits + n * 8}
  end

  # ---------------------------------------------------------------------------
  # THE READER: one bit stream, low bit first, terminal damage (§3.3)
  #
  # The reader is the body as ONE LITTLE-ENDIAN INTEGER and a bit offset: value
  # bit i sits at integer bit (offset + i), so reading n bits is a shift and a
  # mask and never a byte-order question.
  # ---------------------------------------------------------------------------

  defp read_bodies(_vocab, r, 0, acc, _rec_name), do: {:ok, Enum.reverse(acc), r}

  defp read_bodies(vocab, r, n, acc, rec_name) do
    case read_body(vocab, r, [], MapSet.new(), 0, rec_name, record(rec_name), -1) do
      {:ok, body, rest} -> read_bodies(vocab, rest, n - 1, [body | acc], rec_name)
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
  defp read_body(_vocab, _r, _acc, _seen, depth, _rec_name, _rec, _last) when depth > 128,
    do: :malformed

  defp read_body(vocab, r, acc, seen, depth, rec_name, rec, last) do
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
                      read_body(
                        vocab,
                        rest2,
                        acc,
                        MapSet.put(seen, entry.id),
                        depth,
                        rec_name,
                        rec,
                        last
                      )

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
                       false <- stored_at_default?(vocab, decl, value),
                       true <- held?(rec, entry.id, value) do
                    seen = MapSet.put(seen, entry.id)
                    last = next_last(decl, last)

                    acc2 =
                      if entry.kind == 15 and value == :unknown_arm,
                        do: acc,
                        else: [{ref, value} | acc]

                    read_body(vocab, rest2, acc2, seen, depth, rec_name, rec, last)
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

      {index, raw, rule, sub, _range} ->
        if own_build?(vocab) do
          exact = raw == Map.get(entry, :raw)

          if exact and index <= last,
            do: :out_of_order,
            else: {:ok, {index, exact, rule, sub}}
        else
          if peer_kind_compatible?(entry.kind, raw) do
            {:ok, {index, false, rule, sub}}
          else
            :kind_mismatch
          end
        end
    end
  end

  defp peer_kind_compatible?(from, raw) do
    case :binary.encode_unsigned(raw) do
      <<1, _id::little-unsigned-64, to, _::binary>> ->
        from == to or
          (from >= 2 and from <= 5 and ((to > from and to <= 5) or to == 18)) or
          (from >= 6 and from <= 9 and ((to > from and to <= 9) or to == 19)) or
          (from >= 20 and from <= 24 and to > from and to <= 24) or
          (from >= 25 and from <= 29 and to > from and to <= 29) or
          (from == 10 and to == 11)

      _ ->
        true
    end
  end

  # THE READ HOLDS THIS READER'S OWN DECLARED RANGE (docs/SPEC-TABLES.md §3.3,
  # §4). A ranged integer rides as its offset from the announced base in the
  # announced bits, and those bits can spell a value past the bound this
  # record declares: "min = 1, max = 9" rides in four bits, which spell 1..16.
  # A value its own declaration does not admit refuses the batch, at any
  # depth and whatever the record holding this one declares, as it does on a
  # file (the Go engine's messagedecode.go holds the same; test/tables/inner,
  # the inner-message-*-range-past-max rows). An integer array's elements are
  # held the same way.
  defp held?(rec, id, value) do
    case Map.get(rec, id) do
      {_index, _raw, _rule, _sub, {:each, lo, hi}} ->
        is_list(value) and Enum.all?(value, &(is_integer(&1) and &1 >= lo and &1 <= hi))

      {_index, _raw, _rule, _sub, {lo, hi}} ->
        is_integer(value) and value >= lo and value <= hi

      _ ->
        true
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

  defp read_in_range?(owner, {_index, true, _rule, _sub}, entry, value) do
    rules = write_rules()

    rule =
      Map.get(rules, {owner, entry.id, entry.kind}) ||
        Map.get(rules, {entry.id, entry.kind})

    case rule do
      %{min: lo, max: hi} when is_integer(value) ->
        value >= lo and value <= hi

      %{each_min: lo, each_max: hi} when is_list(value) ->
        Enum.all?(value, &(is_integer(&1) and &1 >= lo and &1 <= hi))

      _ ->
        true
    end
  end

  defp read_in_range?(_owner, _decl, _entry, _value), do: true

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

  defp own_scope(%{root: root}) when is_binary(root), do: root
  defp own_scope(_vocab), do: nil

		old := rules[k]
		if old == nil {
			cp := *r
			if r.lo != nil {
				cp.lo = new(big.Int).Set(r.lo)
				cp.hi = new(big.Int).Set(r.hi)
			}
			if r.ids != nil {
				cp.ids = map[uint64]bool{}
				for id := range r.ids {
					cp.ids[id] = true
				}
			}
			if r.retired != nil {
				cp.retired = map[uint64]bool{}
				for id := range r.retired {
					cp.retired[id] = true
				}
			}
			rules[k] = &cp
			return
		}
		if !ruleEq(old, r) {
			disagrees[k] = true
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
	fieldRule := messageRuleOf
	seenUnion := map[*ir.Union]bool{}
	fields := func(owner string, fs []*ir.Field) {
		for _, f := range fs {
			e := ir.TableFieldEntry(f)
			if r := fieldRule(f); r != nil {
				add(owner, key{e.Id, e.Kind}, r)
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
					add(un.Name, key{arm.Id, arm.Kind}, r)
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
			fields(name, st.Fields)
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
		rows = append(rows, messageRuleRows(head, r)...)
	}

	var okeys []ownerKey
	for ok := range ownerRules {
		if disagrees[key{ok.id, ok.kind}] {
			okeys = append(okeys, ok)
		}
	}
	sort.Slice(okeys, func(i, j int) bool {
		if okeys[i].owner != okeys[j].owner {
			return okeys[i].owner < okeys[j].owner
		}
		if okeys[i].id != okeys[j].id {
			return okeys[i].id < okeys[j].id
		}
		return okeys[i].kind < okeys[j].kind
	})
	var ownerRows []string
	for _, ok := range okeys {
		r := ownerRules[ok]
		head := fmt.Sprintf("{%q, 0x%016X, %d, ", ok.owner, ok.id, ok.kind)
		switch r.what {
		case "int":
			ownerRows = append(ownerRows, head+":min, "+fixedIntLiteral(r.lo)+"}", head+":max, "+fixedIntLiteral(r.hi)+"}")
		case "each":
			ownerRows = append(ownerRows, head+":each_min, "+fixedIntLiteral(r.lo)+"}", head+":each_max, "+fixedIntLiteral(r.hi)+"}")
		case "keyed_each":
			ownerRows = append(ownerRows, head+":keyed_each_min, "+fixedIntLiteral(r.lo)+"}", head+":keyed_each_max, "+fixedIntLiteral(r.hi)+"}")
		case "text":
			ownerRows = append(ownerRows, fmt.Sprintf("%s:text, %d}", head, r.text))
		default:
			ids := make([]uint64, 0, len(r.ids))
			for id := range r.ids {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			for _, id := range ids {
				ownerRows = append(ownerRows, fmt.Sprintf("%s:%s, 0x%016X}", head, r.what, id))
			}
			if r.whatRet != "" && len(r.retired) > 0 {
				rids := make([]uint64, 0, len(r.retired))
				for id := range r.retired {
					rids = append(rids, id)
				}
				slices.Sort(rids)
				for _, id := range rids {
					ownerRows = append(ownerRows, fmt.Sprintf("%s:%s, 0x%016X}", head, r.whatRet, id))
				}
			}
		}
	}

	var b strings.Builder
	b.WriteString("\n  # this unit's own write rules, one {id, kind, rule, value} row each\n")
	if len(rows) == 0 {
		b.WriteString("  defp write_rule_rows, do: []\n")
	} else {
		b.WriteString("  defp write_rule_rows do\n    [\n")
		for i, row := range rows {
			b.WriteString("      " + row)
			if i < len(rows)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("    ]\n  end\n")
	}
	if len(ownerRows) == 0 {
		b.WriteString("\n  defp write_owner_rule_rows, do: []\n")
	} else {
		b.WriteString("\n  defp write_owner_rule_rows do\n    [\n")
		for i, row := range ownerRows {
			b.WriteString("      " + row)
			if i < len(ownerRows)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("    ]\n  end\n")
	}
	return b.String()
}

// messageRule is one declaration's write rule (docs/SPEC.md section 5.1).
type messageRule struct {
	what     string // int, each, text, member, each_member, keyed_member, arm
	whatRet  string
	lo, hi   *big.Int
	text     int64
	ids      map[uint64]bool
	retired  map[uint64]bool
	conflict bool
}

// messageRuleOf is the write rule one field declares, nil for a field that
// declares none (docs/SPEC.md section 5.1).
func messageRuleOf(f *ir.Field) *messageRule {
	if f.IsMap() || f.Type.Pointer {
		return nil
	}
	if lo, hi, ok := messageIntBounds(f); ok {
		if f.Array != ir.ArrayNone {
			if f.KeyEnum != "" {
				return &messageRule{what: "keyed_each", lo: lo, hi: hi}
			}
			return &messageRule{what: "each", lo: lo, hi: hi}
		}
		return &messageRule{what: "int", lo: lo, hi: hi}
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
			return &messageRule{what: "keyed_member", whatRet: "keyed_retired", ids: ids, retired: retired}
		}
		if f.Array != ir.ArrayNone {
			return &messageRule{what: "each_member", whatRet: "each_retired", ids: ids, retired: retired}
		}
		return &messageRule{what: "member", whatRet: "retired", ids: ids, retired: retired}
	}
	if f.KeyEnum != "" || f.Array != ir.ArrayNone {
		return nil
	}
	if f.Type.Kind == ir.TString && !f.Type.Blob() && !f.Type.Unbounded && f.Type.Size > 0 {
		return &messageRule{what: "text", text: f.Type.Size}
	}
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	if ref, ok := f.Type.Ref.(*ir.Union); ok {
		ids := map[uint64]bool{}
		for _, v := range ref.Variants {
			ids[ir.TableArmEntry(v).Id] = true
		}
		return &messageRule{what: "arm", ids: ids}
	}
	return nil
}

// messageRuleRows is the rows one rule spells, each led by head.
func messageRuleRows(head string, r *messageRule) []string {
	var rows []string
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
	return rows
}

// messageScopedRuleRows emits scoped_write_rule_rows/0: the same rules as
// write_rule_rows/0, one set per DECLARATION'S OWN SCOPE and never merged. A
// table's fields are the rows of its name, a union's arms the rows of
// {:union, name}. Declarations that share an id and a kind share one wire entry,
// but each holds its own value to its own declaration (docs/SPEC.md section
// 5.1 row 1; SF-23): A.hp = 6 is not lawful on the strength of B.hp.
func messageScopedRuleRows(u *ir.Unit) string {
	closure := tablewire.MessageClosure(u)
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	var rows []string
	seenUnion := map[*ir.Union]bool{}
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			e := ir.TableFieldEntry(f)
			if r := messageRuleOf(f); r != nil {
				rows = append(rows, messageRuleRows(fmt.Sprintf("{%q, 0x%016X, %d, ", name, e.Id, e.Kind), r)...)
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
				if r := messageRuleOf(v.F); r != nil {
					rows = append(rows, messageRuleRows(fmt.Sprintf("{{:union, %q}, 0x%016X, %d, ", un.Name, arm.Id, arm.Kind), r)...)
				}
			}
		}
	}
	var b strings.Builder
	b.WriteString("\n  # the same rules once per declaration scope, {scope, id, kind, rule, value} each\n")
	if len(rows) == 0 {
		b.WriteString("  defp scoped_write_rule_rows, do: []\n")
		return b.String()
	}
	b.WriteString("  defp scoped_write_rule_rows do\n    [\n")
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
