package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaLuauHostileCorpusAndWireFuzzWiring holds the Luau port's hostile
// corpus and wire-fuzz blades without the interpreter in the Go unit tier, as
// TestSchemaElixirRunsHostileCorpusWiring holds the Elixir port's: each gate
// is a make target the port's unit target runs, each control is registered on
// the pull-request tier of make/negative-controls.json, the wire fuzz fails on
// any root its leg cannot open, with no pinned list to hold one back (the
// no-skips ruling, docs/rulings.json). The corpus runs as the registry's
// per-case hostile rows (tools/schema-test/hostile.go).
func TestSchemaLuauHostileCorpusAndWireFuzzWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s is missing: %v", rel, err)
		}
		return string(b)
	}

	carries := func(rel, text string, wants ...string) {
		t.Helper()
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not carry %q", rel, want)
			}
		}
	}
	carries("make/schema-luau-runs-hostile-corpus.mk", read("make/schema-luau-runs-hostile-corpus.mk"),
		"test-luau: schema-luau-runs-hostile-corpus",
		"schema-luau-runs-hostile-corpus-negative-control:",
		"TestLuauRunsHostileCorpusPlantedPattern")
	carries("make/luau-fuzz.mk", read("make/luau-fuzz.mk"),
		"test-luau: tables-luau-wire-fuzz tables-luau-wire-fuzz-negative-control",
		"LUAUFUZZ_NOTE=",
		`if [ "$$n" -ne 0 ]; then`,
		"0 divergences",
		"sh test/conformance/luau/wire-fuzz-negative-control")
	carries("test/conformance/luau/wirefuzz.luau", read("test/conformance/luau/wirefuzz.luau"), "host.note(")

	// no pinned list of absent roots: it held roots back, and it is retired
	for _, retired := range []string{"wire-fuzz-absent", "LUAU_WIRE_FUZZ_ABSENT_COUNT", "LUAU_WIRE_FUZZ_ABSENT_PIN"} {
		if strings.Contains(read("make/luau-fuzz.mk"), retired) {
			t.Errorf("make/luau-fuzz.mk carries %q: a pinned list of absent roots is retired", retired)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "test", "conformance", "luau", "wire-fuzz-absent.txt")); err == nil {
		t.Error("test/conformance/luau/wire-fuzz-absent.txt is back: a pinned list of absent roots is retired")
	}

	controls := read("make/negative-controls.json")
	for _, want := range []struct {
		group   string
		targets []string
	}{
		{"luau-conformance", []string{"schema-luau-runs-hostile-corpus-negative-control"}},
		{"luau-fuzz", []string{"tables-luau-wire-fuzz-negative-control", "luau-nightly-fuzz-negative-control"}},
	} {
		// the group, or the slices it was split into (want.group-a, -b, ...):
		// each target sits in one of them, and that one runs per pull request
		var groups []string
		for rest := controls; ; {
			at := strings.Index(rest, `"name": "`+want.group)
			if at < 0 {
				break
			}
			group, tail, _ := strings.Cut(rest[at:], "}")
			name, _, _ := strings.Cut(strings.TrimPrefix(group, `"name": "`+want.group), `"`)
			if name == "" || (len(name) == 2 && name[0] == '-') {
				groups = append(groups, group)
			}
			rest = tail
		}
		if len(groups) == 0 {
			t.Fatalf("make/negative-controls.json has no %s group", want.group)
		}
		for _, target := range want.targets {
			found := false
			for _, group := range groups {
				if strings.Contains(group, `"`+target+`"`) {
					found = true
					carries("make/negative-controls.json (the "+want.group+" group holding "+target+")", group, `"when": "pull-request"`)
				}
			}
			if !found {
				t.Errorf("make/negative-controls.json's %s group does not carry %q", want.group, target)
			}
		}
	}
}
