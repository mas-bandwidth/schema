package elixirtable

// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2, in Elixir: a batch
// of bitpacked bodies under one announced vocabulary. The surface is
// measure_messages, save_messages and load_messages over the vocabulary this
// backend emits for a `fixed table` root (SPEC.md §6.1's snake_case spelling of
// §3.3's MeasureMessages, SaveMessages, LoadMessages).
//
// The bytes are the C++ reference's. The announcement is ir.TableAnnouncement,
// the vocabulary ir.TableVocabulary, and the bit stream is the packet wire's
// own low-bit-first order (SPEC.md §4.3) — the same arithmetic the Go leg
// spells in internal/codegen/gotable/message.go. Nothing here is a second
// derivation: the widths, slots and shapes are read off the IR through the same
// calls the C++, Go and C# legs make.

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageRuntimeModule is the message form's shared runtime, emitted ONCE PER
// UNIT beside the fixed form's, for the same reason: a unit that declares no
// fixed root gets none of it.
const MessageRuntimeModule = "MessageRuntime"

// MessageModuleSuffix is the file and module suffix the message form takes per
// schema file, beside the fixed form's FixedModuleSuffix.
const MessageModuleSuffix = "Message"

// messageRuntimeModule emits the message form's shared Elixir runtime: the bit
// writer and reader, the report, the refusal reasons and the announcement.
func messageRuntimeModule(u *ir.Unit, ns string) []byte {
	var b strings.Builder
	b.WriteString(header(MessageRuntimeModule, u.Package, "the MESSAGE FORM's shared runtime (docs/SPEC-TABLES.md §3.3)"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "defmodule %s.MessageRuntime do\n", ns)
	body := strings.NewReplacer(
		"@REF_BITS@", fmt.Sprintf("%d", ir.TableMessageRefBits(len(ir.TableVocabulary(u)))),
		"@ANNOUNCEMENT@", elixirBytes(ir.TableAnnouncement(u)),
	).Replace(messageRuntimeBody)
	b.WriteString(body)
	b.WriteString("end\n")
	return []byte(b.String())
}

// messageRuntimeBody is the runtime's Elixir text, with the reference width and
// the announcement bytes interpolated.
const messageRuntimeBody = `  import Bitwise

  @moduledoc """
  The MESSAGE FORM's shared runtime (docs/SPEC-TABLES.md §3.3), form byte 2,
  emitted once per unit.

  A batch is the form byte, a body count, and the bodies as one continuous bit
  stream in the packet wire's own low-bit-first order (SPEC.md §4.3). A body is
  a sequence of fields, each a reference followed by a payload, ending at a zero
  reference. The reference is @REF_BITS@ bits wide because that is bits_required(0, E)
  for this unit's vocabulary.
  """

  @form 2
  @batch_max 256
  @ref_bits @REF_BITS@

  # The unit's announcement, a compile-time constant byte array (§3.3).
  @announcement @ANNOUNCEMENT@

  def form, do: @form
  def batch_max, do: @batch_max
  def ref_bits, do: @ref_bits
  def announce, do: @announcement
  def announce_measure, do: byte_size(@announcement)

  # ---------------------------------------------------------------------------
  # THE REPORT: §4's counters, and the refusal verdict beside them
  # ---------------------------------------------------------------------------

  @doc """
  A fresh read report: §4's counters, the refusal verdict and its reason. A
  REFUSAL IS NOT ONE OF THE SIX: it decodes nothing, so none of them moves and
  malformed does not fire (§3.3).
  """
  def report do
    %{malformed: false, unknown: 0, kind_mismatch: 0, clamped: 0, widened: 0,
      duplicate: 0, refused: false, reason: nil}
  end

  def refuse(report, reason), do: %{report | refused: true, reason: reason}
  def malformed(report), do: %{report | malformed: true}

  # ---------------------------------------------------------------------------
  # THE BIT WRITER, the packet wire's low-bit-first order
  # ---------------------------------------------------------------------------

  # A writer is {integer, bit_count}: bit i of the stream is bit i of the
  # integer, low bit first, which :binary.encode_unsigned/2 spells little-endian
  # byte for byte (SPEC.md §4.3).
  def put({int, n}, _v, 0), do: {int, n}

  def put({int, n}, v, k) do
    masked = if k < 64, do: v &&& (1 <<< k) - 1, else: v
    {int + (masked <<< n), n + k}
  end

  def align({int, n}), do: put({int, n}, 0, rem(8 - rem(n, 8), 8))

  def to_binary({int, n}) do
    bytes = :binary.encode_unsigned(int, :little)
    want = div(n + 7, 8)
    pad = max(want - byte_size(bytes), 0)
    bytes <> :binary.copy(<<0>>, pad)
  end

  # ---------------------------------------------------------------------------
  # THE BIT READER
  # ---------------------------------------------------------------------------

  # A reader is the whole stream decoded once into an integer, beside its total
  # bit length. Reading n bits at offset o is the packet wire's own unaligned
  # load: shift right o and mask n.
  def reader(bin), do: {:binary.decode_unsigned(bin, :little), byte_size(bin) * 8}

  def get({_int, total}, off, n) when off + n > total, do: :error
  def get({int, _total}, off, n), do: {:ok, (int >>> off) &&& (1 <<< n) - 1}

  # A value read back as unsigned, sign-extended for a signed kind's width.
  def signed(v, bits) when v >= 1 <<< (bits - 1), do: v - (1 <<< bits)
  def signed(v, _bits), do: v
`

// elixirBytes renders a byte run as an Elixir binary literal.
func elixirBytes(data []byte) string {
	parts := make([]string, 0, len(data))
	for _, v := range data {
		parts = append(parts, fmt.Sprintf("%d", v))
	}
	return "<<" + strings.Join(parts, ", ") + ">>"
}

// messageSlot is the writer's slot for one entry: its one-based position in the
// unit's vocabulary (§3.3, "the writer's slot numbers are compile-time
// constants").
func messageSlot(u *ir.Unit, e ir.TableVocabularyEntry) int {
	for i, v := range ir.TableVocabulary(u) {
		if v.Key() == e.Key() {
			return i + 1
		}
	}
	return 0
}

// generateMessage emits the per-file message surface: one module per schema
// file whose fixed roots grow measure_messages, save_messages and
// load_messages beside the fixed form's verbs.
func generateMessage(u *ir.Unit, ns string) (map[string][]byte, error) {
	roots := fixedRoots(u)
	if len(roots) == 0 {
		return nil, nil
	}
	refBits := ir.TableMessageRefBits(len(ir.TableVocabulary(u)))
	out := map[string][]byte{}
	byFile := map[string][]*ir.Struct{}
	for _, st := range roots {
		base := u.DeclFile[st.Name]
		byFile[base] = append(byFile[base], st)
	}
	for base, list := range byFile {
		out[base+MessageModuleSuffix+".ex"] = messageFileModule(u, ns, base, list, refBits)
	}
	return out, nil
}

func messageFileModule(u *ir.Unit, ns, base string, roots []*ir.Struct, refBits int) []byte {
	var b strings.Builder
	b.WriteString(header(base, u.Package, "the MESSAGE FORM, form byte 2 (docs/SPEC-TABLES.md §3.3)"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "defmodule %s.%s%s do\n", ns, moduleBase(base), MessageModuleSuffix)
	fmt.Fprintf(&b, "  import Bitwise\n")
	fmt.Fprintf(&b, "  alias %s.MessageRuntime, as: M\n\n", ns)
	fmt.Fprintf(&b, "  @ref_bits %d\n\n", refBits)
	for _, st := range roots {
		b.WriteString(messageRootSurface(u, ns, st))
	}
	b.WriteString("end\n")
	return []byte(b.String())
}

// messageRootSurface emits the three batch verbs and the two body walkers for
// one fixed root.
func messageRootSurface(u *ir.Unit, ns string, st *ir.Struct) string {
	snake := ir.RustSnake(st.Name)
	structType := ns + "." + st.Name
	var b strings.Builder

	// ---- the write body ----
	fmt.Fprintf(&b, "  # %s: the write walker (§3.3), a reference and a payload per field,\n", st.Name)
	fmt.Fprintf(&b, "  # eliding a field at its declared default.\n")
	fmt.Fprintf(&b, "  defp %s_save_body(w, v) do\n", snake)
	for _, f := range st.Fields {
		e := ir.TableFieldEntry(f)
		b.WriteString(messageWriteField(f, e, messageSlot(u, e), "    ", "v."+f.Name))
	}
	fmt.Fprintf(&b, "    M.put(w, 0, @ref_bits)\n")
	fmt.Fprintf(&b, "  end\n\n")

	// ---- the three verbs ----
	fmt.Fprintf(&b, "  @doc \"§3.3: MeasureMessages sizes the buffer; a batch above 256 bodies is refused.\"\n")
	fmt.Fprintf(&b, "  def %s_measure_messages(values) do\n", snake)
	fmt.Fprintf(&b, "    if length(values) < 1 or length(values) > M.batch_max() do\n")
	fmt.Fprintf(&b, "      {:error, :batch_too_large}\n")
	fmt.Fprintf(&b, "    else\n")
	fmt.Fprintf(&b, "      w = M.put(M.put({0, 0}, 2, 8), length(values) - 1, 8)\n")
	fmt.Fprintf(&b, "      w = Enum.reduce(values, w, fn v, w -> %s_save_body(w, v) end)\n", snake)
	fmt.Fprintf(&b, "      byte_size(M.to_binary(M.align(w)))\n")
	fmt.Fprintf(&b, "    end\n")
	fmt.Fprintf(&b, "  end\n\n")

	fmt.Fprintf(&b, "  @doc \"§3.3: SaveMessages writes ONE buffer, the batch bytes, exactly measure's length.\"\n")
	fmt.Fprintf(&b, "  def %s_save_messages(values) do\n", snake)
	fmt.Fprintf(&b, "    case %s_measure_messages(values) do\n", snake)
	fmt.Fprintf(&b, "      {:error, _} = e -> e\n")
	fmt.Fprintf(&b, "      _ ->\n")
	fmt.Fprintf(&b, "        w = M.put(M.put({0, 0}, 2, 8), length(values) - 1, 8)\n")
	fmt.Fprintf(&b, "        w = Enum.reduce(values, w, fn v, w -> %s_save_body(w, v) end)\n", snake)
	fmt.Fprintf(&b, "        M.to_binary(M.align(w))\n")
	fmt.Fprintf(&b, "    end\n")
	fmt.Fprintf(&b, "  end\n\n")

	// ---- the read side: a recursive walker threading the field locals ----
	fmt.Fprintf(&b, "  @doc \"§3.3: LoadMessages reads the batch into caller storage and reports how many bodies it read.\"\n")
	fmt.Fprintf(&b, "  def %s_load_messages(bin, vocab \\\\ nil) do\n", snake)
	fmt.Fprintf(&b, "    report = M.report()\n")
	fmt.Fprintf(&b, "    cond do\n")
	fmt.Fprintf(&b, "      byte_size(bin) < 1 -> {0, M.malformed(report)}\n")
	fmt.Fprintf(&b, "      :binary.at(bin, 0) != 2 -> {0, M.refuse(report, :newer_form)}\n")
	fmt.Fprintf(&b, "      is_nil(vocab) -> {0, M.refuse(report, :no_vocabulary)}\n")
	fmt.Fprintf(&b, "      byte_size(bin) < 2 -> {0, M.malformed(report)}\n")
	fmt.Fprintf(&b, "      true ->\n")
	fmt.Fprintf(&b, "        count = :binary.at(bin, 1) + 1\n")
	fmt.Fprintf(&b, "        rest = binary_part(bin, 2, byte_size(bin) - 2)\n")
	fmt.Fprintf(&b, "        {values, report} = %s_bodies(M.reader(rest), count, [], report)\n", snake)
	fmt.Fprintf(&b, "        {length(values), report}\n")
	fmt.Fprintf(&b, "    end\n")
	fmt.Fprintf(&b, "  end\n\n")

	// the body loop
	fmt.Fprintf(&b, "  defp %s_bodies(_r, 0, values, report), do: {Enum.reverse(values), report}\n", snake)
	fmt.Fprintf(&b, "  defp %s_bodies(r, left, values, report) do\n", snake)
	fmt.Fprintf(&b, "    case %s_body(r, 0, %%{}) do\n", snake)
	fmt.Fprintf(&b, "      {:ok, _off, value} -> %s_bodies(r, left - 1, [value | values], report)\n", snake)
	fmt.Fprintf(&b, "      :error -> {Enum.reverse(values), M.malformed(report)}\n")
	fmt.Fprintf(&b, "    end\n")
	fmt.Fprintf(&b, "  end\n\n")

	// the single body walker, accumulating field values into a map the
	// terminator turns into the struct; an absent field keeps its default.
	fmt.Fprintf(&b, "  defp %s_body(r, off, acc) do\n", snake)
	fmt.Fprintf(&b, "    case M.get(r, off, @ref_bits) do\n")
	fmt.Fprintf(&b, "      :error -> :error\n")
	fmt.Fprintf(&b, "      {:ok, 0} -> {:ok, off + @ref_bits, struct(%s, acc)}\n", structType)
	for _, f := range st.Fields {
		e := ir.TableFieldEntry(f)
		slot := messageSlot(u, e)
		bits := ir.TableMessageValueBits(e.Kind, e.Shape)
		fmt.Fprintf(&b, "      {:ok, %d} ->\n", slot)
		fmt.Fprintf(&b, "        case M.get(r, off + @ref_bits, %d) do\n", bits)
		fmt.Fprintf(&b, "          :error -> :error\n")
		fmt.Fprintf(&b, "          {:ok, v} -> %s_body(r, off + @ref_bits + %d, Map.put(acc, :%s, %s))\n",
			snake, bits, f.Name, messageReadValue(f, e, "v"))
		fmt.Fprintf(&b, "        end\n")
	}
	fmt.Fprintf(&b, "      {:ok, _ref} -> :error\n")
	fmt.Fprintf(&b, "    end\n")
	fmt.Fprintf(&b, "  end\n\n")

	return b.String()
}

// messageReadValue is a field's payload reconstructed from its unsigned bits.
func messageReadValue(f *ir.Field, e ir.TableVocabularyEntry, v string) string {
	switch {
	case f.Type.Kind == ir.TBool:
		return fmt.Sprintf("%s == 1", v)
	case f.Type.Kind == ir.TInt && ir.TableKindSigned(int(e.Kind)):
		return fmt.Sprintf("M.signed(%s, %d)", v, ir.TableMessageValueBits(e.Kind, e.Shape))
	default:
		return v
	}
}

// messageWriteField emits one field's write: the reference and the payload, the
// field elided where it holds its declared default (§3.3). Elixir's `if` does
// not rebind an outer `w`, so the writer threads the accumulator as a VALUE out
// of the branch rather than as statements inside it.
func messageWriteField(f *ir.Field, e ir.TableVocabularyEntry, slot int, ind, expr string) string {
	var b strings.Builder
	rides := messageRides(f, expr)
	bits := ir.TableMessageValueBits(e.Kind, e.Shape)
	put := fmt.Sprintf("M.put(M.put(w, %d, @ref_bits), %s, %d)", slot, messageWriteValue(f, e, bits, expr), bits)
	if rides != "" {
		fmt.Fprintf(&b, "%sw =\n", ind)
		fmt.Fprintf(&b, "%s  if %s do\n", ind, rides)
		fmt.Fprintf(&b, "%s    %s\n", ind, put)
		fmt.Fprintf(&b, "%s  else\n", ind)
		fmt.Fprintf(&b, "%s    w\n", ind)
		fmt.Fprintf(&b, "%s  end\n", ind)
	} else {
		fmt.Fprintf(&b, "%sw = %s\n", ind, put)
	}
	return b.String()
}

// messageRides is the elision test: empty when the field always rides, else an
// Elixir boolean expression naming when it does (§3.3, "a field at its declared
// default is not written").
func messageRides(f *ir.Field, expr string) string {
	switch {
	case f.Type.Optional:
		return expr + "_present"
	case f.Type.Kind == ir.TBool:
		return expr
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TWString:
		return "byte_size(" + expr + ") != 0"
	case f.Array == ir.ArrayCounted:
		return expr + "_count != 0"
	case f.Type.Kind == ir.TInt:
		return expr + " != 0"
	default:
		return ""
	}
}

// messageWriteValue is a field's payload value, the packet wire's own bits.
func messageWriteValue(f *ir.Field, e ir.TableVocabularyEntry, bits int64, expr string) string {
	switch {
	case f.Type.Kind == ir.TBool:
		return fmt.Sprintf("if(%s, do: 1, else: 0)", expr)
	default:
		return expr
	}
}
