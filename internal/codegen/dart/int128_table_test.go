package dart_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/darttable"
)

// TestTableOnlyUnitEmitsInt128Support pins the corpus-completeness fix behind
// the strict-analyze gate: a unit whose only 128-bit fields are TABLE fields
// (the tables/scalars unit is one) gets Int128.dart beside its output. Before
// the fix unitNeeds128 scanned only u.Structs, so the form-3 fixed library the
// darttable emitter writes for such a unit imported an Int128.dart that was
// never emitted — a generated-output defect the analyzer reports as a missing
// import, and one a gate must not paper over by deleting the files. The first
// assertion is the red line: at BASE Generate returns no Int128.dart while the
// table output imports it.
func TestTableOnlyUnitEmitsInt128Support(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, `package probe

fixed table Cell
{
    value int128 | min = -1000, max = 1000
    count uint128
}
`)
	packet, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("packet generate: %v", err)
	}
	support, ok := packet["Int128.dart"]
	if !ok {
		t.Fatalf("a table-only 128-bit unit got no Int128.dart: files=%v", sortedNames(packet))
	}
	for _, want := range []string{"final class Int128", "final class UInt128"} {
		if !strings.Contains(string(support), want) {
			t.Errorf("Int128.dart does not declare %q", want)
		}
	}
	tables, err := darttable.Generate(u)
	if err != nil {
		t.Fatalf("table generate: %v", err)
	}
	imported := false
	for _, body := range tables {
		if strings.Contains(string(body), "import 'Int128.dart'") {
			imported = true
		}
	}
	if !imported {
		t.Fatalf("the table emitter no longer imports Int128.dart, so this test pins nothing; files=%v", sortedNames(tables))
	}
}

// TestTableUnionArmEmitsInt128Support pins the table-union half of the
// corpus-completeness fix: a unit whose only 128-bit field lives in a TABLE
// UNION's arm (not a struct, not a table's own field) still gets Int128.dart
// beside its output. Before the fix unitNeeds128 scanned u.Structs and
// u.Tables but not u.TableUnions, so the table form imported an Int128.dart
// that was never emitted — the same missing-import defect the analyzer
// reports. The first assertion is the red line.
func TestTableUnionArmEmitsInt128Support(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, `package probe

table Root
{
    body Value
}

union Value
{
    tally int128 | min = -1000, max = 1000
}
`)
	packet, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("packet generate: %v", err)
	}
	support, ok := packet["Int128.dart"]
	if !ok {
		t.Fatalf("a unit whose only 128-bit field is a table-union arm got no Int128.dart: files=%v", sortedNames(packet))
	}
	for _, want := range []string{"final class Int128", "final class UInt128"} {
		if !strings.Contains(string(support), want) {
			t.Errorf("Int128.dart does not declare %q", want)
		}
	}
	tables, err := darttable.Generate(u)
	if err != nil {
		t.Fatalf("table generate: %v", err)
	}
	imported := false
	for _, body := range tables {
		if strings.Contains(string(body), "import 'Int128.dart'") {
			imported = true
		}
	}
	if !imported {
		t.Fatalf("the table emitter no longer imports Int128.dart, so this test pins nothing; files=%v", sortedNames(tables))
	}
}

func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
