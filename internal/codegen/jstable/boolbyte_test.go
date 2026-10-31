package jstable

import (
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
// nor 1 is damage (docs/SPEC-TABLES.md §3), and DAMAGE DISCARDS THE WHOLE LOAD
// (§4): the Load hands back no value and the report says malformed, exactly as
// internal/tablewire.Decode refuses it. The wire fuzzer found the JavaScript
// reader reading it true and counting nothing at random mutant #18630 of seed
// 24845619678 (tabledemo.ProfileConfig), a pass ci-full never reaches, so the
// vector is held here, where the emitter lane runs it on every change.
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

	var rep tabletext.Report
	ok, decErr := tablewire.Decode(m, m.New(def), wire, &rep)
	if decErr != nil {
		t.Fatalf("oracle Decode: %v", decErr)
	}
	if ok || !rep.Malformed {
		t.Fatalf("the vector no longer carries a bool byte above 1: the oracle answers ok=%v %+v", ok, rep)
	}

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
console.log("JS loaded=" + (v !== null) + "|malformed=" + rep.malformed);
`, module, wirePath, root)
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
	if want := "loaded=false|malformed=true"; line != want {
		t.Fatalf("JS = %q, want %q (damage hands back no value)", line, want)
	}
}
