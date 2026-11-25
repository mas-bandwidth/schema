package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// THE GUARDS OF THE TABLES CARD, each refusing by name (authored-text rows,
// framed rewrite rows, the BOM entry gate's rows and wrapper).

// An authored-text line that is malformed, named twice, or also an instance is
// refused when the manifest is read (manifest.go).
func TestAuthoredTextLinesAreRefusedByName(t *testing.T) {
	corpus(t)
	base, err := os.ReadFile(defaultManifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ line, says string }{
		{"authored-text edge_map_keys_unsorted mapdemo Fleet", "authored-text takes case, unit, root, wire"},
		{"authored-text edge_map_keys_unsorted mapdemo Fleet testdata/wire/tables/edge_map_keys.bin", "named twice"},
		{"authored-text root_full tabledemo Tables testdata/wire/tables/root_full.bin", "is also an instance"},
	} {
		path := filepath.Join(t.TempDir(), "MANIFEST.txt")
		if err := os.WriteFile(path, append(bytes.Clone(base), []byte(c.line+"\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadManifest(path, defaultJSONDir); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%q: got %v, want the refusal saying %q", c.line, err, c.says)
		}
	}
}

// A text in canonical order that differs from the canonical text only in its
// layout is refused as a trap that tests nothing (jsontext.go, sameTokens).
func TestAuthoredTextInCanonicalOrderIsRefusedWhateverItsLayout(t *testing.T) {
	m, _, u := corpus(t)
	canonical, err := os.ReadFile("testdata/conformance/tables/rewrite/edge_map_keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, canonical); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "compact.json")
	if err := os.WriteFile(path, compact.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	jt := m.JSONTexts[0]
	jt.Text = path
	if err := checkJSONText(u, jt); err == nil || !strings.Contains(err.Error(), "already the canonical text") {
		t.Errorf("the canonical text compacted: got %v, want the refusal naming it canonical", err)
	}
}

// A leg not asked a rewrite row's unit is not handed it, and a framed row the
// leg's framing surface did not write back is left unanswered rather than
// filled (rewrite.go).
func TestRewriteRowsALegIsNotAskedStayOff(t *testing.T) {
	m, _, u := corpus(t)
	framed, err := framedRewrites(u, m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest-rewrite.txt")
	if err := deriveRewriteManifest(m, path, map[string]bool{"mapdemo": true}, framed); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "instance edge_map_keys ") {
		t.Error("a leg not asked unit mapdemo is handed the rewrite row edge_map_keys")
	}
	if !strings.Contains(string(text), "instance edge_graph_diamond ") {
		t.Error("the rewrite rows on units the leg is asked are missing")
	}
	framingOut, rewriteOut := t.TempDir(), t.TempDir()
	if err := answerFramedRewrites(framed, framingOut, rewriteOut); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(rewriteOut); len(entries) != 0 {
		t.Errorf("with no framing write-back, %d rewrite answers were made up", len(entries))
	}
}

// Registry rows for a manifest other than the corpus's need a family of their
// own (adapter.go, rowsFamilyNamed).
func TestRegistryRowsOfAnotherManifestNameTheirFamily(t *testing.T) {
	if err := rowsFamilyNamed(bomManifest, ""); err == nil {
		t.Error("the BOM manifest's rows with no --family took the corpus's families")
	}
	if err := rowsFamilyNamed(bomManifest, bomFamily); err != nil {
		t.Error(err)
	}
	if err := rowsFamilyNamed(defaultManifest, ""); err != nil {
		t.Error(err)
	}
}

// THE BOM WRAPPER'S DRIVER MODE (test/conformance/bom-entry), over a fake leg:
// it lists json-hostile alone, a plant replaces exactly the named answer, and
// a malformed plant, a plant naming a case the leg did not answer, and a name
// that is not a language are each refused.
func TestBOMWrapperDriverModeRefusesByName(t *testing.T) {
	root := conformanceRoot(t)
	script, err := os.ReadFile(filepath.Join(root, "test/conformance/bom-entry"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "test/conformance/fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test/conformance/bom-entry"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\nif [ \"$2\" = list ]; then printf 'wire\\njson-hostile\\n'; exit 0; fi\nprintf 'read\\n' > \"$3/leading-bom\"\nprintf 'refused\\n' > \"$3/cut-bom\"\n"
	if err := os.WriteFile(filepath.Join(dir, "test/conformance/fake/driver"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(plant string, args ...string) (string, error) {
		cmd := exec.Command("sh", append([]string{"test/conformance/bom-entry"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SCHEMA_TEST_PLANT_BOM_ENTRY="+plant)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run("", "--driver", "fake", "m", "list"); err != nil || out != "json-hostile\n" {
		t.Errorf("list: %q, %v; want json-hostile alone", out, err)
	}
	answers := t.TempDir()
	if out, err := run("wrong:cut-bom", "--driver", "fake", "m", "json-hostile", answers); err != nil {
		t.Fatalf("a good plant: %v\n%s", err, out)
	}
	for name, want := range map[string]string{"cut-bom": "planted\n", "leading-bom": "read\n"} {
		if got, _ := os.ReadFile(filepath.Join(answers, name)); string(got) != want {
			t.Errorf("after the plant, %s answers %q, want %q", name, got, want)
		}
	}
	for _, c := range []struct {
		plant string
		args  []string
		says  string
	}{
		{"cut-bom", []string{"--driver", "fake", "m", "json-hostile", t.TempDir()}, "is not wrong:<case>"},
		{"wrong:space-bom", []string{"--driver", "fake", "m", "json-hostile", t.TempDir()}, "did not answer"},
		{"", []string{"--driver", "../fake", "m", "list"}, "is not a language name"},
		{"", []string{"--driver", "absent", "m", "list"}, "is not a driver"},
	} {
		out, err := run(c.plant, c.args...)
		if err == nil || !strings.Contains(out, c.says) {
			t.Errorf("%v (plant %q): err %v, output %q; want a refusal saying %q", c.args, c.plant, err, out, c.says)
		}
	}
}
