package compiler

import (
	"slices"
	"sync/atomic"
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
	units := lobsterManifestUnits(t)
	if len(units) == 0 {
		t.Fatal("the manifest names no unit")
	}
	// Each unit generates in a parallel subtest of its own: one after another
	// they were most of a minute. The group returns when every unit has.
	var witnessed atomic.Int64
	t.Run("units", func(t *testing.T) {
		for key, u := range units {
			t.Run(key, func(t *testing.T) {
				t.Parallel()
				files, err := New().Generate(u, "java", nil)
				if err != nil {
					t.Errorf("java refuses manifest unit %s: %v", key, err)
				}
				if len(files) == 0 {
					t.Errorf("java generated no files for manifest unit %s", key)
				}
				for _, refuse := range []func(*ir.Unit, string) error{refuseUnported, refuseOptionalArrays, refuseLists, refuseMaps} {
					if refuse(u, "unregistered-witness") != nil {
						witnessed.Add(1)
						break
					}
				}
			})
		}
	})
	if witnessed.Load() == 0 {
		t.Fatal("no manifest unit holds a construct the shared refusals name, so this test witnesses nothing")
	}
}
