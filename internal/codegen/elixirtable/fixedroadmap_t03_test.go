package elixirtable

// THE ROADMAP'S DEFINITION-HASH AND FIXED-CLOSURE TASKS ON THE ELIXIR LEG
// (docs/roadmap.sexp node `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §1's
// layout, §5.2's THE HASH and THE DEFINITIONS DIGEST, §5.5's closure;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash";
// docs/SPEC-TABLES.md §3.4). One subtest per task id, table-driven,
// t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE ELIXIR THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling runtime checkout:
// the compile-and-run half of this leg's fixed form is fixedversioning_test.go
// and fixedroadmap_t01_test.go, which skip exactly there.
//
// elixir/R4 [weak]: the hash is fnv1a64 over the layout bytes then DIGEST(T),
// the digest computed at the hash site from the schema; a runtime never
// derives a hash from layout bytes it holds.
//
// elixir/R5 [owed]: the digest carries every range, every resolution (tag 'Q')
// and every reader limit (tag 'L'), and a flags type deduped by name, once.
//
// elixir/W11 [owed]: bytes(N) is layout kind 14.
//
// elixir/W12 [owed]: hash includes the 4-byte count.
//
// elixir/R6 [weak]: a table past §3.4's 65536 ceiling is not a fixed-form root:
// the refusal names the table, no form is emitted, and no lineage entry is
// parsed for it even when the lock carries one.
//
// elixir/R14 [owed]: layout_record_too_large for an entry reaching past the
// writer's declared record, not only for the 65536 bound and a zero root.
//
// elixir/R22 [owed]: the closure rule: every table or type reached by value is
// itself fixed; a pointer, map or unbounded array in the closure is a compile
// refusal; T is never in its own closure.

import (
	"encoding/binary"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- readers of the emitted source ------------------------------------------

var (
	t03LayoutRe = regexp.MustCompile(`@(\w+)_layout\s+("(?:\\.|[^"\\])*")`)
	t03HashRe   = regexp.MustCompile(`@(\w+)_hash 0x([0-9A-Fa-f]{16})`)
)

// t03Table resolves one declared table of a checked unit by name.
func t03Table(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	st := u.Tables[name]
	if st == nil {
		t.Fatalf("the unit declares no table %s", name)
	}
	return st
}

// t03Fixed is the emitted fixed module of a unit: the one <Base>Fixed.ex the
// table backend produced, never the shared FixedRuntime.ex or BuildVersion.ex
// beside it.
func t03Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, FixedModuleSuffix+".ex") {
			return string(data)
		}
	}
	t.Fatalf("no *%s.ex module was emitted", FixedModuleSuffix)
	return ""
}

// t03Layout reads the emitted `@<snake>_layout` binary literal, the layout
// bytes exactly as they ride (§1).
func t03Layout(t *testing.T, text, snake string) []byte {
	t.Helper()
	for _, m := range t03LayoutRe.FindAllStringSubmatch(text, -1) {
		if m[1] != snake {
			continue
		}
		b, err := strconv.Unquote(m[2])
		if err != nil {
			t.Fatalf("@%s_layout literal %s: %v", snake, m[2], err)
		}
		return []byte(b)
	}
	t.Fatalf("@%s_layout is not emitted", snake)
	return nil
}

// t03Hash reads the emitted `@<snake>_hash` constant, the eight bytes every
// record carries (§5.2).
func t03Hash(t *testing.T, text, snake string) uint64 {
	t.Helper()
	for _, m := range t03HashRe.FindAllStringSubmatch(text, -1) {
		if m[1] != snake {
			continue
		}
		n, err := strconv.ParseUint(m[2], 16, 64)
		if err != nil {
			t.Fatalf("@%s_hash: %v", snake, err)
		}
		return n
	}
	t.Fatalf("@%s_hash is not emitted", snake)
	return 0
}

// t03Has is a required substring of the emitted source, named by rule.
func t03Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t03CheckErrs parses and checks one source and answers the checker's errors,
// so a closure refusal can be read by name rather than caught as a fixture bug.
func t03CheckErrs(t *testing.T, src string) []error {
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

// t03Fnv is fnv1a64 over the run, the primitive §5.2's HASH is built from.
func t03Fnv(b []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, v := range b {
		h ^= uint64(v)
		h *= 0x100000001b3
	}
	return h
}

// ---- elixir/R4 --------------------------------------------------------------

// t03R4 holds elixir/R4 to the two clauses that make the hash a wire identity:
// the compiler's number is fnv1a64 over the layout bytes and then the digest
// computed AT THE HASH SITE from the schema (§5.2's HASH(layout_bytes, T), "the
// digest is computed AT THE HASH SITE, from the schema"), and the emitted load
// takes the file's header hash as given and never derives one from the layout
// bytes it holds (§5.2, §5.3).
func t03R4(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
    y float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	st := t03Table(t, u, "T")
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	digest := ir.TableFixedDefinitionsDigest(st)
	if len(digest) == 0 {
		t.Fatal("R4: the fixture carries no digest")
	}
	h := t03Fnv(append(append([]byte(nil), layout...), digest...))
	if got := ir.TableFixedLayoutHash(layout, st); got != h {
		t.Errorf("R4: ir's hash = %#x, want fnv1a64(layout || digest) = %#x", got, h)
	}
	if h == t03Fnv(layout) {
		t.Error("R4: the hash equals fnv1a64 over the layout alone, so the digest is not folded in")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R4: Generate: %v", err)
	}
	text := t03Fixed(t, files)
	if got := t03Hash(t, text, "t"); got != h {
		t.Errorf("R4: the emitted @t_hash = %#x, want fnv1a64(layout || digest) = %#x", got, h)
	}
	t03Has(t, text, "case R.read_file_header(data) do", "R4 the file's header is read")
	t03Has(t, text, "{:ok, stated, layout, records} ->", "R4 the header's hash is taken as given")
	t03Has(t, text, "case R.select(@t_known, @t_floor, stated, layout) do", "R4 the stated hash selects a lane")
	if strings.Contains(text, "R.hash(") {
		t.Error("R4: the emitted module derives a hash from layout bytes it holds")
	}
}

// ---- elixir/R5 --------------------------------------------------------------

// t03R5 holds elixir/R5 to §5.2's digest contract, read positionally by tag: a
// range is 'R' with its two bounds, a float range's resolution is 'Q' straight
// after it, a reader-side limit is 'L' and is RESERVED because no table
// spelling exists, and a flags type is 'F' once by bare name however many
// fields name it. The emitted hash is the leg's proof that the digest is
// folded in: a resolution that moves moves the hash.
func t03R5(t *testing.T) {
	ranged := t03Table(t, unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`), "T")
	rangedDigest := ir.TableFixedDefinitionsDigest(ranged)
	if len(rangedDigest) != 17 || rangedDigest[0] != 'R' || rangedDigest[1] != 0xfb {
		t.Errorf("R5: a range digest = % x, want 'R' and min=-5", rangedDigest)
	}

	compressed := t03Table(t, unitFrom(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`), "T")
	compressedDigest := ir.TableFixedDefinitionsDigest(compressed)
	if len(compressedDigest) != 26 || compressedDigest[0] != 'R' || compressedDigest[17] != 'Q' {
		t.Errorf("R5: a compressed-float digest = % x, want 'R' min max 'Q' resolution", compressedDigest)
	}

	flagsOnce := t03Table(t, unitFrom(t, `package probe

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

	flagsTwice := t03Table(t, unitFrom(t, `package probe

flags Marks { One, Two }

fixed table T
{
    m Marks
    n Marks
}
`), "T")
	twiceDigest := ir.TableFixedDefinitionsDigest(flagsTwice)
	if got := twiceDigest; string(got) != string(once) {
		t.Errorf("R5: the flags type is not deduped by name: once % x, twice % x", once, got)
	}

	// THE EMITTED HASH CARRIES THE DIGEST (the leg's own site, §5.2's HASH).
	compressedFiles, err := Generate(unitFrom(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`))
	if err != nil {
		t.Fatalf("R5: Generate: %v", err)
	}
	coarse := t03Table(t, unitFrom(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.1
}
`), "T")
	coarseFiles, err := Generate(unitFrom(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.1
}
`))
	if err != nil {
		t.Fatalf("R5: Generate: %v", err)
	}
	compressedHash := t03Hash(t, t03Fixed(t, compressedFiles), "t")
	coarseHash := t03Hash(t, t03Fixed(t, coarseFiles), "t")
	if compressedHash == coarseHash {
		t.Error("R5: a range and a resolution that moved left the emitted hash alone, so the digest is not in the hash site")
	}
	if got := ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(compressed)), compressed); got != compressedHash {
		t.Errorf("R5: the emitted @t_hash = %#x, want ir's hash over the digest-bearing layout = %#x", compressedHash, got)
	}
	if got := ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(coarse)), coarse); got != coarseHash {
		t.Errorf("R5: the coarse emitted @t_hash = %#x, want ir's hash = %#x", coarseHash, got)
	}

	// §5.2 reserves 'L' because no table spelling of a reader-side limit exists:
	// no member the walk can reach declares one. Prove that premise, so the
	// empty row is the page's reservation and not a coincidence of these
	// fixtures.
	for _, rt := range []reflect.Type{
		reflect.TypeFor[ir.Field](),
		reflect.TypeFor[ir.Struct](),
		reflect.TypeFor[ir.Unit](),
	} {
		for sf := range rt.Fields() {
			if strings.Contains(strings.ToLower(sf.Name), "limit") {
				t.Fatalf("R5: the IR declares %s.%s, a table-declared reader-side limit; §5.2 reserves 'L' only until one exists, and the digest must then carry it", rt.Name(), sf.Name)
			}
		}
	}
	for _, d := range [][]byte{rangedDigest, compressedDigest, once, twiceDigest} {
		for _, b := range d {
			if b == 'L' {
				t.Error("R5: the digest carries a reader-limit row no table spelling produces")
			}
		}
	}
}

// ---- elixir/W11 --------------------------------------------------------------

// t03W11 holds elixir/W11 to §1's kind table: `bytes(N)` is walked as an ARRAY
// of `u8` (kind 14 with one synthetic child at kind 6, size 1), and the byte
// the emitted layout carries for the field is the same number.
func t03W11(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    blob bytes(6)
}
`)
	st := t03Table(t, u, "T")
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
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("W11: Generate: %v", err)
	}
	emitted := t03Layout(t, t03Fixed(t, files), "t")
	for i, e := range w {
		if e.Note != "blob" {
			continue
		}
		if got := emitted[4+i*17+8]; got != 14 {
			t.Errorf("W11: the emitted layout's bytes(6) kind byte = %d, want 14", got)
		}
	}
}

// ---- elixir/W12 --------------------------------------------------------------

// t03W12 holds elixir/W12 to §1: the layout opens with the u32 entry count, and
// the hash covers it — a count that moves moves the hash. The emitted layout
// opens with the same count the walk produced.
func t03W12(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    x int32
    y int32
}
`)
	st := t03Table(t, u, "T")
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
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("W12: Generate: %v", err)
	}
	emitted := t03Layout(t, t03Fixed(t, files), "t")
	if got := binary.LittleEndian.Uint32(emitted[:4]); got != uint32(len(w)) {
		t.Errorf("W12: the emitted layout's count = %d, want %d entries", got, len(w))
	}
	if got := t03Hash(t, t03Fixed(t, files), "t"); got != ir.TableFixedLayoutHash(emitted, st) {
		t.Errorf("W12: the emitted @t_hash = %#x, want ir's hash over the emitted count-bearing layout = %#x", got, ir.TableFixedLayoutHash(emitted, st))
	}
}

// ---- elixir/R6 -----------------------------------------------------------------

// t03R6 holds elixir/R6 to SPEC-TABLES §3.4 and §5.2: a table whose record body
// is past the 65536-byte ceiling has NO FORM EMITTED, is NAMED by the compiler
// (a warning, not a refusal of the unit, since it keeps form 1), and no lineage
// entry handed in for it is parsed even when the lock carries one.
func t03R6(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Big
{
    blob bytes(70000)
}
`)
	st := t03Table(t, u, "Big")
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
	for name, body := range files {
		if strings.HasSuffix(name, FixedModuleSuffix+".ex") {
			t.Errorf("R6: %s emitted a form for a table past the ceiling", name)
		}
		if strings.Contains(string(body), "0x1122334455667788") {
			t.Errorf("R6: %s parsed the lock's lineage entry for a table past the ceiling", name)
		}
	}
}

// ---- elixir/R14 ----------------------------------------------------------------

// t03R14 holds elixir/R14 to §5.2's PLAN: the runtime bounds a single entry and
// a subtree to the 65536 a reader caps a record at, the zero root to the same
// name, and — the clause the task adds — every compiled entry to the WRITER's
// OWN declared record size, refusing the plan WHOLE under
// layout_record_too_large and never landing a short read.
func t03R14(t *testing.T) {
	runtime := fixedRuntimeBody
	t03Has(t, runtime, "@record_max 65536", "R14 the ceiling constant")
	t03Has(t, runtime, "defp layout_root({_id, 13, size, _children}) when size > 0 and size <= @record_max", "R14 the zero root and the 65536 bound")
	t03Has(t, runtime, "size > @record_max -> {:error, :layout_record_too_large}", "R14 a single entry past the 65536 bound")
	t03Has(t, runtime, "if sum > @record_max do\n          {:error, :layout_record_too_large}", "R14 a subtree's sum past the 65536 bound")
	t03Has(t, runtime, "record = size_at(theirs, 0)", "R14 the writer's own declared record is the bound")
	t03Has(t, runtime, "any_past?(entries, record) ->", "R14 every compiled entry is held to the writer's record")
	t03Has(t, runtime, "defp any_past?(entries, record), do: Enum.any?(entries, fn e -> src_end(e) > record end)", "R14 an entry reaching past the writer's declared record")
	t03Has(t, runtime, "{:error, :layout_record_too_large, report}", "R14 the plan is refused WHOLE and named layout_record_too_large")
}

// ---- elixir/R22 ----------------------------------------------------------------

// t03R22 holds elixir/R22 to §5.5 and SPEC-TABLES §2.2: a table or type reached
// by value from a fixed table is itself fixed (a nested fixed table is a closure
// member, not a break), a pointer, a map or an unbounded array in that closure
// is a compile refusal naming the construct, and a table that reaches itself by
// value — a table in its own closure — is refused.
func t03R22(t *testing.T) {
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
		{"plain table", "package probe\ntable Node { x int32 }\nfixed table T { node Node }\n", "holds the plain table Node by value"},
		{"pointer", "package probe\ntable Node { x int32 }\nfixed table Scene { head *Node }\n", "is a pointer"},
		{"map", "package probe\nfixed table Fleet { ships map[uint32]int32 }\n", "is a map"},
		{"unbounded array", "package probe\nfixed table Log { entries []int32 }\n", "is an unbounded array"},
	} {
		errs := t03CheckErrs(t, tc.src)
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
	if errs := t03CheckErrs(t, "package probe\nfixed table T { next T }\n"); len(errs) == 0 {
		t.Error("R22: a fixed table that reaches itself by value is not refused")
	}
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapT03Plans(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "elixir/R4", check: t03R4},
		{id: "elixir/R5", check: t03R5},
		{id: "elixir/W11", check: t03W11},
		{id: "elixir/W12", check: t03W12},
		{id: "elixir/R6", check: t03R6},
		{id: "elixir/R14", check: t03R14},
		{id: "elixir/R22", check: t03R22},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
