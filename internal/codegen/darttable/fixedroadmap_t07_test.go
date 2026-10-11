package darttable

// THE DART LEG'S FIXED-TABLE WRITE / LIVE-EXTENT / UNION-GUARD ROWS
// (docs/roadmap.sexp node `fixed-tables`, rows live-extents, writing and
// union-guards). Each subtest is one roadmap task id and asserts the clause its
// own title states. The subject is the emitter's OWN bytes — the generated
// <Base>Fixed.dart and the compiler's own plan walk — where the sentence is a
// fact about what the emitter lays down, and the leg's toolchain where a value
// has to be read back.
//
// Law: docs/FIXED-FORM-ALGORITHM.md §3.1 (THE WRITE: "the template is a
// compile-time constant — the hash, then zero everywhere a value lands — which
// zero-fills every byte of declared slack"), §4.1 (THE PLAN: "arg and meta ARE
// TWO LANES BECAUSE THEY ARE TWO FACTS" and the guard chain), §4.3 (THE
// PREFILL: "Prefill the bytes the plan does not write ... for the identity plan
// that list is EMPTY"), §4.6 (THE BOUNDS PASS: "It walks only what a read can
// have written — a counted array's LIVE elements and never its slack"),
// docs/SPEC-TABLES.md §3.4 and ROADMAP.md "NEW Fixed Tables".
//
// The harness is TABLE-DRIVEN, one subtest per task id, and every assertion
// quotes the task sentence it implements. A clause an existing test of this leg
// already holds is named by that test in the card's verdicts and is not
// repeated here. dart/W16 and dart/R28 are REPORTED, not decided here: the
// generated Dart plan carries no second guard lane and no guard chain, so the
// nested-outer-tag clause is unproved on this leg (see the card's verdicts).

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t07LiveSchema is the write/live-extent fixture: a counted array (the only
// construct whose live extent differs from its declared bound), a string and a
// bytes(N), so the template zeros and the live-only walks are all exercised.
const t07LiveSchema = `package probe

enum Tier { bronze, silver, gold }

fixed table Live
{
    lead  uint32 = 1
    tiers [..4]Tier
    name  string(8) = ""
    blob  bytes(6)
    tail  uint32 = 2
}
`

// t07GuardSchema is the union fixture: arms of different widths (so the slack
// behind a narrower arm is visible) and a string under the SECOND arm, so the
// guard's ordinal (2) and the text flavour (1) are two different numbers and a
// single shared lane would be caught.
const t07GuardSchema = `package probe

type Beta   { n int32 = 0 }
type Alpha  { s string(8) = "" }
type Narrow { a int32 = 0 }
type Wide   { a int32 = 0
    b int32 = 0
    c int32 = 0 }

union Pick
{
    beta   Beta
    alpha  Alpha
    narrow Narrow
    wide   Wide
}

fixed table Guard
{
    lead uint32 = 1
    pick Pick
    tail uint32 = 2
}
`

func t07LiveSrc(t *testing.T) (string, *ir.Struct) {
	t.Helper()
	u := unitFrom(t, t07LiveSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate Live: %v", err)
	}
	return t02Source(t, files), findTable(t, u, "Live")
}

func t07GuardSrc(t *testing.T) (string, *ir.Struct) {
	t.Helper()
	u := unitFrom(t, t07GuardSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate Guard: %v", err)
	}
	return t02Source(t, files), findTable(t, u, "Guard")
}

func TestFixedRoadmapDartT07Writes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"dart/C14", t07C14},
		{"dart/W4", t07W4},
		{"dart/W9", t07W9},
		{"dart/W1", t07W1},
		{"dart/W3", t07W3},
		{"dart/W5", t07W5},
		{"dart/W8", t07W8},
		{"dart/W7", t07W7},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t07C14: C14 "bounds pass walks live only". docs/FIXED-FORM-ALGORITHM.md §4.6:
// "THE PASS RUNS OVER STORAGE ... It walks only what a read can have written —
// a counted array's LIVE elements and never its slack — because the prefill's
// defaults are in range by construction, and clamping storage nobody wrote
// would count a clamp on every clean read." The generated decode clamps the
// counted array over its LIVE count and never over the declared bound.
func t07C14(t *testing.T) {
	src, _ := t07LiveSrc(t)
	dec := t02Block(t, src, "void liveFixedDecode(", "\n}\n")
	t02Has(t, src, "ARRAY WALKS ITS LIVE COUNT, never the bound",
		"C14 the decode states the live-only rule")
	t02Has(t, dec, "i < value.tiersCount",
		"C14 the counted array's clamp loop is bounded by the LIVE count")
	if strings.Contains(dec, "i < 4") {
		t.Errorf("C14: the decode walks the declared bound (i < 4), clamping slack nobody wrote")
	}
	t02Has(t, dec, "report.clamped++",
		"C14 the pass clamps a used unit and counts it once")
}

// t07W4: W4 "read slack unspecified". docs/FIXED-FORM-ALGORITHM.md §3.1:
// "Slack is UNSPECIFIED on read and not a refusal: a reader validates the USED
// UNITS only, and non-zero slack is not `malformed`, not a refusal, and moves
// no counter." The generated decode names neither `malformed` nor a refusal,
// and holds the used length to its cap.
func t07W4(t *testing.T) {
	src, _ := t07LiveSrc(t)
	dec := t02Block(t, src, "void liveFixedDecode(", "\n}\n")
	if strings.Contains(dec, "malformed") {
		t.Errorf("W4 read slack unspecified: the decode names malformed, so a slack byte reads as damage")
	}
	if strings.Contains(dec, "refused") {
		t.Errorf("W4 read slack unspecified: the decode names a refusal")
	}
	t02Has(t, dec, "value.nameLength > 8",
		"W4 the reader validates the USED UNITS only (the text length's cap)")
	t02Has(t, dec, "value.blobLength > 6",
		"W4 the reader validates the USED UNITS only (the bytes length's cap)")
}

// t07W9: W9 "prefill unwritten ranges only". docs/FIXED-FORM-ALGORITHM.md §4.3:
// "Prefill the bytes the plan does not write. The plan compiler computes the
// UNWRITTEN RANGES — the destination bytes no entry covers — and only those
// take the declared defaults before the loop; for the identity plan that list
// is EMPTY, so the identity read pays no prefill." The identity prefill list
// the leg emits is empty, while the type's value-byte cover the prefill is
// computed against is not.
func t07W9(t *testing.T) {
	src, st := t07LiveSrc(t)
	t02Has(t, src, "THE PREFILL: the declared defaults as a constant run of bytes",
		"W9 the prefill is a constant of the declared defaults")
	t02Has(t, src, "identity's list is empty",
		"W9 the load copies only the ranges the plan does not land")
	load := t02Block(t, src, "int liveFixedLoad(", "\n}\n")
	t02Has(t, load, "var fillCount = 0;",
		"W9 the prefill's list starts EMPTY and only a compiled lane fills it")
	t02Has(t, load, "fillCount = lane.fillCount;",
		"W9 only a plan compiled from a stranger's layout sets the prefill ranges")
	if strings.Contains(load, "fillCount = liveFixed") {
		t.Errorf("W9: the identity lane fills a prefill list, but identity's list is EMPTY")
	}
	i := strings.Index(src, "liveFixedCoverCount = ")
	if i < 0 {
		t.Fatal("W9: the leg publishes no cover count")
	}
	rest := src[i+len("liveFixedCoverCount = "):]
	end := strings.IndexByte(rest, ';')
	if end < 0 {
		t.Fatal("W9: the published cover count has no end")
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		t.Fatalf("W9: the cover count is not a number: %v", err)
	}
	if n == 0 {
		t.Errorf("W9: the type's value-byte cover is empty, so 'the prefill is the cover minus what a plan lands' cannot bite")
	}
	if len(fixedIdentityCover(fixedIdentityPlan(st))) == 0 {
		t.Errorf("W9: the identity plan lands no value byte, so the prefill subtraction is vacuous")
	}
}

// t07W1: W1 "write slack is template zeros". docs/FIXED-FORM-ALGORITHM.md §3.1:
// "The template is a compile-time constant — the hash, then zero everywhere a
// value lands — which zero-fills every byte of declared slack without the
// writer touching it ... the slack rule is one rule: write the live extent onto
// the zeroed template and stop. Arrays write count elements and text and bytes
// length units, never all Max". The generated save zeroes every record body,
// the counted array writes its LIVE count only, and the text write zeroes the
// slack behind its live length.
func t07W1(t *testing.T) {
	src, _ := t07LiveSrc(t)
	save := t02Block(t, src, "int liveFixedSave(", "\n}\n")
	t02Has(t, save, "bytes.fillRange(", "W1 the write lays the zero template down")
	t02Has(t, save, "liveFixedRecordBytes", "W1 the template's zeros cover the whole record body")
	body := t02Block(t, src, "void liveFixedWriteBody(", "\n}\n")
	t02Has(t, body, "i < value.tiersCount",
		"W1 a counted array writes its LIVE count, so unused slots stay the template's zeros")
	if strings.Contains(body, "i < 4") {
		t.Errorf("W1: the write walks the declared bound (i < 4), putting an element's storage on the wire")
	}
	t02Has(t, body, "bytes.fillRange(",
		"W1 the text/bytes write zeroes the slack behind the live length")
}

// t07W3: W3 "zero behind a narrower arm". docs/FIXED-FORM-ALGORITHM.md §3.1:
// "A union writes the tag and the taken arm only, zero behind a narrower one."
// The generated union write is a switch that stores ONLY the arm the tag names;
// the record template's zeros are what the slack behind a narrower arm holds.
func t07W3(t *testing.T) {
	src, _ := t07GuardSrc(t)
	save := t02Block(t, src, "int guardFixedSave(", "\n}\n")
	t02Has(t, save, "bytes.fillRange(", "W3 the record template is zeroed, so the slack behind a narrower arm is zero")
	t02Has(t, save, "guardFixedRecordBytes", "W3 the zero template covers the whole record")
	body := t02Block(t, src, "void guardFixedWriteBody(", "\n}\n")
	t02Has(t, body, "switch (value.pick.type)", "W3 a union writes the taken arm only")
	t02Has(t, body, "case 1:", "W3 every arm is written under its own tag value")
	t02Has(t, body, "default:", "W3 an arm the tag does not name is never written")
	// the widest arm's tail is not stored unconditionally: the only stores are
	// inside the switch's cases, and the default does nothing.
	def := ""
	if i := strings.Index(body, "default:"); i >= 0 {
		def = body[i:]
	}
	if !strings.Contains(def, "break;") {
		t.Errorf("W3: the union write's default branch stores something, so it writes an arm the tag did not select")
	}
}

// t07W5: W5 "write checks DEBUG only". docs/FIXED-FORM-ALGORITHM.md §3.1:
// "Write-side bound checks are DEBUG ONLY, by rule. `count <= Max` and
// `length <= N` are a caller contract; a release build removes them exactly as
// it removes `assert`, pays nothing, and clamps nothing." The generated write
// holds both checks with `assert(` and neither refuses nor clamps.
func t07W5(t *testing.T) {
	src, _ := t07LiveSrc(t)
	body := t02Block(t, src, "void liveFixedWriteBody(", "\n}\n")
	t02Has(t, body, "assert(value.tiersCount <= 4);", "W5 the count bound is a DEBUG assert")
	t02Has(t, body, "assert(value.nameLength <= 8);", "W5 the text length bound is a DEBUG assert")
	t02Has(t, body, "assert(value.blobLength <= 6);", "W5 the bytes length bound is a DEBUG assert")
	for _, banned := range []string{"report.refused", "report.clamped", "return -1", "clamp"} {
		if strings.Contains(body, banned) {
			t.Errorf("W5: the write names %q, so a bound check runs in release or moves a counter", banned)
		}
	}
}

// t07W8: W8 "bytes(N) takes the array row". docs/FIXED-FORM-ALGORITHM.md §4.1:
// "A `bytes(N)` takes the ARRAY's row, not a text field's — the count to `aux`
// and the elements to `dst`, where a text field's row is the other way round."
// The walk's own destination rows carry it: bytes(N) is Dst the buffer and Aux
// the count; a text field is Dst the length and Aux the buffer.
func t07W8(t *testing.T) {
	_, st := t07LiveSrc(t)
	w := fixedWalkRoot(st)
	var blob, name *fixedDst
	for i, e := range w.entries {
		switch e.note {
		case "blob":
			blob = &w.dst[i]
		case "name":
			name = &w.dst[i]
		}
	}
	if blob == nil || name == nil {
		t.Fatal("W8: the walk carries no blob/name destination row")
	}
	if blob.counted != 1 || blob.stride != 1 || blob.arg != fixedTextBytes {
		t.Errorf("W8: bytes(N)'s row is (counted %d, stride %d, arg %d), want the ARRAY row (1, 1, %d)", blob.counted, blob.stride, blob.arg, fixedTextBytes)
	}
	if blob.dst != blob.aux+fixedCountBytes {
		t.Errorf("W8: bytes(N)'s Dst (%d) is not the buffer past the count field (Aux %d + 4): the count must go to Aux and the elements to Dst", blob.dst, blob.aux)
	}
	if name.counted != 0 || name.arg != fixedTextUtf8 {
		t.Errorf("W8: the text row is (counted %d, arg %d), want the TEXT row (0, %d)", name.counted, name.arg, fixedTextUtf8)
	}
	if name.dst != name.aux-fixedCountBytes {
		t.Errorf("W8: the text Dst (%d) is not the length field before the buffer (Aux %d - 4): the TEXT row is the other way round", name.dst, name.aux)
	}
}

// t07W7: W7 "arg and meta are two lanes". docs/FIXED-FORM-ALGORITHM.md §4.1:
// "`arg` and `meta` ARE TWO LANES BECAUSE THEY ARE TWO FACTS, and must never
// share one. (fix 12) A `string(N)` under a union arm needs both, and one lane
// gives whichever was stamped last: a byte string under arm `2` read as WIDE,
// or an entry that runs only when the tag equals the flavour — under the wrong
// arm." A guarded text entry carries the arm's ordinal in `arg` and the text
// flavour in `meta`, and the two numbers differ.
func t07W7(t *testing.T) {
	src, st := t07GuardSrc(t)
	plan := fixedIdentityPlan(st)
	found := false
	for _, e := range plan {
		if e.op != fixedOpText || e.guard == fixedNoGuard {
			continue
		}
		found = true
		if e.arg != 2 {
			t.Errorf("W7: the guarded text entry's arg is %d, want the arm ordinal 2", e.arg)
		}
		if e.meta != fixedTextUtf8 {
			t.Errorf("W7: the guarded text entry's meta is %d, want the text flavour %d", e.meta, fixedTextUtf8)
		}
		if e.arg == e.meta {
			t.Errorf("W7: arg and meta are both %d, so one lane could carry them", e.arg)
		}
	}
	if !found {
		t.Fatal("W7: no guarded text entry on the identity plan, so the two-lane clause cannot bite")
	}
	rows := t02Rows(t, src, "guardFixedIdentity")
	sawText := false
	for _, row := range rows {
		if len(row) < 9 || row[0] != fixedOpText || row[5] == fixedNoGuard {
			continue
		}
		sawText = true
		if row[6] != 2 || row[7] != fixedTextUtf8 {
			t.Errorf("W7: the emitted row is (arg %d, meta %d), want the ordinal 2 and the flavour %d in TWO lanes", row[6], row[7], fixedTextUtf8)
		}
	}
	if !sawText {
		t.Fatal("W7: the emitted identity plan carries no guarded text row")
	}
}
