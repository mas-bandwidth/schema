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
	u := leafCapUnit(t, "package probe\n\nfixed table Flat\n{\n    marks [..8192]int32\n}\n")
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
//
// THE KEYWORD IS WHAT THE CAP ANSWERS TO (#823, docs/SPEC-TABLES.md §2.2). A
// DECLARED `fixed table` past the cap is a REFUSAL BY NAME and the unit does not
// compile — a fixed table never silently falls back to form 1. A plain `table`
// is the variable wire by declaration and never a fixed root at all, so the cap
// has nothing to say about it and says nothing.
func TestWalkedElementArrayPastTheCapIsRefusedByName(t *testing.T) {
	src := fmt.Sprintf("package probe\n\ntype Cell\n{\n    label string(4)\n}\n\nfixed table Wide\n{\n    cells [..%d]Cell\n}\n", ir.TableFixedLeafCap)

	// DECLARED: the unit does not compile, and the refusal names the table, the
	// leaf count, the cap and the section.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	var warns []string
	c := New()
	c.OnWarn = func(msg string) { warns = append(warns, msg) }
	if _, err := c.Load(paths); err == nil {
		t.Fatal("a DECLARED fixed table past the cap must not compile")
	} else {
		for _, want := range []string{"Wide", fmt.Sprint(ir.TableFixedLeafCap), "leaves", "§3.4", "DECLARED"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal must carry %q: %s", want, err)
			}
		}
	}
	// A REFUSAL REPLACES THE CAP'S WARNING rather than joining it. The record-size
	// advisory (§3.4's other bound) is a different sentence about a different
	// number and is expected here, so only the cap's own warning is read.
	for _, w := range warns {
		if strings.Contains(w, "leaf cap") || strings.Contains(w, "leaves") {
			t.Errorf("the cap warned as well as refused: %s", w)
		}
	}

	// THE SAME SHAPE ON A PLAIN `table` IS THE VARIABLE WIRE BY DECLARATION: no
	// refusal, no warning, and no fixed form to drop.
	plain := leafCapUnit(t, strings.Replace(src, "fixed table Wide", "table Wide", 1))
	pst := plain.Tables["Wide"]
	if pst == nil {
		t.Fatal("the plain probe declares Wide")
	}
	if pst.FixedDeclared {
		t.Fatal("a plain `table` came out of the IR declared fixed")
	}
	pwarns, perrs := ir.TableFixedLeafCapRefusals(plain)
	if len(pwarns) != 0 || len(perrs) != 0 {
		t.Fatalf("a plain table is not a fixed root, so the cap says nothing: warns=%v errs=%v", pwarns, perrs)
	}
	if ir.TableFixedEmitted(plain, pst) {
		t.Error("a plain table got the fixed form")
	}
}

// AND UNDER THE CAP THE SAME SHAPE IS SILENT: the cap is what fires, not the
// walked element.
func TestWalkedElementArrayUnderTheCapIsSilent(t *testing.T) {
	src := "package probe\n\ntype Cell\n{\n    label string(4)\n}\n\nfixed table Narrow\n{\n    cells [..16]Cell\n}\n"
	u := leafCapUnit(t, src)
	warns, errs := ir.TableFixedLeafCapRefusals(u)
	if len(warns) != 0 || len(errs) != 0 {
		t.Fatalf("a walked element under the cap must say nothing: warns=%v errs=%v", warns, errs)
	}
	if !ir.TableFixedEmitted(u, u.Tables["Narrow"]) {
		t.Error("and the fixed form is emitted for it")
	}
}
