package jstable

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsReadsRefuseNonCanonical is the JavaScript read of the non-canonical
// spellings (docs/SPEC-TABLES.md §3, §3.1, §3.3, §4). Each canonical file
// reads and saves back to the same bytes. Every other file is refused. A
// manifest row the probe does not reach fails the test. LoadMeasure answers
// the framing measure. The message load is the batch read.
//
// A message body that drops a field whose only spelling is the writer's
// elided default is that default's canonical encoding. When the file is that
// encoding, the read is the lawful one.
func TestJsReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	node := jsNode(t)

	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "testdata", "wire", "non-canonical")
	text, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	nc := loadUnit(t, filepath.Join(repo, "test", "non-canonical", "NonCanonical.schema"))
	one := loadUnit(t, filepath.Join(repo, "test", "message-one-encoding", "One.schema"))
	model := tabletext.NewModel(nc)
	defMsg, err := tablewire.EncodeMessage(model, model.New(model.Lookup("Message")))
	if err != nil {
		t.Fatal(err)
	}

	type row struct {
		Name    string `json:"name"`
		Form    string `json:"form"`
		Read    bool   `json:"read"`
		Measure int64  `json:"measure"`
		File    string `json:"file"`
		Unit    string `json:"unit"`
	}
	var rows []row
	seen := map[string]bool{}
	for i, line := range strings.Split(strings.TrimSpace(string(text)), "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields", i+1, len(f))
		}
		if seen[f[0]] {
			t.Fatalf("MANIFEST.tsv:%d: duplicate %s", i+1, f[0])
		}
		seen[f[0]] = true
		wirePath := filepath.Join(dir, f[0])
		wire, err := os.ReadFile(wirePath)
		if err != nil {
			t.Fatal(err)
		}
		read := f[3] == "read"
		if f[3] != "read" && f[3] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q", i+1, f[3])
		}
		if f[0] == "message-omits-declared-field.bin" && bytes.Equal(wire, defMsg) {
			read = true
		}
		base := filepath.Base(f[0])
		unit := "nc"
		if strings.Contains(f[0], "message-one-encoding") {
			base = "moe-" + base
			unit = "one"
		}
		r := row{Name: f[0], Form: f[1], Read: read, File: base, Unit: unit}
		if f[1] == "variable" || f[1] == "announcement" {
			root := findTable(t, nc, "Root")
			want, _, refused := tablewire.FileRegionMeasure(nc, root, wire)
			if refused {
				want = -1
			}
			r.Measure = want
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}

	files := map[string][]byte{}
	for _, r := range rows {
		wire, err := os.ReadFile(filepath.Join(dir, r.Name))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := files[r.File]; ok {
			t.Fatalf("probe basename collision %s", r.File)
		}
		files[r.File] = wire
	}
	oneAnn, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "message-one-encoding", "announcement.bin"))
	if err != nil {
		t.Fatal(err)
	}
	files["moe-announcement.bin"] = oneAnn
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	ncTree := generatedTree(nc)
	oneTree := generatedTree(one)
	ncVar := moduleExporting(t, ncTree, "export function RootLoad(")
	ncHome := moduleExporting(t, ncTree, "export const TableMessageAnnouncement")
	ncMsg := moduleExporting(t, ncTree, "export function MessageLoadMessages(")
	oneHome := moduleExporting(t, oneTree, "export const TableMessageAnnouncement")
	oneMsg := moduleExporting(t, oneTree, "export function OneLoadMessages(")
	writeGenerated(t, files, "nc-", ncTree)
	writeGenerated(t, files, "one-", oneTree)

	probe := `import { readFileSync } from "node:fs";
import * as v from "./nc-` + ncVar + `.js";
import * as nh from "./nc-` + ncHome + `.js";
import * as nm from "./nc-` + ncMsg + `.js";
import * as oh from "./one-` + oneHome + `.js";
import * as om from "./one-` + oneMsg + `.js";
let failures = 0;
const reached = [];
function fail(why) { console.error("FAIL: " + why); failures++; }
function bytesEq(a, b) {
  if (a === null || b === null || a.length !== b.length) { return false; }
  for (let i = 0; i < a.length; i++) { if (a[i] !== b[i]) { return false; } }
  return true;
}
function announce(home, file) {
  const vocab = new home.TableVocabulary();
  vocab.Init(new Array(home.TableMessageEntriesHere));
  const ann = new Uint8Array(readFileSync(file));
  const report = {};
  const ok = home.AnnounceRead(vocab, ann, report);
  return { ok, vocab, ann, report };
}
const ncAnn = announce(nh, "announcement.bin");
const oneAnn = announce(oh, "moe-announcement.bin");
const rows = ` + string(encoded) + `;
for (const row of rows) {
  reached.push(row.name);
  const wire = new Uint8Array(readFileSync(row.file));
  if (row.form === "announcement") {
    if (!ncAnn.ok || !bytesEq(ncAnn.ann, nh.TableMessageAnnouncement)) { fail(row.name + " announce"); }
    const measure = Number(v.RootLoadMeasure(wire));
    if (measure !== row.measure) { fail(row.name + " measure " + measure + " " + row.measure); }
    continue;
  }
  if (row.form === "variable") {
    const report = new v.TableVariableReport();
    const loaded = v.RootLoad(wire, report);
    const retain = new v.TableVariableRetain(new Uint8Array(1 << 16), new Array(256));
    const retainReport = new v.TableVariableReport();
    const retained = v.RootLoadRetain(wire, retain, retainReport);
    const measure = Number(v.RootLoadMeasure(wire));
    if (measure !== row.measure) { fail(row.name + " measure " + measure + " " + row.measure); }
    if (row.read) {
      if (loaded === null || retained === null) { fail(row.name + " read"); continue; }
      const saved = v.RootSave(loaded);
      if (!bytesEq(saved, wire)) { fail(row.name + " save"); }
      if (retain.used !== 0 || retain.count !== 0) { fail(row.name + " retain empty"); }
    } else if (loaded !== null || retained !== null || retain.used !== 0 || retain.count !== 0) {
      fail(row.name + " refused");
    }
    continue;
  }
  const home = row.unit === "one" ? oneAnn : ncAnn;
  const Ctor = row.unit === "one" ? oh.One : nh.Message;
  const load = row.unit === "one" ? om.OneLoadMessages : nm.MessageLoadMessages;
  const save = row.unit === "one" ? om.OneSaveMessages : nm.MessageSaveMessages;
  const measureFn = row.unit === "one" ? om.OneMeasureMessages : nm.MessageMeasureMessages;
  if (!home.ok) { fail(row.name + " announce"); continue; }
  const storage = [new Ctor()];
  const report = {};
  const n = load(storage, home.vocab, wire, report);
  const refused = n !== 1 || report.Damaged || report.Verdict !== 0;
  if (row.read) {
    if (refused) { fail(row.name + " read"); continue; }
    const buf = new Uint8Array(wire.length + 64);
    const sn = save([storage[0]], buf, {});
    if (sn !== wire.length || !bytesEq(buf.subarray(0, sn), wire)) { fail(row.name + " save"); }
    const measured = measureFn([storage[0]], {});
    if (measured !== wire.length) { fail(row.name + " measure " + measured + " " + wire.length); }
  } else if (!refused) {
    fail(row.name + " refused");
  }
}
if (reached.length !== rows.length) { fail("reached " + reached.length + " of " + rows.length); }
if (failures !== 0) { process.exit(1); }
console.log("ok");
`
	dirOut := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dirOut, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dirOut, "probe.mjs"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "probe.mjs")
	cmd.Dir = dirOut
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func moduleExporting(t *testing.T, files map[string][]byte, needle string) string {
	t.Helper()
	found := ""
	for name, data := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".js") {
			continue
		}
		if strings.Contains(string(data), needle) {
			if found != "" {
				t.Fatalf("%s is in %s and %s", needle, found, name)
			}
			found = strings.TrimSuffix(name, ".js")
		}
	}
	if found == "" {
		t.Fatalf("no module exports %s", needle)
	}
	return found
}

func writeGenerated(t *testing.T, dst map[string][]byte, prefix string, tree map[string][]byte) {
	t.Helper()
	for name, data := range tree {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".js") {
			continue
		}
		base := prefix + name
		rewritten := rewriteJSImports(string(data), prefix)
		if _, ok := dst[base]; ok {
			t.Fatalf("generated basename collision %s", base)
		}
		dst[base] = []byte(rewritten)
	}
}

// rewriteJSImports points a generated module's relative imports at the
// prefixed copies written beside it. The modules import no runtime.
func rewriteJSImports(src, prefix string) string {
	return strings.ReplaceAll(src, `from "./`, `from "./`+prefix)
}
