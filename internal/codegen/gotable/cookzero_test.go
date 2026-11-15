package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestGoCookWriteZeroesDeadAndAbsentBytes cooks a root whose optional payload is
// absent and one whose counted array rides below its capacity, in both byte
// orders, through the generated Go writer, and holds the images byte for byte
// to the committed cook-write fixtures (docs/SPEC-TABLES.md §7.2, §7.6;
// docs/rulings.json fixed-dead-bytes-are-zero): an absent optional's payload
// and every dead counted-array slot are zero, whatever the element's declared
// default is.
func TestGoCookWriteZeroesDeadAndAbsentBytes(t *testing.T) {
	t.Parallel()
	u := loadUnitPaths(t, "../../..", []string{"tables/examples"})
	for _, tc := range []struct {
		name, root string
	}{
		{"keyed_config", "KeyedConfig"},
		{"root_full", "RootConfig"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wire := fixturePath(t, "testdata/conformance/tables/wire/"+tc.name+".bin")
			little := fixturePath(t, "testdata/conformance/tables/cook-write/"+tc.name+".cook")
			big := fixturePath(t, "testdata/conformance/tables/cook-write/"+tc.name+"-be.cook")
			probe := fmt.Sprintf(`package tabledemo

import (
	"bytes"
	"os"
	"testing"
)

func TestZeroDeadAndAbsent(t *testing.T) {
	wire, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]%s, 1)
	plan := make([]TableFixedEntry, 1<<14)
	var report TableReport
	if %sFixedLoad(values, wire, plan, &report) != 1 || report != (TableReport{}) {
		t.Fatalf("load: %%+v", report)
	}
	for _, tc := range []struct {
		name  string
		file  string
		order TableByteOrder
	}{
		{"little", %q, TableByteOrderLittle},
		{"big", %q, TableByteOrderBig},
	} {
		want, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]byte, %sCookMeasure(&values[0]))
		if !%sCookFrom(&values[0], got, tc.order) {
			t.Fatalf("%%s: cook refused", tc.name)
		}
		if !bytes.Equal(got, want) {
			for i := range want {
				if i == len(got) || got[i] != want[i] {
					t.Fatalf("%%s: cook differs at byte %%d: got %%#x want %%#x", tc.name, i, got[i], want[i])
				}
			}
			t.Fatalf("%%s: cook wrote %%d bytes, want %%d", tc.name, len(got), len(want))
		}
	}
}
`, wire, tc.root, tc.root, little, big, tc.root, tc.root)
			runGeneratedUnit(t, u, probe, nil)
		})
	}
}

func fixturePath(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("../../..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
