// TestSpecPointerTargetTypeChecked pins docs/SPEC-TABLES.md §3.1's rule for a
// pointer's target type (docs/rulings.json `pointer-target-type-checked`): in
// the variable form, "a record the reader cannot name as a node" covers only a
// record whose TYPE the reader does not DECLARE — a newer writer's type,
// counted `unknown` once, every pointer naming it reads null, the load
// accepted. A pointer slot that names a record of a type the reader DOES
// declare but that is not the type the slot requires is damage and refuses the
// whole load. The vectors under testdata/wire/pointer-target-type state the
// rule for the engine and port cards, and test/pointer-target-type/gen
// reproduces each of them byte for byte.
package compiler

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSpecPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()

	page := readSpecTables(t)

	t.Run("§3.1 states both cases", func(t *testing.T) {
		t.Parallel()
		section := specTablesSection(t, page,
			"### 3.1 Pointers on the wire: a flat node table", "### 3.2 ")
		text := strings.Join(strings.Fields(section), " ")
		for _, want := range []string{
			"whose TYPE this reader does not DECLARE",
			"a newer writer's type",
			"a type this reader DOES declare",
			"wrong-type pointer target, §6.5",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("§3.1 the two target cases: the section does not hold %q", want)
			}
		}
	})

	t.Run("§4's damage list carries the second case", func(t *testing.T) {
		t.Parallel()
		section := specTablesSection(t, page,
			"## 4. Versioning, and the one read", "## 5. Identity")
		text := strings.Join(strings.Fields(section), " ")
		const want = "a pointer naming a record of a type the reader declares that is not the type its slot requires"
		if !strings.Contains(text, want) {
			t.Errorf("§4 the damage list: the section does not hold %q", want)
		}
	})

	t.Run("docs/rulings.json carries pointer-target-type-checked", func(t *testing.T) {
		t.Parallel()
		r, ok := readRulings(t)["pointer-target-type-checked"]
		if !ok {
			t.Fatal("docs/rulings.json carries no ruling pointer-target-type-checked")
		}
		if r.Date != "2026-11-10" {
			t.Errorf("date %q, want 2026-11-10", r.Date)
		}
		const wantQuote = "ruled 2026-11-10: a pointer naming a record of a declared type that is not its target type is damage"
		if len(r.Quotes) != 1 || r.Quotes[0] != wantQuote {
			t.Errorf("quotes %q, want exactly %q", r.Quotes, wantQuote)
		}
		if !strings.Contains(r.Source, "strict wins") || !strings.Contains(r.Source, "coordinator") {
			t.Errorf("source does not state the standing rule and the coordinator: %q", r.Source)
		}
	})

	t.Run("the manifest names each vector with its verdict", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join("..", "testdata", "wire", "pointer-target-type")
		manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
		if err != nil {
			t.Fatalf("vectors manifest: %v", err)
		}
		want := []pointerTargetRow{
			{"canonical", "test/pointer-target-type/reader", "Node", "canonical.bin", "read", 0},
			{"next-quant", "test/pointer-target-type/reader", "Node", "next-quant.bin", "refused", -1},
			{"next-bytes", "test/pointer-target-type/reader", "Node", "next-bytes.bin", "refused", -1},
			{"next-writer-only", "test/pointer-target-type/reader", "Node", "next-writer-only.bin", "read", 1},
		}
		rows := pointerTargetRows(t, string(manifest))
		if len(rows) != len(want) {
			t.Fatalf("MANIFEST.tsv names %d vectors, want %d", len(rows), len(want))
		}
		for i, w := range want {
			if rows[i] != w {
				t.Errorf("row %d is %+v, want %+v", i+1, rows[i], w)
			}
		}
		named := map[string]bool{}
		for _, row := range rows {
			named[row.file] = true
			if _, err := os.Stat(filepath.Join(dir, row.file)); err != nil {
				t.Errorf("MANIFEST.tsv names %s: %v", row.file, err)
			}
		}
		vectors, err := filepath.Glob(filepath.Join(dir, "*.bin"))
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vectors {
			if !named[filepath.Base(v)] {
				t.Errorf("%s is not named by MANIFEST.tsv", v)
			}
		}
	})

	t.Run("the generator reproduces each vector", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join("..", "testdata", "wire", "pointer-target-type")
		root, err := filepath.Abs("..")
		if err != nil {
			t.Fatal(err)
		}
		work := t.TempDir()
		bin := filepath.Join(work, "pointertargetgen")
		build := exec.Command("go", "build", "-o", bin, "./test/pointer-target-type/gen")
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("building the generator: %v\n%s", err, out)
		}
		run := exec.Command(bin, "--out", work)
		run.Dir = root
		if out, err := run.CombinedOutput(); err != nil {
			t.Fatalf("running the generator: %v\n%s", err, out)
		}
		for _, name := range []string{
			"canonical.bin", "next-quant.bin", "next-bytes.bin", "next-writer-only.bin",
		} {
			committed, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			regenerated, err := os.ReadFile(filepath.Join(work, name))
			if err != nil {
				t.Fatalf("the generator wrote no %s: %v", name, err)
			}
			if !bytes.Equal(committed, regenerated) {
				t.Errorf("%s: the generator no longer reproduces the committed vector:\n committed %x\ngenerated %x", name, committed, regenerated)
			}
		}
	})
}

type pointerTargetRow struct {
	name    string
	unit    string
	root    string
	file    string
	verdict string
	unknown int // -1 means the row does not pin a count
}

func pointerTargetRows(t *testing.T, manifest string) []pointerTargetRow {
	t.Helper()
	var rows []pointerTargetRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		row := pointerTargetRow{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, row.verdict)
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	return rows
}
