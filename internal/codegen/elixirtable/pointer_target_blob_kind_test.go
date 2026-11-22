// The Elixir leg's half of docs/SPEC-TABLES.md §3.1 and §6.5 for a pointer's
// target type (docs/rulings.json pointer-target-type-checked and
// blob-target-outside-closure-refused). A record of a type this reader NAMES
// (every table the unit declares, and both reserved blob ids, whatever the
// root's closure reaches) under a declared slot of another type refuses the
// whole read wire_damaged; a record no pointer reaches refuses; a record of a
// type the reader cannot name reads null, and a blob record reached only from a
// field the reader does not declare is skipped and counted. Every row of
// testdata/wire/pointer-target-type/MANIFEST.tsv and of its blobkind/
// MANIFEST.tsv is read by T.load and T.load_retain, with the Go engine
// (internal/tablewire) as the oracle for each row's verdict and unknown count.
package elixirtable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type elixirPointerTargetRow struct {
	name, unit, root, file, verdict string
	unknown                         int
}

func elixirPointerTargetRows(t *testing.T, manifest string) []elixirPointerTargetRow {
	t.Helper()
	var rows []elixirPointerTargetRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		row := elixirPointerTargetRow{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
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
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	return rows
}

// elixirPointerTargetShow prints one read's answer as verdict:detail:pointer —
// read:<unknown>:<null|set> or refused:<kind>:<reason>.
const elixirPointerTargetShow = `store = %{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
zero = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
slot = fn
  {root, _directory}, field -> Map.get(root, field)
  root, field -> Map.get(root, field)
end
show = fn result, field ->
  case elem(result, 0) do
    :ok ->
      p = if slot.(elem(result, 1), field) == nil, do: "null", else: "set"
      "read:#{elem(result, 2).unknown}:#{p}"
    :error ->
      "refused:#{elem(result, 1)}:#{elem(result, 2).reason}"
  end
end
`

// elixirPointerTargetProbe reads every row's wire FILE through both reads, and
// prints ROW|name|load|retain.
func elixirPointerTargetProbe(ns string, vectors string, rows []elixirPointerTargetRow, field map[string]string) string {
	var p strings.Builder
	fmt.Fprintf(&p, "alias %s.Table, as: T\n", ns)
	p.WriteString(elixirPointerTargetShow)
	for _, row := range rows {
		fmt.Fprintf(&p, "wire = File.read!(%q)\n", filepath.Join(vectors, row.file))
		fmt.Fprintf(&p, "IO.puts(\"ROW|%s|\" <> show.(T.load(%q, wire), %q) <> \"|\" <> show.(T.load_retain(%q, wire, store, zero), %q))\n",
			row.name, row.root, field[row.root], row.root, field[row.root])
	}
	return p.String()
}

// elixirPointerTargetCheck holds each row's two Elixir answers to the engine's
// verdict and the manifest's pinned unknown count.
func elixirPointerTargetCheck(t *testing.T, out string, rows []elixirPointerTargetRow) {
	t.Helper()
	got := map[string][]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if f := strings.Split(strings.TrimSpace(line), "|"); len(f) == 4 && f[0] == "ROW" {
			got[f[1]] = f[2:]
		}
	}
	for _, row := range rows {
		answers, ok := got[row.name]
		if !ok {
			t.Errorf("%s: no answer\n%s", row.name, out)
			continue
		}
		for i, entry := range []string{"load", "load_retain"} {
			a := strings.Split(answers[i], ":")
			if row.verdict == "refused" {
				if a[0] != "refused" || len(a) != 3 || a[1] != "malformed" || a[2] != "wire_damaged" {
					t.Errorf("%s (%s): %s, want refused:malformed:wire_damaged", row.name, entry, answers[i])
				}
				continue
			}
			if a[0] != "read" || len(a) != 3 {
				t.Errorf("%s (%s): %s, the manifest says read", row.name, entry, answers[i])
				continue
			}
			if row.unknown >= 0 && a[1] != strconv.Itoa(row.unknown) {
				t.Errorf("%s (%s): unknown %s, want %d", row.name, entry, a[1], row.unknown)
			}
		}
	}
}

// elixirPointerTargetOracle holds the engine to each row before the leg is.
func elixirPointerTargetOracle(t *testing.T, u *ir.Unit, vectors string, rows []elixirPointerTargetRow) {
	t.Helper()
	m := tabletext.NewModel(u)
	for _, row := range rows {
		st := u.Tables[row.root]
		if st == nil {
			t.Fatalf("%s: the unit declares no root %s", row.name, row.root)
		}
		wire, err := os.ReadFile(filepath.Join(vectors, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		var r tabletext.Report
		ok, err := tablewire.Decode(m, m.New(st), wire, &r)
		if row.verdict == "refused" {
			if ok || !tablewire.ReadRefusal(err) {
				t.Fatalf("%s engine oracle: accepted (%+v %v), the manifest says refused", row.name, r, err)
			}
			continue
		}
		if !ok || err != nil || (row.unknown >= 0 && r.Unknown != row.unknown) {
			t.Fatalf("%s engine oracle: %v %+v %v, the manifest says read with unknown %d", row.name, ok, r, err, row.unknown)
		}
	}
}

func elixirPointerTargetUnit(t *testing.T, repo, unit string) *ir.Unit {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repo, unit, "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("unit %s: %v", unit, err)
	}
	return loadUnit(t, paths...)
}

// plantRootClosureNaming copies the generated reader with the ROOT CLOSURE as
// the set of types it can name: a record this root cannot place reads as one of
// a type the reader cannot name, the rule this ruling replaced.
func plantRootClosureNaming(t *testing.T, files map[string][]byte) map[string][]byte {
	t.Helper()
	planted := map[string][]byte{}
	n := 0
	for name, src := range files {
		if strings.Contains(string(src), "{:unplaced, ") {
			src = []byte(strings.ReplaceAll(string(src), "{:unplaced, ", "{:unknown, "))
			n++
		}
		planted[name] = src
	}
	if n == 0 {
		t.Fatal("no generated reader names an unplaced record: the control plants nothing")
	}
	return planted
}

func TestElixirPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Elixir blob-kind pointer targets outside the root's closure, read and written")
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(repo, "testdata", "wire", "pointer-target-type", "blobkind")
	manifest, err := os.ReadFile(filepath.Join(vectors, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := elixirPointerTargetRows(t, string(manifest))
	u := elixirPointerTargetUnit(t, repo, rows[0].unit)
	for _, row := range rows {
		if row.unit != rows[0].unit {
			t.Fatalf("%s reads %s; this probe reads %s", row.name, row.unit, rows[0].unit)
		}
	}
	elixirPointerTargetOracle(t, u, vectors, rows)

	// THE WRITER emits no record that no pointer reaches: each root saved with
	// its blob set, empty and null decodes on the engine accepted with unknown
	// 0, matches the engine's own wire, and reads back on the leg the same way.
	m := tabletext.NewModel(u)
	saves := []struct{ root, json string }{
		{"Text", `{"body":"hello"}`},
		{"Text", `{"body":""}`},
		{"Text", `{}`},
		{"Bin", `{"data":"eHl6"}`},
		{"Bin", `{"data":""}`},
		{"Bin", `{}`},
	}
	field := map[string]string{"Text": "body", "Bin": "data"}
	ns := ir.GoExportName(u.Package)
	var p strings.Builder
	p.WriteString(elixirPointerTargetProbe(ns, vectors, rows, field))
	engineWires := make([]string, len(saves))
	for i, s := range saves {
		v := m.New(u.Tables[s.root])
		var r tabletext.Report
		if !m.Read(v, []byte(s.json), &r) || !r.Silent() {
			t.Fatalf("save %d %s %s: the engine reads the text %+v", i, s.root, s.json, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
		engineWires[i] = hex.EncodeToString(wire)
		fmt.Fprintf(&p, `{:ok, v, _} = T.from_json(%q, %q)
{:ok, wire} = T.save(%q, v)
IO.puts("SAVE|%d|" <> Base.encode16(wire, case: :lower) <> "|" <> show.(T.load(%q, wire), %q))
`, s.root, s.json, s.root, i, s.root, field[s.root])
	}
	p.WriteString("IO.puts(\"BLOB_KIND_OK\")\n")
	probe := p.String()

	files := generateElixir(t, u)
	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil || !strings.Contains(out, "BLOB_KIND_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("blob-kind probe: %v\n%s", err, out)
	}
	elixirPointerTargetCheck(t, out, rows)
	saved := map[int][]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if f := strings.Split(strings.TrimSpace(line), "|"); len(f) == 4 && f[0] == "SAVE" {
			i, err := strconv.Atoi(f[1])
			if err != nil {
				t.Fatal(err)
			}
			saved[i] = f[2:]
		}
	}
	for i, s := range saves {
		got, ok := saved[i]
		if !ok {
			t.Errorf("save %d %s %s: no answer", i, s.root, s.json)
			continue
		}
		wire, err := hex.DecodeString(got[0])
		if err != nil {
			t.Fatal(err)
		}
		var r tabletext.Report
		ok, err = tablewire.Decode(m, m.New(u.Tables[s.root]), wire, &r)
		if !ok || err != nil || r.Unknown != 0 || r.Refused {
			t.Errorf("save %d %s %s: the engine reads the leg's wire %v %+v %v", i, s.root, s.json, ok, r, err)
		}
		if got[0] != engineWires[i] {
			t.Errorf("save %d %s %s: the leg writes %s, the engine %s", i, s.root, s.json, got[0], engineWires[i])
		}
		if !strings.HasPrefix(got[1], "read:0:") {
			t.Errorf("save %d %s %s: the leg reads its own wire back %s", i, s.root, s.json, got[1])
		}
	}

	// NEGATIVE CONTROL: the root closure as the set the reader names reads
	// text-names-bytes, a wrong-kind blob under a declared slot, as null.
	t.Run("root-closure-naming-control", func(t *testing.T) {
		out, err := runArmProbe(t, elixirBinary(t), plantRootClosureNaming(t, files), probe)
		if err != nil {
			t.Fatalf("planted probe: %v\n%s", err, out)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if strings.HasPrefix(line, "ROW|text-names-bytes|read:") {
				return
			}
		}
		t.Fatalf("the planted root-closure naming did not read text-names-bytes:\n%s", out)
	})
}

func TestElixirPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Elixir pointer target types the reader declares outside the root's closure")
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(repo, "testdata", "wire", "pointer-target-type")
	manifest, err := os.ReadFile(filepath.Join(vectors, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := elixirPointerTargetRows(t, string(manifest))
	u := elixirPointerTargetUnit(t, repo, rows[0].unit)
	for _, row := range rows {
		if row.unit != rows[0].unit {
			t.Fatalf("%s reads %s; this probe reads %s", row.name, row.unit, rows[0].unit)
		}
	}
	elixirPointerTargetOracle(t, u, vectors, rows)
	probe := elixirPointerTargetProbe(ir.GoExportName(u.Package), vectors, rows, map[string]string{"Node": "next"}) +
		"IO.puts(\"POINTER_TARGET_TYPE_OK\")\n"
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "POINTER_TARGET_TYPE_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("pointer-target-type probe: %v\n%s", err, out)
	}
	elixirPointerTargetCheck(t, out, rows)
	// a record of a type the reader cannot name reads null under its slot
	nulled := 0
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Split(strings.TrimSpace(line), "|")
		if len(f) != 4 || f[0] != "ROW" {
			continue
		}
		for _, row := range rows {
			if row.name == f[1] && row.verdict == "read" && row.unknown > 0 {
				nulled++
				for _, a := range f[2:] {
					if !strings.HasSuffix(a, ":null") {
						t.Errorf("%s: the pointer reads %s, want null", row.name, a)
					}
				}
			}
		}
	}
	if nulled == 0 {
		t.Fatalf("no read row with an unknown record answered, so the null-pointer rule was checked against nothing\n%s", out)
	}
}
