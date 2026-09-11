// THE FOUR LANES §5.8 ROWS 5, 6, 7 AND 14 OWED, held on BOTH TWINS at once
// (docs/FIXED-FORM-ALGORITHM.md §4.1, §4.5, §5.2). Every fixture below reads
// the two emitted runtimes, or the one identity walk both of them lay down, so
// a fix that lands on the reference alone reds here the way the twin gate reds.
package compiler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ctable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// bothFixedRuntimes is the emitted C and C++ fixed runtimes for one probe, by
// leg name, so a fixture states its assertion ONCE and both twins answer it.
func bothFixedRuntimes(t *testing.T, src string) map[string]string {
	t.Helper()
	out := map[string]string{}
	cppFiles, err := cpptable.Generate(unitFromSource(t, src))
	if err != nil {
		t.Fatalf("generate the C++ table header: %v", err)
	}
	cFiles, err := ctable.Generate(unitFromSource(t, src))
	if err != nil {
		t.Fatalf("generate the C table header: %v", err)
	}
	for leg, files := range map[string]map[string][]byte{"cpp": cppFiles, "c": cFiles} {
		for name, data := range files {
			if strings.HasSuffix(name, "Table.h") {
				out[leg] = string(data)
			}
		}
		if out[leg] == "" {
			t.Fatalf("%s: no Table header generated", leg)
		}
	}
	return out
}

// ROW 7 (§4.5): the `ordinal` op reads through a 64-BIT temporary, an ordinal
// width of 8 being admissible, and the `const` op lands a tag of that width out
// of a temporary and never out of the four-byte `aux` lane beside its guard.
// A forged layout is all it takes to ask for either: the width is the WRITER's.
func TestFixedOrdinalWidthEightIsAdmissibleOnBothTwins(t *testing.T) {
	for leg, h := range bothFixedRuntimes(t, `package probe

enum Grade { Bronze, Silver, Gold }

fixed table Root { grade Grade = Silver }
`) {
		if strings.Contains(h, "uint32_t raw = 0;") {
			t.Errorf("%s: a temporary an ordinal of width 8 overruns (§5.8 row 7)", leg)
		}
		if strings.Contains(h, "&p->aux, p->size )") || strings.Contains(h, "&p.aux, p.size )") {
			t.Errorf("%s: a const of a tag wider than 4 reads the member beside aux (§5.8 row 7)", leg)
		}
	}
}

// ROW 5 (bill §12.7): no byte lane anywhere. A union of 256 arms has a two-byte
// tag, so arm 256 is a guard value a byte cannot hold — and truncated it is
// arm 1's, which fires arm 1's entries over arm 256's bytes.
func TestFixedUnionArm256CarriesItsOwnOrdinal(t *testing.T) {
	var b strings.Builder
	b.WriteString("package probe\n\ntype Cell { n int32 }\n\nunion Wide {\n")
	for i := range 256 {
		fmt.Fprintf(&b, "    a%d Cell\n", i)
	}
	b.WriteString("}\n\nfixed table Root { pick Wide }\n")
	src := b.String()
	u := unitFromSource(t, src)
	_, _, pool := ir.TableFixedBuildPlan(u, u.Tables["Root"])
	top := 0
	for _, l := range pool {
		if l.Arg > top {
			top = l.Arg
		}
	}
	if top != 256 {
		t.Fatalf("the 256th arm's guard value is 256, the plan's highest is %d (§5.8 row 5)", top)
	}
	for leg, h := range bothFixedRuntimes(t, src) {
		// THE ORDINAL IS STILL FULL WIDTH, in the chain's two halves now: a
		// byte lane made arm 256 arm 1, and a 32-bit one would make arm 2^32
		// arm 0.
		if !strings.Contains(h, "uint32_t arg_lo") || !strings.Contains(h, "uint32_t arg_hi") {
			t.Errorf("%s: the guard's ordinal is not full width, so arm 256 is arm 1 (§5.8 row 5)", leg)
		}
		if !strings.Contains(h, "{ 0u, 256u, 0u, 2u }") {
			t.Errorf("%s: the emitted guard pool does not carry arm 256's own ordinal (§5.8 row 5)", leg)
		}
	}
}

// ROW 6 (§4.1) AND GLENN'S RULING (§5.9): an arm inside an arm answers to the
// OUTER tag TOO — and its own arm selection survives that, because the tags are
// CONJOINED. Under the outer tag alone, an inner arm's bytes land beneath an
// outer arm that never rode; under the inner tag alone, the inner union is read
// out of another outer arm's bytes. THE CONJUNCTION IS A CHAIN, so the depth it
// holds is not two: this fixture walks two, THREE and FOUR deep, and the
// three-deep case REFUSED on the two-lane shape (it panicked by name) where it
// now reads exactly.
func TestFixedNestedUnionArmAnswersToBothTags(t *testing.T) {
	// nestedSource builds a tower of `depth` unions: the first arm of each holds
	// the next union down, and the last holds a cell.
	nestedSource := func(depth int) string {
		var b strings.Builder
		b.WriteString("package probe\n\ntype Cell { n int32 }\n\n")
		for d := depth; d >= 1; d-- {
			if d == depth {
				fmt.Fprintf(&b, "union U%d\n{\n    x Cell\n    y Cell\n}\n\n", d)
			} else {
				fmt.Fprintf(&b, "type H%d { pick U%d }\n\nunion U%d\n{\n    held  H%d\n    spare Cell\n}\n\n", d, d+1, d, d)
			}
		}
		b.WriteString("fixed table Root { top U1 }\n")
		return b.String()
	}

	for depth := 1; depth <= 4; depth++ {
		u := unitFromSource(t, nestedSource(depth))
		plan, guarded, pool := ir.TableFixedBuildPlan(u, u.Tables["Root"])
		deepest := 0
		for _, e := range plan {
			if len(e.Guards) == 0 {
				continue
			}
			// EVERY LINK IS A TAG OF ITS OWN, at its own width, and the chain
			// runs OUTERMOST FIRST: a repeated offset would be one tag asked
			// two different questions, which is never what nesting means.
			seen := map[int64]bool{}
			for i, l := range e.Guards {
				if seen[l.Guard] {
					t.Fatalf("depth %d: the chain on %s names the tag at %d twice (§5.9)", depth, e.Note, l.Guard)
				}
				seen[l.Guard] = true
				if l.ArgW < 1 {
					t.Fatalf("depth %d: link %d of %s has no width (§5.8 row 5)", depth, i, e.Note)
				}
				if i > 0 && l.Guard <= e.Guards[i-1].Guard {
					t.Fatalf("depth %d: the chain on %s is not outermost-first (§5.9)", depth, e.Note)
				}
			}
			// AND THE POOL HOLDS EXACTLY THAT CHAIN where the entry points.
			at := e.GuardsAt / ir.TableFixedGuardBytes
			for i, l := range e.Guards {
				if pool[at+i] != l {
					t.Fatalf("depth %d: the pool at %d is not %s's link %d (§5.9)", depth, at+i, e.Note, i)
				}
			}
			if len(e.Guards) > deepest {
				deepest = len(e.Guards)
			}
		}
		if deepest != depth {
			t.Fatalf("depth %d: the deepest chain is %d links; a union per level is owed one each (§5.9)", depth, deepest)
		}
		if guarded == len(plan) {
			t.Fatalf("depth %d: a type with a union has guarded entries (§3.4)", depth)
		}
	}

	// THE TOWER'S OWN ARITHMETIC, at depth three: the innermost arm's entries
	// carry the three tags outside-in, and every one of them is a DIFFERENT
	// byte of the record. This is the case the two-lane shape refused by name.
	u := unitFromSource(t, nestedSource(3))
	plan, _, _ := ir.TableFixedBuildPlan(u, u.Tables["Root"])
	three := 0
	for _, e := range plan {
		if len(e.Guards) == 3 {
			three++
		}
	}
	if three == 0 {
		t.Fatal("depth 3: not one entry carries three conditions (§5.9)")
	}

	// AND THE EMITTED TWINS CARRY THE CHAIN, not a lane per level.
	for leg, h := range bothFixedRuntimes(t, nestedSource(3)) {
		if strings.Contains(h, "guard2") || strings.Contains(h, "argw2") {
			t.Errorf("%s: a second guard LANE is still emitted; the chain replaced it (§5.9)", leg)
		}
		if !strings.Contains(h, "uint8_t gcount") {
			t.Errorf("%s: the entry does not carry a chain length (§5.9)", leg)
		}
		if !strings.Contains(h, "g < p.gcount") && !strings.Contains(h, "g < p->gcount") {
			t.Errorf("%s: the run does not test the chain in order (§5.9)", leg)
		}
	}
}

// ROW 14 (§5.2): the enum remap table is as long as the WRITER's variant count.
// Capped at 255 it answered `None` for the writer's 256th variant and beyond as
// though the reader had not named them — a silent wrong value, not a refusal.
func TestFixedEnumRemapIsTheWritersVariantCount(t *testing.T) {
	var b strings.Builder
	b.WriteString("package probe\n\nenum Many {\n")
	for i := range 300 {
		fmt.Fprintf(&b, "    v%d,\n", i)
	}
	b.WriteString("}\n\nfixed table Root { pick Many = v0 }\n")
	for leg, h := range bothFixedRuntimes(t, b.String()) {
		if !strings.Contains(h, "const uint32_t n = te.children;") {
			t.Errorf("%s: the remap's length is not the writer's variant count (§5.8 row 14)", leg)
		}
		if strings.Contains(h, "te.children < 255u ? te.children : 255u") || strings.Contains(h, "remap[256]") {
			t.Errorf("%s: the remap is still capped at 255 entries (§5.8 row 14)", leg)
		}
	}
}
