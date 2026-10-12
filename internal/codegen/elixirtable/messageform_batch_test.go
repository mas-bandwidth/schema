package elixirtable

// THE MESSAGE FORM'S BATCH ON THE ELIXIR LEG (docs/SPEC-TABLES.md §3.3).
// The message form is a batch of bitpacked bodies under one announced
// vocabulary: `MeasureMessages`, `SaveMessages` and `LoadMessages` over the
// vocabulary the announcement card emitted. The bytes are the C++ reference's,
// pinned by testdata/conformance/tables/MANIFEST.txt's `message` rows, and each
// probe runs the generated Elixir against the leg's own toolchain.
//
// RED FIRST. The elixirtable package carried the announcement and the batch
// surface's refusals, but no body codec: a well-formed batch whose bodies
// decode could not round-trip, and the damage halves of clause 2.3 were not
// answered. The implementation is internal/codegen/elixirtable/messageform.go.
//
// THE SURFACE NAMES IN ELIXIR'S NAMING CONVENTION (SPEC.md §6.1): the unit
// module <Package>.TableMessage carries `measure_messages`, `save_messages` and
// `load_messages` beside `announce`, `announce_measure` and `announce_read`. A
// refusal is `{:error, reason}`, `{:error, reason, count}` on the read-side
// capacity refusal, and `{:malformed, count, values}` on damage, the packet
// emitter's reader verdict (docs/SPEC-TABLES.md §3.3's own note).

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageBatchRow is one `message` manifest line: the file-form wire and the
// batch wire under the same name.
type messageBatchRow struct {
	name       string
	connection string
	root       string
	file       string
	batch      string
}

// messageBatchRows reads the `message` rows of the conformance manifest, never
// a copied list (docs/SPEC-TABLES.md §3.3's two manifest line kinds).
func messageBatchRows(t *testing.T, path string) []messageBatchRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []messageBatchRow
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[0] != "message" {
			continue
		}
		rows = append(rows, messageBatchRow{
			name:       fields[1],
			connection: fields[2],
			root:       fields[3],
			file:       fields[4],
			batch:      fields[5],
		})
	}
	if len(rows) == 0 {
		t.Fatalf("the manifest carries no message rows")
	}
	return rows
}

func TestMessageFormBatchElixir(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	units, connections := messageManifest(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	rows := messageBatchRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))

	// 2.1 "every manifest `message` row: LoadMessages of the batch gives the
	// values the form-1 instance decodes to, and SaveMessages gives the batch
	// bytes back exactly; MeasureMessages equals the length."
	//
	// THE FORM IS A FIXED TABLE'S (docs/SPEC-TABLES.md §3.3): "`fixed table T`
	// may ride here, a plain `table T` may not". A message row whose root is a
	// plain `table` (graph_tree's Scene) is refused rather than decoded, so the
	// round trip below covers the fixed roots only.
	t.Run("round_trip", func(t *testing.T) {
		for _, row := range rows {
			conn := connections[row.connection]
			u := messageLoadUnit(t, root, units[conn.unit])
			st := u.Tables[row.root]
			if st == nil || !st.FixedDeclared {
				continue
			}
			row := row
			t.Run(row.name, func(t *testing.T) {
				t.Parallel()
				ann := filepath.Join(root, conn.path)
				batch := filepath.Join(root, row.batch)
				jsonPath := filepath.Join(root, "testdata", "conformance", "tables", "json", row.name+".json")
				expected := expectedMessageBody(t, u, st, jsonPath)
				out, err := runMessageBatchProbe(t, u, ann, batch, roundTripBody(row, expected))
				if err != nil {
					t.Fatalf("%v\n%s", err, out)
				}
			})
		}
	})

	// 2.2 "no_vocabulary; batch_too_large on the write side (M above 256, -1
	// with the reason) and the read side (M above capacity, refused before
	// decoding, count set to M)."
	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		u := messageLoadUnit(t, root, units["backenddemo"])
		ann := filepath.Join(root, connections["backenddemo"].path)
		body := `    vocab =
      case M.announce_read(File.read!(file())) do
        {:ok, v} -> v
        other -> raise "announce_read refused the reference's announcement: #{inspect(other)}"
      end

    check({:error, :no_vocabulary} == M.load_messages(M.new(), <<>>), "load with no vocabulary decoded")
    check({:error, :no_vocabulary} == M.measure_messages(M.new(), []), "measure with no vocabulary answered")
    check({:error, :no_vocabulary} == M.save_messages(M.new(), []), "save with no vocabulary wrote")
    check({:error, :batch_too_large} == M.save_messages(vocab, List.duplicate(nil, 257)), "save accepted 257 bodies")
    check({:error, :batch_too_large} == M.measure_messages(vocab, List.duplicate(nil, 257)), "measure accepted 257 bodies")
    check({:error, :batch_too_large} == M.save_messages(vocab, []), "save accepted an empty batch")
    check({:error, :batch_too_large} == M.measure_messages(vocab, []), "measure accepted an empty batch")

    # "The reader sets the returned count to the wire's M before it returns the
    # refusal" (docs/SPEC-TABLES.md §3.3): the batch <<2, 1>> names two bodies.
    check({:error, :batch_too_large, 2} == M.load_messages(vocab, <<2, 1>>, 1), "load past capacity did not set the count to M")
`
		out, err := runMessageProbe(t, u, ann, body)
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	})

	// 2.3 "damage inside body k delivers k-1 with one `malformed`; bytes after
	// the pad are `malformed`; newer_form, and message_form_as_file from the
	// file reader."
	t.Run("damage_and_framing", func(t *testing.T) {
		t.Parallel()
		u := messageLoadUnit(t, root, units["backenddemo"])
		ann := filepath.Join(root, connections["backenddemo"].path)
		body := `    vocab =
      case M.announce_read(File.read!(file())) do
        {:ok, v} -> v
        other -> raise "announce_read refused the reference's announcement: #{inspect(other)}"
      end

    # "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1" (docs/SPEC-TABLES.md
    # §3.3): the reference pins a three-body LoginRequest batch with body 2's
    # field reference planted at 63, thirty past E.
    {:malformed, 1, [first]} = M.load_messages(vocab, File.read!(golden("backend_batch_damaged_second_message.bin")), 3)
    check(length(first) == 1, "the first body did not stand")

    # "A LENGTH, COUNT OR INDEX WHOSE PAYLOAD RUNS PAST THE BATCH" and "the
    # trailing pad to the byte boundary is not zero" (§3.3).
    {:malformed, 1, _} = M.load_messages(vocab, File.read!(golden("store_full_bad_pad_message.bin")))
    # "BYTES AFTER THE PAD ARE MALFORMED" (§3.3).
    {:malformed, 1, _} = M.load_messages(vocab, File.read!(golden("store_full_trailing_byte_message.bin")))

    # "The FORM BYTE is a whole byte and is read FIRST ... a form a reader does
    # not know is a REFUSAL by name and never damage." (docs/SPEC-TABLES.md §3.3)
    check({:error, :newer_form} == M.load_messages(vocab, <<3, 0, 0>>), "form byte 3 decoded")
    check({:error, :newer_form} == M.load_messages(vocab, <<1, 0, 0>>), "form byte 1 decoded as a message")

    # "a reader handed a form-2 wire where a file was expected refuses by name"
    # (docs/SPEC-TABLES.md §3.3).
    {:error, :message_form_as_file, _} = M.announce_read(<<2, 0, 0, 0>>)
`
		out, err := runMessageProbe(t, u, ann, body)
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	})
}

// roundTripBody is the 2.1 probe: LoadMessages the batch, compare the decoded
// body against the values the form-1 instance's pinned text pins, SaveMessages
// the values back, and MeasureMessages the length.
func roundTripBody(row messageBatchRow, expected string) string {
	b := `    vocab =
      case M.announce_read(File.read!(file())) do
        {:ok, v} -> v
        other -> raise "announce_read refused the reference's announcement: #{inspect(other)}"
      end

    # "THE TWO FORMS ARE TWO ENCODINGS OF ONE VALUE ... loading either form and
    # saving the other, which reproduces the other's pinned bytes", and "a
    # message's text is its variable form's text, byte for byte"
    # (docs/SPEC-TABLES.md §3.3): the batch must decode to the value the
    # form-1 instance's pinned text holds.
    batch = File.read!(batch_file())
    {:ok, values} = M.load_messages(vocab, batch)
    check(values == ` + expected + `, "LoadMessages did not give the values the form-1 instance decodes to")
    {:ok, again} = M.save_messages(vocab, values)
    check(again == batch, "SaveMessages did not reproduce the batch bytes exactly")
    {:ok, measure} = M.measure_messages(vocab, values)
    check(measure == byte_size(batch), "MeasureMessages differs from the batch length")
`
	return b
}

// expectedMessageBody renders the batch body a `message` row's form-1 instance
// pins, as an Elixir list literal in the exact shape LoadMessages returns: one
// body, a list of `{slot, value}` pairs in declared order, a field at its
// declared default elided exactly as the reference's writer elides it. The
// value is the PINS' — testdata/conformance/tables/json/<name>.json — never a
// list copied into this file (docs/SPEC-TABLES.md §3.3: "a message's text is
// its variable form's text, byte for byte").
func expectedMessageBody(t *testing.T, u *ir.Unit, st *ir.Struct, jsonPath string) string {
	t.Helper()
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("the pinned instance text %s is unreadable: %v", jsonPath, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("the pinned instance text %s is not an object: %v", jsonPath, err)
	}
	slots := ir.TableVocabularySlots(u)
	return "[" + expectedStructBody(t, u, slots, st, obj) + "]"
}

// expectedStructBody renders one table's decoded body: the `{slot, value}`
// pairs for its non-default fields in declared order.
func expectedStructBody(t *testing.T, u *ir.Unit, slots map[string]uint64, st *ir.Struct, obj map[string]any) string {
	t.Helper()
	var parts []string
	for _, f := range st.Fields {
		raw, ok := obj[f.Name]
		if !ok {
			continue
		}
		lit, present := expectedFieldValue(t, u, slots, f, raw)
		if !present {
			continue
		}
		slot, ok := slots[ir.TableFieldEntry(f).Key()]
		if !ok {
			t.Fatalf("field %s of %s has no announced slot", f.Name, st.Name)
		}
		parts = append(parts, fmt.Sprintf("{%d, %s}", slot, lit))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// expectedFieldValue translates one pinned JSON value into the term
// LoadMessages decodes for that field, and present=false where the value is the
// field's declared default — the field the reference's writer elides from the
// wire (SPEC §5's zero initialization).
func expectedFieldValue(t *testing.T, u *ir.Unit, slots map[string]uint64, f *ir.Field, raw any) (string, bool) {
	t.Helper()
	switch {
	case f.Array != ir.ArrayNone:
		arr, ok := raw.([]any)
		if !ok {
			t.Fatalf("field %s holds %T, want an array", f.Name, raw)
		}
		return expectedArrayValue(t, u, slots, f, arr), true
	case f.Type.Kind == ir.TNamed:
		if e, ok := f.Type.Ref.(*ir.Enum); ok {
			name, ok := raw.(string)
			if !ok {
				t.Fatalf("enum field %s holds %T, want a string", f.Name, raw)
			}
			if name == "None" {
				return "", false
			}
			return expectedEnumSlot(t, slots, e, name), true
		}
		t.Fatalf("named field %s refers to %T, which this card's fixed roots do not carry", f.Name, f.Type.Ref)
	case f.Type.Kind == ir.TBytes:
		s, ok := raw.(string)
		if !ok {
			t.Fatalf("bytes field %s holds %T, want a string", f.Name, raw)
		}
		if s == "" {
			return "", false
		}
		data, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			t.Fatalf("bytes field %s is not base64: %v", f.Name, err)
		}
		var bytes []string
		for _, b := range data {
			bytes = append(bytes, strconv.Itoa(int(b)))
		}
		return "[" + strings.Join(bytes, ", ") + "]", true
	case f.Type.Kind == ir.TString:
		s, ok := raw.(string)
		if !ok {
			t.Fatalf("string field %s holds %T, want a string", f.Name, raw)
		}
		if s == "" {
			return "", false
		}
		return fmt.Sprintf("%q", s), true
	case f.Type.Kind == ir.TInt:
		n, ok := raw.(json.Number)
		if !ok {
			t.Fatalf("integer field %s holds %T, want a number", f.Name, raw)
		}
		def := "0"
		if f.HasDefault && f.DefInt != nil {
			def = f.DefInt.String()
		}
		if n.String() == def {
			return "", false
		}
		return n.String(), true
	}
	t.Fatalf("field %s of kind %v is not a kind this card's fixed roots carry", f.Name, f.Type.Kind)
	return "", false
}

// expectedArrayValue renders one fixed array's decoded elements: the element is
// the field's named type with the array shape stripped, a table for every
// array this card's roots carry.
func expectedArrayValue(t *testing.T, u *ir.Unit, slots map[string]uint64, f *ir.Field, arr []any) string {
	t.Helper()
	st, ok := f.Type.Ref.(*ir.Struct)
	if !ok {
		t.Fatalf("array field %s holds element type %T, which this card's fixed roots do not carry", f.Name, f.Type.Ref)
	}
	var elems []string
	for _, e := range arr {
		obj, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("array field %s holds element %T, want an object", f.Name, e)
		}
		elems = append(elems, expectedStructBody(t, u, slots, st, obj))
	}
	return "[" + strings.Join(elems, ", ") + "]"
}

// expectedEnumSlot renders a selected enum variant as the reference LoadMessages
// decodes one: the variant's own announced slot (kind 0), never a name
// (docs/SPEC-TABLES.md §3.3).
func expectedEnumSlot(t *testing.T, slots map[string]uint64, e *ir.Enum, name string) string {
	t.Helper()
	for i := range e.Variants {
		if e.Variants[i] != name {
			continue
		}
		entry := ir.TableVocabularyEntry{Id: ir.TableWireId(e.VariantWireName(i))}
		slot, ok := slots[entry.Key()]
		if !ok {
			t.Fatalf("enum %s variant %s has no announced slot", e.Name, name)
		}
		return strconv.FormatUint(slot, 10)
	}
	t.Fatalf("enum %s has no variant named %s", e.Name, name)
	return ""
}

// runMessageBatchProbe generates the table library and runs one generated
// probe against the batch surface: `file` is the announcement, `batch_file` the
// message-form wire, and `golden` the forged files beside them.
func runMessageBatchProbe(t *testing.T, u *ir.Unit, ann, batch, body string) (string, error) {
	t.Helper()
	elixirBin := elixirBinary(t)
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	module, ok := tables[TableMessageModule+".ex"]
	if !ok {
		t.Fatalf("the unit grows no %s.ex; files are %v", TableMessageModule, keysOf(tables))
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, TableMessageModule+".ex"), module, 0o600); err != nil {
		t.Fatal(err)
	}
	ns := ir.GoExportName(u.Package)
	goldenDir := filepath.Dir(ann)
	probe := fmt.Sprintf(`# GENERATED BY internal/codegen/elixirtable/messageform_batch_test.go.
defmodule Probe do
  import Bitwise
  alias %[1]s.TableMessage, as: M

  def file, do: %[2]s
  def batch_file, do: %[3]s
  def golden(name), do: Path.join(%[4]s, name)

  def check(true, _what), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  def run do
%[5]s
    if Process.get(:failed), do: System.halt(1), else: IO.puts("ok")
  end
end

Probe.run()
`, ns, elixirString(ann), elixirString(batch), elixirString(goldenDir), indentBody(body))
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(elixirBin, "-r", TableMessageModule+".ex", "probe.exs")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}
