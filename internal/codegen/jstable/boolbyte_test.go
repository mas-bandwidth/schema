package jstable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestBoolByteAboveOneIsMalformedJavaScript pins the pinned fuzz vector
// bool_byte_above_one (testdata/wire/tables/fuzz-vectors/INDEX.txt) on the
// JavaScript leg's variable-form Load: a bool payload byte that is neither 0
// nor 1 reads TRUE and counts one malformed (docs/SPEC-TABLES.md §3, §6.5),
// exactly as internal/tablewire.Decode answers. The JavaScript reader read it
// true and counted nothing; the wire fuzzer found it at random mutant #18630
// of seed 24845619678 (tabledemo.ProfileConfig), a pass ci-full never reaches,
// so the vector is held here, where the emitter lane runs it on every change.
func TestBoolByteAboveOneIsMalformedJavaScript(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	const root = "ProfileConfig"
	u := loadUnitForMeasure(t, repo, "tables/examples")
	m := tabletext.NewModel(u)
	def := m.Lookup(root)
	if def == nil {
		t.Fatalf("no table %s in tables/examples", root)
	}
	wirePath := filepath.Join(repo, "testdata/wire/tables/fuzz-vectors/bool_byte_above_one.bin")
	wire, err := os.ReadFile(wirePath)
	if err != nil {
		t.Fatal(err)
	}

	inst := m.New(def)
	var rep tabletext.Report
	ok, decErr := tablewire.Decode(m, inst, wire, &rep)
	if decErr != nil || !ok {
		t.Fatalf("oracle Decode: ok=%v err=%v", ok, decErr)
	}
	if !rep.Malformed {
		t.Fatalf("the vector no longer carries a bool byte above 1: the oracle reads it silent")
	}
	wantSaved, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle Encode: %v", err)
	}
	want := fmt.Sprintf("%d|%d|%d|%d|%d|malformed=%v",
		rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate, rep.Malformed)

	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	work := t.TempDir()
	home := runtimeHome(u)
	module := filepath.Join(work, home+"Variable.js")
	data, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no variable module %sVariable.js generated", home)
	}
	if err := os.WriteFile(module, data, 0o600); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf(`
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(%q).href);
const wire = new Uint8Array(readFileSync(%q));
const rep = new m.TableVariableReport();
const v = m.%sLoad(wire, rep);
if (v === null) { console.log("JS null"); process.exit(0); }
const hex = Buffer.from(m.%sSave(v)).toString("hex");
console.log("JS " + [rep.unknown, rep.kindMismatch, rep.widened, rep.clamped, rep.duplicate].join("|") + "|malformed=" + rep.malformed + "|hasLoadout=" + v.has_loadout + "|hex=" + hex);
`, module, wirePath, root, root)
	script := filepath.Join(work, "probe.mjs")
	if err := os.WriteFile(script, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node probe failed: %v\n%s", err, out)
	}
	line := ""
	for l := range strings.SplitSeq(string(out), "\n") {
		if after, found := strings.CutPrefix(l, "JS "); found {
			line = after
		}
	}
	got, gotHex, found := strings.Cut(line, "|hex=")
	if !found {
		t.Fatalf("unexpected JS output:\n%s", out)
	}
	if wantLine := want + "|hasLoadout=true"; got != wantLine {
		t.Fatalf("JS = %q, want %q (the oracle's report, the bool read true)", got, wantLine)
	}
	gotSaved, err := hex.DecodeString(strings.TrimSpace(gotHex))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotSaved, wantSaved) {
		t.Fatalf("JS saved %x, the oracle encodes %x", gotSaved, wantSaved)
	}
}
