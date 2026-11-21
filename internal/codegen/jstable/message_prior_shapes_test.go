package jstable

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
)

// priorShapeRow is one `message-prior` row of the conformance manifest
// (testdata/conformance/tables/FORMAT.md): a prior writer's batch of one body
// and whether today's reader reads it.
type priorShapeRow struct {
	name   string
	reason string // "" reads
}

func priorShapeRows(t *testing.T, repo string) []priorShapeRow {
	t.Helper()
	f, err := os.Open(filepath.Join(repo, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var rows []priorShapeRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 8 && fields[0] == "message-prior" && fields[2] == "tblprior" {
			row := priorShapeRow{name: fields[1]}
			if fields[7] != "read" {
				row.reason = fields[7]
			}
			rows = append(rows, row)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no message-prior row of tblprior")
	}
	return rows
}

// TestJsMessageReadNeverClampsOrCounts is the fast half, over the unit that
// carries one field per shape a ranged value takes in a message
// (test/tables/prior/PriorNarrow.schema): the generated message read holds no
// clamp-and-count or default-and-count statement at all: each ranged shape
// refuses out_of_range and a moved kind refuses kind_mismatch in its place (docs/SPEC-TABLES.md §3.3, §4).
func TestJsMessageReadNeverClampsOrCounts(t *testing.T) {
	t.Parallel()
	files, err := Generate(loadUnit(t, "../../../test/tables/prior/PriorNarrow.schema"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var all strings.Builder
	for name, data := range files {
		if strings.HasSuffix(name, ".js") {
			all.Write(data)
		}
	}
	src := all.String()
	if !strings.Contains(src, "function PriorLoadMessageBody(") {
		t.Fatal("the generated unit has no PriorLoadMessageBody")
	}
	for _, never := range []string{"Oor++", "Oor +=", "Mismatch++", "Mismatch +=", "x = 50", "= 50n;"} {
		if strings.Contains(src, never) {
			t.Errorf("the generated unit still clamps, defaults or counts a value (%q)", never)
		}
	}
	refuse := `{ TableMessageRefuse(r.Report, "out_of_range"); return false; }`
	for _, want := range []string{
		// a ranged integer, at a field, an array element and a nested record
		"if (x < 0 || x > 50) " + refuse,
		// a ranged int64
		"if (x < 0n || x > 50n) " + refuse,
		// bits(5), tested on the value as sent, before any narrowing
		"if (!holds || x < 0n || x > 31n) " + refuse,
		// a kind that is not this reader's and does not widen into it
		`{ TableMessageRefuse(r.Report, "kind_mismatch"); return false; }`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the generated message read lacks %q", want)
		}
	}
}

// TestJsMessagePriorWriterShapes reads the conformance corpus's prior-writer
// rows through the generated JavaScript message reader: the committed
// announcement and batches of the earlier release PriorWide, read as today's
// PriorNarrow. Every shape's value inside the reader's range reads with no
// counter moved, every shape's value past it refuses out_of_range, and a kind
// that moved refuses kind_mismatch unless it is the lawful widening, which
// reads: each the independent engine's answer (TestMessagePriorWriterRows in
// test/conformance/harness holds the engine to the same rows).
func TestJsMessagePriorWriterShapes(t *testing.T) {
	node := jsNode(t)
	t.Parallel()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	rows := priorShapeRows(t, repo)
	reader := loadUnit(t, filepath.Join(repo, "test/tables/prior/PriorNarrow.schema"))
	files, err := Generate(reader)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	base, err := js.Generate(reader)
	if err != nil {
		t.Fatalf("js.Generate: %v", err)
	}
	dir := t.TempDir()
	var modules []string
	for _, set := range []map[string][]byte{base, files} {
		for name, data := range set {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(name, "Table.js") {
				modules = append(modules, name)
			}
		}
	}
	wires := filepath.Join(repo, "testdata/wire/tables/prior")
	var probe strings.Builder
	probe.WriteString("import { readFileSync } from \"node:fs\";\nconst m = {};\n")
	for _, name := range modules {
		fmt.Fprintf(&probe, "Object.assign(m, await import(%q));\n", "./"+name)
	}
	fmt.Fprintf(&probe, "const announce = new Uint8Array(readFileSync(%q));\n", filepath.Join(wires, "tblprior_Prior_conn.bin"))
	for _, row := range rows {
		fmt.Fprintf(&probe, `{
  const voc = new m.TableVocabulary();
  voc.Init(new Array(64));
  if (!m.AnnounceRead(voc, announce, {})) { throw new Error("announce refused"); }
  const rep = {};
  const n = m.PriorLoadMessages([new m.Prior()], voc, new Uint8Array(readFileSync(%q)), rep);
  console.log("ROW %s loaded=" + (n === 1 && !rep.Damaged) + "|reason=" + (rep.Reason ?? "") + "|oor=" + (rep.Oor ?? 0) + "|mismatch=" + (rep.Mismatch ?? 0));
}
`, filepath.Join(wires, row.name+"_message.bin"), row.name)
	}
	probePath := filepath.Join(dir, "probe.mjs")
	if err := os.WriteFile(probePath, []byte(probe.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, probePath)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node probe failed: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "ROW "); ok {
			name, rest, _ := strings.Cut(after, " ")
			got[name] = rest
		}
	}
	for _, row := range rows {
		want := "loaded=true|reason=|oor=0|mismatch=0"
		if row.reason != "" {
			want = "loaded=false|reason=" + row.reason + "|oor=0|mismatch=0"
		}
		if got[row.name] != want {
			t.Errorf("%s: JS=%q, want %q", row.name, got[row.name], want)
		}
	}
}
