package rusttable

// A SCHEMA-NAME STRING IS CAPPED AT 255 BYTES, wherever the read meets one
// (docs/SPEC-TABLES.md §16.2, §16.12): an object key naming a field, an enum
// variant name, a flags element, a union arm key and an `&node` name, in every
// object and inside a skipped value. A longer one refuses the read, an unknown
// enum name over the cap included; a map key and a string value are data and
// are never refused for their length. The vectors hold the verdicts, and this
// test reads each name-cap row through the generated Rust walk.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// rustJsonNameCapSchema is the read-closed unit: a flags field, an enum, a
// union, a map keyed by string, a string field, and a pointered root for the
// `&node` name's site.
const rustJsonNameCapSchema = `package probe

enum Kind { Alpha, Beta }

flags Perms { Read, Write }

type Buff
{
    mult float32 = 1.0
}

union Effect
{
    buff Buff
    ping
}

table Node
{
    value int32 = 0
}

table Scene
{
    head *Node
}

table Closed
{
    kind   Kind
    perms  Perms
    effect Effect
    tags   map[string]int32
    note   string(300)
}
`

// rustJsonNameCapRows is the manifest's name-cap rows: the 255 and 256
// spellings at every name site, and the two data strings a long length never
// refuses.
var rustJsonNameCapRows = []string{
	"field-key-255",
	"field-key-256",
	"skipped-key-255",
	"skipped-key-256",
	"enum-name-255",
	"enum-name-256",
	"flags-element-255",
	"flags-element-256",
	"union-arm-255",
	"union-arm-256",
	"ampersand-255",
	"ampersand-256",
	"map-key-long",
	"string-value-long",
}

type rustJsonNameCapRow struct {
	name    string
	root    string
	json    string
	verdict string
}

// rustJsonNameCapVectors reads the name-cap rows of the vectors' manifest.
func rustJsonNameCapVectors(t *testing.T) (string, []rustJsonNameCapRow) {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-read-closed")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("the vectors' manifest: %v", err)
	}
	wanted := make(map[string]bool, len(rustJsonNameCapRows))
	for _, name := range rustJsonNameCapRows {
		wanted[name] = true
	}
	var rows []rustJsonNameCapRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		if !wanted[fields[0]] {
			continue
		}
		delete(wanted, fields[0])
		if fields[3] != "read" && fields[3] != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", fields[0], fields[3])
		}
		rows = append(rows, rustJsonNameCapRow{name: fields[0], root: fields[1], json: fields[2], verdict: fields[3]})
	}
	for _, name := range rustJsonNameCapRows {
		if wanted[name] {
			t.Errorf("%s/MANIFEST.tsv names no name-cap row %q", dir, name)
		}
	}
	if len(rows) != len(rustJsonNameCapRows) {
		t.Fatalf("read %d name-cap rows, want %d", len(rows), len(rustJsonNameCapRows))
	}
	return dir, rows
}

// TestRustJsonNameCap holds the name-cap rows of
// testdata/wire/json-read-closed/: the 255-byte spelling reads, the 256-byte
// spelling refuses, at every name site, while the long map key and string
// value read.
func TestRustJsonNameCap(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated JSON probe)")

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	dir, rows := rustJsonNameCapVectors(t)

	modules, err := Generate(unitFrom(t, rustJsonNameCapSchema))
	if err != nil {
		t.Fatalf("the name-cap unit was refused: %v", err)
	}
	root := t.TempDir()
	var mods []string
	for _, m := range []string{"variable_runtime", "table_enums", "probe_table", "probe_variable"} {
		data, ok := modules[m+".rs"]
		if !ok {
			t.Fatalf("missing module %s.rs; got %v", m, keys(modules))
		}
		if err := os.WriteFile(filepath.Join(root, m+".rs"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, m)
	}

	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	for _, m := range mods {
		main.WriteString("mod " + m + ";\n")
	}
	main.WriteString(`
use probe_table::*;
use variable_runtime::*;

fn verdict(root: &str, text: &[u8]) -> &'static str {
    let mut r = TableReport::default();
    let ok = if root == "Closed" {
        closed_from_json_bytes(text, &mut r).is_some()
    } else {
        scene_from_json_bytes(text, &mut r).is_some()
    };
    if ok && !r.damaged { "read" } else { "refused" }
}

fn main() {
`)
	for _, row := range rows {
		body, err := os.ReadFile(filepath.Join(dir, row.json))
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		fmt.Fprintf(&main, "    { let got = verdict(%q, &%s); if got != %q { println!(\"MISMATCH {}: got {} want {}\", %q, got, %q); } }\n",
			row.root, rustByteSlice(body), row.verdict, row.name, row.verdict)
	}
	main.WriteString("    println!(\"NAME CAP PROBE DONE\");\n}\n")

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
	if !strings.Contains(string(out), "NAME CAP PROBE DONE") {
		t.Fatalf("the probe did not finish:\n%s", out)
	}
}
