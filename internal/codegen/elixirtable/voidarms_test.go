package elixirtable_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestPayloadFreeArmsOnTheTableWireElixir pins the PAYLOAD-FREE union arm on
// the Elixir variable form (docs/SPEC-TABLES.md §2.6, §3's kind-32 paragraph,
// §16; SPEC §4.8 on the packet side): such an arm has no storage and rides as
// the arm id under kind 32 with L = 0; its value is the arm's name alone, the
// tag of `{arm, payload}` with no payload.
//
//	(a) a round trip of test/tables/A1.schema's Root with each kind of arm
//	    selected, the payload-free arm `f` among them, byte-equal to
//	    internal/tablewire's bytes for the same value in both directions, and
//	    the text equal to internal/tabletext's; and the committed message_ack
//	    instance of tables/messages read, re-saved and written as text.
//	(b) L = 1 under kind 32 is the arm's own framing damage (malformed, the
//	    union None, the parent reading on), and a payload under kind 32's arm
//	    id or kind 32 under a payload arm's id is an ordinary kind mismatch —
//	    each judged by internal/tablewire's report for the same bytes.
//	(c) `schema generate --lang elixir` accepts A1, tables/messages and A2
//	    through the COMPILER path (GatherPaths, Load, Generate), not the
//	    backend's Generate alone.
//
// (a) and (b) run the leg's own toolchain, so they are FUNCTIONAL tests: make
// tables-elixir-void-arms runs this under SCHEMA_REQUIRE_CORPUS=1, where a
// missing toolchain fails by name. (c) only reads generator output.
func TestPayloadFreeArmsOnTheTableWireElixir(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("c_compiler_path", func(t *testing.T) {
		t.Parallel()
		for _, rel := range []string{"test/tables/A1.schema", "tables/messages", "test/tables/A2.schema"} {
			files, _ := voidArmsGenerate(t, root, rel)
			table, ok := files["Table.ex"]
			if !ok {
				t.Fatalf("schema generate --lang elixir %s emitted no Table.ex", rel)
			}
			if rel != "test/tables/A2.schema" && !bytes.Contains(table, []byte("kind: 32, arm_kind: 32, t: :void")) {
				t.Fatalf("schema generate --lang elixir %s: Table.ex carries no payload-free arm descriptor", rel)
			}
			if !bytes.Contains(table, []byte("PAYLOAD-FREE ARM (§2.6) has no storage, so its value is the arm's name")) {
				t.Fatalf("schema generate --lang elixir %s: the Table module doc does not name the payload-free arm's value", rel)
			}
		}
	})

	t.Run("a_round_trip", func(t *testing.T) {
		t.Parallel()
		elixirBin := elixirtable.ElixirBinaryForTest(t)
		files, unit := voidArmsGenerate(t, root, "test/tables/A1.schema")
		files = voidArmsTableOnly(files)
		m := tabletext.NewModel(unit)
		cases := []struct{ name, text, value, arm string }{
			{"none", `{"id": 11, "tail": 22}`, `%{"id" => 11, "tail" => 22}`, `nil`},
			{"int_a", `{"id": 11, "value": {"a": -7}, "tail": 22}`, `%{"id" => 11, "value" => {"a", -7}, "tail" => 22}`, `{"a", -7}`},
			{"int_c", `{"id": 11, "value": {"c": 2147483647}, "tail": 22}`, `%{"id" => 11, "value" => {"c", 2147483647}, "tail" => 22}`, `{"c", 2147483647}`},
			{"float_d", `{"id": 11, "value": {"d": 1.5}, "tail": 22}`, `%{"id" => 11, "value" => {"d", 1.5}, "tail" => 22}`, `{"d", 1.5}`},
			{"body_e", `{"id": 11, "value": {"e": {"n": 3}}, "tail": 22}`, `%{"id" => 11, "value" => {"e", %{"n" => 3}}, "tail" => 22}`, `{"e", %{"n" => 3}}`},
			{"payload_free_f", `{"id": 11, "value": {"f": null}, "tail": 22}`, `%{"id" => 11, "value" => "f", "tail" => 22}`, `"f"`},
		}
		dir := t.TempDir()
		names := elixirtable.WriteModuleDirForTest(t, dir, files)
		probe := voidArmsProbeHeader()
		for _, c := range cases {
			wire, text := voidArmsOracle(t, m, c.text)
			if c.name == "payload_free_f" && !bytes.Contains(wire, []byte{0x0f, 0x03, 0x20, 0x00}) {
				t.Fatalf("the oracle's payload-free arm is not kind 32 with L = 0: % x", wire)
			}
			wf := filepath.Join(dir, c.name+".bin")
			tf := filepath.Join(dir, c.name+".json")
			if err := os.WriteFile(wf, wire, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tf, text, 0o600); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(probe, `
try do
  name = %[1]q
  wire = File.read!(%[2]q)
  text = File.read!(%[3]q)
  native = %[4]s
  {:ok, out} = Tbla1.Table.save("Root", native)
  Probe.check(out == wire, name, "save of the native value equals the tablewire bytes: " <> inspect(out, limit: :infinity) <> " want " <> inspect(wire, limit: :infinity))
  {:ok, v, r} = Tbla1.Table.load("Root", wire)
  Probe.check(Probe.clean(r), name, "clean load: " <> inspect(r))
  Probe.check(Map.get(v, "value") == %[5]s, name, "load reads the arm as " <> inspect(%[5]s) <> ", got " <> inspect(Map.get(v, "value")))
  Probe.check(Map.get(v, "id") == 11 and Map.get(v, "tail") == 22, name, "siblings: " <> inspect(v))
  {:ok, out2} = Tbla1.Table.save("Root", v)
  Probe.check(out2 == wire, name, "re-save byte for byte: " <> inspect(out2, limit: :infinity))
  {:ok, json} = Tbla1.Table.to_json("Root", native)
  Probe.check(json == text, name, "to_json matches tabletext:\n" <> json <> "\nwant\n" <> text)
  {:ok, back, br} = Tbla1.Table.from_json("Root", text)
  Probe.check(Probe.clean(br) and br.clamped == 0, name, "clean text read: " <> inspect(br))
  Probe.check(Map.get(back, "value") == %[5]s, name, "from_json reads the arm as " <> inspect(%[5]s) <> ", got " <> inspect(Map.get(back, "value")))
  {:ok, out3} = Tbla1.Table.save("Root", back)
  Probe.check(out3 == wire, name, "the text read back re-saves byte for byte: " <> inspect(out3, limit: :infinity))
rescue
  e -> Probe.check(false, %[1]q, Exception.format(:error, e, __STACKTRACE__))
end
`, c.name, wf, tf, c.value, c.arm)
		}
		probe.WriteString(voidArmsProbeFooter)
		out := elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe.String())
		if !strings.Contains(out, "ok") {
			t.Fatalf("probe: %s", out)
		}

		// THE COMMITTED INSTANCE, message_ack (testdata/conformance/tables/MANIFEST.txt):
		// the Go engine's wire and text are the oracle.
		mfiles, _ := voidArmsGenerate(t, root, "tables/messages")
		mdir := t.TempDir()
		mnames := elixirtable.WriteModuleDirForTest(t, mdir, mfiles)
		mp := voidArmsProbeHeader()
		fmt.Fprintf(mp, `
try do
  name = "message_ack"
  wire = File.read!(%[1]q)
  text = File.read!(%[2]q)
  {:ok, v, r} = Messagedemo.Table.load("ToolMessage", wire)
  Probe.check(Probe.clean(r), name, "clean load: " <> inspect(r))
  Probe.check(Map.get(v, "body") == "ack", name, "load reads the payload-free arm as \"ack\", got " <> inspect(Map.get(v, "body")))
  {:ok, out} = Messagedemo.Table.save("ToolMessage", v)
  Probe.check(out == wire, name, "re-save byte for byte: " <> inspect(out, limit: :infinity))
  {:ok, json} = Messagedemo.Table.to_json("ToolMessage", v)
  Probe.check(json == text, name, "to_json matches the Go oracle's text:\n" <> json)
  {:ok, back, br} = Messagedemo.Table.from_json("ToolMessage", text)
  Probe.check(Probe.clean(br), name, "clean text read: " <> inspect(br))
  Probe.check(Map.get(back, "body") == "ack", name, "from_json reads \"ack\", got " <> inspect(Map.get(back, "body")))
  {:ok, out2} = Messagedemo.Table.save("ToolMessage", back)
  Probe.check(out2 == wire, name, "the text read back re-saves byte for byte")
rescue
  e -> Probe.check(false, "message_ack", Exception.format(:error, e, __STACKTRACE__))
end
`, filepath.Join(root, "testdata", "wire", "tables", "message_ack.bin"),
			filepath.Join(root, "testdata", "conformance", "tables", "json", "message_ack.json"))
		mp.WriteString(voidArmsProbeFooter)
		elixirtable.RunProbeForTest(t, elixirBin, mdir, mnames, mp.String())
	})

	t.Run("b_malformed_and_kind_mismatch", func(t *testing.T) {
		t.Parallel()
		elixirBin := elixirtable.ElixirBinaryForTest(t)
		files, unit := voidArmsGenerate(t, root, "test/tables/A1.schema")
		files = voidArmsTableOnly(files)
		m := tabletext.NewModel(unit)
		voidWire, _ := voidArmsOracle(t, m, `{"id": 11, "value": {"f": null}, "tail": 22}`)
		intWire, _ := voidArmsOracle(t, m, `{"id": 11, "value": {"a": -7}, "tail": 22}`)
		// the union field is `<ref> 15` then ONE arm header: the arm id's
		// reference, the arm's kind byte, L and the payload (§3). Both values
		// intern id, value, the arm and tail in that order, so the arm is ref 3.
		voidArm := []byte{0x0f, 0x03, 0x20, 0x00}
		intArm := []byte{0x0f, 0x03, 0x04, 0x04, 0xf9, 0xff, 0xff, 0xff}
		cases := []struct {
			name         string
			wire         []byte
			old, new     []byte
			malformed    bool
			kindMismatch int
		}{
			// L = 1 under kind 32: the arm's own framing damage
			{"void_l_one", voidWire, voidArm, []byte{0x0f, 0x03, 0x20, 0x01, 0x00}, true, 0},
			// a payload (int32, kind 4) under the payload-free arm's id
			{"payload_where_void", voidWire, voidArm, []byte{0x0f, 0x03, 0x04, 0x04, 0x01, 0x00, 0x00, 0x00}, false, 1},
			// kind 32 under an int32 arm's id
			{"void_where_payload", intWire, intArm, []byte{0x0f, 0x03, 0x20, 0x00}, false, 1},
		}
		dir := t.TempDir()
		names := elixirtable.WriteModuleDirForTest(t, dir, files)
		probe := voidArmsProbeHeader()
		for _, c := range cases {
			if bytes.Count(c.wire, c.old) != 1 {
				t.Fatalf("%s: the arm header % x is not once in % x", c.name, c.old, c.wire)
			}
			wire := bytes.Replace(c.wire, c.old, c.new, 1)
			// THE ORACLE for these bytes: internal/tablewire's report and value.
			inst := m.New(m.Lookup("Root"))
			var rep tabletext.Report
			if _, err := tablewire.Decode(m, inst, wire, &rep); err != nil {
				t.Fatalf("%s: oracle decode: %v", c.name, err)
			}
			if rep.Malformed != c.malformed || rep.KindMismatch != c.kindMismatch || rep.Unknown != 0 || rep.Widened != 0 || rep.Clamped != 0 {
				t.Fatalf("%s: the oracle's report %+v is not the expected shape (malformed=%v kind_mismatch=%d)", c.name, rep, c.malformed, c.kindMismatch)
			}
			text, err := m.Write(inst)
			if err != nil {
				t.Fatalf("%s: oracle text: %v", c.name, err)
			}
			if !bytes.Contains(text, []byte(`"tail": 22`)) || !bytes.Contains(text, []byte(`"value": {}`)) {
				t.Fatalf("%s: the oracle does not read the union None with the sibling intact:\n%s", c.name, text)
			}
			wf := filepath.Join(dir, c.name+".bin")
			if err := os.WriteFile(wf, wire, 0o600); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(probe, `
try do
  name = %[1]q
  {:ok, v, r} = Tbla1.Table.load("Root", File.read!(%[2]q))
  want = %%{unknown: %[3]d, kind_mismatch: %[4]d, widened: %[5]d, clamped: %[6]d, malformed: %[7]v}
  got = Map.take(r, [:unknown, :kind_mismatch, :widened, :clamped, :malformed])
  Probe.check(got == want, name, "report " <> inspect(got) <> " want the oracle's " <> inspect(want))
  Probe.check(Map.get(v, "value") == nil, name, "the union reads None, got " <> inspect(Map.get(v, "value")))
  Probe.check(Map.get(v, "id") == 11 and Map.get(v, "tail") == 22, name, "the parent reads on: " <> inspect(v))
rescue
  e -> Probe.check(false, %[1]q, Exception.format(:error, e, __STACKTRACE__))
end
`, c.name, wf, rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Malformed)
		}
		probe.WriteString(voidArmsProbeFooter)
		elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe.String())
	})
}

// voidArmsGenerate is `schema generate --lang elixir <rel>`: GatherPaths, Load
// and the target's Generate with its refusals.
func voidArmsGenerate(t *testing.T, root, rel string) (map[string][]byte, *ir.Unit) {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, rel)})
	if err != nil {
		t.Fatalf("schema generate %s: %v", rel, err)
	}
	c := compiler.New()
	unit, err := c.Load(paths)
	if err != nil {
		t.Fatalf("schema generate %s: load: %v", rel, err)
	}
	files, err := c.Generate(unit, "elixir", compiler.Options{})
	if err != nil {
		t.Fatalf("schema generate --lang elixir %s refused: %v", rel, err)
	}
	return files, unit
}

// voidArmsTableOnly keeps the variable form's codec and the BuildVersion it
// names: A1's fixed module loads its runtime on_load, and the probe's sorted
// load order would put A1Fixed.ex before FixedRuntime.ex; the variable form is
// the surface under test.
func voidArmsTableOnly(files map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for _, name := range []string{elixirtable.BuildVersionModule + ".ex", elixirtable.VariableTableModule + ".ex"} {
		if data, ok := files[name]; ok {
			out[name] = data
		}
	}
	return out
}

// voidArmsOracle reads one Root value's text with internal/tabletext and
// returns internal/tablewire's bytes for it and tabletext's canonical text.
func voidArmsOracle(t *testing.T, m *tabletext.Model, text string) ([]byte, []byte) {
	t.Helper()
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep != (tabletext.Report{}) {
		t.Fatalf("oracle text read %s: %+v", text, rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle encode %s: %v", text, err)
	}
	out, err := m.Write(inst)
	if err != nil {
		t.Fatalf("oracle text write %s: %v", text, err)
	}
	// a committed text file ends in one newline, as the harness writes it
	// (testdata/conformance/tables/json), and to_json spells that file
	if !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	return wire, out
}

func voidArmsProbeHeader() *strings.Builder {
	var b strings.Builder
	b.WriteString(`
defmodule Probe do
  def check(true, _name, _what), do: :ok
  def check(false, name, what) do
    IO.puts(:stderr, "FAIL " <> name <> ": " <> what)
    Process.put(:failed, true)
  end
  def clean(r), do: r.malformed == false and r.unknown == 0 and r.kind_mismatch == 0 and r.widened == 0
end
`)
	return &b
}

const voidArmsProbeFooter = "\nif Process.get(:failed), do: System.halt(1), else: IO.puts(\"ok\")\n"
