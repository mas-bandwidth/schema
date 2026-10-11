package jstable

// fixedroadmap_t03 — the js leg's fixed-closure rows of docs/roadmap.sexp's node
// `fixed-tables` (ROADMAP.md "NEW Fixed Tables"): the assertions no other test
// of this leg makes. Law: docs/SPEC-TABLES.md §3.4 (the fixed form and its
// 65536-byte ceiling; the closure a fixed table must have), docs/FIXED-FORM-
// ALGORITHM.md §5.2 and §5.9. One subtest per task id, and the assertion under
// each is the clause the task's own title states, read off the compiler's own
// refusal filter and off the JavaScript this leg emits.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapJsT03PlansVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/R6", t03R6},
		{"js/R22", t03R22},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t03TableNamed is a unit's declared table by name, refused or not: a clause
// about a table the form is NOT emitted for still needs the struct itself.
func t03TableNamed(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == name {
				return st
			}
		}
	}
	t.Fatalf("the fixture declares no table %s", name)
	return nil
}

// t03R6: R6 [weak] "a table past §3.4's 65536 ceiling is not a fixed-form root:
// the refusal names the table, no form is emitted, and no lineage entry is
// parsed for it even when the lock carries one" (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §5.9 #9/#26, fixedjs.go's fixedRefusal ceiling
// branch, fixedform.go's fixedRoots filter, fixedlineage.go's refuseFixedLineage).
func t03R6(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Good { x int32 }

fixed table T
{
    blob bytes(70000)
}
`)
	st := t03TableNamed(t, u, "T")
	if got := fixedTypeBytes(st); got <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: the fixture body is %d bytes, want past the %d ceiling", got, ir.TableFixedRecordMaxBytes)
	}
	if reason := fixedRefusal(st); !strings.Contains(reason, "65536-byte ceiling") {
		t.Errorf("R6: the refusal is %q, want the §3.4 ceiling sentence", reason)
	}
	// not a fixed-form root: the refusal IS the filter.
	roots := jsFixedUnitRoots(u)
	for _, r := range roots {
		if r.Name == "T" {
			t.Errorf("R6: jsFixedUnitRoots keeps T past the ceiling: %v", roots)
		}
	}
	if len(roots) != 1 || roots[0].Name != "Good" {
		t.Errorf("R6: jsFixedUnitRoots = %v, want only the good table", roots)
	}
	// NAMED, NEVER SILENT: the module says which table and why, and carries no
	// fixed form for T.
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R6: Generate: %v", err)
	}
	named := false
	for name, body := range files {
		if strings.Contains(string(body), "// table T has NO FIXED FORM in JavaScript:") {
			named = true
		}
		if strings.Contains(string(body), "TFixedBodyBytes") || strings.Contains(string(body), "TFixedLayout") {
			t.Errorf("R6: %s carries a fixed form for a table past the ceiling", name)
		}
	}
	if !named {
		t.Errorf("R6: no emitted module names T's refusal")
	}
	// no lineage entry is parsed for it even when the lock carries one: a garbage
	// layout handed in for a non-root table is skipped, never taken as a lock bug
	// (refuseFixedLineage walks fixedRoots' names and nobody else's).
	_, err = GenerateLineage(u, map[string][]FixedLineageEntry{
		"T": {{Wire: 0xDEADBEEFCAFEF00D, Layout: []byte{1, 2, 3}, Record: 9}},
	})
	if err != nil {
		t.Errorf("R6: a lineage entry for a table past the ceiling was parsed: %v", err)
	}
}

// t03R22: R22 [owed] "the closure rule: every table or type reached by value is
// itself fixed; a pointer, map or unbounded array in the closure is a compile
// refusal; T is never in its own closure" (docs/SPEC-TABLES.md §3.4, ir's
// TableFixedSupported, fixedjs.go's closure branch, fixedform.go's
// fixedCollectTypes).
func t03R22(t *testing.T) {
	// every table or type reached by value is itself fixed: the predicate
	// recurses, so a variable construct nested behind a by-value field makes the
	// holder variable too.
	holder := unitFrom(t, `package probe

table Leaf { x int32 }

table Holder { p *Leaf }
`)
	if ir.TableFixedSupported(t03TableNamed(t, holder, "Holder"), 0) {
		t.Error("R22: a by-value holder of a pointer is not refused")
	}
	nested := unitFrom(t, `package probe

table Inner { xs []int32 }

table Holder { inner Inner }
`)
	if ir.TableFixedSupported(t03TableNamed(t, nested, "Holder"), 0) {
		t.Error("R22: a table reaching an unbounded array by value is not refused")
	}

	// a pointer, a map or an unbounded array in a FIXED table's closure is a
	// compile refusal: the checker names the construct.
	refusals := []struct {
		what string
		src  string
	}{
		{"a pointer", `package probe

table Leaf { x int32 }

fixed table T { p *Leaf }
`},
		{"a map", `package probe

fixed table T { m map[string(8)]int32 }
`},
		{"an unbounded array", `package probe

fixed table T { xs []int32 }
`},
	}
	for _, c := range refusals {
		err := t03CheckErr(c.src)
		if err == nil {
			t.Errorf("R22 %s: a fixed table with one compiles", c.what)
			continue
		}
		if !strings.Contains(err.Error(), "closure") {
			t.Errorf("R22 %s: the refusal is %q, want it to name the closure", c.what, err)
		}
	}

	// T is never in its own closure: the order fixedCollectTypes lays down holds
	// each type ONCE, with the root itself last — so no type precedes its own
	// declaration through itself.
	u := unitFrom(t, `package probe

type Leaf { x int32 }

fixed table T
{
    a Leaf
    b Leaf
}
`)
	st := t03TableNamed(t, u, "T")
	var order []*ir.Struct
	fixedCollectTypes(st, map[string]bool{}, &order)
	if len(order) == 0 || order[len(order)-1] != st {
		t.Fatalf("R22: fixedCollectTypes does not end on the root itself: %v", order)
	}
	seen := map[string]int{}
	for _, s := range order {
		seen[s.Name]++
	}
	for name, n := range seen {
		if n != 1 {
			t.Errorf("R22: type %s appears %d times in the closure order", name, n)
		}
	}
}

// t03CheckErr parses and checks one schema, answering the first check error or
// nil when it resolves. The pointer/map/unbounded-array closure refusals are the
// CHECKER's (internal/check), before any emitter filter runs.
func t03CheckErr(src string) error {
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return perrs[0]
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		return cerrs[0]
	}
	return nil
}
