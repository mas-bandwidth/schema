package rusttable

// THE ROADMAP'S WRITE-SIDE AND LIVE-EXTENT TASKS ON THE RUST LEG
// (docs/roadmap.sexp node `fixed-tables`, work-sets `live-extents/rust` and
// `writing/rust`; docs/FIXED-FORM-ALGORITHM.md §3.1 "The write", §4.3 "The
// prefill", §4.6 "The bounds pass"; docs/FIXED-FORM-VERSIONING-TESTS.md "The
// rows" — `bytes_grow`, `string_grow`, `array_bounded_grow`; docs/SPEC-TABLES.md
// §3.4). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs. A
// clause that needs the emitted code to RUN is asserted at the emitted site
// that runs it, and that site is named in the check — the shape
// fixedroadmap_t04_test.go states for the same reason.
//
// rust/C14 [owed]: the bounds pass walks only what a read can have written —
// a counted array's LIVE elements, an optional's payload only when the present
// byte says so, a union's arm only when the tag names it.
//
// rust/W4 [weak]: read slack is unspecified and validates the used units only;
// non-zero slack is not malformed, not a refusal, and moves no counter.
//
// rust/W9 [owed]: prefill the bytes the plan does not write; the identity
// plan's list is empty.
//
// rust/W1 [verify]: write slack is the template's zeros — the save zeroes the
// body and the one writer touches the live extent only.
//
// rust/W3 [owed]: a union writes the tag and the taken arm only, zero behind a
// narrower one.
//
// rust/W5 [owed]: write-side bound checks are DEBUG ONLY.
//
// rust/W8 [weak]: bytes(N) takes the ARRAY row — kind 14 with one u8 child.

import (
	"encoding/binary"
	"strings"
	"testing"
)

// ---- fixtures and readers of this leg's output ------------------------------

// t07Fixed is the emitted fixed module of a unit (the one *_fixed.rs the table
// backend produced).
func t07Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

// t07Fn is the emitted body of one `pub fn NAME(...)`, up to its column-zero
// closing brace.
func t07Fn(text, name string) string {
	at := strings.Index(text, "pub fn "+name+"(")
	if at < 0 {
		return ""
	}
	rest := text[at:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// t07Has is a required substring of the emitted source, named by rule.
func t07Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t07BlockEntry is one parsed layout entry, read out of the emitted
// <UP>_FIXED_BLOCK array: seventeen bytes each, a count in front (§3.4).
type t07BlockEntry struct {
	kind     int
	size     uint32
	children uint32
}

// t07Block parses the emitted block constant of one unit.
func t07Block(t *testing.T, text, name string) []t07BlockEntry {
	t.Helper()
	raw := t02ByteArray(t, text, name)
	if len(raw) < 4 {
		t.Fatalf("%s: the block is %d bytes, want at least the count", name, len(raw))
	}
	count := int(binary.LittleEndian.Uint32(raw[0:4]))
	var out []t07BlockEntry
	for i := 0; i < count; i++ {
		at := 4 + i*17
		if at+17 > len(raw) {
			t.Fatalf("%s: entry %d runs past the block", name, i)
		}
		out = append(out, t07BlockEntry{
			kind:     int(raw[at+8]),
			size:     binary.LittleEndian.Uint32(raw[at+9 : at+13]),
			children: binary.LittleEndian.Uint32(raw[at+13 : at+17]),
		})
	}
	return out
}

// ---- rust/C14 -----------------------------------------------------------------

// t07C14 holds rust/C14 to §4.6's walk: "It walks only what a read can have
// written — a counted array's LIVE elements and never its slack, an optional's
// payload only when the present byte says so". A counted array's clamp loop is
// bounded by the live COUNT and never the declared BOUND; an optional's clamp
// stands behind its present byte; a union's clamp is guarded by the tag. The
// fixed `[4]` row is the control: every slot is live there, so `0..4` is right
// and its absence would say the pass stopped walking at all.
func t07C14(t *testing.T) {
	text := t07Fixed(t, t07gen(t, `package probe

union Pick
{
    alpha int32 | min = 0, max = 10
    beta int32
}

fixed table Lineage
{
    a [..4]int32 | min = 0, max = 10
    f ?int32 | min = 0, max = 10
    g Pick
}
`))
	clamp := t07Fn(text, "lineage_fixed_clamp_body")
	if clamp == "" {
		t.Fatal("C14: lineage_fixed_clamp_body is not emitted")
	}
	t07Has(t, clamp, "for i in 0..value.a_count.clamp(0, 4) as usize {", "C14 a counted array walks its LIVE elements")
	t07Has(t, clamp, "if value.f_present {", "C14 an optional's payload is walked only when present")
	t07Has(t, clamp, "pick_fixed_clamp_body(&mut value.g, clamped, damaged);", "C14 the union's own pass is called")
	if strings.Contains(clamp, "for i in 0..4 {") {
		t.Error("C14: the counted array's clamp loop walks the declared bound and not the live count")
	}
	union := t07Fn(text, "pick_fixed_clamp_body")
	if union == "" {
		t.Fatal("C14: pick_fixed_clamp_body is not emitted")
	}
	t07Has(t, union, "if value.tag == 1 {", "C14 a union's bound is held over the arm the tag names")
}

// ---- rust/W4 -----------------------------------------------------------------

// t07W4 holds rust/W4 to §3.1's read rule: "Slack is UNSPECIFIED on read and
// not a refusal: a reader validates the USED UNITS only, and non-zero slack is
// not `malformed`, not a refusal, and moves no counter." The text content rule
// is applied over the used length (`..value.c_length.clamp(0, 8)`), never the
// declared span, so the slack behind it is never read; a counted array's clamp
// loop is the live count; and a `bytes(N)` has no content rule at all, so a
// bytes-only unit emits no bounds body.
func t07W4(t *testing.T) {
	text := t07Fixed(t, t07gen(t, `package probe

fixed table Lineage
{
    a [..4]int32 | min = 0, max = 10
    c string(8)
}
`))
	clamp := t07Fn(text, "lineage_fixed_clamp_body")
	if clamp == "" {
		t.Fatal("W4: lineage_fixed_clamp_body is not emitted")
	}
	t07Has(t, clamp, "table_utf8_valid(&value.c[..value.c_length.clamp(0, 8) as usize])", "W4 text is validated over the USED units")
	t07Has(t, clamp, "for i in 0..value.a_count.clamp(0, 4) as usize {", "W4 a counted array's bounds cover its LIVE elements")
	if strings.Contains(clamp, "value.c[..8]") {
		t.Error("W4: the content rule is applied over the declared span, so the slack is being read")
	}

	// A bytes(N) HAS NO CONTENT RULE: it is bytes, so a unit whose only
	// bounded thing is a bytes(N) emits no bounds body at all.
	bs := t07Fixed(t, t07gen(t, "package probe\n\nfixed table Lineage\n{\n    d bytes(8)\n}\n"))
	if got := t07Fn(bs, "lineage_fixed_clamp_body"); got != "" {
		t.Errorf("W4: a bytes(N) emitted a content bounds body, so slack is being validated:\n%s", got)
	}
}

// ---- rust/W9 -----------------------------------------------------------------

// t07W9 holds rust/W9 to §4.3: "Prefill the bytes the plan does not write. The
// plan compiler computes the UNWRITTEN RANGES — the destination bytes no entry
// covers — and only those take the declared defaults before the loop; for the
// identity plan that list is EMPTY." The load computes the holes and copies the
// default image into exactly those; the runtime's complement walk skips every
// guarded entry (a union arm is not always written), so only unwritten ranges
// are prefilled; and the identity plan is ONE Copy of the whole body, so its
// hole list is empty.
func t07W9(t *testing.T) {
	text := t07Fixed(t, t07gen(t, `package probe

fixed table Lineage
{
    x int32
    y int32
}
`))
	load := t07Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("W9: lineage_fixed_load is not emitted")
	}
	t07Has(t, load, "let hole_n = table_fixed_holes(entries, &mut cover, &mut hole_buf);", "W9 the load computes the plan's unwritten ranges")
	t07Has(t, load, "image[o..o + z].copy_from_slice(&LINEAGE_FIXED_DEFAULTS[o..o + z]);", "W9 the declared defaults land in the holes")
	runtime := string(fixedRuntimeBody)
	t07Has(t, runtime, "if p.guard_count != 0 {", "W9 a guarded entry is not a write, so it stays a hole")
	// THE IDENTITY PLAN IS ONE COPY OF THE WHOLE BODY, so its hole list is
	// empty: there is no identity flag in the loop, an empty list is what
	// skips the work.
	plan := t02Const(text, "LINEAGE_FIXED_PLAN")
	t07Has(t, plan, "LINEAGE_FIXED_PLAN: [TableFixedEntry; 1]", "W9 the identity plan is one entry")
	t07Has(t, plan, "size: LINEAGE_FIXED_BODY_BYTES as u32,", "W9 the one entry covers the whole body, so no hole")
	if i := strings.Index(load, "for k in 0..n"); i < 0 || strings.Contains(load[i:], "identity") {
		t.Error("W9: the record loop still branches on identity; an empty hole list is what skips the work")
	}
}

// ---- rust/W1 -----------------------------------------------------------------

// t07W1 holds rust/W1 to §3.1's template: "The template is a compile-time
// constant — the hash, then zero everywhere a value lands — which zero-fills
// every byte of declared slack without the writer touching it", and "the slack
// rule is one rule: write the live extent onto the zeroed template and stop".
// The save zeroes each record body, the writer's own comment says it touches no
// slack, and the live extents are the count and the used length.
func t07W1(t *testing.T) {
	text := t07Fixed(t, t07gen(t, liveWriteSrc))
	save := t07Fn(text, "probe_fixed_save")
	write := t07Fn(text, "probe_fixed_write_body")
	if save == "" || write == "" {
		t.Fatal("W1: probe_fixed_save or probe_fixed_write_body is not emitted")
	}
	t07Has(t, save, "body.fill(0); // the template's zeros, and every byte of declared slack with them", "W1 the body is the zeroed template")
	t07Has(t, text, "declared slack without this function touching it", "W1 the writer touches no slack")
	t07Has(t, write, "for i in 0..value.marks_count.clamp(0, 4) as usize {", "W1 a counted array writes the live count")
	t07Has(t, write, "let n = value.label_length.clamp(0, 8) as usize;", "W1 text writes the used length")
	if strings.Contains(write, "value.label[..8]") {
		t.Error("W1: the writer dumps the declared span, so the caller's slack reaches the wire")
	}
}

// ---- rust/W3 -----------------------------------------------------------------

// t07W3 holds rust/W3 to §3.1's union: "A union writes the tag and the taken
// arm only, zero behind a narrower one." The tags are of different widths, so
// the bytes past the narrower arm are the template's zeros: the writer stores
// the tag then exactly the taken arm inside its `match`, and the save's
// `body.fill(0)` is what makes the rest zero.
func t07W3(t *testing.T) {
	text := t07Fixed(t, t07gen(t, `package probe

union Pick
{
    alpha int32
    beta int64
}

fixed table Lineage
{
    g Pick
}
`))
	write := t07Fn(text, "pick_fixed_write_body")
	if write == "" {
		t.Fatal("W3: pick_fixed_write_body is not emitted")
	}
	t07Has(t, write, "b[0] = value.tag;", "W3 the tag is written first")
	t07Has(t, write, "match value.tag {", "W3 only the taken arm is written")
	t07Has(t, write, "b[at..at + 4].copy_from_slice(&(arm as u32).to_le_bytes());", "W3 the narrower arm writes four bytes")
	t07Has(t, text, "they stay the template's zeros", "W3 the bytes behind a narrower arm are the template's zeros")
	if strings.Contains(write, ".fill(") {
		t.Error("W3: the union writer clears bytes itself instead of leaving them to the template")
	}
	// the save is what zeroes the bytes behind the narrower arm
	t07Has(t, t07Fn(text, "lineage_fixed_save"), "body.fill(0);", "W3 the template behind the narrower arm is zero")
}

// ---- rust/W5 -----------------------------------------------------------------

// t07W5 holds rust/W5 to §3.1: "Write-side bound checks are DEBUG ONLY, by
// rule. `count <= Max` and `length <= N` are a caller contract; a release build
// removes them exactly as it removes `assert`, pays nothing, and clamps
// nothing." The writer's every bound check is a `debug_assert!` and there is no
// `assert!` or `panic!` beside it.
func t07W5(t *testing.T) {
	text := t07Fixed(t, t07gen(t, liveWriteSrc))
	write := t07Fn(text, "probe_fixed_write_body")
	if write == "" {
		t.Fatal("W5: probe_fixed_write_body is not emitted")
	}
	debug := strings.Count(write, "debug_assert!(")
	if debug == 0 {
		t.Fatal("W5: the writer carries no bound check at all")
	}
	if all := strings.Count(write, "assert!("); all != debug {
		t.Errorf("W5: the writer carries %d bound checks and only %d are DEBUG — a release build still pays", all, debug)
	}
	if strings.Contains(write, "panic!") {
		t.Error("W5: the writer can panic; the write-side check is DEBUG only by rule")
	}
}

// ---- rust/W8 -----------------------------------------------------------------

// t07W8 holds rust/W8 to §4.1: "A `bytes(N)` takes the ARRAY's row, not a text
// field's". On this wire that row is kind 14 with one synthetic u8 child at
// kind 6, size 1 — the same walk `[..N]int32` takes — where a `string(N)` takes
// the text row, kind 12 with the same u8 child.
func t07W8(t *testing.T) {
	text := t07Fixed(t, t07gen(t, `package probe

fixed table Lineage
{
    c string(8)
    d bytes(8)
}
`))
	entries := t07Block(t, text, "LINEAGE_FIXED_BLOCK")
	if len(entries) != 4 {
		t.Fatalf("W8: the block carries %d entries, want the root, the string, the bytes and one u8 child", len(entries))
	}
	if entries[0].kind != 13 {
		t.Fatalf("W8: the root's kind is %d, want 13 (the table)", entries[0].kind)
	}
	// entries[1] is the string: the TEXT row, kind 12 with NO child;
	// entries[2] is the bytes: the ARRAY row, kind 14 with one u8 child;
	// entries[3] is that child.
	if entries[1].kind != 12 || entries[1].children != 0 {
		t.Errorf("W8: the string is kind %d with %d children, want the text row 12 with none", entries[1].kind, entries[1].children)
	}
	if entries[2].kind != 14 || entries[2].children != 1 {
		t.Errorf("W8: the bytes field is kind %d with %d children, want the array row 14 with one", entries[2].kind, entries[2].children)
	}
	if entries[3].kind != 6 || entries[3].size != 1 {
		t.Errorf("W8: the bytes element is kind %d size %d, want a u8 (kind 6) at size 1", entries[3].kind, entries[3].size)
	}
	// THE WIRE SIZE IS THE ARRAY'S TOO: a count word then N bytes.
	if got := t02IntConst(t, text, "LINEAGE_FIXED_BODY_BYTES"); got != 12+12 {
		t.Errorf("W8: the body is %d, want 4 + 8 for the string and 4 + 8 for the bytes", got)
	}
	t07Has(t, string(fixedRuntimeBody), "12 | 33 => {", "W8 text is the string/wstring row")
	t07Has(t, string(fixedRuntimeBody), "14 => {", "W8 the array row, which bytes(N) rides")
}

// t07gen is a fixture's emitted module of files.
func t07gen(t *testing.T, src string) map[string][]byte {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return files
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapRustT07Writes(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/C14", check: t07C14},
		{id: "rust/W4", check: t07W4},
		{id: "rust/W9", check: t07W9},
		{id: "rust/W1", check: t07W1},
		{id: "rust/W3", check: t07W3},
		{id: "rust/W5", check: t07W5},
		{id: "rust/W8", check: t07W8},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
