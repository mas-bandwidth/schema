package tablepack_test

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

var updateDocClaims = flag.Bool("update-docclaims", false, "rewrite each doc-claim case's save.bin and read.json from the engine")

// THE DOC-CLAIM CORPUS (review finding F-173): every sentence a page makes
// about what a READER does with old or bad data is bound here to a saved
// file and an actual read. The tutorial and the usage page described
// clamping, last-wins and partial outcomes for saves the reader refuses
// whole (docs/SPEC-TABLES.md §4: one read, the whole value or a refusal).
//
// The corpus is testdata/docclaims. Each case is a directory:
//
//	writer.schema  the declaration the save was written under
//	reader.schema  the declaration it is read under (absent: the writer's)
//	value.json     the root's JSON under the writer's declaration
//	save.bin       the committed historical save, the writer's exact bytes
//	read.json      the value the reader lands, for a case that reads
//
// and claims.tsv binds each case to the pages: one row per sentence, with
// the verdict the read must give. A row's `says` is text the page must carry
// (white space folded), so a sentence cannot be edited away from the read
// that proves it, and the read cannot change under the sentence.
//
// The verdict is what is held (ruling refusal-verdict-only): `read` with the
// two counters, or `refused`. The reason a refusal names is per-language
// diagnostic detail; the Go engine's is logged, never compared.
//
// `edit` damages the saved bytes before the read, for the claims about a
// damaged file: `cut:N` keeps the first N bytes, `byte0:XX` replaces the
// form byte. `text` in the edit column means the claim is about the TEXT
// read: value.json is read under the reader's declaration and that read
// must give the verdict.
const docClaimsDir = "testdata/docclaims"

type docClaimRow struct {
	line    int
	kase    string
	root    string
	edit    string
	verdict string // "read" or "refused"
	unknown int
	widened int
	page    string
	says    string
}

func readDocClaimRows(t *testing.T) []docClaimRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(docClaimsDir, "claims.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []docClaimRow
	for i, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 8 {
			t.Fatalf("claims.tsv:%d has %d columns, and a row is case, root, edit, verdict, unknown, widened, page, says", i+1, len(cols))
		}
		row := docClaimRow{line: i + 1, kase: cols[0], root: cols[1], edit: cols[2], verdict: cols[3], page: cols[6], says: cols[7]}
		if row.edit == "-" {
			row.edit = ""
		}
		switch row.verdict {
		case "read":
			if row.unknown, err = strconv.Atoi(cols[4]); err != nil {
				t.Fatalf("claims.tsv:%d: unknown %q", i+1, cols[4])
			}
			if row.widened, err = strconv.Atoi(cols[5]); err != nil {
				t.Fatalf("claims.tsv:%d: widened %q", i+1, cols[5])
			}
		case "refused":
			if cols[4] != "-" || cols[5] != "-" {
				t.Fatalf("claims.tsv:%d: a refused read has no counters, so both columns are -", i+1)
			}
		default:
			t.Fatalf("claims.tsv:%d: verdict %q is not read or refused", i+1, row.verdict)
		}
		rows = append(rows, row)
	}
	return rows
}

// docClaimModel compiles one declaration into the engine's model.
func docClaimModel(t *testing.T, path string) *tabletext.Model {
	t.Helper()
	dir := t.TempDir()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "claim.schema"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	unit, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("%s does not compile: %v", path, err)
	}
	return tabletext.NewModel(unit)
}

// docClaimPack packs a case's value.json under a declaration, answering the
// wire and the text read's report.
func docClaimPack(t *testing.T, m *tabletext.Model, kase, root string) ([]byte, tabletext.Report, error) {
	t.Helper()
	tree := t.TempDir()
	value, err := os.ReadFile(filepath.Join(docClaimsDir, kase, "value.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, root+".json"), value, 0o600); err != nil {
		t.Fatal(err)
	}
	wire, _, report, err := tablepack.Pack(m, root, tree)
	return wire, report, err
}

// docClaimEdit applies a row's damage to the saved bytes.
func docClaimEdit(t *testing.T, wire []byte, edit string) []byte {
	t.Helper()
	out := append([]byte(nil), wire...)
	switch {
	case edit == "":
		return out
	case strings.HasPrefix(edit, "cut:"):
		n, err := strconv.Atoi(strings.TrimPrefix(edit, "cut:"))
		if err != nil || n < 0 || n >= len(out) {
			t.Fatalf("edit %q does not cut a %d-byte save", edit, len(out))
		}
		return out[:n]
	case strings.HasPrefix(edit, "byte0:"):
		b, err := hex.DecodeString(strings.TrimPrefix(edit, "byte0:"))
		if err != nil || len(b) != 1 {
			t.Fatalf("edit %q is not one hex byte", edit)
		}
		out[0] = b[0]
		return out
	}
	t.Fatalf("edit %q is not cut:N, byte0:XX or text", edit)
	return nil
}

func docClaimFold(s string) string { return strings.Join(strings.Fields(s), " ") }

// TestDocClaims holds every row to its page and to an actual read of its
// committed save.
func TestDocClaims(t *testing.T) {
	rows := readDocClaimRows(t)
	if len(rows) < 20 {
		t.Fatalf("claims.tsv holds %d rows: the corpus itself is short", len(rows))
	}
	pages := map[string]string{}
	cases := map[string]bool{}
	verdicts := map[string]int{}
	for _, row := range rows {
		cases[row.kase] = true
		verdicts[row.verdict]++
		page, ok := pages[row.page]
		if !ok {
			data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(row.page)))
			if err != nil {
				t.Fatalf("claims.tsv:%d: %v", row.line, err)
			}
			page = docClaimFold(string(data))
			pages[row.page] = page
		}
		t.Run(fmt.Sprintf("%s/%s:%d", row.kase, filepath.Base(row.page), row.line), func(t *testing.T) {
			if !strings.Contains(page, docClaimFold(row.says)) {
				t.Errorf("%s no longer says %q: the sentence this read proves is gone, so move the row with it", row.page, row.says)
			}
			docClaimRead(t, row)
		})
	}
	// THE CONTROLS: the corpus holds reads that return beside reads that
	// refuse, so a reader that refused everything (or nothing) is red here
	if verdicts["read"] == 0 || verdicts["refused"] == 0 {
		t.Fatalf("the corpus needs both verdicts, and holds %v", verdicts)
	}
	// every case directory is bound to at least one sentence
	entries, err := os.ReadDir(docClaimsDir)
	if err != nil {
		t.Fatal(err)
	}
	var orphans []string
	for _, e := range entries {
		if e.IsDir() && !cases[e.Name()] {
			orphans = append(orphans, e.Name())
		}
	}
	sort.Strings(orphans)
	if len(orphans) > 0 {
		t.Errorf("cases no page sentence is bound to: %s", strings.Join(orphans, ", "))
	}
}

func docClaimRead(t *testing.T, row docClaimRow) {
	t.Helper()
	dir := filepath.Join(docClaimsDir, row.kase)
	writerPath := filepath.Join(dir, "writer.schema")
	readerPath := filepath.Join(dir, "reader.schema")
	if _, err := os.Stat(readerPath); err != nil {
		readerPath = writerPath
	}
	reader := docClaimModel(t, readerPath)

	if row.edit == "text" {
		_, report, err := docClaimPack(t, reader, row.kase, row.root)
		refused := err != nil || report.Refused
		why := report.Reason
		if err != nil {
			why = err.Error()
		}
		docClaimVerdict(t, row, refused, why, report)
		return
	}

	writer := docClaimModel(t, writerPath)
	wire, report, err := docClaimPack(t, writer, row.kase, row.root)
	if err != nil || report.Refused {
		t.Fatalf("the writer refused the case's own value: %v %s", err, report.Reason)
	}
	savePath := filepath.Join(dir, "save.bin")
	if *updateDocClaims {
		if err := os.WriteFile(savePath, wire, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatal(err)
	}
	// THE SAVE IS HISTORICAL: the committed bytes are what the writer's
	// declaration wrote, and a writer that drifts is red here before any
	// read is judged
	if !bytes.Equal(saved, wire) {
		t.Fatalf("save.bin (%d bytes) is not what writer.schema writes today (%d bytes): the writer moved, or -update-docclaims is owed", len(saved), len(wire))
	}
	saved = docClaimEdit(t, saved, row.edit)

	out := t.TempDir()
	got, err := tablepack.UnpackOneFile(reader, row.root, saved, out)
	refused := got.Refused
	why := got.Reason
	if err != nil {
		refused, why = true, err.Error()
	}
	docClaimVerdict(t, row, refused, why, got)
	if refused {
		return
	}
	value, err := os.ReadFile(filepath.Join(out, row.root+".json"))
	if err != nil {
		t.Fatal(err)
	}
	readPath := filepath.Join(dir, "read.json")
	if *updateDocClaims {
		if err := os.WriteFile(readPath, value, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(readPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(value, want) {
		t.Fatalf("the read landed\n%s\nand read.json says\n%s", value, want)
	}
}

func docClaimVerdict(t *testing.T, row docClaimRow, refused bool, why string, report tabletext.Report) {
	t.Helper()
	switch {
	case refused && row.verdict == "read":
		t.Fatalf("%s says the load returns, and the reader refused it (%s)", row.page, why)
	case !refused && row.verdict == "refused":
		t.Fatalf("%s says the reader refuses, and the load returned: unknown=%d widened=%d", row.page, report.Unknown, report.Widened)
	case refused:
		t.Logf("refused, as the page says; the Go engine names it %q (diagnostic, not compared)", why)
	case report.Unknown != row.unknown || report.Widened != row.widened:
		t.Fatalf("%s says unknown=%d widened=%d, and the read counted unknown=%d widened=%d",
			row.page, row.unknown, row.widened, report.Unknown, report.Widened)
	}
}
