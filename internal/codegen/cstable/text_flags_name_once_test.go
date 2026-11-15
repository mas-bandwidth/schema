package cstable

// A FLAGS ARRAY NAMES EACH KNOWN VARIANT ONCE (docs/SPEC-TABLES.md §16.2): a
// flags array that names one known variant twice refuses the read, as a
// repeated key does, because a conforming writer writes each set bit's name
// once. An unknown name keeps its treatment, and a flags array inside a value
// the read only skips is held to the JSON grammar alone. The rows are the
// vectors under testdata/wire/json-flags-name-once; each is compared on the
// verdict alone with the Go engine's (internal/tabletext), and the engine is
// first held to the verdict the vectors state. Running the generated C# makes
// this a functional test, gated by slowtest.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const csJsonFlagsNameOnceDir = "../../../testdata/wire/json-flags-name-once"

type csJsonFlagsNameOnceRow struct {
	name, unit, root, text, verdict string
}

func csJsonFlagsNameOnceRows(t *testing.T) []csJsonFlagsNameOnceRow {
	t.Helper()
	f, err := os.Open(filepath.Join(csJsonFlagsNameOnceDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []csJsonFlagsNameOnceRow
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
		rows = append(rows, csJsonFlagsNameOnceRow{c[0], c[1], c[2], c[3], c[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// csJsonFlagsNameOnceUnit builds a unit from every *.schema in a directory.
func csJsonFlagsNameOnceUnit(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var files []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		base := strings.TrimSuffix(e.Name(), ".schema")
		ast, errs := parser.Parse(e.Name(), src)
		if len(errs) != 0 {
			t.Fatalf("parse %s: %v", e.Name(), errs[0])
		}
		files = append(files, check.SourceFile{Path: p, Name: e.Name(), Base: base, Bytes: src, AST: ast})
	}
	if len(files) == 0 {
		t.Fatalf("no .schema files in %s", dir)
	}
	u, errs := check.Unit(files)
	if len(errs) != 0 {
		t.Fatalf("check: %v", errs[0])
	}
	return u
}

// csJsonFlagsNameOnceOracle answers the Go engine's verdict for each row,
// after holding that verdict to the one the vectors state.
func csJsonFlagsNameOnceOracle(t *testing.T, rows []csJsonFlagsNameOnceRow) []string {
	t.Helper()
	units := map[string]*ir.Unit{}
	var want []string
	for _, row := range rows {
		u := units[row.unit]
		if u == nil {
			u = csJsonFlagsNameOnceUnit(t, filepath.Join("../../..", row.unit))
			units[row.unit] = u
		}
		model := tabletext.NewModel(u)
		def := model.Lookup(row.root)
		if def == nil {
			t.Fatalf("%s: the unit declares no %s", row.name, row.root)
		}
		src, err := os.ReadFile(filepath.Join(csJsonFlagsNameOnceDir, row.text))
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

func TestCSJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")

	rows := csJsonFlagsNameOnceRows(t)
	want := csJsonFlagsNameOnceOracle(t, rows)

	u := csJsonFlagsNameOnceUnit(t, filepath.Join("../../..", rows[0].unit))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	packets, err := csharp.Generate(u)
	if err != nil {
		t.Fatalf("csharp.Generate: %v", err)
	}
	for name, data := range packets {
		if _, dup := files[name]; !dup {
			files[name] = data
		}
	}

	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}

	var inputs []string
	for _, row := range rows {
		src, err := os.ReadFile(filepath.Join(csJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, row.text)
		if err := os.WriteFile(p, src, 0o600); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, p)
	}

	const driverSrc = `using System;
using System.IO;
using Tabledemo;

public static class FlagsNameOnceProbe
{
    public static int Main(string[] args)
    {
        foreach (string path in args)
        {
            byte[] input = File.ReadAllBytes(path);
            var v = new ProfileConfig();
            var r = new TableReport();
            bool ok = Schema.ProfileConfigFromJson(v, input, r);
            Console.WriteLine(ok ? "read" : "refused");
        }
        return 0;
    }
}
`
	main := filepath.Join(dir, "FlagsNameOnceProbe.cs")
	if err := os.WriteFile(main, []byte(driverSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)
	serializeRoot, err := filepath.Abs("../../../../serialize.cs")
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, filepath.Join(serializeRoot, "src", "Serialize.cs"), filepath.Join(serializeRoot, "src", "Int128Pair.cs"))
	csc, refs, runtime := csDotnetToolchain(t)
	app := filepath.Join(dir, "probe.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe", "-nowarn:CS8500", "-langversion:latest", "-out:" + app}
	args = append(args, sources...)
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	cmd := exec.Command(csDotnet(), args...)
	cmd.Env = csDotnetEnv(dir, dir)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("csc failed:\n%s", out)
	}
	rc := fmt.Sprintf(`{ "runtimeOptions": { "tfm": "net10.0", "framework": { "name": "Microsoft.NETCore.App", "version": %q } } }`, runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	runArgs := append([]string{"exec", app}, inputs...)
	run := exec.Command(csDotnet(), runArgs...)
	run.Env = csDotnetEnv(dir, dir)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("FlagsNameOnceProbe failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(rows), out)
	}
	for i, row := range rows {
		if lines[i] != want[i] {
			t.Errorf("%s: C# answered %q, the Go engine %q", row.name, lines[i], want[i])
		}
	}
}
