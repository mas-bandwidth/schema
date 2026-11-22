package elixirtable

import (
	"fmt"
	"hash/crc32"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v3/ir"
)

func hasIntegrity(u *ir.Unit) bool {
	for _, st := range u.Tables {
		if st.Integrity != "" {
			return true
		}
	}
	return false
}

// CRC-32C is table driven and walks the input binary without allocating a byte list.
// The same private engine is rendered into the file and message modules; fixed
// roots call their already shared FixedRuntime module.
func integritySource(public bool) string {
	var b strings.Builder
	var vals []string
	for _, v := range crc32.MakeTable(crc32.Castagnoli) {
		vals = append(vals, fmt.Sprintf("0x%08X", v))
	}
	b.WriteString(exAttribute("integrity_table", "{"+strings.Join(vals, ", ")+"}"))
	s := integrityRuntime
	if public {
		s = strings.ReplaceAll(s, "defp integrity_seal(", "def integrity_seal(")
		s = strings.ReplaceAll(s, "defp integrity_open(", "def integrity_open(")
	}
	b.WriteString(strings.TrimRight(s, "\n") + "\n")
	return b.String()
}

const integrityRuntime = `  defp integrity_crc(<<>>, crc), do: bxor(crc, 0xFFFFFFFF)

  defp integrity_crc(<<byte, rest::binary>>, crc) do
    next = bxor(elem(@integrity_table, band(bxor(crc, byte), 255)), crc >>> 8)
    integrity_crc(rest, next)
  end

  defp integrity_seal(data) do
    <<data::binary, integrity_crc(data, 0xFFFFFFFF)::little-32>>
  end

  # Wrong or missing form bytes keep their ordinary framing verdict. Integrity
  # precedes every header, vocabulary, field, and output-buffer walk otherwise.
  defp integrity_open(<<form, _::binary>> = data, form) do
    minimum = if form == 2, do: 6, else: 5

    if byte_size(data) < minimum do
      {:error, :integrity_mismatch}
    else
      size = byte_size(data) - 4
      <<body::binary-size(^size), expected::little-32>> = data

      if integrity_crc(body, 0xFFFFFFFF) == expected,
        do: {:ok, body},
        else: {:error, :integrity_mismatch}
    end
  end

  defp integrity_open(data, _form), do: {:ok, data}

`

func variableIntegrity(u *ir.Unit, runtime, retain string) (string, string) {
	if !hasIntegrity(u) {
		return runtime, retain
	}
	wire := "IO.iodata_to_binary([<<1>>, body, <<0>>, ids_bytes, <<length(order)::little-64>>])"
	runtime = strings.ReplaceAll(runtime, "{:ok, "+wire+"}", "data = "+wire+"\n      {:ok, seal_file(desc, data)}")
	retain = strings.ReplaceAll(retain, "{:ok, "+wire+",\n       retain, report}", "data = "+wire+"\n      {:ok, seal_file(desc, data), retain, report}")
	runtime = strings.ReplaceAll(runtime, "case open(data) do", "case open_file(desc, data) do")
	retain = strings.ReplaceAll(retain, "case open(data) do", "case open_file(desc, data) do")
	retain = strings.ReplaceAll(retain, "{:error, :malformed, reasoned(report, refusal), retain}", "{:error, refusal, reasoned(report, refusal), retain}")
	runtime = strings.Replace(runtime, "  defp reasoned(report, :newer_form)", `  defp reasoned(report, :integrity_mismatch),
    do: Map.put(report, :reason, "integrity_mismatch")

  defp reasoned(report, :newer_form)`, 1)
	runtime += `
  defp seal_file(desc, data) do
    if Map.get(desc, :integrity, false), do: integrity_seal(data), else: data
  end

  defp open_file(desc, data) do
    if Map.get(desc, :integrity, false) do
      with {:ok, body} <- integrity_open(data, 1), do: open(body)
    else
      open(data)
    end
  end

` + integritySource(false)
	return runtime, retain
}

// messageIntegrity seals and opens the batch of a root that declares
// integrity. The binding itself (messageBinding) is every unit's.
func messageIntegrity(u *ir.Unit, source string) string {
	if !hasIntegrity(u) {
		return source
	}
	source = strings.Replace(source, "{:ok, 2 + div(nbits + 7, 8)}", "{:ok, 2 + div(nbits + 7, 8) + if(vocab.integrity, do: 4, else: 0)}", 1)
	source = strings.Replace(source, "{:ok, bits} -> {:ok, frame_batch(n, bits)}\n        :error -> {:error, :malformed}", `{:ok, bits} ->
          data = frame_batch(n, bits)
          {:ok, if(vocab.integrity, do: integrity_seal(data), else: data)}

        :error ->
          {:error, :malformed}`, 1)
	// Keep the batch's exact framing and capacity behavior after opening.
	source = strings.Replace(source, "  def load_messages(vocab, data, capacity), do: load_messages_raw(vocab, data, capacity)", `  def load_messages(%__MODULE__{announced: true, integrity: true} = vocab, data, capacity) do
    with {:ok, body} <- integrity_open(data, 2),
         do: load_messages_raw(vocab, body, capacity)
  end

  def load_messages(vocab, data, capacity), do: load_messages_raw(vocab, data, capacity)`, 1)
	return source + "\n" + integritySource(false)
}

// messageRoots are the unit's fixed tables that carry the message form: the
// roots for_root/2 binds.
func messageRoots(u *ir.Unit) []string {
	var names []string
	for name, st := range u.Tables {
		if st.FixedDeclared && st.CarriesForm("message") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// messageBinding emits THE BOUND BATCH (docs/SPEC-TABLES.md §3.3): for_root/2
// binds a resolved vocabulary to one fixed root, and the per-root verbs bind
// and act in one call. A message read or write is always bound to its root
// record; the module's own load_messages, save_messages and measure_messages
// answer :no_root on a vocabulary no root is bound to.
func messageBinding(u *ir.Unit) string {
	var b strings.Builder
	b.WriteString("\n  @doc \"\"\"\n")
	b.WriteString("  Bind a resolved vocabulary to one fixed root, the record its batches are read\n")
	b.WriteString("  and written against (docs/SPEC-TABLES.md §3.3). A name that is not a fixed\n")
	b.WriteString("  message root of this unit is `:unknown_root`.\n")
	b.WriteString("  \"\"\"\n")
	names := messageRoots(u)
	for _, name := range names {
		st := u.Tables[name]
		fmt.Fprintf(&b, "  def for_root(%%__MODULE__{} = vocab, %q),\n    do: {:ok, %%{vocab | integrity: %v, root: %q}}\n\n", name, st.Integrity != "", name)
	}
	b.WriteString("  def for_root(%__MODULE__{}, _table), do: {:error, :unknown_root}\n\n")
	// the with/do: pair on one line where it fits, else the formatter's wrap
	with := func(name, call string) string {
		head := fmt.Sprintf("    with {:ok, bound} <- for_root(vocab, %q)", name)
		if one := head + ", do: " + call; exWidth(one) <= exLineLimit {
			return one + "\n"
		}
		return head + ",\n         do: " + call + "\n"
	}
	for _, name := range names {
		snake := ir.RustSnake(name)
		for _, verb := range []string{"measure", "save"} {
			fmt.Fprintf(&b, "  def %s_%s_messages(vocab, values) do\n%s  end\n\n", snake, verb, with(name, verb+"_messages(bound, values)"))
		}
		fmt.Fprintf(&b, "  def %s_load_messages(vocab, data, capacity \\\\ @batch_max) do\n%s  end\n\n", snake, with(name, "load_messages(bound, data, capacity)"))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
