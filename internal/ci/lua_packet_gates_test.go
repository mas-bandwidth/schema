package ci

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestLuaPacketGatesWiredWithControls holds the Lua port's three packet gates
// (UTF-8, wide text, defaults) as make targets the leg's test-lua runs, each
// with a negative control registered on the pull-request tier of
// make/negative-controls.json under lua, and confirms the driver scripts and
// the sabotage names that back them exist. It needs no interpreter: it is the
// wiring half of the card, beside the functional make targets themselves.
func TestLuaPacketGatesWiredWithControls(t *testing.T) {
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

	lua := read("make/lua.mk")
	// The gates are wired only when the six names are prerequisites of
	// test-lua itself: they also occur in the .PHONY lines and in the target
	// definitions, so a whole-file search cannot tell "test-lua runs these
	// six targets" from "these six targets exist somewhere in the file".
	prereqs := targetPrereqs(lua, "test-lua")
	if len(prereqs) == 0 {
		t.Fatal("make/lua.mk has no test-lua target")
	}
	for _, want := range []string{
		"packet-utf8-lua", "packet-utf8-lua-negative-control",
		"packet-wide-lua", "packet-wide-lua-negative-control",
		"packet-defaults-lua", "packet-defaults-lua-negative-control",
		"packet-finite-lua",
	} {
		if !slices.Contains(prereqs, want) {
			t.Errorf("make/lua.mk (the test-lua prerequisites) does not carry %q", want)
		}
	}
	for _, gate := range []string{
		"packet-utf8-lua packet-utf8-lua-negative-control",
		"packet-wide-lua packet-wide-lua-negative-control",
		"packet-defaults-lua packet-defaults-lua-negative-control",
	} {
		carries("make/lua.mk", lua, ".PHONY: "+gate)
	}

	for _, driver := range []string{
		"test/packet-text/lua/main.lua",
		"test/packet-wide/lua/main.lua",
		"test/packet-defaults/lua/main.lua",
	} {
		txt := read(driver)
		if txt == "" {
			t.Errorf("%s is empty", driver)
		}
	}

	sabotage := read("tools/sabotage/packet_lua.go")
	carries("tools/sabotage/packet_lua.go", sabotage,
		`sabotages["packet-utf8-lua-read"]`,
		`sabotages["packet-wide-lua-pairing"]`,
		`sabotages["packet-defaults-lua-constructor-bytes"]`)

	controls := read("make/negative-controls.json")
	at := strings.Index(controls, `"name": "lua"`)
	if at < 0 {
		t.Fatal("make/negative-controls.json has no lua group")
	}
	group, _, _ := strings.Cut(controls[at:], "}")
	carries("make/negative-controls.json (the lua group)", group,
		`"when": "pull-request"`,
		`"packet-utf8-lua-negative-control"`,
		`"packet-wide-lua-negative-control"`,
		`"packet-defaults-lua-negative-control"`)
}

// targetPrereqs returns the prerequisite names a make target is given in a
// make file's text. Make allows a target to carry more than one rule, so
// every "<target>:" line counts, and a rule may continue over a backslash.
// Recipe lines are not rules and are skipped.
func targetPrereqs(makeText, target string) []string {
	var names []string
	lines := strings.Split(makeText, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "\t") {
			continue
		}
		rule := strings.TrimSpace(lines[i])
		for strings.HasSuffix(rule, "\\") && i+1 < len(lines) {
			rule = strings.TrimSuffix(rule, "\\") + " " + strings.TrimSpace(lines[i+1])
			i++
		}
		rest, ok := strings.CutPrefix(rule, target+":")
		if !ok {
			continue
		}
		names = append(names, strings.Fields(rest)...)
	}
	return names
}
