package javatable

// fixedroadmap_t02 — the java leg's compiled-plans, definition-hash and
// fixed-closure rows of docs/roadmap.sexp's node `fixed-tables`
// (ROADMAP.md "NEW Fixed Tables"), the assertions no other test of this leg
// makes. Law: docs/FIXED-FORM-ALGORITHM.md §5.2 (COMPILE(lock, T)) and §5.5
// (the closure), docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the hash"),
// docs/SPEC-TABLES.md §3.4. One subtest per task id, and the assertion under
// each is the clause the task's own title states.
//
// A clause already held by an existing test of this leg is named by that test
// in the card's verdicts and is not repeated here; this file carries the direct
// assertion of every clause that had none.

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02Flat is the smallest declared fixed table: two adjacent scalars, which the
// coalescer lands as ONE run, so the plan's dst and size are the record's.
const t02Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t02Block is the text between two markers, the first occurrence of `end` after
// `start`. It fails rather than answering "" so a moved section is red by name.
func t02Block(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("the generated source has no %q", start)
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("the generated source has no %q after %q", end, start)
	}
	return rest[:j]
}

// t02CheckErrs parses and checks one source and answers the checker's errors, so
// a closure refusal can be read by name rather than caught as a fixture bug.
func t02CheckErrs(t *testing.T, src string) []error {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return []error{perrs[0]}
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	return cerrs
}

// t02Has is a required substring of the emitted source, named by rule.
func t02Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

func TestFixedRoadmapT02Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"java/R1", t02R1},
		{"java/R2", t02R2},
		{"java/R23", t02R23},
		{"java/R25", t02R25},
		{"java/R26", t02R26},
		{"java/W14", t02W14},
		{"java/R4", t02R4},
		{"java/R5", t02R5},
		{"java/W11", t02W11},
		{"java/W12", t02W12},
		{"java/R6", t02R6},
		{"java/R14", t02R14},
		{"java/R22", t02R22},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t02R1: R1 "COMPILE lays the lineage down as static data at build time, oldest
// first and the current layout last, from the lock". The lineage the build
// holds for a table rides as the `known` array (the lock's entries first, the
// current layout last), and one plan per entry is built in the class
// initializer from those bytes, never on a load path.
func t02R1(t *testing.T) {
	u := unitFrom(t, t02Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R1: FixedLineageOf has no entry for T")
	}
	older := FixedLineageEntry{Wire: own.Wire ^ 0x5a5a5a5a, Layout: own.Layout, Record: own.Record}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older}})
	if err != nil {
		t.Fatalf("R1: GenerateLineage: %v", err)
	}
	src := string(files["TFixed.java"])

	t02Has(t, src, "static final TableFixed.Plan[] plans = TableFixed.lineagePlans(known, layout, dest, hash)", "R1 the plans are static data built from the lock")
	block := t02Block(t, src, "known = {", "};")
	olderAt := strings.Index(block, fmt.Sprintf("0x%016xL", older.Wire))
	ownAt := strings.Index(block, fmt.Sprintf("0x%016xL", own.Wire))
	if olderAt < 0 || ownAt < 0 {
		t.Fatalf("R1: the known array does not carry both hashes:\n%s", block)
	}
	if olderAt >= ownAt {
		t.Errorf("R1: the lock's entry (at %d) is not before the current layout (at %d)", olderAt, ownAt)
	}
	if !strings.Contains(block[ownAt:], "layout,") {
		t.Error("R1: the last entry is not the current `layout` constant")
	}
}

// t02R2: R2 "record_bytes is 8 + body: the lock stores the body, COMPILE adds
// the eight once, and no backend adds anything". The leg's own entry is 8 +
// sizeof(body), the emitted constant is that sum spelled once, and the static
// data writes the number it was handed without adding anything to it.
func t02R2(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := findTable(t, u, "T")
	body := ir.TableFixedTypeBytes(st)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R2: FixedLineageOf has no entry for T")
	}
	if own.Record != 8+body {
		t.Errorf("R2: the leg's own entry record = %d, want 8 + body = %d", own.Record, 8+body)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R2: Generate: %v", err)
	}
	src := string(files["TFixed.java"])
	t02Has(t, src, "public static final int recordBytes = 8 + bodyBytes;", "R2 the eight is added once")
	t02Has(t, src, fmt.Sprintf(", %d),", own.Record), "R2 the known entry writes the handed number verbatim")
	t02Has(t, src, "final long recordSize = known_.recordBytes;", "R2 the record size comes from the lock's entry")
}

// t02R23: R23 "the static data's member names and order — TableFixedKnownLayout
// = hash, layout, layout_bytes, record_bytes; the report's layout_hash last and
// zero on every other path".
func t02R23(t *testing.T) {
	u := unitFrom(t, t02Flat)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R23: Generate: %v", err)
	}
	src := string(files["TableFixed.java"])

	at := -1
	for _, member := range []string{
		"public final long hash;",
		"public final byte[] layout;",
		"public final int layoutBytes;",
		"public final int recordBytes;",
	} {
		i := strings.Index(src, member)
		if i < 0 {
			t.Fatalf("R23: KnownLayout has no member %q", member)
		}
		if i < at {
			t.Errorf("R23: KnownLayout's member %q is out of order", member)
		}
		at = i
	}

	report := t02Block(t, src, "public static final class Report {", "\n    }")
	last := ""
	for _, line := range strings.Split(report, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "public ") && strings.HasSuffix(s, ";") {
			last = s
		}
	}
	if !strings.HasPrefix(last, "public long layoutHash;") {
		t.Errorf("R23: layoutHash is not the report's last member; last = %q", last)
	}
	t02Has(t, report, "layoutHash = 0;", "R23 reset zeroes the hash")
	if strings.Contains(t02Block(t, report, "public void refuse(Reason why)", "}"), "layoutHash") {
		t.Error("R23: refuse() touches layoutHash; only the two layout refusals carry a hash")
	}
	t02Has(t, report, "public void refuseHash(Reason why, long hash) { layoutHash = hash; refuse(why); }", "R23 refuseHash is the one setter")
}

// t02R25: R25 "plan_too_large when the plan does not fit the caller's capacity".
// The load refuses by name and returns -1 before a record byte lands.
func t02R25(t *testing.T) {
	u := unitFrom(t, t02Flat)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R25: Generate: %v", err)
	}
	t02Has(t, string(files["TableFixed.java"]), "planTooLarge,", "R25 the reason exists")
	t02Has(t, string(files["TFixed.java"]),
		"if (plan != null && lane.count > plan.length) { report.refuse(TableFixed.Reason.planTooLarge); return -1; }",
		"R25 the caller's capacity is the bound")
}

// t02R26: R26 "a known hash whose lineage entry would not build →
// layout_malformed / plan_too_large by that entry's own lane, never a throw".
// An entry that will not build carries its own reason in its lane and the load
// refuses by that name; nothing throws out of the initializer or the read.
func t02R26(t *testing.T) {
	u := unitFrom(t, t02Flat)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R26: Generate: %v", err)
	}
	runtime := string(files["TableFixed.java"])
	load := string(files["TFixed.java"])
	t02Has(t, runtime, "out[i] = new Plan(null, 0, null, 0, 0, Reason.layoutMalformed);", "R26 an unbuildable entry carries layout_malformed")
	t02Has(t, runtime, "final Reason why = (census.refused && census.reason == Reason.layoutRecordTooLarge)", "R26 the lane keeps the entry's own reason")
	t02Has(t, runtime, "out[i] = new Plan(null, 0, null, 0, 0, why);", "R26 the reason rides the lane")
	t02Has(t, load, "if (lane == null || lane.why != TableFixed.Reason.none) {", "R26 the load reads the lane")
	t02Has(t, load, "report.refuse(lane == null ? TableFixed.Reason.layoutMalformed : lane.why);", "R26 the refusal is by name")
	if strings.Contains(t02Block(t, runtime, "public static Plan[] lineagePlans(", "\n    }"), "throw ") {
		t.Error("R26: lineagePlans throws; the entry's own lane carries the reason")
	}
}

// t02W14: W14 "plan dst == offsetof/sizeof". Two adjacent int32 fields coalesce
// to one run whose dst is offsetof(x) and whose size is sizeof(T).
func t02W14(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := findTable(t, u, "T")
	plan, _ := ir.TableFixedBuildPlan(u, st)
	if len(plan) != 1 {
		t.Fatalf("W14: the two adjacent int32 fields are one run; got %d leaves", len(plan))
	}
	if got, want := plan[0].Dst, ir.TableFixedMemberOffset(u, st, "x"); got != want {
		t.Errorf("W14: the plan's dst = %d, want offsetof(x) = %d", got, want)
	}
	if got, want := plan[0].Size, ir.TableFixedTypeBytes(st); got != want {
		t.Errorf("W14: the plan's size = %d, want sizeof(T) = %d", got, want)
	}
}

// t02R4: R4 "the hash is fnv1a64 over the layout bytes then DIGEST(T), the
// digest computed at the hash site from the schema; a runtime never derives a
// hash from layout bytes it holds". The compiler's hash equals fnv1a64 of the
// layout followed by the definitions digest, the emitted constant is that
// number, and the load takes the header's hash as given.
func t02R4(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
    y float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	st := findTable(t, u, "T")
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	digest := ir.TableFixedDefinitionsDigest(st)
	if len(digest) == 0 {
		t.Fatal("R4: the fixture carries no digest")
	}
	h := uint64(0xcbf29ce484222325)
	for _, b := range append(append([]byte(nil), layout...), digest...) {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	if got := ir.TableFixedLayoutHash(layout, st); got != h {
		t.Errorf("R4: hash = %#x, want fnv1a64(layout || digest) = %#x", got, h)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R4: Generate: %v", err)
	}
	t02Has(t, string(files["TFixed.java"]), fmt.Sprintf("public static final long hash = 0x%016xL;", h), "R4 the emitted hash constant")
	load := methodBody(string(files["TFixed.java"]), "public static int load(")
	if load == "" {
		t.Fatal("R4: no emitted load function found")
	}
	t02Has(t, load, "final long fileHash = head.hash;", "R4 the file's hash is taken as given")
	if strings.Contains(load, "fnv") || strings.Contains(load, "TableFixed.hash(") {
		t.Error("R4: the load path derives a hash from layout bytes it holds")
	}
}

// t02R5: R5 "the digest carries every range, every resolution (tag 'Q') and
// every reader limit (tag 'L'), and a flags type deduped by name, once". The
// bytes are positional and named by tag, so each clause is read off the digest
// directly. A reader-side limit has no table spelling (ir.TableFixedDefinitionsDigest
// marks 'L' RESERVED), so this leg can produce no 'L' row to read.
func t02R5(t *testing.T) {
	ranged := findTable(t, unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`), "T")
	if d := ir.TableFixedDefinitionsDigest(ranged); len(d) != 17 || d[0] != 'R' || d[1] != 0xfb {
		t.Errorf("R5: a range digest = % x, want 'R' and min=-5", d)
	}

	compressed := findTable(t, unitFrom(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`), "T")
	if d := ir.TableFixedDefinitionsDigest(compressed); len(d) != 26 || d[0] != 'R' || d[17] != 'Q' {
		t.Errorf("R5: a compressed-float digest = % x, want 'R' min max 'Q' resolution", d)
	}

	flagsOnce := findTable(t, unitFrom(t, `package probe

flags Marks { One, Two }

fixed table T
{
    m Marks
}
`), "T")
	once := ir.TableFixedDefinitionsDigest(flagsOnce)
	if len(once) != 23 || once[0] != 'F' || once[5] != 'f' || once[14] != 'f' {
		t.Errorf("R5: a flags digest = % x, want 'F' count, then 'f' name per flag", once)
	}
	if n := binary.LittleEndian.Uint32(once[1:5]); n != 2 {
		t.Errorf("R5: the flags row names %d wire bits, want 2", n)
	}

	flagsTwice := findTable(t, unitFrom(t, `package probe

flags Marks { One, Two }

fixed table T
{
    m Marks
    n Marks
}
`), "T")
	if got := ir.TableFixedDefinitionsDigest(flagsTwice); string(got) != string(once) {
		t.Errorf("R5: the flags type is not deduped by name: once % x, twice % x", once, got)
	}
	// 'L' would be a reader-side limit. No table spelling makes one, so the
	// reserved row is absent rather than silently unhashed.
	for _, d := range [][]byte{once, ir.TableFixedDefinitionsDigest(flagsTwice)} {
		for _, b := range d {
			if b == 'L' {
				t.Error("R5: the digest carries a reader-limit row no table spelling produces")
			}
		}
	}
}

// t02W11: W11 "bytes(N) is layout kind 14". A bytes(N) field walks as an ARRAY
// (kind 14) with a u8 child, and the byte that rides is the same number.
func t02W11(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    blob bytes(6)
}
`)
	st := findTable(t, u, "T")
	w := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(w)
	found := false
	for i, e := range w {
		if e.Note != "blob" {
			continue
		}
		found = true
		if e.Kind != ir.TableKindArray {
			t.Errorf("W11: bytes(6) walks as kind %d, want %d (array)", e.Kind, ir.TableKindArray)
		}
		if got := int(layout[4+i*17+8]); got != 14 {
			t.Errorf("W11: the bytes(6) layout kind byte = %d, want 14", got)
		}
	}
	if !found {
		t.Fatal("W11: the walk has no blob entry")
	}
	if ir.TableKindArray != 14 {
		t.Fatalf("W11: TableKindArray = %d, want 14", ir.TableKindArray)
	}
}

// t02W12: W12 "hash includes the 4-byte count". The layout opens with the u32
// entry count, and the hash moves when that count moves.
func t02W12(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := findTable(t, u, "T")
	w := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(w)
	if got := binary.LittleEndian.Uint32(layout[:4]); got != uint32(len(w)) {
		t.Errorf("W12: the layout's count = %d, want %d entries", got, len(w))
	}
	mutated := append([]byte(nil), layout...)
	binary.LittleEndian.PutUint32(mutated[:4], uint32(len(w))+1)
	if ir.TableFixedLayoutHash(layout, st) == ir.TableFixedLayoutHash(mutated, st) {
		t.Error("W12: the hash ignores the layout's 4-byte entry count")
	}
}

// t02R6: R6 "a table past §3.4's 65536 ceiling is not a fixed-form root: the
// refusal names the table, no form is emitted, and no lineage entry is parsed
// for it even when the lock carries one".
func t02R6(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Big
{
    blob bytes(70000)
}
`)
	st := findTable(t, u, "Big")
	if ir.TableFixedTypeBytes(st) <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: the fixture body = %d, not past %d", ir.TableFixedTypeBytes(st), ir.TableFixedRecordMaxBytes)
	}
	for _, r := range ir.TableFixedFormRoots(u) {
		if r.Name == "Big" {
			t.Error("R6: a table past the ceiling is a fixed-form root")
		}
	}
	warnings, errs := ir.TableFixedRecordBounds(u, 0)
	if len(errs) != 0 {
		t.Errorf("R6: past the ceiling is a warning, not a refusal: %v", errs)
	}
	if joined := strings.Join(warnings, "\n"); !strings.Contains(joined, "Big") || !strings.Contains(joined, "ceiling") {
		t.Errorf("R6: the warning does not name the table and the ceiling: %s", joined)
	}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{
		"Big": {{Wire: 0x1122334455667788, Layout: []byte{1, 2, 3, 4}, Record: 70008}},
	})
	if err != nil {
		t.Fatalf("R6: GenerateLineage: %v", err)
	}
	if _, ok := files["BigFixed.java"]; ok {
		t.Error("R6: a form was emitted for a table past the ceiling")
	}
	for name, body := range files {
		if strings.Contains(string(body), "0x1122334455667788") {
			t.Errorf("R6: %s parsed the lock's lineage entry for a table past the ceiling", name)
		}
	}
}

// t02R14: R14 "layout_record_too_large for an entry reaching past the writer's
// declared record, not only for the 65536 bound and a zero root". The runtime
// holds a single entry and a subtree to the bound, the zero root to the same
// name, and bounds a compiled plan against the writer's OWN declared record.
func t02R14(t *testing.T) {
	u := unitFrom(t, t02Flat)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R14: Generate: %v", err)
	}
	runtime := string(files["TableFixed.java"])
	t02Has(t, runtime, "if (size > recordMaxBytes) { c.fail(Reason.layoutRecordTooLarge); return 0; }", "R14 a single entry past the bound")
	t02Has(t, runtime, "if (sum > recordMaxBytes) { c.fail(Reason.layoutRecordTooLarge); return 0; }", "R14 a subtree past the bound")
	t02Has(t, runtime, "if (root == 0 || root > recordMaxBytes) { report.refuse(Reason.layoutRecordTooLarge); return null; }", "R14 the zero root and the bound")
	t02Has(t, runtime, "c.theirBytes = (int) theirs.size(0);", "R14 the writer's own record is the bound")
	t02Has(t, runtime, "if (c.recordTooLarge) {", "R14 an entry past the writer's declared record")
	t02Has(t, runtime, "report.refuse(Reason.layoutRecordTooLarge);", "R14 the refusal is named")
}

// t02R22: R22 "the closure rule: every table or type reached by value is itself
// fixed; a pointer, map or unbounded array in the closure is a compile refusal;
// T is never in its own closure".
func t02R22(t *testing.T) {
	fixed := unitFrom(t, `package probe

fixed table Inner { a int32 }
fixed table T { inner Inner }
`)
	if breaks := ir.FixedClosureBreaks(func(name string) *ir.Struct { return fixed.Tables[name] }, "T"); len(breaks) != 0 {
		t.Errorf("R22: a fixed table nested by value is a closure break: %v", breaks)
	}

	for _, tc := range []struct {
		name, src, want string
	}{
		{"pointer", "package probe\ntable Node { x int32 }\nfixed table Scene { head *Node }\n", "is a pointer"},
		{"map", "package probe\nfixed table Fleet { ships map[uint32]int32 }\n", "is a map"},
		{"unbounded array", "package probe\nfixed table Log { entries []int32 }\n", "is an unbounded array"},
	} {
		errs := t02CheckErrs(t, tc.src)
		if len(errs) == 0 {
			t.Errorf("R22 %s: the closure break is not a compile refusal", tc.name)
			continue
		}
		got := errs[0].Error()
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "docs/SPEC-TABLES.md") {
			t.Errorf("R22 %s: refusal = %q, want it to name %q and §2.2", tc.name, got, tc.want)
		}
	}

	// T is never in its own closure: a fixed table that reaches itself by value
	// is refused, so a table cannot be its own closure member.
	if errs := t02CheckErrs(t, "package probe\nfixed table T { next T }\n"); len(errs) == 0 {
		t.Error("R22: a fixed table that reaches itself by value is not refused")
	}
}
