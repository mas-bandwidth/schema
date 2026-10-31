package ctable

// TestFixedRoadmapCT08WritesConformance is the c leg's card
// schema-c-t08-writes-conformance, rows union-guards, plan-execution,
// interoperability and hostile-input of docs/roadmap.sexp's node
// `fixed-tables`. One subtest per task, named for the task id.
//
// The laws are docs/FIXED-FORM-ALGORITHM.md §4.1 (the plan entry, its guard
// lanes and the partition), §4.2 (identity and compiled plans run by one loop),
// §5.9 #12 (the hash is the shared function's), §5.9 #32/#37 (the manifest is
// the value oracle), §7 items 4 and 5 (the wrong plan goes red; the byte-flip
// fuzz under a sanitizer) and docs/SPEC-TABLES.md §3.4 (the fixed form).
//
// EVERY READ-SIDE CLAUSE IS ASSERTED ON BOTH PLANS AT THE PRODUCTION SITE. The
// probe generates the unit with a lineage entry that is the unit's OWN layout
// under another hash (cT08Probe), so `<snake>_fixed_load` over a file stamped
// with that hash runs the plan the generated lineage build COMPILED from the
// lock's bytes — the compiled path, with nothing else moving — and over the
// file as saved runs the baked identity plan.

import (
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapCT08WritesConformance(t *testing.T) {
	t.Parallel()
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"c/W7", cT08ArgAndMetaTwoLanes},
		{"c/W16", cT08NestedUnionAnswersOuterTag},
		{"c/R28", cT08GuardChain},
		{"c/W6", cT08PlanPartitionSplit},
		{"c/P1", cT08IdentityEqualsCompiled},
		{"c/W13", cT08LayoutHashEqualsReference},
		{"c/P2", cT08WriteReadWriteIdentical},
		{"c/R27", cT08ManifestIsTheWire},
		{"c/P3", cT08HostileByteSweep},
		{"c/P4", cT08WrongPlanGoesRed},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// cT08OtherHash is the mask a probe's second lineage entry is stamped with: the
// unit's own layout bytes under a hash that is not this build's, so a file
// carrying it selects the lineage entry and its COMPILED plan (§4.2: "For any
// other hash the SAME loop runs over a plan compiled once from the writer's
// layout").
const cT08OtherHash = 0x00ff00ff00ff00ff

// cT08Probe generates the C for one schema — the type headers, the table
// header with a lineage of two (this build's layout under its own hash, and the
// SAME layout under another hash) — compiles `source` beside it and runs it. The
// source sees T08_OTHER_HASH, T08_REHASH(file) (stamp record 0's file with the
// other hash) and T08_BODY(file) (record 0's body), all for table `root`.
func cT08Probe(t *testing.T, schema, root, source string, extraCC ...string) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Fatalf("the generated C needs a compiler: %v", err)
	}
	u := unitFrom(t, schema)
	self, ok := FixedLineageOf(u, root)
	if !ok {
		t.Fatalf("no lineage entry for %s", root)
	}
	other := self.Wire ^ cT08OtherHash
	lineage := map[string][]FixedLineageEntry{root: {{Wire: other, Layout: self.Layout, Record: self.Record}}}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snake := ir.RustSnake(root)
	prelude := fmt.Sprintf(`#define T08_OTHER_HASH 0x%016xull
#define T08_BODY( F ) ( (F) + kTableFixedHeaderBytes + 4 + %[2]s_fixed_layout_bytes + 8 )
#define T08_REHASH( F ) do { table_fixed_put64( (F) + kTableFixedHashAt, T08_OTHER_HASH ); \
    table_fixed_put64( (F) + kTableFixedHeaderBytes + 4 + %[2]s_fixed_layout_bytes, T08_OTHER_HASH ); } while ( 0 )
`, other, snake)
	at := strings.Index(source, "\nint main")
	if at < 0 {
		t.Fatal("the probe source has no main")
	}
	source = source[:at+1] + prelude + source[at+1:]
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, extraCC...)
	args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
	for name := range files {
		if strings.HasSuffix(name, ".c") && !strings.Contains(name, "/") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if output, err := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("execute: %v\n%s", err, output)
	}
}

// cT08Header is the one <Base>Table.h the C leg emits for a schema.
func cT08Header(t *testing.T, schema string) string {
	t.Helper()
	return t03Header(t, unitFrom(t, schema))
}

// cT08PlanRows reads the identity plan `<snake>_fixed_plan[]` the C header
// PUBLISHES, one slice of comma-separated fields per entry, and its split
// `<snake>_fixed_plan_guarded`.
func cT08PlanRows(t *testing.T, header, snake string) (rows [][]string, guarded int) {
	t.Helper()
	marker := "TableFixedEntry " + snake + "_fixed_plan[] = {\n"
	at := strings.Index(header, marker)
	if at < 0 {
		t.Fatalf("the header publishes no %s_fixed_plan", snake)
	}
	body := header[at+len(marker):]
	end := strings.Index(body, "};")
	if end < 0 {
		t.Fatalf("the header's %s_fixed_plan has no closing brace", snake)
	}
	body = body[:end]
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = line[strings.Index(line, "{")+1 : strings.Index(line, "}")]
		fields := strings.Split(line, ",")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		rows = append(rows, fields)
	}
	gm := snake + "_fixed_plan_guarded = "
	g := strings.Index(header, gm)
	if g < 0 {
		t.Fatalf("the header publishes no %s_fixed_plan_guarded", snake)
	}
	rest := header[g+len(gm):]
	semi := strings.Index(rest, ";")
	if semi < 0 {
		t.Fatalf("the header's %s_fixed_plan_guarded has no terminating semicolon", snake)
	}
	n, err := strconv.Atoi(rest[:semi])
	if err != nil {
		t.Fatal(err)
	}
	return rows, n
}

// cT08UnionSchema is a union whose second arm carries a text field, so a leaf
// under that arm carries BOTH a guard's tag (arg) and the text op's flavour
// (meta) at once.
const cT08UnionSchema = `package probe
type Payload { label string(8) = "" }
type Other { n int32 = 0 }
union Pick {
    q Other
    p Payload
}
fixed table Root { pick Pick }
`

// c/W7 — "arg and meta are two lanes". FIXED-FORM-ALGORITHM §4.1: "`arg` and
// `meta` ARE TWO LANES BECAUSE THEY ARE TWO FACTS, and must never share one. A
// `string(N)` under a union arm needs both, and one lane gives whichever was
// stamped last: a text string under arm `3` read as BYTES, or an entry that
// runs only when the tag equals the flavour — under the wrong arm." The text field
// under arm 2 is the one entry where both lanes are live. The test asserts the
// PUBLISHED identity row carries arg 2 and meta 1 (utf8), the plan COMPILED by
// the runtime carries the same two, and the text lands whole on both plans.
func cT08ArgAndMetaTwoLanes(t *testing.T) {
	header := cT08Header(t, cT08UnionSchema)
	rows, _ := cT08PlanRows(t, header, "root")
	var text []string
	for _, r := range rows {
		if r[5] == "kTableFixedText" {
			text = r
		}
	}
	if text == nil {
		t.Fatal("the published identity plan lost the arm's text op")
	}
	if text[4] == "SCHEMA_TABLE_FIXED_NO_GUARD" {
		t.Fatal("the text field under a union arm is unguarded: the guard lane was dropped")
	}
	if text[6] != "2" {
		t.Fatalf("arg is the guard's tag and nothing else: want the second arm's ordinal 2, the published row says %s", text[6])
	}
	if text[7] != "1" {
		t.Fatalf("meta is the op's own flavour: want utf8 1, the published row says %s", text[7])
	}
	cT08Probe(t, cT08UnionSchema, "Root", `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    static TableFixedEntry plan[4096];
    static TableFixedEntry compiled[4096];
    TableFixedLayoutView parsed;
    TableReport r;
    Root value, back;
    uint8_t wire[512], file[512];
    int64_t need;
    int32_t made, guarded = 0, fill_count = 0, i, texts = 0;
    uint32_t fill_at = 0;
    int why = SCHEMA_TABLE_LAYOUT_MALFORMED, path;
    /* THE COMPILED PLAN carries the same two facts on the same two lanes. */
    CHECK(table_fixed_parse_layout(root_fixed_layout, root_fixed_layout_bytes, &parsed, &why));
    memset(&r, 0, sizeof(r));
    made = table_fixed_compile(&parsed, root_fixed_layout, (int32_t) root_fixed_layout_bytes, root_fixed_dst,
                               root_fixed_cover, root_fixed_cover_count, compiled, 4096, &guarded, &fill_at, &fill_count, &r);
    CHECK(made > 0);
    for (i = 0; i < made; ++i)
    {
        if (compiled[i].op == kTableFixedText)
        {
            ++texts;
            if (compiled[i].arg != 2 || compiled[i].meta != kTableFixedTextUtf8)
                { fprintf(stderr, "the compiled text entry shares a lane: arg=%d meta=%d\n", (int) compiled[i].arg, (int) compiled[i].meta); return 1; }
        }
    }
    CHECK(texts == 1);
    /* AND THE TEXT LANDS WHOLE UNDER ARM 2 on both plans. */
    memset(&value, 0, sizeof(value));
    value.pick.type = 2;
    memcpy(value.pick.as.p.label, "arm2", 5);
    value.pick.as.p.label_length = 4;
    need = root_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t) sizeof(wire));
    CHECK(root_fixed_save(&value, 1, wire, (int64_t) sizeof(wire)) == need);
    for (path = 0; path < 2; ++path)
    {
        memcpy(file, wire, (size_t) need);
        if (path == 1) { T08_REHASH(file); }
        memset(&back, 0x5A, sizeof(back));
        memset(&r, 0, sizeof(r));
        CHECK(root_fixed_load_tolerant(&back, 1, file, need, plan, 4096, NULL, &r) == 1);
        CHECK(!r.refused && !r.malformed && r.clamped == 0);
        CHECK(back.pick.type == 2);
        CHECK(back.pick.as.p.label_length == 4 && memcmp(back.pick.as.p.label, "arm2", 5) == 0);
    }
    return 0;
}
`)
}

// cT08NestedUnionSchema nests a union inside an arm of an outer union, so the
// inner arm's leaves carry both tags: the inner one on guard/arg and the outer
// one on guard2/arg2.
const cT08NestedUnionSchema = `package probe
type InnerA { x int32 = 0 }
type InnerB { n int32 = 0 }
union Inner {
    ia InnerA
    ib InnerB
}
type OuterA { inner Inner }
type OuterB { m int32 = 0 }
union Outer {
    oa OuterA
    ob OuterB
}
fixed table Root { o Outer }
`

// c/W16 [owed] — "nested union answers outer tag". FIXED-FORM-ALGORITHM §4.1:
// "`guard2` / `arg2` / `argw2` ARE THE OUTER TAG AN ARM INSIDE AN ARM ALSO
// ANSWERS TO, CONJOINED with `guard`/`arg`: the entry runs when BOTH tags hold
// their ordinal. ... the inner tag alone landed an inner arm's bytes beneath an
// outer arm that never rode." The identity entry carries both lanes at their
// own offsets; then a record whose OUTER tag names the other arm — the inner
// tag's byte still saying arm 1 — must land the outer arm and never the inner
// one, on the identity plan and on the compiled one.
func cT08NestedUnionAnswersOuterTag(t *testing.T) {
	u := unitFrom(t, cT08NestedUnionSchema)
	plan, _, _ := ir.TableFixedBuildPlanNormalisedWithPool(u, u.Tables["Root"])
	var nested *ir.TableFixedLeaf
	for i := range plan {
		if plan[i].Guard != ir.TableFixedNoGuard && plan[i].Guard2 != ir.TableFixedNoGuard {
			nested = &plan[i]
			break
		}
	}
	if nested == nil {
		t.Fatal("no entry answers to two tags: the outer tag was stamped over the inner one")
	}
	if nested.Arg != 1 || nested.Arg2 != 1 || nested.ArgW == 0 || nested.ArgW2 == 0 {
		t.Fatalf("each tag holds its own ordinal at its own width: arg=%d/%d arg2=%d/%d", nested.Arg, nested.ArgW, nested.Arg2, nested.ArgW2)
	}
	if nested.Guard2 >= nested.Guard {
		t.Fatalf("the outer tag sits before the inner one in the record: guard2=%d guard=%d", nested.Guard2, nested.Guard)
	}
	cT08Probe(t, cT08NestedUnionSchema, "Root", fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %%d: %%s\n", __LINE__, #x); return 1; } } while (0)
#define OUTER_AT %d
int main(void)
{
    static TableFixedEntry plan[4096];
    TableReport r;
    Root value, back;
    uint8_t wire[512], file[512];
    int64_t need;
    int path, poison;
    memset(&value, 0, sizeof(value));
    value.o.type = 1;
    value.o.as.oa.inner.type = 1;
    value.o.as.oa.inner.as.ia.x = 42;
    need = root_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t) sizeof(wire));
    CHECK(root_fixed_save(&value, 1, wire, (int64_t) sizeof(wire)) == need);
    CHECK(T08_BODY(wire)[OUTER_AT] == 1); /* the WRITER puts the outer tag there */
    for (path = 0; path < 2; ++path)
    {
        for (poison = 0; poison < 2; ++poison)
        {
            memcpy(file, wire, (size_t) need);
            if (path == 1) { T08_REHASH(file); }
            if (poison == 1) { T08_BODY(file)[OUTER_AT] = 2; }
            memset(&back, 0xAA, sizeof(back));
            memset(&r, 0, sizeof(r));
            CHECK(root_fixed_load_tolerant(&back, 1, file, need, plan, 4096, NULL, &r) == 1);
            CHECK(!r.refused && !r.malformed);
            if (poison == 0)
            {
                CHECK(back.o.type == 1 && back.o.as.oa.inner.type == 1 && back.o.as.oa.inner.as.ia.x == 42);
            }
            else
            {
                CHECK(back.o.type == 2);
                if (back.o.as.oa.inner.as.ia.x == 42)
                    { fprintf(stderr, "path %%d: the inner arm landed beneath an outer arm that never rode\n", path); return 1; }
            }
        }
    }
    return 0;
}
`, nested.Guard2))
}

// cT08TripleUnionSchema nests THREE unions on one path to a leaf: an arm of
// OuterMost holds Out, whose arm holds Mid, whose arm holds LeafA. The deepest
// leaf answers to all three tags, which two inline lanes cannot carry — the
// guard chain carries it as offset+count into a pool of links.
const cT08TripleUnionSchema = `package probe
type LeafA { x int32 = 0 }
type LeafB { y int32 = 0 }
union Inner {
    a LeafA
    b LeafB
}
type Mid { i Inner }
type MidB { z int32 = 0 }
union Middle {
    m Mid
    n MidB
}
type Out { mm Middle }
type OutB { w int32 = 0 }
union OuterMost {
    o Out
    p OutB
}
fixed table Root { oo OuterMost }
`

// cT08DeepUnionSchema lays N unions on one path to a leaf: S0's x sits under
// U0's arm a, which sits under U1's arm a, and so on out to U{N-1}; every union
// has a second arm b, so a tag can name the other arm. Each union adds two
// levels to the layout's depth, so N = 31 is the deepest chain the layout's
// 64-entry depth bound carries and N = 32 is past it.
func cT08DeepUnionSchema(n int) string {
	var b strings.Builder
	b.WriteString("package probe\n")
	b.WriteString("type S0 { x int32 = 0 }\n")
	b.WriteString("type Sb { y int32 = 0 }\n")
	for i := range n {
		fmt.Fprintf(&b, "union U%d {\n    a S%d\n    b Sb\n}\n", i, i)
		fmt.Fprintf(&b, "type S%d { u U%d }\n", i+1, i)
	}
	fmt.Fprintf(&b, "fixed table Root { u U%d }\n", n-1)
	return b.String()
}

// cT08UnionChainSchema lays N unions on one path where each arm's type IS the
// next union in (one level a union): U0's arm a holds S0's x, U{i}'s arm a is
// U{i-1}, and the outermost union's arm b is a SECOND nested path (V1, whose
// arm a is the union V0), so the outermost union restamps two paths at once.
func cT08UnionChainSchema(n int) string {
	var b strings.Builder
	b.WriteString("package probe\n")
	b.WriteString("type S0 { x int32 = 0 }\n")
	b.WriteString("type Sb { y int32 = 0 }\n")
	b.WriteString("type Sc { z int32 = 0 }\n")
	b.WriteString("union V0 {\n    a Sb\n    b Sc\n}\n")
	b.WriteString("union V1 {\n    a V0\n    b Sc\n}\n")
	b.WriteString("union U0 {\n    a S0\n    b Sb\n}\n")
	for i := 1; i < n; i++ {
		second := "Sb"
		if i == n-1 {
			second = "V1"
		}
		fmt.Fprintf(&b, "union U%d {\n    a U%d\n    b %s\n}\n", i, i-1, second)
	}
	// A UNION WHOSE ARM IS A UNION IS A TABLE-CLOSURE CONSTRUCT (SPEC-TABLES
	// §2.6): every union on the path is held in the table's body too.
	fmt.Fprintf(&b, "fixed table Root {\n    u U%d\n", n-1)
	for i := 0; i < n-1; i++ {
		fmt.Fprintf(&b, "    r%d U%d\n", i, i)
	}
	b.WriteString("    s V1\n}\n")
	return b.String()
}

// cT08ChainProbe is the C that proves a chain on both plans: the record as
// written lands the deepest leaf; with the OUTERMOST tag naming the other arm
// (a condition only the chain's first link carries) and with the next tag in
// naming the other arm, the leaf must NOT land. `set` writes the value, `leaf`
// is the deepest leaf's lvalue, `outer2` the outermost tag's lvalue.
func cT08ChainProbe(set, leaf, outerTag, nextTag string, outerAt, nextAt int64) string {
	return fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %%d: %%s\n", __LINE__, #x); return 1; } } while (0)
#define OUTER_AT %d
#define NEXT_AT %d
static Root value, back;
static uint8_t wire[1 << 22], file[1 << 22];
int main(void)
{
    static TableFixedEntry plan[1 << 16];
    TableReport r;
    int64_t need;
    int path, poison;
    memset(&value, 0, sizeof(value));
%s
    need = root_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t) sizeof(wire));
    CHECK(root_fixed_save(&value, 1, wire, (int64_t) sizeof(wire)) == need);
    /* THE WRITER puts the two tags where the chain's links say they are. */
    CHECK(T08_BODY(wire)[OUTER_AT] == 1 && T08_BODY(wire)[NEXT_AT] == 1);
    for (path = 0; path < 2; ++path)
    {
        for (poison = 0; poison < 3; ++poison)
        {
            memcpy(file, wire, (size_t) need);
            if (path == 1) { T08_REHASH(file); }
            if (poison == 1) { T08_BODY(file)[OUTER_AT] = 2; }
            if (poison == 2) { T08_BODY(file)[NEXT_AT] = 2; }
            memset(&back, 0xAA, sizeof(back));
            memset(&r, 0, sizeof(r));
            if (path == 1)
            {
                /* THE COMPILED PLAN REFUSES A THIRD NESTED UNION BY NAME: the
                   identity plan carries the chain, a stranger's two-lane plan
                   does not, and the reference's name for it is
                   layout_record_too_large (§5.8 row 6). */
                if (root_fixed_load_tolerant(&back, 1, file, need, plan, 1 << 16, NULL, &r) != -1 ||
                    !r.refused || r.malformed || r.reason != SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE)
                    { fprintf(stderr, "path %%d poison %%d: the compiled plan did not refuse the third nested union by name (refused=%%d reason=%%d)\n", path, poison, (int) r.refused, (int) r.reason); return 1; }
                continue;
            }
            if (root_fixed_load_tolerant(&back, 1, file, need, plan, 1 << 16, NULL, &r) != 1)
                { fprintf(stderr, "path %%d poison %%d: the read refused (reason %%d): the chain is bounded by something other than the layout's 64\n", path, poison, (int) r.reason); return 1; }
            CHECK(!r.refused && !r.malformed);
            if (poison == 0)
            {
                if (%s != 42)
                    { fprintf(stderr, "path %%d: the record as written did not land its deepest leaf\n", path); return 1; }
            }
            else
            {
                if (poison == 1) { CHECK(%s == 2); }
                if (poison == 2) { CHECK(%s == 1 && %s == 2); }
                if (%s == 42)
                    { fprintf(stderr, "path %%d poison %%d: the deepest leaf landed while %%s tag names another arm: the chain's link was not executed\n", path, poison, poison == 1 ? "the OUTERMOST" : "the next"); return 1; }
            }
        }
    }
    return 0;
}
`, outerAt, nextAt, set, leaf, outerTag, outerTag, nextTag, leaf)
}

// c/R28 [owed] — "the guard chain: the plan entry carries offset+count into a
// pool of (guard, arg, argw) links, outermost first, the only bound the
// layout's 64; an arm inside an arm answers to the OUTER tag" (docs/roadmap.sexp
// c/R28, FIXED-FORM-ALGORITHM §4.1: "A union's guard is stamped over everything
// its arm produced — an arm inside an arm answers to the OUTER tag").
//
// The subtest asserts, in order: the deepest leaf of a THREE-union chain
// carries three links OUTERMOST FIRST at offset+count into the pool; the C
// header publishes that pool; the generated C reads the three-armed value whole
// on the IDENTITY plan and refuses to land the leaf when the outermost tag (a
// condition only the chain carries) or the next one names another arm; and the
// COMPILED plan — a stranger's layout, walked two lanes at a time — refuses the
// third nested union by name (layout_record_too_large, the reference's name).
// Then the layout's 64 as the ONLY bound: 31 nested unions (the deepest inside
// the 64-entry depth bound) build a 31-link chain the identity plan runs, and
// 32 are refused BY THE DEPTH BOUND — the fixed form is not emitted and the
// compiler names the table — and not by the chain.
func cT08GuardChain(t *testing.T) {
	u := unitFrom(t, cT08TripleUnionSchema)
	plan, _, pool := ir.TableFixedBuildPlanNormalisedWithPool(u, u.Tables["Root"])
	var leaf *ir.TableFixedLeaf
	for i := range plan {
		if plan[i].GuardCount == 3 {
			leaf = &plan[i]
			break
		}
	}
	if leaf == nil {
		t.Fatal("no entry carries three guard links: a third nested union is still refused")
	}
	if leaf.GuardIndex < 0 || leaf.GuardIndex+leaf.GuardCount > len(pool) {
		t.Fatalf("the entry's offset+count point outside the pool: index=%d count=%d pool=%d", leaf.GuardIndex, leaf.GuardCount, len(pool))
	}
	links := pool[leaf.GuardIndex : leaf.GuardIndex+leaf.GuardCount]
	if links[0].Guard >= links[1].Guard || links[1].Guard >= links[2].Guard {
		t.Fatalf("the links are not OUTERMOST FIRST: guards %d, %d, %d", links[0].Guard, links[1].Guard, links[2].Guard)
	}
	for i, l := range links {
		if l.Arg != 1 || l.ArgW != 1 {
			t.Fatalf("link %d is (guard, arg 1, argw 1) for arm 1 of a two-arm union: arg=%d argw=%d", i, l.Arg, l.ArgW)
		}
	}
	header := cT08Header(t, cT08TripleUnionSchema)
	if !strings.Contains(header, fmt.Sprintf("    { %du, %du, %d },\n", links[0].Guard, links[0].Arg, links[0].ArgW)) ||
		!strings.Contains(header, "root_fixed_guards[] = {") {
		t.Fatal("the header does not publish the pool with the outermost link")
	}
	if !strings.Contains(header, fmt.Sprintf(", 3, %d * (int) sizeof( TableFixedGuardLink ) },", leaf.GuardIndex)) {
		t.Fatal("no published plan entry carries count 3 at the chain's offset")
	}
	// The C++ reference now carries the same identity-plan guard chain rather
	// than consuming the two-lane plan. Assert the emitted entry and pool both
	// name the OUTERMOST link, or the deepest leaf could run under a false tag.
	cpp, err := cpptable.Generate(unitFrom(t, cT08TripleUnionSchema))
	if err != nil {
		t.Fatal(err)
	}
	cppHeader := string(cpp["ProbeTable.h"])
	if !strings.Contains(cppHeader, "constexpr TableFixedGuardLink RootFixedGuards[] = {") ||
		!strings.Contains(cppHeader, fmt.Sprintf("    { %du, %du, %d },\n", links[0].Guard, links[0].Arg, links[0].ArgW)) ||
		!strings.Contains(cppHeader, fmt.Sprintf(", 3, %d * (int) sizeof( TableFixedGuardLink ) },", leaf.GuardIndex)) ||
		!strings.Contains(cppHeader, "report, RootFixedGuards );") {
		t.Fatal("the C++ reference does not pass the three-link identity plan through its guard pool")
	}
	cT08Probe(t, cT08TripleUnionSchema, "Root", cT08ChainProbe(`    value.oo.type = 1;
    value.oo.as.o.mm.type = 1;
    value.oo.as.o.mm.as.m.i.type = 1;
    value.oo.as.o.mm.as.m.i.as.a.x = 42;`,
		"back.oo.as.o.mm.as.m.i.as.a.x", "back.oo.type", "back.oo.as.o.mm.type", links[0].Guard, links[1].Guard))

	// THE ONLY BOUND IS THE LAYOUT'S 64.
	const deepest = 31
	deep := unitFrom(t, cT08DeepUnionSchema(deepest))
	if !ir.TableFixedWithinDepth(deep.Tables["Root"]) {
		t.Fatalf("%d nested unions are inside the 64-entry depth bound", deepest)
	}
	deepPlan, _, deepPool := ir.TableFixedBuildPlanNormalisedWithPool(deep, deep.Tables["Root"])
	var chain *ir.TableFixedLeaf
	for i := range deepPlan {
		if deepPlan[i].GuardCount == deepest {
			chain = &deepPlan[i]
			break
		}
	}
	if chain == nil || chain.GuardIndex+chain.GuardCount > len(deepPool) {
		t.Fatalf("%d nested unions do not build a %d-link chain inside its pool", deepest, deepest)
	}
	deepLinks := deepPool[chain.GuardIndex : chain.GuardIndex+chain.GuardCount]
	var set strings.Builder
	path := "value.u"
	for i := range deepest {
		fmt.Fprintf(&set, "    %s.type = 1;\n", path)
		path += ".as.a"
		if i < deepest-1 {
			path += ".u"
		}
	}
	fmt.Fprintf(&set, "    %s.x = 42;", path)
	cT08Probe(t, cT08DeepUnionSchema(deepest), "Root", cT08ChainProbe(set.String(),
		"back"+strings.TrimPrefix(path, "value")+".x", "back.u.type", "back.u.as.a.u.type", deepLinks[0].Guard, deepLinks[1].Guard))

	// THE EDGE: an arm whose type is a union costs one level a union, so the
	// longest chain the fixed form emits is found by walking N down from past
	// the bound, and its outermost union carries a SECOND nested path, so the
	// outermost restamp reads an already-restamped chain of full length while it
	// lays the second (the case the restamp's link buffers are sized for). Run
	// on both plans under the sanitizer.
	edge := 0
	for n := ir.TableFixedMaxDepth + 4; n >= 3; n-- {
		eu := unitFrom(t, cT08UnionChainSchema(n))
		if ir.TableFixedEmitted(eu, eu.Tables["Root"]) {
			edge = n
			break
		}
	}
	if edge < 3 {
		t.Fatal("no union-of-union chain emits the fixed form")
	}
	eu := unitFrom(t, cT08UnionChainSchema(edge))
	edgePlan, _, edgePool := ir.TableFixedBuildPlanNormalisedWithPool(eu, eu.Tables["Root"])
	var edgeLeaf *ir.TableFixedLeaf
	for i := range edgePlan {
		if edgePlan[i].GuardCount == edge {
			edgeLeaf = &edgePlan[i]
		}
	}
	if edgeLeaf == nil {
		t.Fatalf("the %d-union chain does not build a %d-link chain", edge, edge)
	}
	t.Logf("the longest union-of-union chain the fixed form emits is %d links (the layout's bound is %d)", edge, ir.TableFixedMaxDepth)
	edgeLinks := edgePool[edgeLeaf.GuardIndex : edgeLeaf.GuardIndex+edgeLeaf.GuardCount]
	var edgeSet strings.Builder
	edgePath := "value.u"
	for i := 0; i < edge; i++ {
		fmt.Fprintf(&edgeSet, "    %s.type = 1;\n", edgePath)
		edgePath += ".as.a"
	}
	fmt.Fprintf(&edgeSet, "    %s.x = 42;", edgePath)
	// THE EDGE PROBE IS THE SLOW TIER. The chain above is built and checked in
	// the IR on every run; compiling it is not cheap: the edge chain's
	// generated header is about 17 MB, and the sanitized -O2 compile of it was
	// 18.7 s of this row's 22 s on the studio (2026-10-22) and minutes on a
	// two-core runner. The three-union and 31-deep probes above stay in the
	// default run; this one runs wherever SCHEMA_SLOW=1 (ci-full's go-test
	// lanes, the nightly and the make leg gates), so no check is lost.
	t.Run("edge-probe", func(t *testing.T) {
		slowtest.Gate(t, "the C compiler with ASan and UBSan (a 17 MB generated header at the chain's edge)")
		cT08Probe(t, cT08UnionChainSchema(edge), "Root", cT08ChainProbe(edgeSet.String(),
			"back"+strings.TrimPrefix(edgePath, "value")+".x", "back.u.type", "back.u.as.a.type", edgeLinks[0].Guard, edgeLinks[1].Guard),
			"-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-fno-omit-frame-pointer", "-g")
	})

	past := unitFrom(t, cT08DeepUnionSchema(deepest+1))
	if ir.TableFixedWithinDepth(past.Tables["Root"]) {
		t.Fatalf("%d nested unions are past the 64-entry depth bound", deepest+1)
	}
	if strings.Contains(t03Header(t, past), "root_fixed_load_tolerant(") {
		t.Fatalf("%d nested unions: the fixed form is emitted past the depth bound", deepest+1)
	}
	named := false
	for _, w := range ir.TableFixedDepthBounds(past) {
		named = named || strings.Contains(w, "table Root:")
	}
	if !named {
		t.Fatalf("%d nested unions: the depth bound turned the table away without naming it", deepest+1)
	}
}

// c/W6 [owed] — "plan partition / split". FIXED-FORM-ALGORITHM §4.1: "The plan
// is PARTITIONED: every unguarded entry first, then every guarded one, and the
// plan states where the second half starts", and §4.2: "Coalesce inside each
// half, never across the split." The test reads the PUBLISHED identity plan and
// its split, and the plan the runtime COMPILES, and asserts on both that every
// entry before the split is unguarded, every entry from it on is guarded, and
// both halves are non-empty for a schema that has both.
func cT08PlanPartitionSplit(t *testing.T) {
	rows, guarded := cT08PlanRows(t, cT08Header(t, cT08NestedUnionSchema), "root")
	if guarded <= 0 || guarded >= len(rows) {
		t.Fatalf("the published partition has an empty half: guarded=%d of %d", guarded, len(rows))
	}
	for i, r := range rows {
		unguarded := r[4] == "SCHEMA_TABLE_FIXED_NO_GUARD"
		if (i < guarded) != unguarded {
			t.Fatalf("published entry %d is on the wrong side of the split %d: guard %s", i, guarded, r[4])
		}
	}
	cT08Probe(t, cT08NestedUnionSchema, "Root", `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    static TableFixedEntry compiled[4096];
    TableFixedLayoutView parsed;
    TableReport r;
    int32_t made, guarded = -1, fill_count = 0, i;
    uint32_t fill_at = 0;
    int why = SCHEMA_TABLE_LAYOUT_MALFORMED;
    CHECK(table_fixed_parse_layout(root_fixed_layout, root_fixed_layout_bytes, &parsed, &why));
    memset(&r, 0, sizeof(r));
    made = table_fixed_compile(&parsed, root_fixed_layout, (int32_t) root_fixed_layout_bytes, root_fixed_dst,
                               root_fixed_cover, root_fixed_cover_count, compiled, 4096, &guarded, &fill_at, &fill_count, &r);
    CHECK(made > 0);
    CHECK(guarded > 0 && guarded < made);
    for (i = 0; i < made; ++i)
    {
        if ((i < guarded) != (compiled[i].guard == SCHEMA_TABLE_FIXED_NO_GUARD))
            { fprintf(stderr, "compiled entry %d is on the wrong side of the split %d\n", i, guarded); return 1; }
    }
    return 0;
}
`)
}

// c/P1 [owed] — "identity == compiled". FIXED-FORM-ALGORITHM §4.2: "For any
// other hash the SAME loop runs over a plan compiled once from the writer's
// layout"; SPEC-TABLES §3.4 holds the two plans to one answer. A plan compiled
// from this build's OWN layout is the compiled path with nothing else moving,
// so the identity read and the compiled read of the same record must leave the
// destination byte for byte the same. The probe reads each value — both arms of
// a union and a text field — through `_fixed_load` once under this build's hash
// (the identity plan) and once under the lineage entry's (the compiled plan),
// into destinations stained alike, and memcmps them; the header shows the load
// is the one table_fixed_run loop. (The three-union chain is out of this row's
// reach: a stranger's compiled plan refuses it by name, which c/R28 asserts.)
func cT08IdentityEqualsCompiled(t *testing.T) {
	header := cT08Header(t, cT08UnionSchema)
	if !strings.Contains(header, "table_fixed_run( entries, entry_count, entry_guarded,") {
		t.Fatal("the load is not the one loop over whichever plan the hash selected")
	}
	same := `    for (k = 0; k < cases; ++k)
    {
        uint8_t first[1024], second[1024];
        Root a, b;
        memset(&value, 0, sizeof(value));
        set(&value, k);
        need = root_fixed_measure(1);
        CHECK(need > 0 && need <= (int64_t) sizeof(first));
        CHECK(root_fixed_save(&value, 1, first, (int64_t) sizeof(first)) == need);
        memcpy(second, first, (size_t) need);
        T08_REHASH(second);
        memset(&a, 0x5A, sizeof(a));
        memset(&b, 0x5A, sizeof(b));
        memset(&r, 0, sizeof(r));
        CHECK(root_fixed_load_tolerant(&a, 1, first, need, plan, 4096, NULL, &r) == 1 && !r.refused);
        memset(&r, 0, sizeof(r));
        CHECK(root_fixed_load_tolerant(&b, 1, second, need, plan, 4096, NULL, &r) == 1 && !r.refused);
        if (memcmp(&a, &b, sizeof(a)) != 0)
            { fprintf(stderr, "case %d: the compiled plan's read differs from the identity plan's\n", k); return 1; }
    }
    return 0;
}
`
	cT08Probe(t, cT08UnionSchema, "Root", `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
static void set(Root * v, int k)
{
    if (k == 0) { v->pick.type = 1; v->pick.as.q.n = -7; }
    if (k == 1) { v->pick.type = 2; memcpy(v->pick.as.p.label, "eightch", 8); v->pick.as.p.label_length = 7; }
}
int main(void)
{
    static TableFixedEntry plan[4096];
    TableReport r;
    Root value;
    int64_t need;
    int k, cases = 2;
`+same)
}

// cT08LayoutSchema is W13's probe beside test/tables/FX1.schema: a
// ranged scalar so the hash folds the definitions digest, beside two plain
// scalars.
const cT08LayoutSchema = `package probe
fixed table Root {
    lead uint32 = 1
    v    int32 = 0 | min = 0, max = 100
    tail uint32 = 2
}
`

// cT08Bytes reads a published `{ 0x.., ... }` byte array that follows marker.
func cT08Bytes(t *testing.T, text, marker string) []byte {
	t.Helper()
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("no %q in the generated source", marker)
	}
	rest := text[at+len(marker):]
	end := strings.Index(rest, "}")
	if end < 0 {
		t.Fatalf("the byte array after %q has no closing brace", marker)
	}
	rest = rest[:end]
	var out []byte
	for f := range strings.SplitSeq(rest, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimPrefix(f, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("byte %q after %q: %v", f, marker, err)
		}
		out = append(out, byte(n))
	}
	return out
}

// cT08Hex reads a published `0x...ull` constant that follows marker.
func cT08Hex(t *testing.T, text, marker string) uint64 {
	t.Helper()
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("no %q in the generated source", marker)
	}
	rest := text[at+len(marker):]
	before, _, ok := strings.Cut(rest, "ull")
	if !ok {
		t.Fatalf("the constant after %q has no ull suffix", marker)
	}
	v, err := strconv.ParseUint(before, 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// c/W13 [verify] — "layout+hash == C++ reference". FIXED-FORM-ALGORITHM §5.9
// #12: "THE HASH IS THE SHARED FUNCTION'S: ir.TableFixedLayoutHash, whose
// signature has no digest argument for a leg to omit." The reference is the C++
// emitter's OWN OUTPUT (internal/codegen/cpptable): for test/tables/FX1.schema —
// the schema test/conformance/c/rows/W13.c names — and the probe schema,
// every fixed table's `<Name>FixedHash` and `<Name>FixedLayout[]` the C++ header
// publishes are read back and asserted equal to the `<snake>_fixed_hash` and
// `<snake>_fixed_layout[]` the C header publishes, byte for byte. Two emitters'
// published constants are compared, not a shared helper's answer with itself.
//
// THE CLAUSE IS THE LAYOUT AND THE HASH, AND ONLY THEM: equal constants say
// nothing about the predicates either runtime applies to a record. Both legs'
// three-union guard chains are checked under c/R28.
func cT08LayoutHashEqualsReference(t *testing.T) {
	sources := []string{cReadSchema(t, "FX1"), cT08LayoutSchema}
	tables := 0
	for _, src := range sources {
		u := unitFrom(t, src)
		ctext := t03Header(t, u)
		ref, err := cpptable.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		var cpp string
		for name, data := range ref {
			if strings.HasSuffix(name, "Table.h") {
				cpp += string(data)
			}
		}
		for _, st := range ir.TableFixedRoots(u) {
			if !ir.TableFixedEmitted(u, st) {
				t.Fatalf("%s: a fixed root the C leg does not emit; this row cannot be compared", st.Name)
			}
			snake := ir.RustSnake(st.Name)
			if c, ref := cT08Hex(t, ctext, snake+"_fixed_hash = 0x"), cT08Hex(t, cpp, st.Name+"FixedHash = 0x"); c != ref {
				t.Fatalf("%s: the C hash 0x%016x is not the C++ reference's 0x%016x", st.Name, c, ref)
			}
			c := cT08Bytes(t, ctext, snake+"_fixed_layout[] = {")
			r := cT08Bytes(t, cpp, st.Name+"FixedLayout[] = {")
			if string(c) != string(r) {
				t.Fatalf("%s: the C layout (%d bytes) is not the C++ reference's (%d bytes)", st.Name, len(c), len(r))
			}
			tables++
		}
	}
	if tables < 3 {
		t.Fatalf("only %d tables compared: the rows are not all here", tables)
	}
}

// c/P2 [owed] — "write-read-write byte-identical". SPEC-TABLES §3.4: a saved
// fixed image read back and saved again is the same bytes. The probe writes a
// value with a non-default count and text, reads it into a stained destination
// on the identity plan and on the compiled plan, writes each again, and
// memcmps every image with the first, byte for byte.
func cT08WriteReadWriteIdentical(t *testing.T) {
	cT08Probe(t, identityHeldSchema, "Held", `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    static TableFixedEntry plan[4096];
    Held value, loaded;
    TableReport report;
    uint8_t first[512], file[512], second[512];
    int64_t need;
    int path;
    memset(&value, 0, sizeof(value));
    value.marks[0] = 7;
    value.marks_count = 1;
    value.label[0] = 'h';
    value.label[1] = 'i';
    value.label_length = 2;
    need = held_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t) sizeof(first));
    CHECK(held_fixed_save(&value, 1, first, (int64_t) sizeof(first)) == need);
    for (path = 0; path < 2; ++path)
    {
        memcpy(file, first, (size_t) need);
        if (path == 1) { T08_REHASH(file); }
        memset(&loaded, 0xAA, sizeof(loaded));
        memset(&report, 0, sizeof(report));
        CHECK(held_fixed_load_tolerant(&loaded, 1, file, need, plan, 4096, NULL, &report) == 1);
        CHECK(!report.malformed && !report.refused && report.clamped == 0);
        CHECK(held_fixed_save(&loaded, 1, second, (int64_t) sizeof(second)) == need);
        if (memcmp(first, second, (size_t) need) != 0)
            { fprintf(stderr, "path %d: write-read-write is not byte-identical\n", path); return 1; }
    }
    return 0;
}
`, identityCCFlags()...)
}

// c/R27 [verify] — "the manifest is the wire". FIXED-FORM-ALGORITHM §5.9 #32:
// "A PORT ASSERTS THE MANIFEST'S VALUES, NEVER READS THE DUMP", and #37: "The
// oracle is `manifest.txt` (#32) and nothing else." The manifest's stated
// lead/trail for old_array_bounded_grow.bin are read off the manifest and
// asserted to BE the bytes on the wire — the record body's first and last four.
// A manifest that went missing, or that drifted from the dump that wrote the
// bytes, fails by name.
func cT08ManifestIsTheWire(t *testing.T) {
	corpus := cFixedCorpus(t)
	lead, trail, ok := cManifestBrackets(t, corpus, "old_array_bounded_grow.bin")
	if !ok {
		t.Fatal("the manifest is the single value oracle: it carries no brackets for old_array_bounded_grow.bin")
	}
	data, err := os.ReadFile(filepath.Join(corpus, "old_array_bounded_grow.bin"))
	if err != nil || len(data) < 20 {
		t.Fatalf("the corpus row is not readable: %v", err)
	}
	root := cFixedRoot(t, cUnitOf(t, cReadSchema(t, "VOLD_array_bounded_grow")))
	bodyBytes := ir.TableFixedTypeBytes(root)
	at := 20 + int(binary.LittleEndian.Uint32(data[16:20])) + 8
	if int64(at)+bodyBytes > int64(len(data)) {
		t.Fatal("the corpus row is shorter than its own record")
	}
	body := data[at : int64(at)+bodyBytes]
	first := binary.LittleEndian.Uint32(body[:4])
	last := binary.LittleEndian.Uint32(body[len(body)-4:])
	if first != lead || last != trail {
		t.Fatalf("the manifest is not the wire: manifest lead=%d trail=%d, the wire's own first=%d last=%d", lead, trail, first, last)
	}
}

// cT08HostileSchema is P3's probe unit: a ranged scalar, a bounded array, an
// optional and a trailing scalar, so the byte-flip sweep forges a count, a
// present byte and a ranged value at once.
const cT08HostileSchema = `package probe
fixed table Host {
    small int32 = 5 | min = 0, max = 1000
    marks [..4]int32 | min = 0, max = 1000
    note ?int32 | min = 0, max = 1000
    tail int32 = 7
}
`

// c/P3 [verify] — "hostile bytes, sweep + sanitizer". FIXED-FORM-ALGORITHM §7
// item 5: "a byte-flip fuzz over the whole file, under a sanitizer, and it is
// not optional — every offset is arithmetic over sizes a stranger wrote down, so
// every byte, one bit at a time, is answered one of three ways and never a
// fourth: a refusal by name, a `malformed` read, or a read that lands values."
// Every byte of the saved FILE (header, layout and record) is flipped one bit
// at a time and also poisoned with six whole values, on the identity plan and
// on the compiled plan; the read must answer one of the three ways, and a read
// that lands values must hold every declared bound. The probe compiles under
// -fsanitize=address,undefined, so a step outside the buffer aborts it.
func cT08HostileByteSweep(t *testing.T) {
	cT08Probe(t, cT08HostileSchema, "Host", `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
static const uint8_t poison[6] = { 0x00, 0x01, 0x02, 0x7f, 0x80, 0xff };
int main(void)
{
    static TableFixedEntry plan[4096];
    Host value, back;
    TableReport r;
    uint8_t clean[4096], base[4096], hit[4096];
    int64_t need, n;
    int at, p, i, path, answered = 0;
    memset(&value, 0, sizeof(value));
    value.small = 5;
    value.marks_count = 2;
    value.marks[0] = 10;
    value.marks[1] = 20;
    value.note_present = 1;
    value.note = 30;
    value.tail = 7;
    need = host_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t) sizeof(clean));
    CHECK(host_fixed_save(&value, 1, clean, (int64_t) sizeof(clean)) == need);
    for (path = 0; path < 2; ++path)
    {
        memcpy(base, clean, (size_t) need);
        if (path == 1) { T08_REHASH(base); }
        for (at = 0; at < (int) need; ++at)
        {
            for (p = 0; p < 14; ++p)
            {
                memcpy(hit, base, (size_t) need);
                if (p < 6) { hit[at] = poison[p]; } else { hit[at] ^= (uint8_t) (1u << (p - 6)); }
                memset(&back, 0x5A, sizeof(back));
                memset(&r, 0, sizeof(r));
                n = host_fixed_load_tolerant(&back, 1, hit, need, plan, 4096, NULL, &r);
                if (n < 0)
                {
                    /* A REFUSAL BY NAME, or A MALFORMED READ — exactly one. */
                    if (r.refused == r.malformed || (r.refused && r.reason == 0))
                        { fprintf(stderr, "path %d byte %d poison %d: a negative read is not exactly one of refused-by-name and malformed\n", path, at, p); return 1; }
                }
                else
                {
                    /* A READ THAT LANDS VALUES, and every one inside its bound. */
                    CHECK(n == 1 && !r.refused && !r.malformed && r.reason == 0);
                    if (back.small < 0 || back.small > 1000)
                        { fprintf(stderr, "path %d byte %d poison %d: success with small=%d out of bound\n", path, at, p, (int) back.small); return 1; }
                    if (back.marks_count < 0 || back.marks_count > 4)
                        { fprintf(stderr, "path %d byte %d poison %d: success with marks_count=%d out of bound\n", path, at, p, (int) back.marks_count); return 1; }
                    for (i = 0; i < back.marks_count; ++i)
                        if (back.marks[i] < 0 || back.marks[i] > 1000)
                            { fprintf(stderr, "path %d byte %d poison %d: success with marks[%d]=%d out of bound\n", path, at, p, i, (int) back.marks[i]); return 1; }
                    if (back.note_present && (back.note < 0 || back.note > 1000))
                        { fprintf(stderr, "path %d byte %d poison %d: success with note=%d out of bound\n", path, at, p, (int) back.note); return 1; }
                }
                ++answered;
            }
        }
    }
    CHECK(answered == 2 * 14 * (int) need);
    return 0;
}
`, append([]string{"-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-fno-omit-frame-pointer", "-g"}, identityCCFlags()...)...)
	// AND THE SAME SWEEP OVER THE THREE-UNION CHAIN, so the guard chain's code
	// on both plans takes every forged byte too: one of three answers, and a
	// read that lands values holds each tag it landed inside its two arms.
	cT08Probe(t, cT08TripleUnionSchema, "Root", `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
static const uint8_t poison[6] = { 0x00, 0x01, 0x02, 0x7f, 0x80, 0xff };
int main(void)
{
    static TableFixedEntry plan[4096];
    Root value, back;
    TableReport r;
    uint8_t clean[4096], base[4096], hit[4096];
    int64_t need, n;
    int at, p, path, answered = 0;
    memset(&value, 0, sizeof(value));
    value.oo.type = 1;
    value.oo.as.o.mm.type = 1;
    value.oo.as.o.mm.as.m.i.type = 1;
    value.oo.as.o.mm.as.m.i.as.a.x = 42;
    need = root_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t) sizeof(clean));
    CHECK(root_fixed_save(&value, 1, clean, (int64_t) sizeof(clean)) == need);
    for (path = 0; path < 2; ++path)
    {
        memcpy(base, clean, (size_t) need);
        if (path == 1) { T08_REHASH(base); }
        for (at = 0; at < (int) need; ++at)
        {
            for (p = 0; p < 14; ++p)
            {
                memcpy(hit, base, (size_t) need);
                if (p < 6) { hit[at] = poison[p]; } else { hit[at] ^= (uint8_t) (1u << (p - 6)); }
                memset(&back, 0x5A, sizeof(back));
                memset(&r, 0, sizeof(r));
                n = root_fixed_load_tolerant(&back, 1, hit, need, plan, 4096, NULL, &r);
                if (n < 0)
                {
                    if (r.refused == r.malformed || (r.refused && r.reason == 0))
                        { fprintf(stderr, "path %d byte %d poison %d: a negative read is not exactly one of refused-by-name and malformed\n", path, at, p); return 1; }
                }
                else
                {
                    CHECK(n == 1 && !r.refused && !r.malformed && r.reason == 0);
                    if (back.oo.type > 2 || (back.oo.type == 1 && (back.oo.as.o.mm.type > 2 ||
                        (back.oo.as.o.mm.type == 1 && back.oo.as.o.mm.as.m.i.type > 2))))
                        { fprintf(stderr, "path %d byte %d poison %d: success with a tag past its arms\n", path, at, p); return 1; }
                }
                ++answered;
            }
        }
    }
    CHECK(answered == 2 * 14 * (int) need);
    return 0;
}
`, append([]string{"-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-fno-omit-frame-pointer", "-g"}, identityCCFlags()...)...)
}

// cT08WrongPlanSchema is P4's pair: Fxnew widens `narrow`, renames `renamed`
// and appends `added`, so Fxold's identity plan run over an Fxnew record body
// lands the moved fields at the wrong offsets.
const cT08WrongPlanSchema = `package probe
fixed table Fxold {
    keep    uint32 = 7
    narrow  uint16 = 3
    renamed int32 = 5
}
fixed table Fxnew {
    keep       uint32 = 7
    narrow     uint32 = 3
    renamed_to int32 = 5
    added      int32 = 11
}
`

// c/P4 [verify] — "wrong plan goes red". FIXED-FORM-ALGORITHM §7 item 4: "the
// wrong plan must go red, and so must each fix's own bug: this build's identity
// plan over another schema's record". Two halves: the loader REFUSES a file
// whose hash it does not lock (§4.2 row 4: "a hash in no entry is
// `layout_newer`"), counting nothing; and Fxold's identity plan run straight
// over Fxnew's record body does NOT reproduce the record — the path
// select-by-hash exists to prevent. The legitimate value the rule admits
// (Fxold reading its own record) is asserted too, so neither half is vacuous.
func cT08WrongPlanGoesRed(t *testing.T) {
	runCGenerated(t, cT08WrongPlanSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Fxnew two;
    Fxold wrong, one, back;
    TableReport r;
    TableFixedEntry plan[1024];
    uint8_t w2[4096], w1[4096];
    int64_t need;
    const uint8_t * body;
    memset(&two, 0, sizeof(two));
    two.keep = 5150;
    two.narrow = 70000;
    two.renamed_to = 808;
    two.added = 909;
    need = fxnew_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t)sizeof(w2));
    CHECK(fxnew_fixed_save(&two, 1, w2, (int64_t)sizeof(w2)) == need);

    /* (2) the loader refuses: Fxold locks no lineage entry for Fxnew's hash. */
    memset(&back, 0x5A, sizeof(back));
    memset(&r, 0, sizeof(r));
    CHECK(fxold_fixed_load_tolerant(&back, 1, w2, need, plan, 1024, NULL, &r) == -1);
    CHECK(r.refused && r.reason == SCHEMA_TABLE_LAYOUT_NEWER);
    CHECK(!r.malformed && r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && r.widened == 0);

    /* (1) THE WRONG PLAN IS WRONG: Fxold's identity plan over Fxnew's body. */
    body = w2 + need - fxnew_fixed_body_bytes;
    memset(&wrong, 0x5A, sizeof(wrong));
    table_fixed_run(fxold_fixed_plan, fxold_fixed_plan_count, fxold_fixed_plan_guarded, body, (uint8_t *) &wrong, &r, fxold_fixed_guards);
    if (wrong.renamed == 808)
        { fprintf(stderr, "NEGATIVE CONTROL FAILED: Fxold's identity plan reproduced an Fxnew record (renamed=%d)\n", (int)wrong.renamed); return 1; }

    /* the legitimate value the rule admits: Fxold reads its own record whole. */
    memset(&one, 0, sizeof(one));
    one.keep = 4242;
    one.narrow = 40000;
    one.renamed = 321;
    need = fxold_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t)sizeof(w1));
    CHECK(fxold_fixed_save(&one, 1, w1, (int64_t)sizeof(w1)) == need);
    memset(&back, 0x5A, sizeof(back));
    memset(&r, 0, sizeof(r));
    CHECK(fxold_fixed_load_tolerant(&back, 1, w1, need, plan, 1024, NULL, &r) == 1);
    CHECK(back.renamed == 321 && back.narrow == 40000 && back.keep == 4242);
    return 0;
}
`, identityCCFlags()...)
}
