package luautable_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sort"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// undeclaredSchemaModules are the emitted files whose bytes depend on the
// schema of a one-table unit: its <Base>Table.luau and the unit view, whose
// manifest emitUnitView templates from the unit (luautable.go:190).
var undeclaredSchemaModules = []string{"ProbeTable.luau", "schema_view.luau"}

// TestLuauUndeclaredUnitKeepsTableAndRuntime holds docs/SPEC-TABLES.md §2.12
// on the undeclared table: a table with no `forms` line carries every form its
// backend emits, exactly as before the declaration existed, and the runtime
// modules it emits are byte-identical to their sources under
// internal/codegen/luau/runtime/ and internal/codegen/luautable/runtime/.
func TestLuauUndeclaredUnitKeepsTableAndRuntime(t *testing.T) {
	t.Parallel()
	defaultUnit := generateFiles(t, map[string]string{"Probe.schema": "package probe\ntable Pong { x int32 }\n"})

	// (1) The schema-dependent modules — <Base>Table.luau and the unit's
	// schema_view.luau — are pinned by a digest over those files only, so a runtime
	// fix never makes this test need editing (§2.12).
	for _, name := range undeclaredSchemaModules {
		if _, ok := defaultUnit.files[name]; !ok {
			t.Errorf("undeclared unit lost its schema-dependent %s", name)
		}
	}
	gotDigest := schemaModuleDigest(defaultUnit.files, undeclaredSchemaModules)
	if gotDigest != wantUndeclaredSchemaDigest {
		t.Errorf("the undeclared unit's schema-dependent bytes changed from BASE: got %s, want %s", gotDigest, wantUndeclaredSchemaDigest)
	}
	if bytes.Contains(defaultUnit.files["ProbeTable.luau"], []byte("forms = {")) {
		t.Error("undeclared table gained a forms marker")
	}
	for _, name := range []string{"schema_json.luau", "schema_textform.luau", "schema_cook.luau", "schema_cookread.luau", "schema_block.luau", "schema_blockread.luau"} {
		if _, ok := defaultUnit.files[name]; !ok {
			t.Errorf("undeclared table lost the default %s runtime", name)
		}
	}

	// (2) Every emitted runtime file is byte-equal to its source under the
	// package's own embed, so a runtime fix needs no edit here while a forms
	// change that leaked into a runtime module still fails. schema_view.luau is the
	// one runtime template the unit manifest rewrites, so it is pinned by the
	// digest above, not by byte-equality.
	tableRuntime := luautable.Runtime()
	luauRuntime := luau.Runtime()
	for name, src := range tableRuntime {
		if containsName(undeclaredSchemaModules, name) {
			continue
		}
		got, ok := defaultUnit.files[name]
		if !ok {
			t.Errorf("emitted tree lacks the %s runtime", name)
			continue
		}
		if !bytes.Equal(got, src) {
			t.Errorf("emitted %s differs from its source in luautable/runtime/", name)
		}
	}
	for name, src := range luauRuntime {
		got, ok := defaultUnit.files[name]
		if !ok {
			t.Errorf("emitted tree lacks the %s runtime", name)
			continue
		}
		if !bytes.Equal(got, src) {
			t.Errorf("emitted %s differs from its source in luau/runtime/", name)
		}
	}

	// (3) Every emitted file is covered by one of the two checks above: a new
	// file neither pinned nor copied from a source is a gap in this test, not
	// an unchecked emission.
	for name := range defaultUnit.files {
		if containsName(undeclaredSchemaModules, name) {
			continue
		}
		if _, ok := tableRuntime[name]; ok {
			continue
		}
		if _, ok := luauRuntime[name]; ok {
			continue
		}
		t.Errorf("emitted %s is neither a pinned schema module nor a runtime module copied from source", name)
	}
}

// wantUndeclaredSchemaDigest is the sha256 over ProbeTable.luau and schema_view.luau,
// name then bytes in sorted order, at this head. Both depend on the schema (the
// table descriptor and the unit manifest), so they are the modules a forms
// change can alter; a table with no declaration must generate exactly what it
// generated before (docs/SPEC-TABLES.md §2.12). Runtime modules are pinned
// separately, by byte-equality to their sources, so a runtime fix never edits
// this constant. Moved when every table module began registering the unit's wire
// ids (retainids.go, docs/SPEC-TABLES.md §6.6). The table module carries the
// unit's build versions, so a change to the build version's form moves it too.
const wantUndeclaredSchemaDigest = "ffa06d71d1f98f15118130cdcded59121f17bcc39f756f82e09a8d4cc11b6527"

// schemaModuleDigest hashes the named schema-dependent modules of a unit —
// name then bytes, in sorted name order — so an edit to either is pinned
// (docs/SPEC-TABLES.md §2.12).
func schemaModuleDigest(files map[string][]byte, modules []string) string {
	names := append([]string(nil), modules...)
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write(files[name])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func containsName(names []string, name string) bool {
	return slices.Contains(names, name)
}
