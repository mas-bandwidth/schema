// §5.2'S DEPTH BOUND, AND THE ONE NUMBER BEHIND IT (docs/FIXED-FORM-ALGORITHM.md
// §5.2, §6, docs/SPEC-TABLES.md §3.4).
//
// The red-team pass of 2026-09-11 found two numbers for one fact: the compiler
// held a fixed table's closure to 16 and every runtime holds the wire to 64
// (`kTableFixedMaxDepth`). A DECLARED `fixed table` nested past 16 therefore lost
// the fixed form WITH NOTHING SAID — at 17, at 101, at any depth — which is the
// silent drop §4.5 fix 4 forbids and the opposite of what every other bound of
// §3.4 does.
//
// So: one bound, the LAYOUT's 64, checked where the layout is; and past it the
// table is NAMED, with its depth, on the 65536 ceiling's own terms (the form is
// not emitted, the table keeps form 1, the class is untouched).
package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// deepSchema is a chain of nested `type`s under one table: `levels` of nesting,
// the innermost a scalar. It is the only shape that reaches this bound, because
// by-value nesting is all the fixed form has — a pointer or a map makes the
// holder variable and never reaches the walk at all.
func deepSchema(keyword string, levels int) string {
	var b strings.Builder
	b.WriteString("package probe\n\ntype N0\n{\n    v int32\n}\n\n")
	for i := 1; i < levels; i++ {
		fmt.Fprintf(&b, "type N%d\n{\n    n N%d\n}\n\n", i, i-1)
	}
	fmt.Fprintf(&b, "%s Deep\n{\n    n N%d\n}\n", keyword, levels-1)
	return b.String()
}

func depthUnit(t *testing.T, src string) (*ir.Unit, []string) {
	t.Helper()
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
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("the probe must compile — a depth past the bound is a named warning and not a refusal: %v", err)
	}
	return u, warns
}

// THE COMPILE BOUND IS THE LAYOUT'S BOUND. The runtimes' `kTableFixedMaxDepth`
// is 64 and §5.2 refuses `layout_malformed` past 64; the compiler must hold the
// same number, or it drops a form the wire would have carried.
func TestFixedDepthBoundIsTheLayoutsSixtyFour(t *testing.T) {
	if ir.TableFixedMaxDepth != 64 {
		t.Fatalf("§5.2, §6 and SPEC-TABLES §3.4 all say 64 nested bodies; the compiler holds %d", ir.TableFixedMaxDepth)
	}
}

// A NESTING INSIDE THE BOUND KEEPS THE FORM AND SAYS NOTHING — and the old
// 16 is inside it, so this is also the regression: a table nested 20 deep used to
// lose the fixed form in silence and now carries it.
func TestFixedDepthInsideTheBoundIsSilentAndEmitted(t *testing.T) {
	u, warns := depthUnit(t, deepSchema("fixed table", 20))
	st := u.Tables["Deep"]
	if st == nil {
		t.Fatal("the probe declares Deep")
	}
	if n := ir.TableFixedTypeDepth(st); n > ir.TableFixedMaxDepth {
		t.Fatalf("a 20-type chain must sit inside the bound; its layout nests %d", n)
	}
	if w := ir.TableFixedDepthBounds(u); len(w) != 0 {
		t.Fatalf("a nesting inside the bound must say nothing: %v", w)
	}
	for _, w := range warns {
		if strings.Contains(w, "depth bound") {
			t.Errorf("the depth bound warned inside itself: %s", w)
		}
	}
	if !ir.TableFixedEmitted(u, st) {
		t.Error("and the fixed form is emitted for it — the old bound of 16 is what dropped it")
	}
}

// A DECLARED FIXED TABLE PAST THE BOUND IS NAMED, WITH ITS DEPTH, and the form
// is not emitted for it. This is the red-team finding: before the fix the form
// went away and the compiler said nothing, at any depth.
func TestFixedDepthPastTheBoundIsNamedNeverDropped(t *testing.T) {
	u, warns := depthUnit(t, deepSchema("fixed table", ir.TableFixedMaxDepth+8))
	st := u.Tables["Deep"]
	if st == nil {
		t.Fatal("the probe declares Deep")
	}
	depth := ir.TableFixedTypeDepth(st)
	if depth <= ir.TableFixedMaxDepth {
		t.Fatalf("the probe must nest past the bound; its layout nests %d", depth)
	}
	if ir.TableFixedEmitted(u, st) {
		t.Error("no conforming reader takes a walk that deep, so the form must not be emitted")
	}
	got := ir.TableFixedDepthBounds(u)
	if len(got) != 1 {
		t.Fatalf("want one sentence naming the table, got %d: %v", len(got), got)
	}
	for _, want := range []string{"Deep", "DECLARED", fmt.Sprint(depth), fmt.Sprint(ir.TableFixedMaxDepth), "§3.4", "NOT EMITTED"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("the sentence must carry %q: %s", want, got[0])
		}
	}
	// AND IT REACHES THE PERSON. A bound nobody is told about is not a bound.
	named := false
	for _, w := range warns {
		if strings.Contains(w, "depth bound") && strings.Contains(w, "Deep") {
			named = true
		}
	}
	if !named {
		t.Errorf("the compiler must hand the depth sentence to OnWarn: %v", warns)
	}
}

// AND AT ANY DEPTH BEYOND IT, which is the other half of the finding: 101 deep
// compiled clean.
func TestFixedDepthFarPastTheBoundIsStillNamed(t *testing.T) {
	u, _ := depthUnit(t, deepSchema("fixed table", 101))
	got := ir.TableFixedDepthBounds(u)
	if len(got) != 1 || !strings.Contains(got[0], "Deep") {
		t.Fatalf("101 deep must be named too: %v", got)
	}
}

// A PLAIN `table` IS THE VARIABLE WIRE BY DECLARATION, so the bound has nothing
// to say about the depth it nests.
func TestFixedDepthSaysNothingAboutAPlainTable(t *testing.T) {
	u, warns := depthUnit(t, deepSchema("table", ir.TableFixedMaxDepth+8))
	if w := ir.TableFixedDepthBounds(u); len(w) != 0 {
		t.Fatalf("a plain table is not a fixed root: %v", w)
	}
	for _, w := range warns {
		if strings.Contains(w, "depth bound") {
			t.Errorf("a plain table was held to the fixed form's depth bound: %s", w)
		}
	}
}

// THE LAYOUT'S DEPTH IS READ FROM THE LAYOUT'S BYTES, the way a reader reads it
// and the way a lock holds it: the root at 0, each child one deeper.
func TestFixedLayoutDepthReadsTheBytes(t *testing.T) {
	u, _ := depthUnit(t, "package probe\n\ntype Inner\n{\n    v int32\n}\n\nfixed table Flat\n{\n    a int32\n    b Inner\n}\n")
	st := u.Tables["Flat"]
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	if n := ir.TableFixedLayoutDepth(layout); n != 2 {
		t.Fatalf("the root is 0, `b` is 1 and Inner's `v` is 2; got %d", n)
	}
	if ir.TableFixedLayoutDepth(nil) != 0 {
		t.Error("no layout is no depth")
	}
}
