package ci

import (
	"os"
	"path/filepath"
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
	carries("make/lua.mk (test-lua)", lua,
		"packet-utf8-lua", "packet-utf8-lua-negative-control",
		"packet-wide-lua", "packet-wide-lua-negative-control",
		"packet-defaults-lua", "packet-defaults-lua-negative-control",
		"packet-finite-lua")
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
