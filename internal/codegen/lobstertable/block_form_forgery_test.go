package lobstertable_test

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// forgeryRow is one `forgery` line of the committed tables manifest: a patch
// over a base fixture, the buffer the caller holds and the extent it claims
// (testdata/conformance/tables/MANIFEST.txt; docs/SPEC-TABLES.md §7, §19.2).
type forgeryRow struct {
	name, kind, subject, base string
	pointer                   string // "null", or the bytes past an aligned base
	offsets, widths, values   []string
	extent                    int64
	verdict                   string
	reason                    string // the `refusal` line's reason, or ""
}

func forgeryRows(t *testing.T, kind string) []forgeryRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []forgeryRow
	reasons := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		if len(x) == 0 {
			continue
		}
		switch x[0] {
		case "forgery":
			if len(x) < 11 || x[2] != kind {
				continue
			}
			extent, err := strconv.ParseInt(x[9], 0, 64)
			if err != nil {
				t.Fatalf("%s: extent %q: %v", x[1], x[9], err)
			}
			rows = append(rows, forgeryRow{name: x[1], kind: x[2], subject: x[3], base: x[4], pointer: x[5],
				offsets: strings.Split(x[6], ","), widths: strings.Split(x[7], ","), values: strings.Split(x[8], ","),
				extent: extent, verdict: x[10]})
		case "refusal":
			if len(x) >= 3 {
				reasons[x[1]] = x[2]
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		rows[i].reason = reasons[rows[i].name]
	}
	return rows
}

// forge applies one row's patch to its base and cuts or zero-extends the
// result to the extent the caller claims, as the harness materialises it.
func forge(t *testing.T, r forgeryRow, base []byte) []byte {
	t.Helper()
	data := append([]byte(nil), base...)
	for i := range r.offsets {
		off, err := strconv.ParseInt(r.offsets[i], 0, 64)
		if err != nil {
			t.Fatal(err)
		}
		if off < 0 {
			continue
		}
		width, err := strconv.Atoi(r.widths[i])
		if err != nil {
			t.Fatal(err)
		}
		value, err := strconv.ParseUint(r.values[i], 0, 64)
		if err != nil {
			t.Fatal(err)
		}
		var word [8]byte
		binary.LittleEndian.PutUint64(word[:], value)
		copy(data[off:off+int64(width)], word[:width])
	}
	if r.extent >= 0 {
		claim := make([]byte, r.extent)
		copy(claim, data)
		data = claim
	}
	return data
}

// openReasons runs one Lobster program over every row and returns the reason
// each answered: call spells the open, with DATA, LEAD and NOBUF to fill in.
func openReasons(t *testing.T, dir string, imports []string, rows []forgeryRow, call func(forgeryRow) string, datas [][]byte) map[string]string {
	t.Helper()
	var b strings.Builder
	for _, m := range imports {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	inputs := map[string][]byte{}
	for i, r := range rows {
		file := fmt.Sprintf("f%d", i)
		inputs[file] = datas[i]
		lead, nobuf := "0", "false"
		if r.pointer == "null" {
			nobuf = "true"
		} else {
			lead = r.pointer
		}
		fmt.Fprintf(&b, "let d%d = read_file(%q)\nassert d%d\n", i, file, i)
		call := strings.NewReplacer("DATA", fmt.Sprintf("d%d", i), "LEAD", lead, "NOBUF", nobuf).Replace(call(r))
		fmt.Fprintf(&b, "print(%q + %s)\n", r.name+" ", call)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", inputs))), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			out[f[0]] = f[1]
		}
	}
	return out
}

// checkReasons holds every row's answer to the manifest: the verdict for all,
// and the reason where a `refusal` line names one. It returns the rows that
// disagreed.
func checkReasons(rows []forgeryRow, got map[string]string) []string {
	var bad []string
	for _, r := range rows {
		reason, ok := got[r.name]
		verdict := "refuse"
		if reason == "ok" {
			verdict = "open"
		}
		if !ok || verdict != r.verdict || (r.reason != "" && reason != r.reason) {
			bad = append(bad, r.name)
		}
	}
	return bad
}

func blockForgeryRun(t *testing.T, sabotage func(string) string) ([]forgeryRow, map[string]string) {
	t.Helper()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["blockdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("blockdemo is refused: %v", err)
	}
	if sabotage != nil {
		p := filepath.Join(g.dir, "block_form.lobster")
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(sabotage(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bases := map[string]string{
		"block_render": "testdata/wire/tables/block_render.bin",
		"block_padded": "testdata/wire/tables/block_padded.bin",
	}
	rows := forgeryRows(t, "block")
	if len(rows) == 0 {
		t.Fatal("the manifest names no block forgery: the comparison is vacuous")
	}
	datas := make([][]byte, len(rows))
	for i, r := range rows {
		base, err := os.ReadFile(filepath.Join(root, bases[r.base]))
		if err != nil {
			t.Fatalf("%s: base %s: %v", r.name, r.base, err)
		}
		datas[i] = forge(t, r, base)
	}
	got := openReasons(t, g.dir, append(g.mods, "block_form"), rows, func(r forgeryRow) string {
		root := "RenderFrame"
		if strings.HasPrefix(r.subject, "block_padded") {
			root = "PaddedFrame"
		}
		return fmt.Sprintf("schema_rt.block_open_reason(%q, DATA, LEAD, NOBUF)", root)
	}, datas)
	return rows, got
}

// TestLobsterBlockForgeries opens every block forgery of the manifest through
// the block form's Open (docs/SPEC-TABLES.md §19.2) and holds each to its
// verdict and, where a `refusal` line names one, its first failing clause.
func TestLobsterBlockForgeries(t *testing.T) {
	t.Parallel()
	rows, got := blockForgeryRun(t, nil)
	if bad := checkReasons(rows, got); len(bad) > 0 {
		t.Fatalf("block forgeries that disagree with the manifest: %v (answers %v)", bad, got)
	}
	t.Logf("%d block forgeries answer the manifest's verdict and reason", len(rows))
}

// TestLobsterBlockForgeriesNegativeControl plants the old Open back, the one
// that checked only the prologue, and requires exactly the layout rows — the
// pitch, the array starts and counts, the offset overflow, the used extent's
// padding and the base — to go red, and nothing else.
func TestLobsterBlockForgeriesNegativeControl(t *testing.T) {
	t.Parallel()
	const pitch = "        if stride != bf.stride:\n            return \"bad_layout\"\n"
	rows, got := blockForgeryRun(t, func(src string) string {
		if !strings.Contains(src, pitch) {
			t.Fatal("the control's anchor, the pitch clause, is not in block_form.lobster")
		}
		return strings.Replace(src, pitch, "", 1)
	})
	bad := checkReasons(rows, got)
	if len(bad) != 1 || bad[0] != "block_pitch" {
		t.Fatalf("with the pitch clause removed, the rows that went red are %v, want exactly [block_pitch]", bad)
	}
	t.Logf("negative control: the pitch clause removed, exactly block_pitch went red")
}
