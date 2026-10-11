package darttable

// THE DART LEG'S FIXED-TABLE WRITE / LIVE-EXTENT / UNION-GUARD ROWS
// (docs/roadmap.sexp node `fixed-tables`, rows live-extents, writing and
// union-guards). Each subtest is one roadmap task id and asserts the clause its
// own title states. The subject is the emitter's OWN bytes — the generated
// <Base>Fixed.dart and the compiler's own plan walk — where the sentence is a
// fact about what the emitter lays down.
//
// Law: docs/FIXED-FORM-ALGORITHM.md §3.1 (THE WRITE: "the template is a
// compile-time constant — the hash, then zero everywhere a value lands — which
// zero-fills every byte of declared slack"), §4.1 (THE PLAN: "arg and meta ARE
// TWO LANES BECAUSE THEY ARE TWO FACTS"), §4.3 (THE PREFILL: "Prefill the bytes
// the plan does not write ... for the identity plan that list is EMPTY"), §4.6
// (THE BOUNDS PASS: "It walks only what a read can have written — a counted
// array's LIVE elements and never its slack"), docs/SPEC-TABLES.md §3.4 and
// ROADMAP.md "NEW Fixed Tables".
//
// dart/W16 and dart/R28 are the guard-chain clauses: this leg's generated
// reader carries no guard chain, so a nested union entry answers only to its
// inner tag. They are recorded `unknown` in docs/roadmap-evidence/fixed-dart.sexp
// and have no subtest here.

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
