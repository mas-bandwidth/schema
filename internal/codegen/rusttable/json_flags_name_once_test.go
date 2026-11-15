package rusttable_test

// A FLAGS ARRAY NAMES EACH KNOWN VARIANT ONCE (docs/SPEC-TABLES.md §16.2): a
// flags array that names one known variant twice refuses the read, as a
// repeated key does, because a conforming writer writes each set bit's name
// once. An unknown name keeps its treatment (skipped and counted), and a flags
// array inside a value the read only skips is held to the JSON grammar alone.
// The rows are the vectors under testdata/wire/json-flags-name-once; each is
// compared on the verdict alone with the Go engine's (internal/tabletext), and
// the engine is first held to the verdict the vectors state. Building the
// generated Rust with rustc makes this a functional test, gated by slowtest.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rusttable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const rustJsonFlagsNameOnceDir = "../../../testdata/wire/json-flags-name-once"

type rustJsonFlagsNameOnceRow struct {
	name, unit, root, text, verdict string
}

func rustJsonFlagsNameOnceRows(t *testing.T) []rustJsonFlagsNameOnceRow {
	t.Helper()
	f, err := os.Open(filepath.Join(rustJsonFlagsNameOnceDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []rustJsonFlagsNameOnceRow
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
		rows = append(rows, rustJsonFlagsNameOnceRow{c[0], c[1], c[2], c[3], c[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

func TestRustJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated JSON flags name-once probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}

	rows := rustJsonFlagsNameOnceRows(t)
	units := map[string]*ir.Unit{}
	models := map[string]*tabletext.Model{}
	for _, row := range rows {
		if models[row.unit] != nil {
			continue
		}
		paths, err := compiler.GatherPaths([]string{filepath.Join("../../..", row.unit)})
		if err != nil {
			t.Fatalf("%s: %v", row.unit, err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("%s: %v", row.unit, err)
		}
		units[row.unit] = u
		models[row.unit] = tabletext.NewModel(u)
	}

	// THE REFERENCE FIRST: the Go engine answers every row with the verdict the
	// vectors state. A row it answers differently refutes the premise.
	texts := map[string][]byte{}
	for _, row := range rows {
		model := models[row.unit]
		def := model.Lookup(row.root)
		if def == nil {
			t.Fatalf("%s: the unit declares no %s", row.unit, row.root)
		}
		text, err := os.ReadFile(filepath.Join(rustJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		texts[row.text] = text
		var report tabletext.Report
		verdict := "refused"
		if model.Read(model.New(def), text, &report) {
			verdict = "read"
		}
		if verdict != row.verdict {
			t.Fatalf("%s: the Go engine answers %s, the vectors state %s (%+v)", row.name, verdict, row.verdict, report)
		}
	}

	modules, err := rusttable.Generate(units[rows[0].unit])
	if err != nil {
		t.Fatalf("the unit was refused: %v", err)
	}
	root := t.TempDir()
	var mods []string
	for name, data := range modules {
		mod := strings.TrimSuffix(name, ".rs")
		if mod != rusttable.VariableRuntimeModule && mod != "table_enums" &&
			!strings.HasSuffix(mod, "_table") && !strings.HasSuffix(mod, "_variable") {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, mod)
	}

	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	for _, mod := range mods {
		main.WriteString("mod " + mod + ";\n")
	}
	main.WriteString(`
use tables_table::*;
use variable_runtime::*;

fn verdict(text: &[u8]) -> &'static str {
    let mut report = TableReport::default();
    match profile_config_from_json_bytes(text, &mut report) {
        Some(_) if !report.damaged => "read",
        _ => "refused",
    }
}

fn main() {
`)
	for _, row := range rows {
		fmt.Fprintf(&main, "    {\n        let text: &[u8] = &%s;\n        let got = verdict(text);\n        if got != %q { println!(\"MISMATCH %s: got {} want %s\", got); }\n    }\n",
			rustJsonFlagsNameOnceBytes(texts[row.text]), row.verdict, row.name, row.verdict)
	}
	main.WriteString("    println!(\"FLAGS PROBE DONE\");\n}\n")

	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "probe")
	build := exec.Command("rustc", "--edition", "2024", "-o", bin, "main.rs")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build (modules %v): %v\n%s", mods, err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "MISMATCH ") {
			t.Errorf("%s", line)
		}
	}
	if !strings.Contains(string(out), "FLAGS PROBE DONE") {
		t.Fatalf("the probe did not finish:\n%s", out)
	}
}

func rustJsonFlagsNameOnceBytes(b []byte) string {
	var parts []string
	for _, x := range b {
		parts = append(parts, fmt.Sprintf("0x%02xu8", x))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
