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
// capacity refusal, and `{:malformed, 0, []}` on damage, the packet
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
		if len(fields) < 6 || fields[0] != "message" || fields[5] == "-" { // a named refusal row (a variable table, §3.3) carries no message bytes
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
	// THE FORM IS FIXED TABLES ONLY (docs/SPEC-TABLES.md §3.3): a message row
	// whose root is a variable table is refused rather than decoded, so the
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

    # DAMAGE ANYWHERE IN THE BATCH DISCARDS THE WHOLE BATCH
    # (docs/SPEC-TABLES.md §3.3, §4): the reference pins a three-body
    # LoginRequest batch with body 2's field reference planted at 31, seven
    # past E, and no body is delivered.
    {:malformed, 0, []} = M.load_messages(vocab, File.read!(golden("backend_batch_damaged_second_message.bin")), 3)

    # "A LENGTH, COUNT OR INDEX WHOSE PAYLOAD RUNS PAST THE BATCH" and "the
    # trailing pad to the byte boundary is not zero" (§3.3).
    {:malformed, 0, []} = M.load_messages(vocab, File.read!(golden("login_full_bad_pad_message.bin")))
    # "BYTES AFTER THE PAD ARE MALFORMED" (§3.3).
    {:malformed, 0, []} = M.load_messages(vocab, File.read!(golden("login_full_trailing_byte_message.bin")))

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

	// Empty text and zero-width counts. The probes build their own
	// announcements and batches; they take no clock, socket, or network.
	// The Go skip oracle TestGoMessageSkipOfZeroWidthElementsIsBounded keeps
	// its flat 0xFFFFFFFF acceptance (zero iterations, no list). This reader
	// returns the list, so that announcement is malformed when the bits left
	// cannot back the count. Subtests stay inside this function so
	// tables-elixir-message-batch's -run filter reaches them.
	t.Run("bounds", func(t *testing.T) {
		t.Parallel()
		u := messageLoadUnit(t, root, units["backenddemo"])
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		module, ok := tables[TableMessageModule+".ex"]
		if !ok {
			t.Fatalf("the unit grows no %s.ex", TableMessageModule)
		}
		src, err := os.ReadFile(filepath.Join(root, "internal", "codegen", "elixirtable", "messageform.go"))
		if err != nil {
			t.Fatal(err)
		}
		ns := ir.GoExportName(u.Package)

		t.Run("source", func(t *testing.T) {
			t.Parallel()
			assertElixirBoundsSource(t, string(src), string(module))
		})

		t.Run("values", func(t *testing.T) {
			t.Parallel()
			out, err := runElixirModuleProbe(t, module, boundsValuesProbe(ns))
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})

		t.Run("known_bad_descending_range", func(t *testing.T) {
			t.Parallel()
			bad := sabotageDescendingEmptyRange(t, module)
			out, err := runElixirModuleProbe(t, bad, boundsDescendingProbe(ns))
			if err == nil || !strings.Contains(out, "empty text") || strings.Contains(out, "control did not restore") || strings.Contains(out, "CompileError") || strings.Contains(out, "SyntaxError") {
				t.Fatalf("known-bad descending range did not fail for the empty-text reason:\nerr=%v\n%s", err, out)
			}
		})

		t.Run("known_bad_count_loop", func(t *testing.T) {
			t.Parallel()
			bad := sabotageCountLoop(t, module)
			out, err := runElixirModuleProbe(t, bad, boundsCountProbe(ns))
			if err == nil || !strings.Contains(out, "count-driven zero-progress materialization") || strings.Contains(out, "control did not restore") || strings.Contains(out, "CompileError") || strings.Contains(out, "SyntaxError") {
				t.Fatalf("known-bad count loop did not fail for the count-driven reason:\nerr=%v\n%s", err, out)
			}
		})

		t.Run("known_bad_declared_capacity_cap", func(t *testing.T) {
			t.Parallel()
			bad := sabotageDeclaredCapacityCap(t, module)
			out, err := runElixirModuleProbe(t, bad, boundsDeclaredProbe(ns))
			if err == nil || !strings.Contains(out, "declared capacity shrank") || strings.Contains(out, "control did not restore") || strings.Contains(out, "CompileError") || strings.Contains(out, "SyntaxError") {
				t.Fatalf("known-bad declared capacity cap did not fail for the element-cap reason:\nerr=%v\n%s", err, out)
			}
		})
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

// assertElixirBoundsSource pins the empty-text clause, the zero-width
// materialization rule, and the batch comment. It does not spawn Elixir.
func assertElixirBoundsSource(t *testing.T, src, module string) {
	t.Helper()
	header := src
	if before, _, ok := strings.Cut(src, "\npackage "); ok {
		header = before
	}
	for _, bad := range []string{
		"ANNOUNCEMENT SURFACE only",
		"nothing here reads a form-",
		"are the NEXT",
	} {
		if strings.Contains(header, bad) {
			t.Errorf("messageform.go header still says %q", bad)
		}
	}
	if !strings.Contains(header, "form-`2` body") {
		t.Error("messageform.go header does not say the module reads and writes a form-2 body")
	}
	for _, want := range []string{
		"defp take_bytes({_int, _off, _total} = r, 0), do: {:ok, <<>>, r}",
		"for i <- 0..(n - 1)//1, into:",
		"defp zero_width_run(_vocab, shape, n, r, acc, v)",
		"shape.max == 0xFFFFFFFF and n > bits_left(r)",
		"min > 0xFFFFFFFF or max > 0xFFFFFFFF or max < min",
		"defp read_value(_vocab, 0, _shape, _elem, r), do: {:ok, :name, r}",
		"defp read_value(_vocab, 32, _shape, _elem, r), do: {:ok, :none, r}",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("messageform.go is missing %q", want)
		}
		if !strings.Contains(module, want) {
			t.Errorf("generated TableMessage is missing %q", want)
		}
	}
	const descending = "for i <- 0..(n - 1), into:"
	if strings.Contains(src, descending) || strings.Contains(module, descending) {
		t.Errorf("the unstepped range %s is still emitted", descending)
	}
	const elementCap = "n > vocab.max_bytes or"
	if strings.Contains(src, elementCap) || strings.Contains(module, elementCap) {
		t.Errorf("the vocabulary byte budget is still spent as an element cap (%s)", elementCap)
	}
}

// runElixirModuleProbe runs one probe against an already generated
// TableMessage module. The probe builds its announcements itself.
func runElixirModuleProbe(t *testing.T, module []byte, probe string) (string, error) {
	t.Helper()
	elixirBin := elixirBinary(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, TableMessageModule+".ex"), module, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(elixirBin, "-r", TableMessageModule+".ex", "probe.exs")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func boundsValuesProbe(ns string) string {
	return boundsProbe(ns, boundsValueFns, `
    empty_text_and_lengths()
    small_arrays()
    wide_counts()
    nested_counts()
    second_body_damage()
    fixed_nine_last_field()
    declared_capacity_above_budget()
`)
}

func boundsDescendingProbe(ns string) string {
	return boundsProbe(ns, boundsDescendingFn, `
    descending_control()
`)
}

func boundsCountProbe(ns string) string {
	return boundsProbe(ns, boundsCountFn, `
    count_control()
`)
}

func boundsDeclaredProbe(ns string) string {
	return boundsProbe(ns, boundsDeclaredFn, `
    declared_capacity_control()
`)
}

func boundsProbe(ns, fns, body string) string {
	return fmt.Sprintf(`# GENERATED BY internal/codegen/elixirtable/messageform_batch_test.go.
defmodule Probe do
  import Bitwise
  alias %s.TableMessage, as: M
%s
  def run do
%s
    if Process.get(:failed), do: System.halt(1), else: IO.puts("ok")
  end
end

Probe.run()
`, ns, fns, indentBody(body))
}

func sabotageDescendingEmptyRange(t *testing.T, module []byte) []byte {
	t.Helper()
	const have = `  defp take_bytes({_int, _off, _total} = r, 0), do: {:ok, <<>>, r}

  defp take_bytes({int, off, total}, n) when is_integer(n) and n > 0 do
    need = n * 8

    if off + need > total do
      :error
    else
      bytes = for i <- 0..(n - 1)//1, into: <<>>, do: <<int >>> (off + 8 * i) &&& 0xFF>>
      {:ok, bytes, {int, off + need, total}}
    end
  end`
	const restored = `  defp take_bytes({int, off, total}, n) do
    if off + n * 8 > total do
      :error
    else
      bytes = for i <- 0..(n - 1), into: <<>>, do: <<int >>> (off + 8 * i) &&& 0xFF>>
      {:ok, bytes, {int, off + n * 8, total}}
    end
  end`
	return replaceModuleOnce(t, module, have, restored, "descending empty range")
}

func sabotageCountLoop(t *testing.T, module []byte) []byte {
	t.Helper()
	const have = `      {:ok, v, ^r} ->
        zero_width_run(vocab, shape, n, r, acc, v)`
	const restored = `      {:ok, v, rest} when rest == r ->
        seen = Process.get(:zero_iters, 0) + 1
        Process.put(:zero_iters, seen)

        if seen > 32 do
          raise "count-driven zero-progress materialization"
        end

        read_elements(vocab, shape, elem, n - 1, rest, [v | acc], depth)`
	return replaceModuleOnce(t, module, have, restored, "count-driven zero-progress materialization")
}

func sabotageDeclaredCapacityCap(t *testing.T, module []byte) []byte {
	t.Helper()
	const have = `    if shape.max == 0xFFFFFFFF and n > bits_left(r) do`
	const restored = `    if n > _vocab.max_bytes do`
	return replaceModuleOnce(t, module, have, restored, "declared capacity element cap")
}

func replaceModuleOnce(t *testing.T, module []byte, old, new, what string) []byte {
	t.Helper()
	text := string(module)
	if strings.Count(text, old) != 1 {
		t.Fatalf("%s: the generated module does not contain the anchor once", what)
	}
	return []byte(strings.Replace(text, old, new, 1))
}

const boundsElixirHelpers = `
  def check(true, _what), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  defp leb(v) when v < 0x80, do: <<v>>

  defp leb(v) do
    <<(v &&& 0x7F) ||| 0x80>> <> leb(v >>> 7)
  end

  defp entry(id, payload), do: <<id::little-unsigned-64>> <> payload

  defp announce(words) do
    vocab_sub = <<6>> <> leb(byte_size(words)) <> words
    version = <<1, 9, 0, 0, 0, 0, 0, 0, 0, 0>>
    vocab = <<2, 14>> <> leb(byte_size(vocab_sub)) <> vocab_sub
    body = version <> vocab <> <<0>>
    <<1>> <> body <>
      <<0xFE, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
        0xFD, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
        2, 0, 0, 0, 0, 0, 0, 0>>
  end

  defp read!(words) do
    case M.announce_read(announce(words)) do
      {:ok, vocab} -> vocab
      other -> raise "announce_read #{inspect(other)}"
    end
  end

  defp ceiling(vocab) do
    case vocab.entries do
      [%{shape: %{min: 0xFFFFFFFF, max: 0xFFFFFFFF}} | _] -> :ok
      [e | _] -> check(false, "uint32 ceiling changed #{inspect(e.shape)}")
      [] -> check(false, "announcement kept no entry")
    end
  end

  defp round_trip(vocab, values) do
    case M.save_messages(vocab, [values]) do
      {:ok, batch} ->
        case M.load_messages(vocab, batch) do
          {:ok, [got]} when got == values -> :ok
          other -> check(false, "round trip got #{inspect_small(other)} want #{inspect(values)}")
        end

      other ->
        check(false, "save got #{inspect(other)}")
    end
  end

  defp refuse_wide(vocab) do
    case M.save_messages(vocab, [[{1, []}]]) do
      {:ok, batch} ->
        case M.load_messages(vocab, batch) do
          {:malformed, 0, []} -> :ok
          other -> check(false, "wide count #{inspect_small(other)}")
        end

      other ->
        check(false, "save wide #{inspect(other)}")
    end
  end

  defp inspect_small({:ok, bodies}) when is_list(bodies), do: "ok n=#{length(bodies)}"
  defp inspect_small({:malformed, k, bodies}) when is_list(bodies), do: "malformed #{k} n=#{length(bodies)}"
  defp inspect_small({:error, reason}), do: "error #{inspect(reason)}"
  defp inspect_small({:error, reason, extra}), do: "error #{inspect(reason)} #{inspect(extra)}"
  defp inspect_small(other), do: inspect(other)
`

const boundsValueFns = boundsElixirHelpers + `
  defp empty_text_and_lengths do
    words = entry(0x1000, <<12>> <> leb(15)) <> entry(0x1001, <<6, 0>>)
    vocab = read!(words)
    round_trip(vocab, [{1, <<>>}, {2, 7}])
    round_trip(vocab, [{1, <<"z">>}, {2, 1}])
    round_trip(vocab, [{1, :binary.copy(<<65>>, 15)}, {2, 2}])

    only = read!(entry(0x1000, <<12>> <> leb(15)))

    case M.save_messages(only, [[{1, <<"abcd">>}]]) do
      {:ok, batch} ->
        short = binary_part(batch, 0, byte_size(batch) - 2)

        case M.load_messages(only, short) do
          {:malformed, 0, []} -> :ok
          other -> check(false, "truncated text decoded #{inspect_small(other)}")
        end

      other ->
        check(false, "save of abcd #{inspect(other)}")
    end
  end

  defp small_arrays do
    uints = entry(0x1000, <<14>> <> leb(3) <> leb(3) <> <<6, 0>>) <> entry(0x1001, <<6, 0>>)
    round_trip(read!(uints), [{1, [9, 8, 7]}, {2, 4}])

    names = entry(0x1000, <<14>> <> leb(2) <> leb(2) <> <<0>>) <> entry(0x1001, <<6, 0>>)
    round_trip(read!(names), [{1, [:name, :name]}, {2, 5}])

    none = entry(0x1000, <<14>> <> leb(2) <> leb(2) <> <<32>>) <> entry(0x1001, <<6, 0>>)
    round_trip(read!(none), [{1, [:none, :none]}, {2, 6}])

    empty = entry(0x1000, <<14>> <> leb(0) <> leb(0) <> <<0>>) <> entry(0x1001, <<6, 0>>)
    round_trip(read!(empty), [{1, []}, {2, 8}])

    one = entry(0x1000, <<14>> <> leb(1) <> leb(1) <> <<0>>) <> entry(0x1001, <<6, 0>>)
    round_trip(read!(one), [{1, [:name]}, {2, 8}])

    ranged =
      entry(0x1000, <<14>> <> leb(2) <> leb(2) <> <<6, 1>> <> leb(0) <> leb(7)) <>
        entry(0x1001, <<6, 0>>)

    round_trip(read!(ranged), [{1, [7, 7]}, {2, 3}])
  end

  defp wide_counts do
    kind0 = read!(entry(0x1000, <<14>> <> leb(0xFFFFFFFF) <> leb(0xFFFFFFFF) <> <<0>>))
    ceiling(kind0)
    refuse_wide(kind0)

    kind32 = read!(entry(0x1000, <<14>> <> leb(0xFFFFFFFF) <> leb(0xFFFFFFFF) <> <<32>>))
    ceiling(kind32)
    refuse_wide(kind32)

    # An unbounded list (min = 0, max = 0xFFFFFFFF) carrying a hostile wire
    # count (9 elements in a 6-byte batch with only 7 trailing bits) is
    # refused without allocating the list.
    unbounded = read!(entry(0x1000, <<14>> <> leb(0) <> leb(0xFFFFFFFF) <> <<0>>))

    case M.load_messages(unbounded, <<2, 0, 19, 0, 0, 0, 0>>) do
      {:malformed, 0, []} -> :ok
      other -> check(false, "unbounded zero-width count #{inspect_small(other)}")
    end

    # The unbounded count is compared with n > bits_left, each element costing
    # at least one bit: a count equal to the seven bits left is backed by the
    # wire and is accepted, and one past it is refused. The body is five bytes,
    # so the 1-bit reference and 32-bit count leave seven bits.
    case M.load_messages(unbounded, <<2, 0, 15, 0, 0, 0, 0>>) do
      {:ok, [[{1, seven}]]} ->
        if seven == List.duplicate(:name, 7) do
          :ok
        else
          check(false, "unbounded count at the bit bound #{inspect_small({:ok, [[{1, seven}]]})}")
        end

      other ->
        check(false, "unbounded count at the bit bound #{inspect_small(other)}")
    end

    case M.load_messages(unbounded, <<2, 0, 17, 0, 0, 0, 0>>) do
      {:malformed, 0, []} -> :ok
      other -> check(false, "unbounded count past the bit bound #{inspect_small(other)}")
    end
  end

  defp nested_counts do
    wide =
      entry(0x1000, <<14>> <> leb(0xFFFFFFFF) <> leb(0xFFFFFFFF) <> <<14>> <> leb(0) <> leb(0) <> <<0>>)

    vocab = read!(wide)
    ceiling(vocab)
    refuse_wide(vocab)

    small =
      entry(0x1000, <<14>> <> leb(2) <> leb(2) <> <<14>> <> leb(2) <> leb(2) <> <<0>>) <>
        entry(0x1001, <<6, 0>>)

    round_trip(read!(small), [{1, [[:name, :name], [:name, :name]]}, {2, 9}])
  end

  defp second_body_damage do
    words =
      entry(0x1000, <<12>> <> leb(15)) <>
        entry(0x2000, <<14>> <> leb(0xFFFFFFFF) <> leb(0xFFFFFFFF) <> <<0>>)

    vocab = read!(words)

    case M.save_messages(vocab, [[{1, "hi"}], [{2, []}]]) do
      {:ok, batch} ->
        case M.load_messages(vocab, batch) do
          {:malformed, 0, []} -> :ok
          other -> check(false, "second body damage #{inspect_small(other)}")
        end

      other ->
        check(false, "save of two bodies #{inspect(other)}")
    end
  end

  defp fixed_nine_last_field do
    words = entry(0x1000, <<14>> <> leb(9) <> leb(9) <> <<0>>)
    vocab = read!(words)

    # One announced array entry, ref_bits 1. A fixed zero-width count is a
    # schema constant: the batch is <<2, 0, 1>> and the body is the reference
    # bit, the nine zero-width elements, the terminator bit and the pad. There
    # is no following payload or padding to back the count, and it must still
    # round-trip exactly.
    case M.save_messages(vocab, [[{1, List.duplicate(:name, 9)}]]) do
      {:ok, <<2, 0, 1>>} ->
        case M.load_messages(vocab, <<2, 0, 1>>) do
          {:ok, [[{1, nine}]]} ->
            if nine == List.duplicate(:name, 9) do
              :ok
            else
              check(false, "fixed nine last field #{inspect_small({:ok, [[{1, nine}]]})}")
            end

          other -> check(false, "fixed nine last field #{inspect_small(other)}")
        end

      other ->
        check(false, "fixed nine save #{inspect_small(other)}")
    end

    # A bounded variable zero-width array (min = 0, max = 9) as the last field
    # of a batch writes 1 ref bit + 4 count bits + 1 terminator bit + 2 pad
    # bits = <<2, 0, 19>> (only 3 bits remain after the count is read), and
    # must also round-trip all 9 elements without depending on trailing bits.
    bounded = read!(entry(0x1000, <<14>> <> leb(0) <> leb(9) <> <<0>>))

    case M.save_messages(bounded, [[{1, List.duplicate(:name, 9)}]]) do
      {:ok, <<2, 0, 19>>} ->
        case M.load_messages(bounded, <<2, 0, 19>>) do
          {:ok, [[{1, nine}]]} ->
            if nine == List.duplicate(:name, 9) do
              :ok
            else
              check(false, "bounded nine last field #{inspect_small({:ok, [[{1, nine}]]})}")
            end

          other -> check(false, "bounded nine last field #{inspect_small(other)}")
        end

      other ->
        check(false, "bounded nine save #{inspect_small(other)}")
    end
  end

  defp declared_capacity_above_budget do
    # vocab.max_bytes bounds the announcement's bytes, not an array's declared
    # element count. A fixed zero-width array of 70_000 elements is above the
    # 65_536-byte default and is still legitimate required capacity.
    n = 70_000
    words = entry(0x1000, <<14>> <> leb(n) <> leb(n) <> <<0>>)
    vocab = read!(words)

    case M.save_messages(vocab, [[{1, List.duplicate(:name, n)}]]) do
      {:ok, batch} ->
        case M.load_messages(vocab, batch) do
          {:ok, [[{1, got}]]} when length(got) == n -> :ok
          other -> check(false, "declared zero-width capacity shrank #{inspect_small(other)}")
        end

      other ->
        check(false, "declared zero-width capacity save #{inspect_small(other)}")
    end

    # The same capacity is legitimate when the count is a bounded variable,
    # not only when min equals max.
    bounded = read!(entry(0x1000, <<14>> <> leb(0) <> leb(n) <> <<0>>))

    case M.save_messages(bounded, [[{1, List.duplicate(:name, n)}]]) do
      {:ok, batch} ->
        case M.load_messages(bounded, batch) do
          {:ok, [[{1, got}]]} when length(got) == n -> :ok
          other -> check(false, "bounded zero-width capacity shrank #{inspect_small(other)}")
        end

      other ->
        check(false, "bounded zero-width capacity save #{inspect_small(other)}")
    end
  end
`

const boundsDescendingFn = boundsElixirHelpers + `
  defp descending_control do
    words = entry(0x1000, <<12>> <> leb(15)) <> entry(0x1001, <<6, 0>>)
    vocab = read!(words)

    try do
      case M.save_messages(vocab, [[{1, <<>>}, {2, 7}]]) do
        {:ok, batch} ->
          case M.load_messages(vocab, batch) do
            {:ok, [[{1, text}, {2, _}]]} when text != <<>> ->
              IO.puts(:stderr, "empty text")

            {:ok, [[{1, <<>>}, {2, 7}]]} ->
              IO.puts(:stderr, "control did not restore the descending empty range")

            other ->
              IO.puts(:stderr, "control did not restore the descending empty range: #{inspect_small(other)}")
          end

        other ->
          IO.puts(:stderr, "control did not restore the descending empty range: #{inspect(other)}")
      end
    rescue
      e ->
        IO.puts(:stderr, "empty text raised (descending range): #{Exception.message(e)}")
    end

    System.halt(1)
  end
`

const boundsCountFn = boundsElixirHelpers + `
  defp count_control do
    words = entry(0x1000, <<14>> <> leb(0xFFFFFFFF) <> leb(0xFFFFFFFF) <> <<0>>)
    vocab = read!(words)

    try do
      case M.save_messages(vocab, [[{1, []}]]) do
        {:ok, batch} ->
          case M.load_messages(vocab, batch) do
            other ->
              IO.puts(:stderr, "control did not restore the count-driven loop: #{inspect_small(other)}")
          end

        other ->
          IO.puts(:stderr, "control did not restore the count-driven loop: #{inspect(other)}")
      end
    rescue
      e ->
        msg = Exception.message(e)

        if String.contains?(msg, "count-driven zero-progress materialization") do
          IO.puts(:stderr, msg)
        else
          IO.puts(:stderr, "control did not restore the count-driven loop: #{msg}")
        end
    end

    System.halt(1)
  end
`

const boundsDeclaredFn = boundsElixirHelpers + `
  defp declared_capacity_control do
    n = 70_000
    words = entry(0x1000, <<14>> <> leb(n) <> leb(n) <> <<0>>)
    vocab = read!(words)

    case M.save_messages(vocab, [[{1, List.duplicate(:name, n)}]]) do
      {:ok, batch} ->
        case M.load_messages(vocab, batch) do
          {:ok, [[{1, got}]]} when length(got) == n ->
            IO.puts(:stderr, "control did not restore the declared capacity cap: #{inspect_small({:ok, batch})}")

          _ ->
            IO.puts(:stderr, "declared capacity shrank under an element cap")
        end

      other ->
        IO.puts(:stderr, "declared capacity shrank under an element cap: #{inspect(other)}")
    end

    System.halt(1)
  end
`
