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

// TestStreamCountPastBodyClampsLikeGoJavaScript pins the JavaScript leg's carry
// of the clamped counter on a stream count whose own spelling runs past the
// body it is framed by (docs/SPEC-TABLES.md §3, §4; the fuzz vector
// stream_count_past_body). The oracle, internal/tablewire Decode — which the Go
// leg's generated Load matches — counts ONE clamped for an element count that
// runs past the body, and the JavaScript Load must count the same clamp. The
// test generates tables/stream into a temp dir, loads stream_count_past_body.bin
// as streamdemo.Feed in JS, and asserts loaded, malformed and the five counters
// equal internal/tablewire Decode's report on the same bytes.
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
	oracle := fmt.Sprintf("%d|%d|%d|%d|%d|%v",
		rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate, rep.Malformed)

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
if (v === null) { console.log("JS refused"); }
else { console.log("JS " + [rep.unknown, rep.kindMismatch, rep.widened, rep.clamped, rep.duplicate, rep.malformed].join("|")); }
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
		t.Fatalf("the probe refused or printed no verdict:\n%s", out)
	}
	// the leg loads the wire (returns a value) exactly as the oracle produces a
	// decoded value, and the counters agree
	if jsLine != oracle {
		t.Errorf("JavaScript report %q does not match the oracle %q", jsLine, oracle)
	}
}
