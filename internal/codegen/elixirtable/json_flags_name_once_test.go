package elixirtable

// A FLAGS ARRAY NAMES EACH KNOWN VARIANT ONCE (docs/SPEC-TABLES.md §16.2): a
// flags array that names one known variant twice refuses the read, as a
// repeated key does, because a conforming writer writes each set bit's name
// once. An unknown name keeps its treatment (skipped and counted), and a flags
// array inside a value the read only skips is held to the JSON grammar alone.
// The rows are the vectors under testdata/wire/json-flags-name-once; each is
// compared on the verdict alone with the Go engine's (internal/tabletext), and
// the engine is first held to the verdict the vectors state. Running the
// generated Elixir makes this a functional test, gated by slowtest.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const elixirJsonFlagsNameOnceDir = "../../../testdata/wire/json-flags-name-once"

type elixirJsonFlagsNameOnceRow struct {
	name, unit, root, text, verdict string
}

func elixirJsonFlagsNameOnceRows(t *testing.T) []elixirJsonFlagsNameOnceRow {
	t.Helper()
	f, err := os.Open(filepath.Join(elixirJsonFlagsNameOnceDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []elixirJsonFlagsNameOnceRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 6 {
			t.Fatalf("MANIFEST.tsv: %d columns, want 6: %q", len(c), line)
		}
		rows = append(rows, elixirJsonFlagsNameOnceRow{c[0], c[1], c[2], c[3], c[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// elixirJsonFlagsNameOnceOracle answers the Go engine's verdict for each row,
// and holds it to the verdict the vectors state.
func elixirJsonFlagsNameOnceOracle(t *testing.T, rows []elixirJsonFlagsNameOnceRow) []string {
	t.Helper()
	var want []string
	for _, row := range rows {
		u := loadDirUnit(t, filepath.Join("../../..", row.unit))
		model := tabletext.NewModel(u)
		def := model.Lookup(row.root)
		if def == nil {
			t.Fatalf("%s: the unit declares no %s", row.name, row.root)
		}
		src, err := os.ReadFile(filepath.Join(elixirJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		inst := model.New(def)
		var rep tabletext.Report
		verdict := "refused"
		if model.Read(inst, src, &rep) {
			verdict = "read"
		}
		if verdict != row.verdict {
			t.Fatalf("%s: the Go engine answers %s, the vectors state %s", row.name, verdict, row.verdict)
		}
		want = append(want, verdict)
	}
	return want
}

func TestElixirJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	rows := elixirJsonFlagsNameOnceRows(t)
	want := elixirJsonFlagsNameOnceOracle(t, rows)

	unit, root := rows[0].unit, rows[0].root
	for _, row := range rows {
		if row.unit != unit || row.root != root {
			t.Fatalf("%s: unit %s root %s; this test reads one unit and root, %s %s", row.name, row.unit, row.root, unit, root)
		}
	}
	u := loadDirUnit(t, filepath.Join("../../..", unit))
	mod := ir.GoExportName(u.Package) + ".Table"

	// The text read lives in the variable form's Table module and its runtime
	// support. The unit's other modules are not needed here, and a fixed-form
	// module that names a struct from another file can require a load order this
	// test does not otherwise care about.
	all := generateElixir(t, u)
	files := map[string][]byte{}
	for _, name := range []string{"BuildVersion.ex", "TableRuntime.ex", "FixedRuntime.ex", "Table.ex"} {
		src, ok := all[name]
		if !ok {
			t.Fatalf("Generate emitted no %s", name)
		}
		files[name] = src
	}

	var probe strings.Builder
	probe.WriteString(`
defmodule FlagsProbe do
 def record(name, got, want) do
  if got != want do
   IO.puts("FAIL " <> name <> ": got " <> got <> ", want " <> want)
   Process.put(:failures, Process.get(:failures, 0) + 1)
  end
 end
 def verdict(result) do
  case result do
   {:ok, _, _} -> "read"
   {:error, _, _} -> "refused"
   {:error, _} -> "refused"
   other -> raise("unexpected from_json result: " <> inspect(other))
  end
 end
end
`)
	for i, row := range rows {
		src, err := os.ReadFile(filepath.Join(elixirJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&probe, "FlagsProbe.record(%s, FlagsProbe.verdict(%s.from_json(%s, %s)), %s)\n",
			elixirString(row.name), mod, elixirString(row.root), elixirString(string(src)), elixirString(want[i]))
	}
	probe.WriteString(`if Process.get(:failures, 0) != 0, do: raise("flags name once assertions failed")
IO.puts("ok")
`)
	out, err := runArmProbe(t, elixirBinary(t), files, probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("flags name once: %v\n%s", err, out)
	}
}
