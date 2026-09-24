package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Normalising bools changes which arrays can be folded into one copy run.
// The declared plan cap must cover the plan actually emitted for C++.
func TestFixedNormalisedBoolArrayRespectsPlanCap(t *testing.T) {
	dir := t.TempDir()
	src := "package probe\n\ntype Cell { flag bool\n tag uint8 }\n\nfixed table Wide { cells [3000]Cell }\n"
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(paths)
	if err != nil {
		if !strings.Contains(err.Error(), "Wide") || !strings.Contains(strings.ToLower(err.Error()), "leaf") {
			t.Fatalf("unexpected refusal: %v", err)
		}
		return
	}
	st := u.Tables["Wide"]
	if st == nil {
		t.Fatal("missing Wide")
	}
	plan, _ := ir.TableFixedBuildPlanNormalised(u, st)
	if len(plan) > ir.TableFixedLeafCap {
		t.Fatalf("accepted Wide: reported leaves=%d, normalised plan=%d entries, cap=%d", ir.TableFixedLeafCount(u, st), len(plan), ir.TableFixedLeafCap)
	}
}
