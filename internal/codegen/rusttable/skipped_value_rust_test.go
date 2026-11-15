package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestRustSkippedValueIsValidated runs every row of the shared skipped-value
// vectors through the Rust leg's reads (docs/SPEC-TABLES.md §4, §16.3;
// docs/rulings.json skipped-value-is-validated): the JSON rows through the
// reader root's text read, and the message rows through its batch load under
// the newer writer unit's announcement. A value the reader SKIPS is held to
// what a read of it would hold, and damage inside it refuses the whole read.
// The verdict is the contract; a refusal's name is the leg's own diagnostic.
func TestRustSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated skipped-value probe)")

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}

	root := filepath.Join("..", "..", "..")
	dir := filepath.Join(root, "testdata", "wire", "skipped-value")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}

	type row struct{ file, form, verdict string }
	var rows []row
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("%s:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", manifest, i+1, line)
		}
		if f[1] != "json" && f[1] != "message" {
			t.Fatalf("%s:%d: form %q, want json or message", manifest, i+1, f[1])
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("%s:%d: verdict %q, want read or refused", manifest, i+1, f[2])
		}
		rows = append(rows, row{f[0], f[1], f[2]})
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no rows", manifest)
	}

	schemaBytes, err := os.ReadFile(filepath.Join(root, "test", "skipped-value", "reader", "Reader.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schemaBytes))
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	annIdx := -1
	for i, r := range rows {
		if r.form == "message" && r.file == "announcement.bin" {
			annIdx = i
		}
	}
	if annIdx < 0 {
		t.Fatalf("%s names no announcement.bin message row", manifest)
	}
	announcement, err := os.ReadFile(filepath.Join(dir, "announcement.bin"))
	if err != nil {
		t.Fatal(err)
	}

	var body strings.Builder
	fmt.Fprintf(&body, "const ANN: &[u8] = &%s;\n", rustByteVector(announcement))
	body.WriteString(`
fn announce_verdict(wire: &[u8]) -> bool {
    let mut storage = vec![TableMessageEntry::default(); 8192];
    let mut vocab = TableVocabulary::new(&mut storage);
    announce_read(&mut vocab, wire, &mut TableReport::default())
}

fn message_verdict(wire: &[u8]) -> bool {
    let mut storage = vec![TableMessageEntry::default(); 8192];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut ar = TableReport::default();
    if !announce_read(&mut vocab, ANN, &mut ar) {
        return false;
    }
    let mut values = vec![root_message_default(); 1];
    let mut report = TableReport::default();
    let (count, ok) = root_load_messages(&mut values, &vocab, wire, &mut report);
    ok && count == 1 && !report.damaged && !report.refused
}

fn json_verdict(wire: &[u8]) -> bool {
    let mut report = TableReport::default();
    root_from_json_bytes(wire, &mut report).is_some() && !report.damaged && !report.refused
}

fn main() {
`)
	for i, r := range rows {
		data, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatalf("%s: %v", r.file, err)
		}
		var call string
		if r.form == "json" {
			call = "json_verdict"
		} else if i == annIdx {
			call = "announce_verdict"
		} else {
			call = "message_verdict"
		}
		fmt.Fprintf(&body, "    println!(\"%d {}\", if %s(&%s) { \"read\" } else { \"refused\" });\n",
			i, call, rustByteVector(data))
	}
	body.WriteString("}\n")

	build := t.TempDir()
	layoutRustUnit(t, build, "probe", modules)
	main := "#![allow(dead_code, unused_imports)]\nmod probe;\nuse probe::*;\n\n" + body.String()
	if err := os.WriteFile(filepath.Join(build, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--edition", "2024"}
	if cc, err := exec.LookPath("cc"); err == nil {
		args = append(args, "-C", "linker="+cc)
	}
	args = append(args, "-o", "run", "main.rs")
	cmd := exec.Command("rustc", args...)
	cmd.Dir = build
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(build, "run")).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}

	got := map[int]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		var index int
		var verdict string
		if _, err := fmt.Sscanf(line, "%d %s", &index, &verdict); err != nil {
			t.Fatalf("the probe answered %q: %v", line, err)
		}
		got[index] = verdict
	}
	if len(got) != len(rows) {
		t.Fatalf("the probe answered %d rows of %d:\n%s", len(got), len(rows), out)
	}
	for i, r := range rows {
		if got[i] != r.verdict {
			t.Errorf("%s (%s): verdict %s, want %s", r.file, r.form, got[i], r.verdict)
		}
	}
}
