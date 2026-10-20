package luautable_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// TestLuauUndeclaredUnitKeepsTableAndRuntime holds docs/SPEC-TABLES.md §2.12
// on the undeclared table: a table with no `forms` line generates exactly the
// per-table module it generated before the declaration existed, and the
// runtime modules it emits are byte-identical to their sources under
// internal/codegen/luautable/runtime/.
func TestLuauUndeclaredUnitKeepsTableAndRuntime(t *testing.T) {
	t.Parallel()
	defaultUnit := generateFiles(t, map[string]string{"Probe.schema": "package probe\ntable Pong { x int32 }\n"})

	// (1) The per-table module(s) — the <Base>Table.luau files, whose bytes
	// depend on the schema — are pinned by a digest over those files only, so
	// a runtime fix never makes this test need editing (§2.12).
	gotDigest := tableModuleDigest(defaultUnit.files)
	if gotDigest != wantUndeclaredTableDigest {
		t.Errorf("the undeclared unit's per-table bytes changed from BASE: got %s, want %s", gotDigest, wantUndeclaredTableDigest)
	}
	if bytes.Contains(defaultUnit.files["ProbeTable.luau"], []byte("forms = {")) {
		t.Error("undeclared table gained a forms marker")
	}
	for _, name := range []string{"json.luau", "textform.luau", "cook.luau", "cookread.luau", "block.luau", "blockread.luau"} {
		if _, ok := defaultUnit.files[name]; !ok {
			t.Errorf("undeclared table lost the default %s runtime", name)
		}
	}

	// (2) Every emitted runtime file is byte-equal to its source under
	// internal/codegen/luautable/runtime/, read through the package's own
	// embed, so a runtime fix needs no edit here while a forms change that
	// leaked into a runtime module still fails. view.luau carries the unit
	// manifest templated by emitUnitView, so it is not a straight copy.
	for name, src := range luautable.Runtime() {
		if name == "view.luau" {
			continue
		}
		got, ok := defaultUnit.files[name]
		if !ok {
			t.Errorf("emitted tree lacks the %s runtime", name)
			continue
		}
		if !bytes.Equal(got, src) {
			t.Errorf("emitted %s differs from its source in runtime/", name)
		}
	}
}

// wantUndeclaredTableDigest is the sha256 over the per-table module(s) of an
// undeclared table unit at this head, name and bytes in sorted order — the
// <Base>Table.luau files, whose bytes depend on the schema. A table with no
// declaration must generate exactly the module it generated before
// (docs/SPEC-TABLES.md §2.12). Runtime modules are pinned separately, by
// byte-equality to their sources in runtime/, so a runtime fix never edits
// this constant.
const wantUndeclaredTableDigest = "931fe4c3bd8fdd3ae4fa4ae84700ee0dabf62428f23a0073ebe135664e41fd0d"

// tableModuleDigest hashes the per-table module(s) of a unit — the
// <Base>Table.luau files, name then bytes, in sorted name order — so an edit
// to any generated table module is pinned (docs/SPEC-TABLES.md §2.12).
func tableModuleDigest(files map[string][]byte) string {
	var names []string
	for name := range files {
		if strings.HasSuffix(name, "Table.luau") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write(files[name])
	}
	return hex.EncodeToString(h.Sum(nil))
}
