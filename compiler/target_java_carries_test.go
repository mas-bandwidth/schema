package compiler

import (
	"slices"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestJavaCarriesAllElevenConstructs(t *testing.T) {
	t.Parallel()
	got, ok := Carried("java")
	if !ok {
		t.Fatal("Carried(java) returned false")
	}
	want := CarryConstructs()
	if !slices.Equal(got, want) {
		t.Fatalf("Carried(\"java\") = %v, want %v", got, want)
	}
}

func TestJavaGeneratesEveryManifestUnit(t *testing.T) {
	t.Parallel()
	c := New()
	units := lobsterManifestUnits(t)
	if len(units) == 0 {
		t.Fatal("the manifest names no unit")
	}
	witnessed := 0
	for key, u := range units {
		files, err := c.Generate(u, "java", nil)
		if err != nil {
			t.Errorf("java refuses manifest unit %s: %v", key, err)
		}
		if len(files) == 0 {
			t.Errorf("java generated no files for manifest unit %s", key)
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
}
