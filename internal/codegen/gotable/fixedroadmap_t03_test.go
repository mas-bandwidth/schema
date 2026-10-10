package gotable

// fixedroadmap_t03 — the go leg's fixed-closure, retirement, retired-runtime and
// numeric-evolution rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md
// "NEW Fixed Tables"), the assertions no other test of this leg makes. Law:
// docs/FIXED-FORM-ALGORITHM.md §1.1 (the record's 65536 ceiling), §5.2
// (COMPILE: the writer's record bounds every entry, the floor), §5.3 (LOAD:
// select by hash, refuse by name, never walk a stranger), §5.6 (what is
// retired), §5.9 #23, docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the
// hash", "The old contract's tests, retired by name"), docs/SPEC-TABLES.md
// §2.2, §3.4, §21.2.
//
// A clause already held by an existing test of this leg is named by that test
// in the card's verdicts and is not repeated here; this file carries the direct
// assertion of every clause that had none. Every subtest RUNS under a bare
// `go test ./internal/codegen/gotable/`: nothing is hidden behind slowtest.Gate
// (that is what let t02's predecessor report verdicts over assertions that
// never executed). Where a clause needs the emitted code to RUN, the generated
// fixed module is compiled and run by this file's own harness — the emitted
// fixed form imports no sibling `../serialize.go`, so it builds with nothing
// but the standard library.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t03Flat is the smallest declared fixed table: two adjacent int32 scalars.
const t03Flat = `package probe
fixed table T
{
    x int32
    y int32
}
`

func TestFixedRoadmapGoT03PlansVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/R6", t03R6},
		{"go/R14", t03R14},
		{"go/R22", t03R22},
		{"go/R3", t03R3},
		{"go/R32", t03R32},
		{"go/R10", t03R10},
		{"go/R15", t03R15},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// ---- the emitted source, and the harness -----------------------------------

// t03Emit is the emitted Go table module for a unit, the file the fixed surface
// lands in.
func t03Emit(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return tableFile(files)
}

// t03EmitLineage is [GenerateLineage]'s Go module for a unit the build hands a
// lineage.
func t03EmitLineage(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry) string {
	t.Helper()
	files, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return tableFile(files)
}

// t03Check is [unitFrom] that ANSWERS the check's refusals instead of failing on
// them, so a closure refusal can be read by name.
func t03Check(t *testing.T, src string) (*ir.Unit, string) {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return nil, perrs[0].Error()
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		return u, cerrs[0].Error()
	}
	return u, ""
}

// t03Has is a required substring, named by rule.
func t03Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t03RunGo compiles and runs the fixed module [Generate]/[GenerateLineage]
// emits, together with the probe, and answers the probe's combined output. The
// emitted fixed form is standard-library-only, so this runs under a bare
// `go test` with no sibling runtime.
func t03RunGo(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry, probe string) string {
	t.Helper()
	var (
		files map[string][]byte
		err   error
	)
	if lineage == nil {
		files, err = Generate(u)
	} else {
		files, err = GenerateLineage(u, lineage)
	}
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.HasSuffix(name, ".go") {
			if werr := os.WriteFile(filepath.Join(dir, name), data, 0o600); werr != nil {
				t.Fatal(werr)
			}
		}
	}
	write := func(name, body string) {
		if werr := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); werr != nil {
			t.Fatal(werr)
		}
	}
	write("go.mod", "module probe\n\ngo 1.26\n")
	write("t03_probe_test.go", probe)
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "120s", ".")
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("the generated probe failed: %v\n%s", runErr, out)
	}
	return string(out)
}

// t03FormRoots is whether a table is one the fixed form is emitted for, read
// from the ir function that owns the bound.
func t03FormRoots(u *ir.Unit, name string) bool {
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name == name {
			return true
		}
	}
	return false
}

// ---- go/R6 -----------------------------------------------------------------

// t03R6 holds R6: "a table past §3.4's 65536 ceiling is not a fixed-form root:
// the refusal names the table, no form is emitted, and no lineage entry is
// parsed for it even when the lock carries one". §3.4: a reader holds an
// untrusted peer's layout to 65536, so a record past it is one no conforming
// reader decodes; ir.TableFixedRecordBounds names the table and the size and
// ir.TableFixedFormRoots drops it, and the go leg's hasFixedForm must apply the
// same bound — without it the leg emitted TFixedLoad/TFixedKnown for a record
// no peer reads.
func t03R6(t *testing.T) {
	u := unitFrom(t, `package probe
fixed table T
{
    s string(70000)
}
`)
	st := u.Tables["T"]
	if st == nil {
		t.Fatal("R6: the fixture declares no fixed table T")
	}
	if got := ir.TableFixedTypeBytes(st); got <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: the fixture's record body is %d, not past the %d-byte ceiling", got, ir.TableFixedRecordMaxBytes)
	}
	if t03FormRoots(u, "T") {
		t.Error("R6: a table past §3.4's 65536 ceiling is a fixed-form root")
	}
	warns, _ := ir.TableFixedRecordBounds(u, 0)
	named := false
	for _, w := range warns {
		if strings.Contains(w, "table T") && strings.Contains(w, "65536") {
			named = true
		}
	}
	if !named {
		t.Errorf("R6: the refusal does not name the table: %v", warns)
	}
	src := t03Emit(t, u)
	if strings.Contains(src, "func TFixedLoad(") {
		t.Error("R6: the fixed form is emitted for a table past the 65536 ceiling (§3.4)")
	}
	if strings.Contains(src, "var TFixedKnown") {
		t.Error("R6: the lineage is emitted for a table past the 65536 ceiling")
	}
	// AND NO LINEAGE ENTRY IS PARSED FOR IT even when the lock carries one.
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R6: FixedLineageOf has no entry for T")
	}
	withLock := t03EmitLineage(t, u, map[string][]FixedLineageEntry{"T": {{
		Wire: own.Wire ^ 0x1, Layout: own.Layout, Record: own.Record,
	}}})
	if strings.Contains(withLock, "TFixedKnown") || strings.Contains(withLock, "TFixedLineagePlans") {
		t.Error("R6: a lineage entry is parsed for a table past the 65536 ceiling even though no form is emitted")
	}
}

// ---- go/R14 -----------------------------------------------------------------

// t03R14 holds R14: "layout_record_too_large for an entry reaching past the
// writer's declared record, not only for the 65536 bound and a zero root".
// §5.2's tableFixedPush: "the bound is the writer's declared root size and not
// this reader's: an entry that reached past it would read the NEXT record's
// bytes"; the root size is taken from the layout (root entry 0), guarded
// against a zero root, and a compiler that sets tooLarge refuses the plan whole
// with that name.
func t03R14(t *testing.T) {
	src := t03Emit(t, unitFrom(t, t03Flat))
	t03Has(t, src, "if c.record != 0 && tableFixedSrcEnd(e) > uint64(c.record) {",
		"R14 the entry bound is the writer's declared record, with a zero root guarded")
	t03Has(t, src, "record: tableFixedEntryAt(theirs, 0).Size",
		"R14 the writer's record is the root entry's declared size, taken from the layout")
	t03Has(t, src, "out[i].Why = \"layout_record_too_large\"",
		"R14 an entry past the writer's record carries layout_record_too_large on its lane")
	t03Has(t, src, "if c.tooLarge {\n\t\treturn -3\n\t}",
		"R14 one entry past the record refuses the plan whole")
	// NOT ONLY THE 65536 BOUND: the push bound is per-ENTRY (the entry's source
	// reach against the writer's record), and it is not the layout's own
	// e.Size > 65536 check, which is a different rule with the same name.
	t03Has(t, src, "func tableFixedSrcEnd(e TableFixedEntry) uint64 {",
		"R14 the bound is the entry's source reach, not the layout entry's size")
}

// ---- go/R22 -----------------------------------------------------------------

// t03R22 holds R22: "the closure rule: every table or type reached by value is
// itself fixed; a pointer, map or unbounded array in the closure is a compile
// refusal; T is never in its own closure". §2.2: "if we add any feature that
// stops it from being fixed, it is a compile error"; ir.FixedClosureBreaks is
// the check behind it. A nested FIXED table is lawful (its own closure is
// checked where it is declared); a nested plain table, a pointer, a map and an
// unbounded array are each refused by name; a table reaching itself by value is
// refused as a composition cycle, so it never enters its own closure.
func t03R22(t *testing.T) {
	ok := `package probe
fixed table T { v V }
fixed table V { x int32 }
`
	if _, err := t03Check(t, ok); err != "" {
		t.Errorf("R22: a nested FIXED table is lawful, but the check refused it: %s", err)
	}
	for _, c := range []struct {
		what, field, src string
	}{
		{"plain table", "T.v", "package probe\nfixed table T { v V }\ntable V { x int32 }\n"},
		{"pointer", "T.p", "package probe\nfixed table T { p *V }\nfixed table V { x int32 }\n"},
		{"map", "T.m", "package probe\nfixed table T { m map[string(8)]V }\nfixed table V { x int32 }\n"},
		{"unbounded array", "T.a", "package probe\nfixed table T { a []V }\nfixed table V { x int32 }\n"},
	} {
		_, err := t03Check(t, c.src)
		if err == "" {
			t.Errorf("R22: a %s in the closure was not a compile refusal", c.what)
			continue
		}
		if !strings.Contains(err, c.field) || !strings.Contains(err, "§2.2") {
			t.Errorf("R22: the %s refusal does not name %s and cite §2.2: %s", c.what, c.field, err)
		}
	}
	// T IS NEVER IN ITS OWN CLOSURE: reaching itself by value is a composition
	// cycle, refused before any closure walk.
	_, self := t03Check(t, "package probe\nfixed table T { a [2]T }\n")
	if !strings.Contains(self, "cycle") || !strings.Contains(self, "T") {
		t.Errorf("R22: a self-reaching table is not refused as a cycle: %s", self)
	}
}

// ---- go/R3 ------------------------------------------------------------------

// t03R3 holds R3: "the floor is 1 + the highest retired index (0 when none);
// below the floor is layout_unsupported, reporting the file's hash". §5.2: the
// floor is one past the highest retired lineage index; a known hash below it is
// layout_unsupported, which (§5.3) reports THE FILE'S hash. The emitted
// constant is read for each retirement shape, and the emitted load's refusal is
// read for the name and the hash.
func t03R3(t *testing.T) {
	u := unitFrom(t, t03Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R3: FixedLineageOf has no entry for T")
	}
	older := []FixedLineageEntry{
		{Wire: own.Wire ^ 0x1111111111111111, Layout: own.Layout, Record: own.Record},
		{Wire: own.Wire ^ 0x2222222222222222, Layout: own.Layout, Record: own.Record},
	}
	for _, tc := range []struct {
		retire []int
		want   int
	}{
		{nil, 0},
		{[]int{0}, 1},
		{[]int{1}, 2},
		{[]int{0, 1}, 2},
	} {
		entries := append([]FixedLineageEntry(nil), older...)
		for _, i := range tc.retire {
			entries[i].Retired = true
			entries[i].Reason = "the client is gone"
		}
		src := t03EmitLineage(t, u, map[string][]FixedLineageEntry{"T": entries})
		t03Has(t, src, "const TFixedFloor = "+itoa(tc.want),
			"R3 the floor is 1 + the highest retired index, 0 when none is")
	}
	src := t03EmitLineage(t, u, map[string][]FixedLineageEntry{"T": older})
	t03Has(t, src, "if pick < TFixedFloor {\n\t\treturn tableFixedRefuseHash(report, \"layout_unsupported\", hash)\n\t}",
		"R3 below the floor is layout_unsupported, reporting the file's hash")
}

// ---- go/R32 -----------------------------------------------------------------

// t03R32 holds R32: "retire for real: a retired version is refused by name,
// once, idempotently". §5.2's fixedLineage carries the retired entry with the
// operator's reason and moves the floor; the emitted load refuses it by name
// with the file's hash, and a second GenerateLineage of the same lock lays the
// same floor. The behavioural half (a file at the floor reads, one below
// refuses once with the hash) runs against the emitted module.
func t03R32(t *testing.T) {
	u := unitFrom(t, t03Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R32: FixedLineageOf has no entry for T")
	}
	retired := FixedLineageEntry{Wire: own.Wire ^ 0x33, Layout: own.Layout, Record: own.Record, Retired: true, Reason: "the oldest client is gone"}
	src := t03EmitLineage(t, u, map[string][]FixedLineageEntry{"T": {retired}})
	t03Has(t, src, "// RETIRED: the oldest client is gone",
		"R32 the retired entry carries the operator's reason")
	t03Has(t, src, "const TFixedFloor = 1", "R32 a retired entry moves the floor past it")
	// IDEMPOTENT: the same lock lays the same floor and the same retired entry,
	// twice. The whole emitted build is compared through its deterministic
	// fixed-form section, not the file map's iteration order.
	block := func(src string) string {
		return t02GoBlock(t, src, "var TFixedKnown = []TableFixedKnownLayout{", "\n}\n")
	}
	again := t03EmitLineage(t, u, map[string][]FixedLineageEntry{"T": {retired}})
	if block(src) != block(again) {
		t.Error("R32: the same lock laid two different builds; retire is not idempotent")
	}
	// A FORGED FILE stamped with the retired entry's hash refuses by name, with
	// the file's own hash, once, leaving every counter zero and the destination
	// fresh. Both reads answer the same in one process.
	probe := `package probe
import "testing"
func stamp(hash uint64, layout []byte) []byte {
    file := make([]byte, TableFixedHeaderBytes+4+len(layout)+int(TFixedRecordBytes))
    file[0] = TableFixedForm
    tableFixedPut64(file[TableFixedHashAt:], hash)
    tableFixedPut32(file[TableFixedHeaderBytes:], uint32(len(layout)))
    copy(file[TableFixedHeaderBytes+4:], layout)
    tableFixedPut64(file[TableFixedHeaderBytes+4+len(layout):], hash)
    return file
}
func TestR32Once(t *testing.T) {
    known := TFixedKnown[0]
    file := stamp(known.Hash, known.Layout)
    want := known.Hash
    for half := 0; half < 2; half++ {
        got := []T{{X: 0x5a5a, Y: 0x5a5a}}
        var r TableReport
        n := TFixedLoad(got, file, make([]TableFixedEntry, 64), &r)
        if n != -1 || r.Verdict != TableOpenRefused || r.Reason != "layout_unsupported" {
            t.Fatalf("half %d: n=%d %+v, want -1 layout_unsupported by name", half, n, r)
        }
        if r.LayoutHash != want {
            t.Fatalf("half %d: layout_hash=%#x, want the file's own %#x", half, r.LayoutHash, want)
        }
        if r.Malformed || r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 {
            t.Fatalf("half %d: a refusal moved something: %+v", half, r)
        }
    }
}
`
	t03RunGo(t, u, map[string][]FixedLineageEntry{"T": {retired}}, probe)
}

// ---- go/R10 -----------------------------------------------------------------

// t03R10 holds R10: "the run-time walk of a stranger's layout and the recompute
// of the header's hash are retired". §5.3 step 4: "nothing is recomputed from
// the wire ... the definitions digest is not on the wire, so the hash cannot be
// re-derived from a file at all"; step 7: a known hash is read under THE LOCK'S
// layout bytes, a difference is layout_malformed, and "the seven §1.1 rules do
// not run at read time at all: no stranger's layout is ever walked".
func t03R10(t *testing.T) {
	u := unitFrom(t, t03Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R10: FixedLineageOf has no entry for T")
	}
	src := t03EmitLineage(t, u, map[string][]FixedLineageEntry{"T": {{
		Wire: own.Wire ^ 0x5a5a, Layout: own.Layout, Record: own.Record,
	}}})
	// THE RECOMPUTE IS RETIRED: no FNV-1a prime anywhere in the emitted build,
	// so no function in it can hash a layout; the header's hash is taken as
	// given.
	for _, prime := range []string{"0xcbf29ce484222325", "0x100000001b3", "1099511628211"} {
		if strings.Contains(src, prime) {
			t.Errorf("R10: the emitted build holds the FNV-1a prime %s; a runtime never computes a hash from layout bytes it holds (§5.3)", prime)
		}
	}
	load := funcSource(src, "func TFixedLoad(")
	if load == "" {
		t.Fatal("R10: TFixedLoad is not emitted")
	}
	t03Has(t, load, "hash := tableFixedGet64(data[TableFixedHashAt:])",
		"R10 the header's hash is taken as given")
	// THE STRANGER'S WALK IS RETIRED: the load never parses or validates the
	// file's layout bytes; it compares them against the lock's byte for byte.
	t03Has(t, load, "for i := range layout {\n\t\tif layout[i] != known.Layout[i] {",
		"R10 a known hash is read under the lock's bytes, byte for byte")
	for _, banned := range []string{"tableFixedParseLayout", "tableFixedCheckEntry", "layout_kind_unknown", "layout_size_mismatch"} {
		if strings.Contains(load, banned) {
			t.Errorf("R10: the load reaches %q: a stranger's layout is never walked at run time (§5.3 step 7)", banned)
		}
	}
}

// ---- go/R15 -----------------------------------------------------------------

// t03R15 holds R15: "the four forward-read clamps are retired — count clamp
// across bounds, range clamp across versions, remap of an unknown variant to
// None, drop-and-count of an unknown field: each is layout_newer now". §5.3
// step 5: "outside the lineage is layout_newer"; §5.6: "what is retired by this
// section" is every cross-version forward read, replaced by the refusal. A file
// whose hash is not in the lineage refuses layout_newer before any record, with
// the file's hash and nothing else, on a build that carries a lineage and so
// could otherwise have clamped.
func t03R15(t *testing.T) {
	u := unitFrom(t, t03Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R15: FixedLineageOf has no entry for T")
	}
	lineage := map[string][]FixedLineageEntry{"T": {{
		Wire: own.Wire ^ 0x77, Layout: own.Layout, Record: own.Record,
	}}}
	src := t03EmitLineage(t, u, lineage)
	t03Has(t, src, "if pick < 0 {\n\t\treturn tableFixedRefuseHash(report, \"layout_newer\", hash)\n\t}",
		"R15 a hash outside the lineage is layout_newer")
	// THE FOUR CLAMPS ARE RETIRED: the only decode is behind the selection, so
	// no forward read can clamp, remap or drop. The refusal stands before it.
	load := funcSource(src, "func TFixedLoad(")
	if i, j := strings.Index(load, "layout_newer"), strings.Index(load, "tableFixedRun("); i < 0 || j < 0 || j < i {
		t.Errorf("R15: the decode is not gated behind the layout_newer refusal (layout_newer at %d, decode at %d)", i, j)
	}
	probe := `package probe
import "testing"
func TestR15Newer(t *testing.T) {
    layout := TFixedLayout
    file := make([]byte, TableFixedHeaderBytes+4+len(layout)+int(TFixedRecordBytes))
    file[0] = TableFixedForm
    tableFixedPut64(file[TableFixedHashAt:], 0x0123456789abcdef)
    tableFixedPut32(file[TableFixedHeaderBytes:], uint32(len(layout)))
    copy(file[TableFixedHeaderBytes+4:], layout)
    tableFixedPut64(file[TableFixedHeaderBytes+4+len(layout):], 0x0123456789abcdef)
    got := []T{{X: 0x5a5a, Y: 0x5a5a}}
    var r TableReport
    n := TFixedLoad(got, file, make([]TableFixedEntry, 64), &r)
    if n != -1 || r.Verdict != TableOpenRefused || r.Reason != "layout_newer" {
        t.Fatalf("n=%d %+v, want -1 layout_newer", n, r)
    }
    if r.LayoutHash != 0x0123456789abcdef {
        t.Fatalf("layout_hash=%#x, want the file's own hash", r.LayoutHash)
    }
    if r.Malformed || r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 {
        t.Fatalf("a forward read moved something: %+v", r)
    }
}
`
	t03RunGo(t, u, lineage, probe)
}

// itoa is a strconv.Itoa for the one call site, kept local so the imports stay
// the ones the assertions read.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
