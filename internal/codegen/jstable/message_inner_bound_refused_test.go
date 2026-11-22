package jstable

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// innerSchemaPath and innerCorpusDir are the committed inner-bound schema and
// the shared hostile corpus that carries its message rows
// (internal/ci/hostilecorpus/inner_inputs_test.go pins them to the Go engine).
const (
	innerSchemaPath = "../../../test/tables/inner/Inner.schema"
	innerCorpusDir  = "../../../internal/ci/hostilecorpus/corpus"
)

// innerFixedRoots are the Inner.schema roots the message rows name: an array
// of one child kind, with no minimum of its own (Loose) and with a minimum of
// one (Tight).
var innerFixedRoots = []string{
	"FixedLooseCount", "FixedTightCount", "FixedLooseRange", "FixedTightRange",
	"FixedLooseText", "FixedTightText", "FixedLooseBytes", "FixedTightBytes",
}

// TestJsMessageInnerBoundRefused pins the strict message read
// (docs/SPEC-TABLES.md §4, docs/plans/strict-only.md): a body whose child one
// record down carries a count past its Max, a value past its range, or a
// string or bytes longer than its capacity REFUSES the batch, as the Go engine
// refuses it (tablewire/messagedecode.go). The body walk counts each of those
// in report.Oor and cuts the value to the bound; LoadMessages handed that cut
// value back as one delivered body, n = 1 with no verdict, so a caller holding
// to the verdict read the cut child as the sender's. The node half runs every
// inner-message row of the committed hostile corpus: each past-the-bound row is
// refused with the open-refused verdict and n = -1, and each at-the-bound row
// (the control) reads one body.
func TestJsMessageInnerBoundRefused(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(innerSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	table := string(files["ProbeTable.js"])
	for _, root := range innerFixedRoots {
		fn := jsFn(table, root+"LoadMessages")
		if fn == "" {
			t.Fatalf("no %sLoadMessages in ProbeTable.js", root)
		}
		if !strings.Contains(fn, "if (report.Oor > 0 || report.Mismatch > 0) {") ||
			!strings.Contains(fn, `TableMessageRefuse(report, report.Oor > 0 ? "out_of_range" : "kind_mismatch");`) {
			t.Errorf("%sLoadMessages delivers a body its walk counted out of range or mismatched; it must refuse the batch", root)
		}
	}

	t.Run("node", func(t *testing.T) {
		slowtest.Gate(t, "node (it runs the generated reads)")
		node := jsNode(t)
		dir := t.TempDir()
		base, err := js.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		for _, set := range []map[string][]byte{base, files} {
			for name, data := range set {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		rows := innerMessageRows(t)
		if len(rows) != 16 {
			t.Fatalf("the corpus carries %d inner-message rows; want 16", len(rows))
		}
		var list strings.Builder
		for _, r := range rows {
			input, err := os.ReadFile(filepath.Join(innerCorpusDir, r.input))
			if err != nil {
				t.Fatal(err)
			}
			list.WriteString(r.name + "\t" + r.table + "\t" + r.verdict + "\t")
			for i, b := range input {
				if i > 0 {
					list.WriteByte(',')
				}
				list.WriteString(strconv.Itoa(int(b)))
			}
			list.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(dir, "rows.tsv"), []byte(list.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(jsInnerMessageProbe), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := testproc.Command(t, node, "probe.mjs")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "OK 16" {
			t.Errorf("inner-message rows (%v):\n%s", err, out)
		}
	})
}

type innerMessageRow struct{ name, table, input, verdict string }

// innerMessageRows reads the manifest's inner-message rows: name, root, input
// file and the engine's verdict.
func innerMessageRows(t *testing.T) []innerMessageRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(innerCorpusDir, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []innerMessageRow
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 9 || !strings.HasPrefix(f[0], "inner-message-") {
			continue
		}
		if f[1] != "message" || f[2] != "test/tables/inner" {
			t.Fatalf("row %s names carrier %s, schema %s", f[0], f[1], f[2])
		}
		rows = append(rows, innerMessageRow{name: f[0], table: f[3], input: f[4], verdict: f[6]})
	}
	return rows
}

const jsInnerMessageProbe = `import { readdirSync, readFileSync } from "node:fs";
// the batch verbs, the fixed class and the vocabulary live in different
// modules of the unit, and the fixed and variable classes share a name, so a
// lookup names everything it needs from one module
const mods = [];
for (const f of readdirSync(".").filter((f) => f.endsWith(".js")).sort()) { mods.push(await import("./" + f)); }
const pick = (...names) => mods.find((m) => names.every((n) => m[n] !== undefined));
let failed = false, ran = 0;
function fail(why) { failed = true; console.log("FAILED: " + why); }
const a = pick("TableVocabulary", "AnnounceRead", "Announce", "AnnounceMeasure", "TableMessageOpenRefused");
const voc = new a.TableVocabulary();
voc.Init(new Array(4096).fill(null));
const own = new Uint8Array(a.AnnounceMeasure());
a.Announce(own);
if (!a.AnnounceRead(voc, own, {})) { fail("the unit could not read its own announcement"); }
for (const line of readFileSync("rows.tsv", "utf8").split("\n").filter((l) => l)) {
  const [name, T, want, csv] = line.split("\t");
  const data = Uint8Array.from(csv.split(",").map(Number));
  const c = pick("Init" + T, T);
  const value = new c[T]();
  c["Init" + T](value);
  const rep = {};
  const n = pick(T + "LoadMessages")[T + "LoadMessages"]([value], voc, data, rep);
  const refused = rep.Verdict === a.TableMessageOpenRefused;
  ran++;
  if (want === "refuse" && !(refused && n === -1)) { fail(name + ": delivered n=" + n + " " + JSON.stringify(rep)); }
  if (want === "accept" && (refused || n !== 1 || rep.Damaged)) { fail(name + ": refused n=" + n + " " + JSON.stringify(rep)); }
}
if (!failed) { console.log("OK " + ran); }
`
