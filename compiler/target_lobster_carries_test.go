package compiler

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLobsterCarriesOptionalVariableClosure holds §2.3's `?T` over a variable
// closure as the Lobster target's own: Lobster generates the probe unit
// through c.Generate, and the registry says it carries the construct.
func TestLobsterCarriesOptionalVariableClosure(t *testing.T) {
	t.Parallel()
	c := New()
	src := "package probe\ntable Inner\n{\n    text *string\n}\ntable Outer\n{\n    inner ?Inner\n}\n"
	u := unitFromSource(t, src)
	if !carries("lobster", carryOptionalVariableClosure) {
		t.Fatalf("lobster is not registered as carrying optional-variable-closure")
	}
	if _, err := c.Generate(u, "lobster", nil); err != nil {
		t.Fatalf("lobster refused ?T over a variable closure: %v", err)
	}
}

// lobsterManifestUnits loads every unit the tables conformance manifest names,
// reached from the compiler package one directory up.
func lobsterManifestUnits(t *testing.T) map[string]*ir.Unit {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	units := map[string]*ir.Unit{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		if len(x) < 3 || x[0] != "unit" {
			continue
		}
		var roots []string
		for _, p := range x[2:] {
			roots = append(roots, filepath.Join("..", p))
		}
		paths, err := GatherPaths(roots)
		if err != nil {
			t.Fatalf("unit %s: %v", x[1], err)
		}
		u, err := New().Load(paths)
		if err != nil {
			t.Fatalf("unit %s does not load: %v", x[1], err)
		}
		units[x[1]] = u
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return units
}

// TestLobsterGeneratesEveryManifestUnit holds the target to the runtime it
// emits: every unit of the tables conformance manifest generates for lobster,
// so the conformance leg answers every one of its cases. THE REVERSED
// WITNESS: the shared refusals the target applied before its load and save
// covered these constructs (refuseUnported, refuseOptionalArrays, refuseLists,
// refuseMaps) still refuse units of this same manifest by name for a target
// that registers no carrier, so unregistering lobster turns this test red
// rather than passing vacuously.
func TestLobsterGeneratesEveryManifestUnit(t *testing.T) {
	t.Parallel()
	c := New()
	units := lobsterManifestUnits(t)
	if len(units) == 0 {
		t.Fatal("the manifest names no unit")
	}
	witnessed := 0
	for key, u := range units {
		if _, err := c.Generate(u, "lobster", nil); err != nil {
			t.Errorf("lobster refuses manifest unit %s: %v", key, err)
		}
		for _, refuse := range []func(*ir.Unit, string) error{refuseUnported, refuseOptionalArrays, refuseLists, refuseMaps} {
			if refuse(u, "unregistered-witness") != nil {
				witnessed++
				break
			}
		}
	}
	if witnessed == 0 {
		t.Fatal("no manifest unit holds a construct the shared refusals name, so this test witnesses nothing")
	}
	t.Logf("%d manifest units generate for lobster; %d of them the shared refusals would refuse", len(units), witnessed)
}
