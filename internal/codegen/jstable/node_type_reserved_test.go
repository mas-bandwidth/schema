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

// TestNodeRecordReservedTypeRefusedJavaScript holds the JavaScript leg's
// variable Load, plain and retaining, to the engine on a node record whose TYPE
// reference resolves to one of the three ids the language holds back
// (docs/SPEC-TABLES.md §3.1, §3.3, §5): the record is malformed and the whole
// load refuses. The wires are the held framing vectors the engine's own gate
// reads. graph_shared is the control: the same root and the same probe read a
// lawful node table, so a refusal below is the rule and not the probe.
func TestNodeRecordReservedTypeRefusedJavaScript(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/pointers")
	m := tabletext.NewModel(u)
	def := m.Lookup("Scene")
	if def == nil {
		t.Fatal("no table Scene in tables/pointers")
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
	if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(module, data, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		wire     string
		accepted bool
	}{
		{"graph_shared", "testdata/wire/tables/graph_shared.bin", true},
		{"node_type_node_table_id", "testdata/wire/tables/framing/node_type_node_table_id.bin", false},
		{"node_type_build_version_id", "testdata/wire/tables/framing/node_type_build_version_id.bin", false},
		{"node_type_vocabulary_id", "testdata/wire/tables/framing/node_type_vocabulary_id.bin", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := os.ReadFile(filepath.Join(repo, tc.wire))
			if err != nil {
				t.Fatal(err)
			}
			var rep tabletext.Report
			ok, decErr := tablewire.Decode(m, m.New(def), wire, &rep)
			if decErr != nil && !tablewire.ReadRefusal(decErr) {
				t.Fatalf("engine Decode returned an error: %v", decErr)
			}
			if ok != tc.accepted {
				t.Fatalf("the engine's verdict moved: accepted=%t, want %t (%v)", ok, tc.accepted, decErr)
			}
			want := fmt.Sprintf("loaded=%t|malformed=%t", tc.accepted, !tc.accepted)
			src := fmt.Sprintf(`
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(%q).href);
const wire = new Uint8Array(readFileSync(%q));
const rep = new m.TableVariableReport();
const v = m.SceneLoad(wire, rep);
console.log("JS plain loaded=" + (v !== null) + "|malformed=" + rep.damaged);
const rrep = new m.TableVariableReport();
const store = new m.TableVariableRetain(new Uint8Array(1 << 20), new Array((1 << 20) / 8));
const rv = m.SceneLoadRetain(wire, store, rrep);
console.log("JS retain loaded=" + (rv !== null) + "|malformed=" + rrep.damaged);
`, module, filepath.Join(repo, tc.wire))
			script := filepath.Join(work, "probe_"+tc.name+".mjs")
			if err := os.WriteFile(script, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(node, script)
			cmd.Dir = work
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("node probe failed: %v\n%s", err, out)
			}
			got := map[string]string{}
			for line := range strings.SplitSeq(string(out), "\n") {
				if after, ok := strings.CutPrefix(line, "JS "); ok {
					arm, verdict, _ := strings.Cut(after, " ")
					got[arm] = verdict
				}
			}
			for _, arm := range []string{"plain", "retain"} {
				if got[arm] != want {
					t.Errorf("%s load: JS=%q, want %q\n%s", arm, got[arm], want, out)
				}
			}
		})
	}
}
