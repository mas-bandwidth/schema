package cpptable

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cppDiscardAnchor is one current spelling a discard control's sed must find:
// the text as it is escaped for sed in the recipe, and the text as it stands
// in the emitter source the control copies.
type cppDiscardAnchor struct {
	recipe string
	source string
}

// cppDiscardControl names one C++ WHOLE-LOAD DISCARD control: the make target,
// the manifest group that runs it, the emitter source its overlay patches, the
// current anchors it must match, the current gate message it must require, and
// the retired names it may not carry.
type cppDiscardControl struct {
	target  string
	group   string
	source  string
	anchors []cppDiscardAnchor
	message string
	retired []string
}

var cppDiscardControls = []cppDiscardControl{
	{
		target: "tables-cpp-discard-negative-control",
		group:  "base-cpp-discard",
		source: "internal/codegen/cpptable/codecs.go",
		anchors: []cppDiscardAnchor{{
			recipe: "if ( to->damaged )\\\\n    {\\\\n        %\\[1\\]sReset( value );\\\\n        return TableOpenDamaged;",
			source: "if ( to->damaged )\\n    {\\n        %[1]sReset( value );\\n        return TableOpenDamaged;",
		}},
		message: "fixed: the report does not say malformed",
		retired: []string{"to->malformed", "LoadTolerant"},
	},
	{
		target: "tables-cpp-region-discard-negative-control",
		group:  "base-cpp-discard",
		source: "internal/codegen/cpptable/pointers.go",
		anchors: []cppDiscardAnchor{{
			recipe: "if ( out->damaged )",
			source: "if ( out->damaged )",
		}},
		message: "variable: the report does not say malformed",
		retired: []string{"out->malformed", "LoadTolerant"},
	},
	{
		target: "tables-cpp-builder-discard-negative-control",
		group:  "base-cpp-discard",
		source: "internal/codegen/cpptable/pointers.go",
		anchors: []cppDiscardAnchor{
			{
				recipe: "if ( out->damaged || !ok ) { %sReset( \\*root ); }",
				source: "if ( out->damaged || !ok ) { %sReset( *root ); }",
			},
			{
				recipe: "if ( refused_root != NULL ) { %sReset( \\*refused_root ); }",
				source: "if ( refused_root != NULL ) { %sReset( *refused_root ); }",
			},
		},
		message: "builder: a damaged load left a usable root",
		retired: []string{"out->malformed", "LoadTolerant"},
	},
}

// cppDiscardArtifacts is the real material the gate reads: the root Makefile
// that carries the control recipes, the manifest that runs them, the emitter
// sources their overlays copy, and the gate that prints the verdicts they
// require.
type cppDiscardArtifacts struct {
	makefile string
	manifest string
	sources  map[string]string
	gate     string
}

func loadCppDiscardArtifacts(t *testing.T) cppDiscardArtifacts {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(data)
	}
	a := cppDiscardArtifacts{
		makefile: read("Makefile"),
		manifest: read("make/negative-controls.json"),
		sources:  map[string]string{},
		gate:     read("internal/codegen/cpptable/discard_test.go"),
	}
	for _, c := range cppDiscardControls {
		if _, ok := a.sources[c.source]; !ok {
			a.sources[c.source] = read(c.source)
		}
	}
	return a
}

// cppMakeRule returns the recipe lines under target, or "" when no rule head
// names it. Every recipe line in this Makefile starts with a tab.
func cppMakeRule(body, target string) string {
	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, target+":") {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	var out strings.Builder
	for _, line := range lines[start+1:] {
		if !strings.HasPrefix(line, "\t") {
			break
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

func (a cppDiscardArtifacts) findings() []string {
	var out []string

	groups := map[string]map[string]bool{}
	var manifest struct {
		Groups []struct {
			Name    string   `json:"name"`
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(a.manifest), &manifest); err != nil {
		out = append(out, "make/negative-controls.json does not parse: "+err.Error())
	} else {
		for _, g := range manifest.Groups {
			groups[g.Name] = map[string]bool{}
			for _, target := range g.Targets {
				groups[g.Name][target] = true
			}
		}
	}

	for _, c := range cppDiscardControls {
		rule := cppMakeRule(a.makefile, c.target)
		if rule == "" {
			out = append(out, c.target+": no rule in the Makefile")
			continue
		}
		for _, anchor := range c.anchors {
			if !strings.Contains(rule, anchor.recipe) {
				out = append(out, fmt.Sprintf("%s: the recipe does not anchor on the current text %q", c.target, anchor.recipe))
			}
			if source := a.sources[c.source]; !strings.Contains(source, anchor.source) {
				out = append(out, fmt.Sprintf("%s: %s no longer carries %q", c.target, c.source, anchor.source))
			}
		}
		if !strings.Contains(rule, c.message) {
			out = append(out, fmt.Sprintf("%s: the recipe does not require the gate's current verdict %q", c.target, c.message))
		}
		if !strings.Contains(a.gate, c.message) {
			out = append(out, fmt.Sprintf("%s: discard_test.go no longer prints %q", c.target, c.message))
		}
		for _, retired := range c.retired {
			if strings.Contains(rule, retired) {
				out = append(out, fmt.Sprintf("%s: the recipe still uses the retired name %q", c.target, retired))
			}
		}
		if groups[c.group] == nil {
			out = append(out, fmt.Sprintf("%s: make/negative-controls.json has no %q group", c.target, c.group))
		} else if !groups[c.group][c.target] {
			out = append(out, fmt.Sprintf("%s: make/negative-controls.json does not register it under %q", c.target, c.group))
		}
	}
	return out
}

func cppDiscardHas(got []string, substr string) bool {
	for _, f := range got {
		if strings.Contains(f, substr) {
			return true
		}
	}
	return false
}

// TestCppDiscardControlsCurrentNames holds the C++ half of the STRICT-100 cell
// "C++ discard negative controls" (docs/SPEC-TABLES.md §4): the strict cut
// retired LoadTolerant and the malformed verdict, so the controls that prove
// damage discards the whole load must anchor on the plain Load and the damaged
// flag. It reads the real control recipes, the real emitter sources, the real
// gate messages and the real manifest, without compiling C++ or running make.
func TestCppDiscardControlsCurrentNames(t *testing.T) {
	t.Parallel()
	a := loadCppDiscardArtifacts(t)
	for _, finding := range a.findings() {
		t.Error(finding)
	}
}

// TestCppDiscardControlsCurrentNamesNegativeControl is the gate's reversed
// witness: it plants a retired spelling in the real artifacts the gate reads
// — the actual Makefile bytes, the actual emitter source, the actual gate
// source and the actual manifest — and requires the gate's own check to flag
// each plant. A check that cannot see a plant in the real artifact proves
// nothing about the unplanted one.
func TestCppDiscardControlsCurrentNamesNegativeControl(t *testing.T) {
	t.Parallel()
	a := loadCppDiscardArtifacts(t)
	if got := a.findings(); len(got) != 0 {
		t.Fatalf("the unplanted real artifacts already fail the gate: %v", got)
	}

	// THE MAKEFILE: put the retired verdict back in the value read's anchor.
	planted := a
	discardAnchor := cppDiscardControls[0].anchors[0]
	planted.makefile = strings.Replace(a.makefile,
		discardAnchor.recipe,
		strings.Replace(discardAnchor.recipe, "to->damaged", "to->malformed", 1), 1)
	if planted.makefile == a.makefile {
		t.Fatal("the Makefile plant patched nothing")
	}
	if got := planted.findings(); !cppDiscardHas(got, "tables-cpp-discard-negative-control") || !cppDiscardHas(got, "retired name") {
		t.Fatalf("the gate did not flag the retired verdict planted in the real Makefile: %v", got)
	}

	// THE EMITTER: move the value read's discard back to the retired spelling,
	// so the control's current anchor no longer names the real source.
	planted = a
	planted.sources = map[string]string{}
	maps.Copy(planted.sources, a.sources)
	planted.sources["internal/codegen/cpptable/codecs.go"] = strings.Replace(a.sources["internal/codegen/cpptable/codecs.go"],
		discardAnchor.source,
		strings.Replace(discardAnchor.source, "to->damaged", "to->malformed", 1), 1)
	if got := planted.findings(); !cppDiscardHas(got, "codecs.go no longer carries") {
		t.Fatalf("the gate did not flag the retired verdict planted in the real emitter: %v", got)
	}

	// THE GATE: remove the current builder verdict the control requires.
	planted = a
	planted.gate = strings.Replace(a.gate,
		"builder: a damaged load left a usable root",
		"builder: LoadTolerant answered true", 1)
	if planted.gate == a.gate {
		t.Fatal("the gate plant patched nothing")
	}
	if got := planted.findings(); !cppDiscardHas(got, "discard_test.go no longer prints") {
		t.Fatalf("the gate did not flag the retired builder verdict planted in the real gate: %v", got)
	}

	// THE MANIFEST: unregister the builder control under its current group.
	planted = a
	planted.manifest = strings.Replace(a.manifest,
		`"tables-cpp-builder-discard-negative-control"`,
		`"tables-cpp-load-tolerant-builder-discard-negative-control"`, 1)
	if planted.manifest == a.manifest {
		t.Fatal("the manifest plant patched nothing")
	}
	if got := planted.findings(); !cppDiscardHas(got, "does not register it under") {
		t.Fatalf("the gate did not flag the control unregistered in the real manifest: %v", got)
	}
}
