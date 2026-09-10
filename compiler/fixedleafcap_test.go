// §3.4'S PLAN CAP, and the FLAT-ELEMENT FOLD that decides what reaches it
// (docs/SPEC-TABLES.md §3.4). Every backend lays a fixed table's identity plan
// down as STATIC DATA, so the plan's length is source a consumer's compiler
// parses on every build — which is what the cap bounds, and it is not the
// record size and not the field count.
//
// WHAT THE FOLD DOES IS THE WHOLE DIFFERENCE. An array whose element's storage
// image IS its wire image — a scalar, or a struct of them — is ONE leaf however
// many elements it has, because the whole array is one run. So a table reaches
// this cap only by declaring a big array OF A WALKED TYPE: one carrying text, a
// count, a union or an optional. Both halves are held here, because a cap that
// nobody can characterize is a cap nobody can design against.
package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func leafCapUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// THE FLAT-ELEMENT FOLD, held by test: an array of a scalar is ONE leaf, so a
// table can declare eight thousand of them and spend two leaves on the field —
// the count and the run. This is the control for the cases below: without it
// they would be measuring the array bound rather than the fold.
func TestFlatElementArrayCostsOneLeaf(t *testing.T) {
	u := leafCapUnit(t, "package probe\n\ntable Flat\n{\n    marks [..8192]int32\n}\n")
	st := u.Tables["Flat"]
	if st == nil {
		t.Fatal("the probe declares Flat")
	}
	if n := ir.TableFixedLeafCount(u, st); n != 2 {
		t.Fatalf("an array of a FLAT element is the count and one run, 2 leaves; got %d", n)
	}
	warns, errs := ir.TableFixedLeafCapRefusals(u)
	if len(warns) != 0 || len(errs) != 0 {
		t.Fatalf("8192 flat elements must say nothing: warns=%v errs=%v", warns, errs)
	}
	if !ir.TableFixedEmitted(u, st) {
		t.Error("and the fixed form is emitted for it")
	}
}

// AN ARRAY OF A WALKED ELEMENT SPENDS A LEAF PER ELEMENT, which is what reaches
// the cap. `Cell` carries a `string(4)`, so this form cannot fold it into a run.
func TestWalkedElementArrayPastTheCapWarnsAndDropsTheForm(t *testing.T) {
	src := fmt.Sprintf("package probe\n\ntype Cell\n{\n    label string(4)\n}\n\ntable Wide\n{\n    cells [..%d]Cell\n}\n", ir.TableFixedLeafCap)
	u := leafCapUnit(t, src)
	st := u.Tables["Wide"]
	if st == nil {
		t.Fatal("the probe declares Wide")
	}
	leaves := ir.TableFixedLeafCount(u, st)
	if leaves <= ir.TableFixedLeafCap {
		t.Fatalf("the probe must be past the cap to measure anything; %d leaves", leaves)
	}
	// DERIVED: a warning naming the table and the count, and the unit compiles.
	warns, errs := ir.TableFixedLeafCapRefusals(u)
	if len(errs) != 0 || len(warns) != 1 {
		t.Fatalf("a DERIVED fixed table past the cap warns and compiles: warns=%v errs=%v", warns, errs)
	}
	for _, want := range []string{"Wide", fmt.Sprint(leaves), fmt.Sprint(ir.TableFixedLeafCap), "§3.4"} {
		if !strings.Contains(warns[0], want) {
			t.Errorf("the warning must carry %q: %s", want, warns[0])
		}
	}
	if ir.TableFixedEmitted(u, st) {
		t.Error("and the fixed form is not emitted for it")
	}

	// DECLARED: a refusal BY NAME, and no warning pretending the form was kept.
	// The `fixed table` keyword lives on branch `fixed-table-keyword` and is
	// not merged, so this sets the IR marker the keyword will set — the same
	// half the record ceiling's own declared case sets.
	st.FixedDeclared = true
	warns, errs = ir.TableFixedLeafCapRefusals(u)
	if len(errs) != 1 {
		t.Fatalf("a DECLARED fixed table past the cap must not compile: warns=%v errs=%v", warns, errs)
	}
	if len(warns) != 0 {
		t.Errorf("a refusal replaces the warning rather than joining it: %v", warns)
	}
	for _, want := range []string{"Wide", fmt.Sprint(leaves), fmt.Sprint(ir.TableFixedLeafCap), "§3.4", "DECLARED"} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Errorf("the refusal must carry %q: %s", want, errs[0])
		}
	}
}

// AND UNDER THE CAP THE SAME SHAPE IS SILENT: the cap is what fires, not the
// walked element.
func TestWalkedElementArrayUnderTheCapIsSilent(t *testing.T) {
	src := "package probe\n\ntype Cell\n{\n    label string(4)\n}\n\ntable Narrow\n{\n    cells [..16]Cell\n}\n"
	u := leafCapUnit(t, src)
	warns, errs := ir.TableFixedLeafCapRefusals(u)
	if len(warns) != 0 || len(errs) != 0 {
		t.Fatalf("a walked element under the cap must say nothing: warns=%v errs=%v", warns, errs)
	}
	if !ir.TableFixedEmitted(u, u.Tables["Narrow"]) {
		t.Error("and the fixed form is emitted for it")
	}
}
