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
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
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
		slowtest.Gate(t, "the Go toolchain (it builds and runs the vector generator)")
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

// TestSpecBlobTargetOutsideClosureRefused pins docs/SPEC-TABLES.md §3.1's rule
// for a blob record of a kind the root's closure lacks (docs/rulings.json
// `blob-target-outside-closure-refused`, 2026-11-17): the two reserved blob ids
// are types every reader of the variable form can name, so a declared slot
// naming a blob record of the other kind refuses the whole read whatever the
// root reaches; a blob record reached only from fields the reader does not
// declare is evolution, counted `unknown`; and a blob record no pointer reaches
// is damage. A newer writer's TYPE under a declared slot stays the separate,
// accepted case of TestSpecPointerTargetTypeChecked. The vectors under
// testdata/wire/pointer-target-type/blobkind state the rule for the engine and
// every leg, and test/pointer-target-type/blobkind/gen reproduces each byte for
// byte. Every gate here carries a negative control that plants its fault in a
// copy of the real artifact and must go red.
func TestSpecBlobTargetOutsideClosureRefused(t *testing.T) {
	t.Parallel()

	page := readSpecTables(t)
	section31 := strings.Join(strings.Fields(specTablesSection(t, page,
		"### 3.1 Pointers on the wire: a flat node table", "### 3.2 ")), " ")
	section65 := strings.Join(strings.Fields(specTablesSection(t, page,
		"### 6.5 The two load paths, and the one read", "### 6.6 ")), " ")

	t.Run("§3.1 and §6.5 state the ruled rule", func(t *testing.T) {
		t.Parallel()
		for _, p := range blobRuleProblems(section31, section65) {
			t.Error(p)
		}
	})
	t.Run("negative control: §3.1 with the root-closure exception restored", func(t *testing.T) {
		t.Parallel()
		planted := strings.Replace(section31, "whatever its root reaches", "", 1) +
			" A ROOT that reaches no blob of that shape skips it by its length and counts it `unknown`"
		if len(blobRuleProblems(planted, section65)) < 2 {
			t.Error("the gate passed a §3.1 that lost the rule and kept the stale exception")
		}
	})
	t.Run("negative control: §6.5 without the CANNOT-name boundary", func(t *testing.T) {
		t.Parallel()
		planted := strings.Replace(section65, blobCannotNamePhrase, "", 1)
		if len(blobRuleProblems(section31, planted)) == 0 {
			t.Error("the gate passed a §6.5 that does not say which records a reader cannot name")
		}
	})

	rulings := readRulings(t)
	t.Run("docs/rulings.json carries the ruling and the standing principle", func(t *testing.T) {
		t.Parallel()
		for _, p := range blobRulingProblems(rulings) {
			t.Error(p)
		}
	})
	t.Run("negative control: a reworded quote", func(t *testing.T) {
		t.Parallel()
		planted := map[string]ruling{}
		maps.Copy(planted, rulings)
		r := planted["blob-target-outside-closure-refused"]
		r.Quotes = []string{"when in doubt, skip it"}
		planted["blob-target-outside-closure-refused"] = r
		if len(blobRulingProblems(planted)) == 0 {
			t.Error("the gate passed a ruling whose quote is not the maintainer's words")
		}
	})

	dir := filepath.Join("..", "testdata", "wire", "pointer-target-type", "blobkind")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("blobkind manifest: %v", err)
	}
	vectors, err := filepath.Glob(filepath.Join(dir, "*.bin"))
	if err != nil {
		t.Fatal(err)
	}
	var bins []string
	for _, v := range vectors {
		bins = append(bins, filepath.Base(v))
	}
	t.Run("the manifest names each vector with its verdict", func(t *testing.T) {
		t.Parallel()
		for _, p := range blobKindManifestProblems(string(manifest), bins) {
			t.Error(p)
		}
	})
	t.Run("negative control: a flipped verdict", func(t *testing.T) {
		t.Parallel()
		planted := strings.Replace(string(manifest), "text-names-bytes.bin\trefused", "text-names-bytes.bin\tread", 1)
		if planted == string(manifest) {
			t.Fatal("the planted fault did not apply: the manifest has no text-names-bytes refused row")
		}
		if len(blobKindManifestProblems(planted, bins)) == 0 {
			t.Error("the gate passed a manifest that reads text-names-bytes")
		}
	})
	t.Run("negative control: a vector the manifest does not name", func(t *testing.T) {
		t.Parallel()
		if len(blobKindManifestProblems(string(manifest), append(slices.Clone(bins), "stray.bin"))) == 0 {
			t.Error("the gate passed a directory holding a vector the manifest does not name")
		}
	})

	t.Run("the generator reproduces each vector", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "the Go toolchain (it builds and runs the blob-kind vector generator)")
		root, err := filepath.Abs("..")
		if err != nil {
			t.Fatal(err)
		}
		work := t.TempDir()
		bin := filepath.Join(work, "blobkindgen")
		build := exec.Command("go", "build", "-o", bin, "./test/pointer-target-type/blobkind/gen")
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("building the generator: %v\n%s", err, out)
		}
		out := filepath.Join(work, "out")
		if err := os.Mkdir(out, 0o755); err != nil {
			t.Fatal(err)
		}
		run := exec.Command(bin, "--out", out)
		run.Dir = root
		if msg, err := run.CombinedOutput(); err != nil {
			t.Fatalf("running the generator: %v\n%s", err, msg)
		}
		for _, p := range blobKindReproductionProblems(dir, out) {
			t.Error(p)
		}

		// NEGATIVE CONTROL: a copy of the committed vectors with one byte of
		// text-names-bytes.bin flipped must fail the reproduction check, by
		// that file's name.
		t.Run("negative control: a corrupted committed vector", func(t *testing.T) {
			planted := t.TempDir()
			for _, row := range blobKindRows {
				data, err := os.ReadFile(filepath.Join(dir, row.file))
				if err != nil {
					t.Fatal(err)
				}
				if row.file == "text-names-bytes.bin" {
					data[len(data)/2] ^= 0xFF
				}
				if err := os.WriteFile(filepath.Join(planted, row.file), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			problems := blobKindReproductionProblems(planted, out)
			if len(problems) != 1 || !strings.Contains(problems[0], "text-names-bytes.bin") {
				t.Errorf("the reproduction check did not name the corrupted vector: %q", problems)
			}
		})
	})
}

// blobKindReproductionProblems names every blob-kind vector the generator's
// output in `generated` does not reproduce byte for byte from `committed`.
func blobKindReproductionProblems(committed, generated string) []string {
	var problems []string
	written, err := filepath.Glob(filepath.Join(generated, "*.bin"))
	if err != nil {
		return []string{err.Error()}
	}
	if len(written) != len(blobKindRows) {
		problems = append(problems, fmt.Sprintf("the generator wrote %d vectors, want %d", len(written), len(blobKindRows)))
	}
	for _, row := range blobKindRows {
		want, err := os.ReadFile(filepath.Join(committed, row.file))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", row.file, err))
			continue
		}
		got, err := os.ReadFile(filepath.Join(generated, row.file))
		if err != nil {
			problems = append(problems, fmt.Sprintf("the generator wrote no %s: %v", row.file, err))
			continue
		}
		if !bytes.Equal(want, got) {
			problems = append(problems, fmt.Sprintf("%s: the generator no longer reproduces the committed vector:\n committed %x\ngenerated %x", row.file, want, got))
		}
	}
	return problems
}

// blobKindRows are the blob-kind vectors, exactly, in manifest order.
var blobKindRows = []pointerTargetRow{
	{"text-canonical", "test/pointer-target-type/blobkind/reader", "Text", "text-canonical.bin", "read", 0},
	{"text-names-bytes", "test/pointer-target-type/blobkind/reader", "Text", "text-names-bytes.bin", "refused", -1},
	{"data-names-string", "test/pointer-target-type/blobkind/reader", "Bin", "data-names-string.bin", "refused", -1},
	{"text-unknown-field-bytes", "test/pointer-target-type/blobkind/reader", "Text", "text-unknown-field-bytes.bin", "read", 2},
	{"text-orphan-bytes", "test/pointer-target-type/blobkind/reader", "Text", "text-orphan-bytes.bin", "refused", -1},
}

// blobRuleQuote is the maintainer's sentence, 2026-11-17, verbatim.
const blobRuleQuote = "if there is ever any uncertainty about how to read something, and it is weird or not a natural case for somebody sending us well formed data, the answer is always reject."

// blobCannotNamePhrase is §6.5's boundary: which records a reader CANNOT name.
const blobCannotNamePhrase = "a table type the reader's unit does not declare, never one of the two reserved blob ids"

// blobRuleProblems names what §3.1 (whitespace-normalised) and §6.5 lack of
// the ruled rule, or still hold of the rule it replaced.
func blobRuleProblems(section31, section65 string) []string {
	var problems []string
	for _, want := range []string{
		"The two reserved ids are types EVERY reader of the variable form can name, whatever its root reaches.",
		"a `*string` slot naming a `*bytes` record, a `*bytes` slot naming a `*string` record, a `*T` slot naming either",
		"whether or not the root's closure has an edge of that kind",
		"A blob record of a kind the root's closure lacks, reached only from fields this reader does not declare",
		"A blob record no pointer reaches",
		"`blob-target-outside-closure-refused`, 2026-11-17",
		blobRuleQuote,
		"every leg refuses it, in every build",
		"A newer writer's TYPE is not this case",
		"`next-writer-only`",
	} {
		if !strings.Contains(section31, want) {
			problems = append(problems, fmt.Sprintf("§3.1 does not hold %q", want))
		}
	}
	for _, stale := range []string{
		"A ROOT that reaches no blob of that shape skips it",
		"edge sits below is a node this reader cannot place",
	} {
		if strings.Contains(section31, stale) {
			problems = append(problems, fmt.Sprintf("§3.1 still holds the root-closure exception %q", stale))
		}
	}
	if !strings.Contains(section65, blobCannotNamePhrase) {
		problems = append(problems, fmt.Sprintf("§6.5 does not hold %q", blobCannotNamePhrase))
	}
	return problems
}

// blobRulingProblems names what docs/rulings.json lacks of the case's ruling
// and of the standing principle it applies.
func blobRulingProblems(rulings map[string]ruling) []string {
	var problems []string
	r, ok := rulings["blob-target-outside-closure-refused"]
	if !ok {
		return append(problems, "docs/rulings.json carries no ruling blob-target-outside-closure-refused")
	}
	if r.Date != "2026-11-17" {
		problems = append(problems, fmt.Sprintf("blob-target-outside-closure-refused: date %q, want 2026-11-17", r.Date))
	}
	if len(r.Quotes) != 1 || r.Quotes[0] != blobRuleQuote {
		problems = append(problems, fmt.Sprintf("blob-target-outside-closure-refused: quotes %q, want exactly %q", r.Quotes, blobRuleQuote))
	}
	const wantSource = "the maintainer, relayed verbatim by the coordinator from the live session of 2026-11-17: the standing principle that an uncertain or unnatural read is refused, applied to a blob record outside the root's closure named from a declared slot"
	if r.Source != wantSource {
		problems = append(problems, fmt.Sprintf("blob-target-outside-closure-refused: source %q, want %q", r.Source, wantSource))
	}
	p, ok := rulings["uncertain-read-refused"]
	if !ok {
		return append(problems, "docs/rulings.json carries no standing principle uncertain-read-refused")
	}
	if p.Date != "2026-11-17" {
		problems = append(problems, fmt.Sprintf("uncertain-read-refused: date %q, want 2026-11-17", p.Date))
	}
	if len(p.Quotes) == 0 || p.Quotes[0] != blobRuleQuote {
		problems = append(problems, fmt.Sprintf("uncertain-read-refused: the first quote is not %q", blobRuleQuote))
	}
	if !strings.Contains(p.Source, "decided as refuse") || !strings.Contains(p.Source, "lawful evolution") {
		problems = append(problems, fmt.Sprintf("uncertain-read-refused: the source does not state the rule and its boundary: %q", p.Source))
	}
	return problems
}

// blobKindManifestProblems names how the blob-kind MANIFEST.tsv differs from
// the five rows the ruling fixes, and any vector it does not name or names
// without a file.
func blobKindManifestProblems(manifest string, bins []string) []string {
	var problems []string
	var rows []pointerTargetRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			problems = append(problems, fmt.Sprintf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f)))
			continue
		}
		row := pointerTargetRow{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				problems = append(problems, fmt.Sprintf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err))
				continue
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	if len(rows) != len(blobKindRows) {
		problems = append(problems, fmt.Sprintf("MANIFEST.tsv names %d vectors, want %d", len(rows), len(blobKindRows)))
	}
	for i := range min(len(rows), len(blobKindRows)) {
		if rows[i] != blobKindRows[i] {
			problems = append(problems, fmt.Sprintf("row %d is %+v, want %+v", i+1, rows[i], blobKindRows[i]))
		}
	}
	named := map[string]bool{}
	for _, row := range rows {
		named[row.file] = true
		if !slices.Contains(bins, row.file) {
			problems = append(problems, fmt.Sprintf("MANIFEST.tsv names %s, which the directory does not hold", row.file))
		}
	}
	for _, b := range bins {
		if !named[b] {
			problems = append(problems, fmt.Sprintf("%s is not named by MANIFEST.tsv", b))
		}
	}
	return problems
}
