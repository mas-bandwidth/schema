package jstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestStreamCountPastBodyClampsLikeGoJavaScript pins the JavaScript leg's read
// of a stream count whose own spelling runs past the body it is framed by
// (docs/SPEC-TABLES.md §3, §4; the fuzz vector stream_count_past_body). The
// oracle, internal/tablewire Decode, reads it as damage, and DAMAGE DISCARDS
// THE WHOLE LOAD (§4): the JavaScript Load of streamdemo.Feed must hand back no
// value with the report saying malformed. The counters after damage are not
// defined, so they are not compared.
func TestStreamCountPastBodyClampsLikeGoJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	u := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "stream")})
	m := tabletext.NewModel(u)

	wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "fuzz-vectors", "stream_count_past_body.bin"))
	if err != nil {
		t.Fatal(err)
	}

	inst := m.New(m.Lookup("Feed"))
	var rep tabletext.Report
	if _, decErr := tablewire.Decode(m, inst, wire, &rep); decErr != nil {
		t.Fatal(decErr)
	}
	if !rep.Malformed {
		t.Fatalf("the oracle does not read the vector as damage: %+v", rep)
	}
	const want = "loaded=false|malformed=true"

	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u)
	module := filepath.Join(work, home+"Variable.js")
	if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
		t.Fatal(err)
	}
	data, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no variable module %sVariable.js was generated", home)
	}
	if err := os.WriteFile(module, data, 0o600); err != nil {
		t.Fatal(err)
	}
	wirePath := filepath.Join(work, "stream_count_past_body.wire")
	if err := os.WriteFile(wirePath, wire, 0o600); err != nil {
		t.Fatal(err)
	}

	src := fmt.Sprintf(`
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(%q).href);
const wire = new Uint8Array(readFileSync(%q));
const rep = new m.TableVariableReport();
const v = m.FeedLoad(wire, rep);
console.log("JS loaded=" + (v !== null) + "|malformed=" + rep.malformed);
`, module, wirePath)
	script := filepath.Join(work, "probe.mjs")
	if err := os.WriteFile(script, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	jsLine := ""
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "JS "); ok {
			jsLine = after
		}
	}
	if jsLine == "" {
		t.Fatalf("the probe printed no verdict:\n%s", out)
	}
	if jsLine != want {
		t.Errorf("JavaScript answered %q on damage, want %q", jsLine, want)
	}
}
