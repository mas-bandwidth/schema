package gotable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestGoGenerateSurfaceEmitsOnlyWhatItNames holds the --surface flag
// (docs/SPEC-TABLES.md §8.4) to the contract: a generate emits only the files
// and functions the requested surfaces (and their dependencies) call for, the
// default is byte-identical to the full set, and every emitted subset compiles
// and round-trips.
func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()

	schema := `package probe
fixed table P { x uint32 }
table V { name string(8) }
`
	u := unitFrom(t, schema)

	full, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	fullSrc := concatSources(full)

	// The generated file names for this unit: base "Probe", package "probe".
	const (
		tableGo = "ProbeTable.go"
		blockGo = "ProbeBlock.go"
		cookGo  = "ProbeCook.go"
		jsonGo  = "ProbeTableJson.go"
		viewGo  = "ProbeView.go"
	)

	// Function markers that live in <Base>Table.go. The wire markers are the
	// table codecs; fixed/message/retain each add their own.
	wireFns := []string{"PMeasure", "PLoad", "VMeasure", "VLoad", "PTableType", "VTableType"}
	fixedFn := "THE FIXED FORM"
	messageFns := []string{"VSaveMessageBody", "VLoadMessageBody"}
	retainFn := "retain"

	cases := []struct {
		name         string
		list         string
		wantFiles    []string
		missFiles    []string
		wantFns      []string
		notFns       []string
	}{
		{"all", "wire,fixed,json,cook,block,view,message,retain",
			[]string{tableGo, blockGo, cookGo, jsonGo, viewGo}, nil,
			append(append(append(append([]string{}, wireFns...), fixedFn), messageFns...), retainFn), nil},
		{"defaultIsAll", "", nil, nil, nil, nil},
		{"wire", "wire", []string{tableGo}, []string{blockGo, cookGo, jsonGo, viewGo},
			wireFns, []string{fixedFn, "SaveMessageBody", retainFn}},
		{"fixedPullsWire", "fixed", []string{tableGo}, []string{blockGo, cookGo, jsonGo, viewGo},
			append(append([]string{}, wireFns...), fixedFn), []string{"SaveMessageBody", retainFn}},
		{"messagePullsWire", "message", []string{tableGo}, []string{blockGo, cookGo, jsonGo, viewGo},
			append(append([]string{}, wireFns...), messageFns...), []string{fixedFn, retainFn}},
		{"blockPullsWire", "block", []string{tableGo, blockGo}, []string{cookGo, jsonGo, viewGo},
			wireFns, []string{fixedFn, "SaveMessageBody", retainFn}},
		{"cookPullsBlockWire", "cook", []string{tableGo, blockGo, cookGo}, []string{jsonGo, viewGo},
			wireFns, []string{fixedFn, "SaveMessageBody", retainFn}},
		{"jsonPullsWire", "json", []string{tableGo, jsonGo}, []string{blockGo, cookGo, viewGo},
			wireFns, []string{fixedFn, "SaveMessageBody", retainFn}},
		{"viewPullsWire", "view", []string{tableGo, viewGo}, []string{blockGo, cookGo, jsonGo},
			wireFns, []string{fixedFn, "SaveMessageBody", retainFn}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files, err := GenerateLineageSurfaces(u, nil, tc.list)
			if err != nil {
				t.Fatal(err)
			}
			src := concatSources(files)
			for _, fn := range tc.wantFns {
				if !strings.Contains(src, fn) {
					t.Errorf("surface %q: expected %q in generated output", tc.list, fn)
				}
			}
			for _, fn := range tc.notFns {
				if strings.Contains(src, fn) {
					t.Errorf("surface %q: %q should not be emitted", tc.list, fn)
				}
			}
			for _, f := range tc.wantFiles {
				if _, ok := files[f]; !ok {
					t.Errorf("surface %q: missing file %s", tc.list, f)
				}
			}
			for _, f := range tc.missFiles {
				if _, ok := files[f]; ok {
					t.Errorf("surface %q: should not emit file %s", tc.list, f)
				}
			}
			if tc.name != "defaultIsAll" {
				runSurfaceCompile(t, u, files)
			}
		})
	}

	// The default (empty list) is byte-identical to the full set.
	byDefault, err := GenerateLineageSurfaces(u, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if concatSources(byDefault) != fullSrc {
		t.Error("a generate with no --surface is not byte-identical to the full set")
	}
}

// concatSources sorts and concatenates generated files for stable comparison.
func concatSources(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString("=== ")
		b.WriteString(n)
		b.WriteString(" ===\n")
		b.Write(files[n])
		b.WriteString("\n")
	}
	return b.String()
}

// runSurfaceCompile compiles the given gotable surface output together with
// the packet emitter's output, in a t.TempDir().
func runSurfaceCompile(t *testing.T, u *ir.Unit, tables map[string][]byte) {
	t.Helper()
	pkt, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	maps.Copy(files, pkt)
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated surface subset does not compile: %v\n%s", err, out)
	}
}
