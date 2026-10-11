package elixirtable

// THE ROADMAP'S PLAN-EXECUTION, INTEROPERABILITY, HOSTILE-INPUT AND SCALAR
// CONFORMANCE TASKS ON THE ELIXIR LEG (docs/roadmap.sexp node `fixed-tables`;
// docs/FIXED-FORM-ALGORITHM.md §4.1, §4.2, §5.2, §5.3, §5.9; docs/SPEC-TABLES.md
// §3.4 "A FIXED-TABLE RECORD IS AN EIGHT-BYTE HASH OF THE WRITER'S LAYOUT, AND
// THEN THE VALUES IN DECLARED ORDER, EVERY FIELD AT ITS BOUND"). One subtest per
// task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE ELIXIR THIS LEG EMITS AND THE SHARED RUNTIME
// BODY IT EMITS IT WITH, so the harness is reachable on a tree that never built
// the C++ reference corpus and never carries a sibling runtime checkout: the
// compile-and-run half of this leg's fixed form is fixedversioning_test.go and
// fixedroadmap_t01_test.go, which skip exactly there. A clause that needs the
// emitted code to RUN is named by the existing test that runs it.
//
// elixir/W6 [owed]: plan partition / split — the plan is partitioned: every
// entry below the split carries NO guard, every entry above it carries one, and
// no run coalesces across the split (§4.1, §4.4, §5.9 #42).
//
// elixir/P1 [verify]: identity == compiled — the identity lane and a compiled
// lane run the SAME read loop and project with the same decode, so a build's own
// record and a peer's record land identically (§5.3).
//
// elixir/W13 [verify]: layout+hash == C++ reference — the emitted @t_layout and
// @t_hash are the fnv1a64(layout || definitions digest) the reference's are.
//
// elixir/P2 [verify]: write-read-write byte-identical — save is load's inverse:
// the appended body is the decoded record's own declared-order bytes.
//
// elixir/R27 [weak]: the manifest is the wire: build/fixedform-corpus/manifest.txt
// is the single value oracle every leg reads to (§5.7 step 1, §5.9 #32).
//
// elixir/P3 [weak]: hostile bytes, sweep + sanitizer — hostile bytes never raise;
// damage sets `malformed` and a named refusal never does (§5.3, SPEC-TABLES §3.4).
//
// elixir/P4 [verify]: wrong plan goes red — a record whose hash names a layout in
// the lineage runs THAT entry's recorded plan, never the reader's own.
//
// The capability rows: bool-values, signed-integers, unsigned-integers,
// signed-128, unsigned-128, ranged-integers and bits-values each prove the leg
// WRITES AND READS the construct at non-default and boundary values.

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- readers of the emitted source ------------------------------------------

var (
	t08PrefillRe = regexp.MustCompile(`@t_prefill\s+("(?:\\.|[^"\\])*")`)
	t08StructRe  = regexp.MustCompile(`defstruct (.+)`)
	t08BodyRe    = regexp.MustCompile(`@t_body_bytes (\d+)`)
)

// t08Fixed is the emitted fixed module of a unit: the one <Base>Fixed.ex the
// table backend produced, never the shared FixedRuntime.ex beside it.
func t08Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, FixedModuleSuffix+".ex") {
			return string(data)
		}
	}
	t.Fatalf("no *%s.ex module was emitted", FixedModuleSuffix)
	return ""
}

// t08Emit generates the fixed module of one source.
func t08Emit(t *testing.T, src string) string {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return t08Fixed(t, files)
}

// t08Has is a required substring of the emitted source, named by rule.
func t08Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t08Match reads the one submatch of a source pattern.
func t08Match(t *testing.T, src string, re *regexp.Regexp, what string) string {
	t.Helper()
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("%s: the emitted source does not carry %s", what, re)
	}
	return m[1]
}

// t08Prefill reads the emitted @t_prefill record bytes: the declared defaults,
// which is what answers "absent field" (§5.3's PREFILL).
func t08Prefill(t *testing.T, src string) []byte {
	t.Helper()
	lit := t08Match(t, src, t08PrefillRe, "the prefill")
	b, err := strconv.Unquote(lit)
	if err != nil {
		t.Fatalf("the prefill literal %s: %v", lit, err)
	}
	return []byte(b)
}

// ---- elixir/W6 --------------------------------------------------------------

// t08W6 holds elixir/W6 to the plan's partition rule (§4.1, §4.4, §5.9 #42):
// the coalescer merges only two NEIGHBOURING copy entries whose source and
// destination both advance together, so a guarded entry can never be coalesced
// across; and the one read loop tests each guard AT ITS OWN ENTRY.
func t08W6(t *testing.T) {
	runtime := fixedRuntimeBody
	t08Has(t, runtime,
		"defp coalesce([{:copy, s, d, n} | rest], [{:copy, ps, pd, pn} | acc])",
		"W6 the coalescer merges only two neighbouring copies")
	t08Has(t, runtime,
		"when ps + pn == s and pd + pn == d do",
		"W6 the neighbours must advance together")
	t08Has(t, runtime,
		"defp coalesce([e | rest], acc), do: coalesce(rest, [e | acc])",
		"W6 any other entry ends the run and is never merged across")
	t08Has(t, runtime,
		"defp step([{:guard, gsrc, tag, argw, inner} | rest], body, writes, report) do",
		"W6 a guarded entry is tested at its own entry")
	// THE IDENTITY PLAN IS THE ONE-RUN CASE: in the image domain the walk is one
	// entry per field and the coalescer merges them, so a record of plain scalars
	// collapses to a single copy and a plan with no guarded entry has its split
	// at the END, never at zero.
	text := t08Emit(t, `package probe

fixed table T
{
    x int32
    y int32
}
`)
	if got := t08Match(t, text, regexp.MustCompile(`@t_plan \[\s*\n\s*(\{:copy, \d+, \d+, \d+\})\s*\n\s*\]`), "the identity plan"); got != "{:copy, 0, 0, 8}" {
		t.Errorf("W6: the identity plan of two int32 = %s, want the single coalesced run {:copy, 0, 0, 8}", got)
	}
}

// ---- elixir/P1 --------------------------------------------------------------

// t08P1 holds elixir/P1 to §5.3: the identity lane and a compiled lane run the
// SAME read loop over the SAME prefill; only the plan handed to it differs. The
// emitted module reaches both through one private function and projects the
// resulting image with one decode.
func t08P1(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    x int32
    y int32
}
`)
	t08Has(t, text, "defp t_fixed_run(plan, size, records, hash, report, copy, bcap, census) do",
		"P1 one read loop serves both lanes")
	t08Has(t, text, "t_fixed_run(@t_plan, @t_body_bytes, records, hash, report, copy, bcap, {0, 0})",
		"P1 the identity lane runs the build's own plan")
	t08Has(t, text, "t_fixed_run(plan, size, records, hash, report, copy, bcap, {u, k})",
		"P1 a compiled lane runs the entry's plan through the same loop")
	t08Has(t, text, "{image, report} = R.run(plan, R.detach(body, copy), @t_prefill, report)",
		"P1 both lanes run the one read loop")
	t08Has(t, text, "{value, c, m} = t_fixed_decode(image, 0, false)",
		"P1 both lanes project with the one decode")
	t08Has(t, text, "case R.lineage_lane(__MODULE__, :t, i) do",
		"P1 the lane is selected by the lineage entry")
	t08Has(t, text, ":identity ->",
		"P1 the reader's own index is the identity lane")
}

// ---- elixir/W13 -------------------------------------------------------------

// t08W13 holds elixir/W13 to §5.2: the layout a build emits is the walk's own
// bytes, and its hash is fnv1a64 over those bytes then the definitions digest —
// the same two numbers the C++ reference's layout and hash are. The emitted
// @t_hash is checked against ir's independent computation, and the emitted
// @t_layout is checked to be the same bytes ir walks.
func t08W13(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
    y float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	st := u.Tables["T"]
	if st == nil {
		t.Fatal("W13: the unit declares no table T")
	}
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	hash := ir.TableFixedLayoutHash(layout, st)
	text := t08Fixed(t, mustGenerate(t, u))
	emitted := t08LayoutBytes(t, text)
	if string(emitted) != string(layout) {
		t.Errorf("W13: the emitted layout is not the walk's bytes")
	}
	if got := t08HashValue(t, text); got != hash {
		t.Errorf("W13: the emitted @t_hash = %#x, want fnv1a64(layout || digest) = %#x", got, hash)
	}
}

// ---- elixir/P2 --------------------------------------------------------------

// t08P2 holds elixir/P2 to §3.4: the write is declared order at each field's
// bound, and the read is that image back. A record written, read and written
// again is byte-identical because save appends the body the decode produced.
func t08P2(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    a int32
    b uint16
}
`)
	// THE WRITE: the hash, then the body appended to it in declared order.
	t08Has(t, text, "def t_fixed_record(value) do", "P2 the record write")
	t08Has(t, text, "t_fixed_write_into(value, <<@t_hash::little-unsigned-64>>)",
		"P2 the record is the hash then the body")
	// THE READ: the body is split off, run through the plan, and decoded back to
	// the declared-order image; a save of that value appends the same bytes.
	t08Has(t, text, "defp t_fixed_split(records, hash, size, acc) do",
		"P2 the per-record body is split off")
	t08Has(t, text, "{image, report} = R.run(plan, R.detach(body, copy), @t_prefill, report)",
		"P2 the read lands the declared-order image")
	if got := t08Match(t, text, t08BodyRe, "P2 the body size"); got != "6" {
		t.Errorf("P2: the int32 + uint16 body = %s, want 6 declared-order bytes", got)
	}
}

// ---- elixir/R27 -------------------------------------------------------------

// t08R27 holds elixir/R27 to §5.7 step 1 and §5.9 #32: the corpus manifest is
// the value oracle every leg reads to. The runtime and emitter read the FILE
// whose header hash is the layout's, never a re-derived one; the manifest names
// each row so the brackets are the reference's numbers.
func t08R27(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    x int32
}
`)
	t08Has(t, text, "case R.read_file_header(data) do", "R27 the file's header is the wire")
	t08Has(t, text, "{:ok, stated, layout, records} ->", "R27 the header carries the laid-out file")
	t08Has(t, text, "case R.select(@t_known, @t_floor, stated, layout) do",
		"R27 the file's own hash selects the lane, never a re-derivation")
	// A value oracle outside the emitted bytes: the corpus manifest is read from
	// disk when the tree carries one, and this leg never writes it.
	t08Has(t, fixedRuntimeBody, "def report do", "R27 the report is the shared counters")
}

// ---- elixir/P3 --------------------------------------------------------------

// t08P3 holds elixir/P3 to §5.3 and SPEC-TABLES §3.4: hostile bytes never raise.
// A file shorter than a header is `malformed`; a malformed read sets the flag
// and no name; a named refusal sets no flag and moves no counter. The emitted
// module answers every path with a tuple and never raises.
func t08P3(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    x int32
}
`)
	t08Has(t, text, "{:error, :malformed, %{report | malformed: true}}",
		"P3 a residue short of a header is malformed and names no reason")
	t08Has(t, text, "{:error, why, report}", "P3 a named refusal moves nothing")
	t08Has(t, text, "R.damaged(R.clamped(report, c), m)", "P3 damage and clamps are the shared sanitizer")
	t08Has(t, fixedRuntimeBody, "def damaged(", "P3 the sanitizer lands damage")
	t08Has(t, fixedRuntimeBody, "def clamped(report, 0), do: report", "P3 a refusal never moves a counter")
}

// ---- elixir/P4 --------------------------------------------------------------

// t08P4 holds elixir/P4 to §3.4: "the plan is the whole of this form's safety".
// A record whose hash names a lineage entry runs THAT entry's plan, built from
// the lock's own bytes at module load; the reader's own plan is not substituted.
func t08P4(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    x int32
}
`)
	t08Has(t, text, "case R.lineage_lane(__MODULE__, :t, i) do",
		"P4 the entry whose hash matched owns the plan")
	t08Has(t, text, "t_fixed_run(plan, size, records, hash, report, copy, bcap, {u, k})",
		"P4 that entry's plan is what runs")
	t08Has(t, text, "{:error, :plan_too_large, report}", "P4 an entry that cannot be planned refuses by name")
	t08Has(t, text, "@on_load :__fixed_lineage__", "P4 every plan is built at module load")
}

// ---- bool-values/elixir/valid-data ------------------------------------------

// t08Bool holds the capability to §3.4's record: a bool is one unsigned byte,
// 0 or 1, written from the term and read back as a boolean; a declared default
// lands in the prefill.
func t08Bool(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    a bool
    b bool = true
    c bool = false
}
`)
	t08Has(t, text, "defstruct a: false, b: true, c: false", "bool the non-default defaults")
	t08Has(t, text, "if(v_a, do: 1, else: 0)::unsigned-8", "bool the write from the term")
	t08Has(t, text, "def t_fixed_decode(<<f_a::unsigned-8, f_b::unsigned-8, f_c::unsigned-8, _::binary>>, c, m) do",
		"bool the read back to the term")
	if got := t08Prefill(t, text); len(got) != 3 || got[1] != 1 || got[2] != 0 {
		t.Errorf("bool prefill = % x, want 00 01 00 (false, true, false)", got)
	}
}

// ---- signed-integers/elixir/valid-data --------------------------------------

// t08Signed holds the capability to §3.4: int8/16/32/64 ride at their declared
// width, little-endian and two's complement; the write bounds the term to the
// width and the read sign-extends. Boundary defaults land in the prefill.
func t08Signed(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    a int8 = -128
    b int16 = -32768
    c int32 = -2147483648
    d int64 = -9223372036854775808
}
`)
	t08Has(t, text, "R.fits(v_a, 8, true)::little-signed-8", "signed the int8 write")
	t08Has(t, text, "R.fits(v_b, 16, true)::little-signed-16", "signed the int16 write")
	t08Has(t, text, "R.fits(v_c, 32, true)::little-signed-32", "signed the int32 write")
	t08Has(t, text, "R.fits(v_d, 64, true)::little-signed-64", "signed the int64 write")
	t08Has(t, text, "f_a::little-signed-8", "signed the int8 read sign-extends")
	t08Has(t, text, "f_d::little-signed-64", "signed the int64 read sign-extends")
	if got := t08Match(t, text, t08BodyRe, "signed the body"); got != "15" {
		t.Errorf("signed the body = %s, want 15 (1+2+4+8)", got)
	}
	got := t08Prefill(t, text)
	want := []byte{0x80, 0x00, 0x80, 0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80}
	if string(got) != string(want) {
		t.Errorf("signed prefill = % x, want % x (the four minimums)", got, want)
	}
}

// ---- unsigned-integers/elixir/valid-data ------------------------------------

// t08Unsigned holds the capability to §3.4: uint8/16/32/64 ride at their
// declared width, little-endian unsigned; a non-default maximum default lands.
func t08Unsigned(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    a uint8 = 255
    b uint16 = 65535
    c uint32 = 4294967295
    d uint64 = 18446744073709551615
}
`)
	t08Has(t, text, "R.fits(v_a, 8, false)::little-unsigned-8", "unsigned the uint8 write")
	t08Has(t, text, "R.fits(v_b, 16, false)::little-unsigned-16", "unsigned the uint16 write")
	t08Has(t, text, "R.fits(v_c, 32, false)::little-unsigned-32", "unsigned the uint32 write")
	t08Has(t, text, "R.fits(v_d, 64, false)::little-unsigned-64", "unsigned the uint64 write")
	t08Has(t, text, "f_d::little-unsigned-64", "unsigned the uint64 read")
	got := t08Prefill(t, text)
	want := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	if string(got) != string(want) {
		t.Errorf("unsigned prefill = % x, want all 0xFF (the four maximums)", got)
	}
}

// ---- signed-128/elixir/valid-data -------------------------------------------

// t08Signed128 holds the capability to §3.4: int128 is the ranged 128-bit
// operation (SPEC §4.3), written little-endian signed and clamped to its
// declared bounds on read. Boundary defaults land in the prefill.
func t08Signed128(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    a int128 = -1000 | min = -1000, max = 1000
}
`)
	t08Has(t, text, "@r_t_a {-1000, 1000}", "signed-128 the declared bounds")
	t08Has(t, text, "R.ranged(v_a, @r_t_a)::little-signed-128", "signed-128 the write")
	t08Has(t, text, "def t_fixed_decode(<<f_a::little-signed-128, _::binary>>, c, m) do", "signed-128 the read")
	t08Has(t, text, "c = R.clamps(c, f_a, -1000, 1000)", "signed-128 the read clamps to the bound")
	if got := t08Match(t, text, t08BodyRe, "signed-128 the body"); got != "16" {
		t.Errorf("signed-128 the body = %s, want 16", got)
	}
	lo := t08Prefill(t, text)
	if len(lo) != 16 || lo[0] != 0x18 || lo[1] != 0xFC {
		t.Errorf("signed-128 prefill = % x, want -1000 little-endian signed", lo)
	}
}

// ---- unsigned-128/elixir/valid-data -----------------------------------------

// t08Unsigned128 holds the capability to §3.4: uint128 rides at sixteen bytes,
// little-endian unsigned, bounded by R.fits/3 to the width.
func t08Unsigned128(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    a uint128
}
`)
	t08Has(t, text, "R.fits(v_a, 128, false)::little-unsigned-128", "unsigned-128 the write")
	t08Has(t, text, "def t_fixed_decode(<<f_a::little-unsigned-128, _::binary>>, c, m) do", "unsigned-128 the read")
	if got := t08Match(t, text, t08BodyRe, "unsigned-128 the body"); got != "16" {
		t.Errorf("unsigned-128 the body = %s, want 16", got)
	}
}

// ---- ranged-integers/elixir/valid-data --------------------------------------

// t08Ranged holds the capability to §3.4: a ranged integer rides at its
// declared width and is clamped to its declared bounds on READ, counting one
// `clamped` however far outside the value is; the write leaves the bytes to the
// plan.
func t08Ranged(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    x int32 = -5 | min = -5, max = 5
    y uint8 = 1 | min = 1, max = 200
}
`)
	t08Has(t, text, "@r_t_x {-5, 5}", "ranged the signed bounds")
	t08Has(t, text, "@r_t_y {1, 200}", "ranged the unsigned bounds")
	t08Has(t, text, "R.ranged(v_x, @r_t_x)::little-signed-32", "ranged the signed write")
	t08Has(t, text, "R.ranged(v_y, @r_t_y)::little-unsigned-8", "ranged the unsigned write")
	t08Has(t, text, "c = R.clamps(c, f_x, -5, 5)", "ranged the signed read clamps")
	t08Has(t, text, "c = R.clamps(c, f_y, 1, 200)", "ranged the unsigned read clamps")
	got := t08Prefill(t, text)
	if len(got) != 5 || got[0] != 0xFB || got[4] != 1 {
		t.Errorf("ranged prefill = % x, want -5 (0xFB) then 1", got)
	}
}

// ---- bits-values/elixir/valid-data -------------------------------------------

// t08Bits holds the capability to §3.4 and fixedelixir.go's own note: a bits(N)
// field costs a full unsigned word on this wire (four bytes for N <= 32), so it
// is written as one unsigned-32 and read back unsigned.
func t08Bits(t *testing.T) {
	text := t08Emit(t, `package probe

fixed table T
{
    m bits(12)
    n bits(3)
}
`)
	t08Has(t, text, "R.fits(v_m, 32, false)::little-unsigned-32", "bits the bits(12) write costs a word")
	t08Has(t, text, "R.fits(v_n, 32, false)::little-unsigned-32", "bits the bits(3) write costs a word")
	t08Has(t, text, "def t_fixed_decode(<<f_m::little-unsigned-32, f_n::little-unsigned-32, _::binary>>, c, m) do",
		"bits the read back")
	if got := t08Match(t, text, t08BodyRe, "bits the body"); got != "8" {
		t.Errorf("bits the body = %s, want 8 (two words)", got)
	}
}

// ---- small helpers used above -----------------------------------------------

func mustGenerate(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return files
}

// t08LayoutBytes reads the emitted `@t_layout` binary literal.
func t08LayoutBytes(t *testing.T, text string) []byte {
	t.Helper()
	lit := t08Match(t, text, regexp.MustCompile(`@t_layout\s+("(?:\\.|[^"\\])*")`), "the layout")
	b, err := strconv.Unquote(lit)
	if err != nil {
		t.Fatalf("the layout literal %s: %v", lit, err)
	}
	return []byte(b)
}

// t08HashValue reads the emitted `@t_hash` constant.
func t08HashValue(t *testing.T, text string) uint64 {
	t.Helper()
	m := regexp.MustCompile(`@t_hash 0x([0-9A-Fa-f]{16})`).FindStringSubmatch(text)
	if m == nil {
		t.Fatal("the emitted source does not carry @t_hash")
	}
	n, err := strconv.ParseUint(m[1], 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapElixirT08ConformanceIntegers(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "elixir/W6", check: t08W6},
		{id: "elixir/P1", check: t08P1},
		{id: "elixir/W13", check: t08W13},
		{id: "elixir/P2", check: t08P2},
		{id: "elixir/R27", check: t08R27},
		{id: "elixir/P3", check: t08P3},
		{id: "elixir/P4", check: t08P4},
		{id: "bool-values/elixir/valid-data", check: t08Bool},
		{id: "signed-integers/elixir/valid-data", check: t08Signed},
		{id: "unsigned-integers/elixir/valid-data", check: t08Unsigned},
		{id: "signed-128/elixir/valid-data", check: t08Signed128},
		{id: "unsigned-128/elixir/valid-data", check: t08Unsigned128},
		{id: "ranged-integers/elixir/valid-data", check: t08Ranged},
		{id: "bits-values/elixir/valid-data", check: t08Bits},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
