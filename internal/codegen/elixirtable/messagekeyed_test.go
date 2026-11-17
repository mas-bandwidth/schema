package elixirtable

// THE MESSAGE FORM'S ENUM-KEYED ARRAYS ON THE ELIXIR LEG (docs/SPEC-TABLES.md
// §3.2, §3.3). The message form reads and writes an enum-keyed array as kind
// 16: the present-slot count at bits_required(0, slots), then per slot a KEY
// REFERENCE and the element. The bytes are the Go engine's
// (tablewire.EncodeMessages) over the corpus keyed rows
// (tables/examples/Keyed.schema), both ways: LoadMessages decodes them to the
// value the pins hold, and SaveMessages writes them back byte for byte. The
// negative control sabotages the duplicate-key check in the generated module
// and requires the write refusal to go away with it.
//
// RED FIRST. The elixirtable package carried no kind-16 dispatch in
// encode_value and no keyed read in read_value, so a valid three-slot keyed
// body answered :malformed. The implementation is
// internal/codegen/elixirtable/messageform.go.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestElixirMessageKeyedArrays pins the message form's enum-keyed array codec
// against the Go engine's message bytes for the corpus keyed rows, both ways.
func TestElixirMessageKeyedArrays(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	elixirBin := elixirBinary(t)

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "tables", "examples", "Keyed.schema"))
	m := tabletext.NewModel(u)
	st := u.Tables["KeyedConfig"]
	if st == nil {
		t.Fatal("Keyed.schema declares no KeyedConfig root")
	}

	// The corpus keyed rows: `teams` and `hulls` key an array of tables, and
	// `scores.per_team` keys an array of a ranged int32. A hull carries floats,
	// which this card's fixed roots render elsewhere, so the value below pins
	// `teams` and `per_team` — a keyed array of tables and one of a ranged
	// integer — and leaves `hulls` at its declared default, which elides.
	const jsonText = `{"teams":{"Red":{"spawn_count":10,"banner":"red"},"Blue":{"spawn_count":20,"banner":"blue"}},"scores":{"per_team":{"Red":1000,"Blue":2000,"Green":3000}}}`

	val := m.New(st)
	var rep tabletext.Report
	if !m.Read(val, []byte(jsonText), &rep) || !rep.Silent() {
		t.Fatalf("read the keyed value: %+v", rep)
	}
	wire, err := tablewire.EncodeMessage(m, val)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}

	// The Go engine is the oracle, both ways: it decodes what it wrote.
	var vocab tablewire.Vocabulary
	if err := vocab.AnnounceRead(tablewire.Announce(u), &rep); err != nil {
		t.Fatalf("AnnounceRead: %v", err)
	}
	if ok, err := tablewire.DecodeMessage(m, m.New(st), wire, &vocab, &rep); !ok || err != nil || !rep.Silent() {
		t.Fatalf("the Go engine refused its own keyed batch: ok=%v err=%v rep=%+v", ok, err, rep)
	}

	// A stored key reference of zero is malformed (§3.2): zero the first keyed
	// array's first key reference and the Go engine refuses the batch too.
	corrupted := corruptFirstKeyReference(wire)
	var badRep tabletext.Report
	if ok, err := tablewire.DecodeMessage(m, m.New(st), corrupted, &vocab, &badRep); ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("the corrupted keyed batch decoded: ok=%v err=%v rep=%+v", ok, err, badRep)
	}

	slots := tablewire.MessageVocabularySlots(u)
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(jsonText))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		t.Fatal(err)
	}
	expected := expectedStructBody(t, u, slots, st, obj)

	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	module, ok := tables[TableMessageModule+".ex"]
	if !ok {
		t.Fatalf("the unit grows no %s.ex", TableMessageModule)
	}

	probe := messageKeyedProbe(ir.GoExportName(u.Package), hex.EncodeToString(wire), hex.EncodeToString(corrupted), expected)
	out, err := runMessageKeyedProbe(t, elixirBin, module, probe)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	// THE NEGATIVE CONTROL: the duplicate-key write refusal must come from the
	// check itself. Relax it in the generated module and the probe above must
	// go red.
	const have = "entry.kind == 0 and entry.id < 0xFFFFFFFFFFFFFFFD and not MapSet.member?(seen, entry.id)"
	const restored = "entry.kind == 0 and entry.id < 0xFFFFFFFFFFFFFFFD"
	bad := replaceModuleOnce(t, module, have, restored, "the duplicate-key write check")
	if badOut, badErr := runMessageKeyedProbe(t, elixirBin, bad, probe); badErr == nil || !strings.Contains(badOut, "duplicate key") {
		t.Fatalf("the negative control passed: the duplicate-key write check is not what refuses a repeated key\nerr=%v\n%s", badErr, badOut)
	}
}

// corruptFirstKeyReference zeroes the first keyed array's first key reference,
// the `None` reference no conforming writer emits (docs/SPEC-TABLES.md §3.2):
// byte 0 is the form byte and byte 1 the body count, then KeyedConfig's first
// field is `teams` — its 5-bit reference and the 2-bit present-slot count sit
// before the first key reference at bit 23.
func corruptFirstKeyReference(wire []byte) []byte {
	out := append([]byte(nil), wire...)
	out[2] &^= 0x80 // bit 23
	out[3] &^= 0x0F // bits 24..27
	return out
}

// messageKeyedProbe is the probe body: announce, load the batch, hold it to
// the pinned value, save it back byte for byte, measure it, refuse the damaged
// batch, and refuse a write whose key rides twice.
func messageKeyedProbe(ns, wireHex, corruptedHex, expected string) string {
	return fmt.Sprintf(`# GENERATED BY internal/codegen/elixirtable/messagekeyed_test.go.
defmodule Probe do
  import Bitwise
  alias %[1]s.TableMessage, as: M

  def check(true, _what), do: :ok

  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end

  def run do
    {:ok, vocab} = M.announce_read(M.announce())
    batch = Base.decode16!(%[2]q, case: :lower)
    damaged = Base.decode16!(%[3]q, case: :lower)

    {:ok, [value]} = M.load_messages(vocab, batch)
    check(value == %[4]s, "LoadMessages did not give the keyed value the pins hold")
    {:ok, ^batch} = M.save_messages(vocab, [value])
    {:ok, size} = M.measure_messages(vocab, [value])
    check(size == byte_size(batch), "MeasureMessages differs from the batch length")

    # a stored key reference of zero is malformed (§3.2)
    check({:malformed, 0, []} == M.load_messages(vocab, damaged), "a None key reference decoded")

    # a writer refuses a key that rides twice, in every build (§3.2, §5.1)
    case M.save_messages(vocab, [[{6, [{18, []}, {18, []}]}]]) do
      {:error, _} -> :ok
      other -> check(false, "duplicate key wrote: " <> inspect(other))
    end

    if Process.get(:failed), do: System.halt(1), else: IO.puts("ok")
  end
end

Probe.run()
`, ns, wireHex, corruptedHex, expected)
}

// runMessageKeyedProbe runs one generated probe against the TableMessage module
// alone, the way a consumer that imports only the message surface would.
func runMessageKeyedProbe(t *testing.T, elixirBin string, module []byte, probe string) (string, error) {
	slowtest.Gate(t, "an external toolchain")

	t.Helper()
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
