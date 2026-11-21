package jstable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// Both the wire registry and report reader must reach the variable envelope,
// while the original fixed ToolMessage keeps its previous-form refusal.
func TestJavaScriptDriverToolEnvelope(t *testing.T) {
	slowtest.Gate(t, "node and the shipped JavaScript generator")
	node := jsNode(t)
	repo := jsStrictRepo(t)
	tree := t.TempDir()
	writeConformanceTree(t, tree, [][2]string{{"block", "tables/block"}, {"pointers", "tables/pointers"}, {"messages", "tables/messages"}})
	run := func(surface, rows string) string {
		t.Helper()
		manifest := filepath.Join(t.TempDir(), "MANIFEST.txt")
		if err := os.WriteFile(manifest, []byte(rows), 0600); err != nil {
			t.Fatal(err)
		}
		out := t.TempDir()
		cmd := exec.Command(node, filepath.Join(repo, "test/conformance/js/main.mjs"), manifest, surface, out)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+tree)
		if log, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("driver %s: %v\n%s", surface, err, log)
		}
		return out
	}
	// The report surface has a text-codec fallback, so test wire dispatch too.
	out := run("wire", "instance exact messagedemo ToolEnvelope testdata/wire/tables/nested_arm_exact.bin\n")
	got, err := os.ReadFile(filepath.Join(out, "exact"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/nested_arm_exact.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("exact-envelope canonical wire differs")
	}
	var rows strings.Builder
	for _, name := range []string{"nested_arm_exact", "nested_arm_trailing", "nested_arm_trailing_top"} {
		fmt.Fprintf(&rows, "report %s messagedemo ToolEnvelope testdata/wire/tables/%s.bin\n", name, name)
	}
	rows.WriteString("report fixed_previous_form messagedemo ToolMessage testdata/wire/tables/nested_arm_exact.bin\n")
	out = run("report", rows.String())
	for _, name := range []string{"nested_arm_exact", "nested_arm_trailing", "nested_arm_trailing_top", "fixed_previous_form"} {
		t.Run(name, func(t *testing.T) {
			answer, err := os.ReadFile(filepath.Join(out, name))
			if err != nil {
				t.Fatal(err)
			}
			if name != "nested_arm_exact" {
				if string(answer) != "refused\n" {
					t.Fatalf("expected refusal without value: %q", answer)
				}
				if _, err := os.Stat(filepath.Join(out, name+".wire")); !os.IsNotExist(err) {
					t.Fatalf("refused read wrote wire: %v", err)
				}
				return
			}
			verdict, text, ok := strings.Cut(string(answer), "\n")
			var value struct {
				Sequence uint32
				Body     struct{ Origin struct{ Pid uint32 } }
			}
			if !ok || verdict != "read" || json.Unmarshal([]byte(text), &value) != nil || value.Sequence != 41 || value.Body.Origin.Pid != 5 {
				t.Fatalf("wrong exact-frame value: %q", answer)
			}
			saved, err := os.ReadFile(filepath.Join(out, name+".wire"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(saved, want) {
				t.Fatal("report exact-envelope canonical save differs")
			}
		})
	}
}
