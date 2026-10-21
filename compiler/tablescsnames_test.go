// The tables tests of ONE language, in its own file so a port adds a file and
// edits no shared one (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"maps"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
)

// TestCsRuntimeNameScanGoesRed is the C# runtime-name scan's own NEGATIVE
// CONTROL. The cs leg already carries the scan — TestTableRuntimeNamesAreClaimed
// — but nothing ever proved it could go RED: a scan that has gone blind passes
// every registry it is pointed at. This control plants a name nobody registered
// and requires the scan to see it.
//
// The cs scan's collector is inline inside TestTableRuntimeNamesAreClaimed and
// is not callable from here, so this file carries a faithful copy — csRuntimeIdent
// and csEmittedNames below — and proves the copy faithful FIRST, before it
// trusts it. The plant is a struct AND an enum, because the scan's own comment
// names the enum as "The dangerous shape, for the record: an ENUM" — a scan
// that has to recognise declaration syntax goes quietly blind the day the
// syntax changes.
//
// The docs/PORTING.md cs cell of the I6 row is owed and deliberately not edited
// here, for the same reason sibling leg-cards hold that one line.
var csRuntimeIdent = regexp.MustCompile(`\bTable[A-Za-z0-9_]*\b`)

// csEmittedNames is a faithful copy of the collector inlined in
// TestTableRuntimeNamesAreClaimed (compiler/tables_test.go): every Table*-
// prefixed identifier in the emitted text, line comments stripped first, the
// unit's own namespace excluded.
func csEmittedNames(files map[string][]byte, namespace string) map[string]bool {
	emitted := map[string]bool{}
	for _, data := range files {
		for line := range strings.SplitSeq(string(data), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			for _, m := range csRuntimeIdent.FindAllString(line, -1) {
				if m != namespace {
					emitted[m] = true
				}
			}
		}
	}
	return emitted
}

func TestCsRuntimeNameScanGoesRed(t *testing.T) {
	source := runtimeSrc + `
table NativeRoot
{
    next *NativeRoot
    values []int32
    keys map[uint32]int32
}
`
	files, err := New().Generate(unitFromSource(t, source), "cs", Options{})
	if err != nil {
		t.Fatal(err)
	}
	namespace := capitalizeFirst(unitFromSource(t, runtimeSrc).Package)

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	t.Logf("the cs target emitted %d files: %s", len(files), strings.Join(names, " "))

	clean := csEmittedNames(files, namespace)
	t.Logf("the copied collector sees %d Table* names over the clean emitted C#", len(clean))
	if len(clean) == 0 {
		t.Fatal("the scan found no Table* identifier in the emitted C# at all — the scan, not the registry, is what broke")
	}
	for _, name := range tablenames.DefinedBy(tablenames.Cs) {
		if !clean[name] {
			t.Errorf("registry says Cs defines %s and the copied collector does not see it", name)
		}
	}
	for name := range clean {
		if !tablenames.Registered(name) {
			t.Errorf("the copied collector sees %s and internal/tablenames does not register it", name)
		}
	}

	sabotaged := map[string][]byte{}
	maps.Copy(sabotaged, files)
	table, ok := sabotaged["ProbeTable.cs"]
	if !ok {
		t.Fatal("--lang cs emitted no ProbeTable.cs for the runtime unit")
	}
	sabotaged["ProbeTable.cs"] = append([]byte("namespace Probe\n{\n    public struct TableProbe { }\n    public enum TableProbeKind { A, B }\n}\n\n"), table...)

	emitted := csEmittedNames(sabotaged, namespace)
	if !emitted["TableProbe"] {
		t.Fatal("the scan did not see a namespace-scope TableProbe — it is blind, and every green run above proves nothing")
	}
	if !emitted["TableProbeKind"] {
		t.Fatal("the scan did not see a namespace-scope TableProbeKind — it is blind, and every green run above proves nothing")
	}
	if tablenames.Registered("TableProbe") {
		t.Fatal("TableProbe is registered, so the control proves nothing — pick a name the registry does not hold")
	}
	if tablenames.Registered("TableProbeKind") {
		t.Fatal("TableProbeKind is registered, so the control proves nothing — pick a name the registry does not hold")
	}
}
