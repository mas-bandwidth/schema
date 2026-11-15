// TestPortLuauPageCurrent pins docs/PORT-LUAU.md to the tree and to the
// strict-only read: the opening names the codec the tree carries, THE RUNTIME
// LAYOUT lists the table runtime files under their schema_*.luau names, and
// THE CARD MAP and the bench name the strict read. The later-work runtime
// names and the tolerant load are stale phrases the page must not state.
package compiler

import (
	"os"
	"testing"
)

func TestPortLuauPageCurrent(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/PORT-LUAU.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)

	rows := []struct {
		what   string
		wants  []string
		stales []string
	}{
		{
			what: "the opening describes the codec in the tree",
			wants: []string{
				"The codec is in the tree:",
				"the type surface reads and writes packets",
				"the table runtime carries the strict load",
			},
			stales: []string{
				"The skeleton has no codec",
				"every wire, form and text surface is a later",
			},
		},
		{
			what: "THE RUNTIME LAYOUT lists the table runtime files that exist",
			wants: []string{
				"`schema_tables.luau`",
				"`schema_load.luau`",
				"`schema_save.luau`",
				"`schema_fixed.luau`",
				"`schema_message.luau`",
				"`schema_json.luau`",
				"`schema_textform.luau`",
				"`schema_text.luau`",
				"`schema_refusal.luau`",
				"`schema_retain.luau`",
				"`schema_view.luau`",
				"`schema_block.luau`",
				"`schema_blockread.luau`",
				"`schema_cook.luau`",
				"`schema_cookread.luau`",
			},
			stales: []string{
				"`load.luau`",
				"`save.luau`",
				"`fixed.luau`",
				"`message.luau`",
				"`json.luau`",
				"`text.luau`",
				"`refusal.luau`",
			},
		},
		{
			what: "THE CARD MAP names the strict load",
			wants: []string{
				"the variable table wire's strict load",
				"`luautable/runtime/schema_load.luau`",
			},
			stales: []string{
				"tolerant load",
				"`luautable/runtime/load.luau`",
			},
		},
		{
			what: "the bench describes the table wire without a tolerant read",
			wants: []string{
				"the table wire, TableMixed, family `table`",
			},
			stales: []string{
				"the tolerant table wire",
			},
		},
	}
	for _, r := range rows {
		for _, want := range r.wants {
			mustState(t, r.what, page, want)
		}
		for _, stale := range r.stales {
			mustNotState(t, r.what, page, stale)
		}
	}
}
