// Package wrpacket is the shared half of the `write-refusal-packet` surface
// (docs/SPEC.md §5.1, test/conformance/README.md): the schema, the row list,
// the generation step and the verdict check every leg's driver shares. A leg's
// test generates PacketWriteRefusal.schema with Generate, runs its own probe
// over rows.txt, and hands the probe's output to Check. The probe is the leg's
// own code and speaks one line per row: "<name> refused" or "<name> wrote".
package wrpacket

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// Dir is this package's directory relative to the repository root.
const Dir = "test/conformance/write-refusal-packet"

// Row is one row of rows.txt: a case name, the verdict the writer owes, and one
// op on the fresh value.
type Row struct {
	Name, Verdict, Op, Arg string
}

// RowsPath is the absolute path of rows.txt for a repository root.
func RowsPath(root string) string {
	abs, err := filepath.Abs(filepath.Join(root, Dir, "rows.txt"))
	if err != nil {
		panic(err)
	}
	return abs
}

// Rows reads rows.txt under root.
func Rows(t testing.TB, root string) []Row {
	t.Helper()
	data, err := os.ReadFile(RowsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var rows []Row
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 || (f[1] != "refused" && f[1] != "wrote") {
			t.Fatalf("rows.txt: %q is not <name> refused|wrote <op> <arg>", line)
		}
		rows = append(rows, Row{f[0], f[1], f[2], f[3]})
	}
	if len(rows) == 0 {
		t.Fatal("rows.txt holds no rows")
	}
	return rows
}

// Generate loads PacketWriteRefusal.schema under root, generates it for lang
// and writes the files into a fresh temporary directory, which it returns.
func Generate(t testing.TB, root, lang string, opts compiler.Options) string {
	t.Helper()
	c := compiler.New()
	u, err := c.Load([]string{filepath.Join(root, Dir, "PacketWriteRefusal.schema")})
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, lang, opts)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Check holds a probe's output against the rows. Every row must be answered
// exactly once with its verdict; a miss prints `FAILED: <name>: ...` so a
// negative control can name the row it expects red. exempt names rows a leg
// cannot build (the reason is the value), and an exempt row must still be
// answered no other way than as absent: it may not be answered at all.
func Check(t testing.TB, out string, rows []Row, exempt map[string]string) {
	t.Helper()
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && (f[1] == "refused" || f[1] == "wrote") {
			if _, dup := got[f[0]]; dup {
				t.Errorf("FAILED: %s: answered twice", f[0])
			}
			got[f[0]] = f[1]
		}
	}
	known := map[string]bool{}
	for _, r := range rows {
		known[r.Name] = true
		if _, ok := exempt[r.Name]; ok {
			if v, answered := got[r.Name]; answered {
				t.Errorf("FAILED: %s: exempt (%s) but answered %q; drop the exemption", r.Name, exempt[r.Name], v)
			}
			continue
		}
		v, answered := got[r.Name]
		switch {
		case !answered:
			t.Errorf("FAILED: %s: the probe did not answer this row", r.Name)
		case v != r.Verdict && r.Verdict == "refused":
			t.Errorf("FAILED: %s: wrote a value the declaration refuses", r.Name)
		case v != r.Verdict:
			t.Errorf("FAILED: %s: refused the lawful value at the boundary", r.Name)
		}
	}
	var stray []string
	for name := range got {
		if !known[name] {
			stray = append(stray, name)
		}
	}
	sort.Strings(stray)
	if len(stray) > 0 {
		t.Errorf("FAILED: the probe answered rows rows.txt does not hold: %s", strings.Join(stray, " "))
	}
	if t.Failed() {
		t.Fatalf("write-refusal-packet: the probe's verdicts differ from rows.txt\n%s", out)
	}
}
