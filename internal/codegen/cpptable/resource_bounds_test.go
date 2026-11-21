package cpptable

// THE C++ READERS' RESOURCE BOUNDS, held on the emitted text (fast lane: no
// compiler, no files). The run of the same inputs is the leg's own target,
// make cpp-hostile-bounds (test/cpp-hostile/bounds/), which reads them on a
// 512 KiB stack and counts the work.
//
//   - the pre-strict skip walk counts a union whose arm is a union as a level,
//     so a chain of them is bounded at the strict walk's 128 (review CPP-1);
//   - the message form's seen set is not on any frame of the walks and is not
//     zeroed per body: the marks live in the vocabulary's entries, and the
//     first-slot lookup is resolved at AnnounceRead (CPP-2, CPP-3);
//   - a named ranged read holds base plus offset to the ANNOUNCED kind's
//     domain through the skip path's own helper (CPP-4);
//   - a JSON object's repeated-key check is an index lookup, not a scan of
//     every earlier key (CPP-5).

import (
	"sort"
	"strings"
	"testing"
)

const resourceBoundsSchema = `package boundsfast

enum Axis
{
    X
    Y
}

type Inner
{
    v int32 | min = 0, max = 10
}

fixed table Root
{
    inner    Inner
    position fixed(48, 16)  | min = -30000, max = 30000
    reach    fixed(112, 16) | min = -1000000, max = 1000000
    small    int8           | min = -100, max = 100
    samples  [3]fixed(16, 16)    | min = -8, max = 8
    axes     [Axis]fixed(32, 32) | min = -100, max = 100
    items    [..4]Inner
    blob     bytes(16)
    heading  float32 | min = -180.0, max = 180.0, resolution = 0.01
    gains    [2]float32 | min = -1.0, max = 1.0, resolution = 0.001
    mask     bits(13)
}
`

func resourceBoundsEmitted(t *testing.T) string {
	t.Helper()
	files, err := Generate(cppUnitFrom(t, resourceBoundsSchema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, name := range names {
		sb.Write(files[name])
	}
	return sb.String()
}

// cppFunctionText is the text of the first definition whose head contains
// head, from the head to its closing brace, matched by brace depth.
func cppFunctionText(t *testing.T, src, head string) string {
	t.Helper()
	for from := 0; ; {
		at := strings.Index(src[from:], head)
		if at < 0 {
			t.Fatalf("no definition with head %q in the emitted C++", head)
		}
		at += from
		open := strings.IndexAny(src[at:], "{;")
		if open < 0 {
			t.Fatalf("head %q has no body", head)
		}
		if src[at+open] == ';' { // a declaration: look for the definition
			from = at + open + 1
			continue
		}
		depth := 0
		for i := at + open; i < len(src); i++ {
			switch src[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return src[at : i+1]
				}
			}
		}
		t.Fatalf("head %q: unbalanced braces", head)
	}
}

func TestCppPreStrictWalkCountsUnionNesting(t *testing.T) {
	t.Parallel()
	src := resourceBoundsEmitted(t)
	content := cppFunctionText(t, src, "bool badElementKindContent( uint8_t kind,")
	if !strings.Contains(content, "if ( depth >= 128 )") {
		t.Fatalf("badElementKindContent lost its depth bound:\n%s", content)
	}
	// every structural content, a union among them, is one level deeper
	for _, callee := range []string{"badElementKindTable", "badElementKindArray", "badElementKindKeyed", "badElementKindUnion"} {
		calls := strings.Count(content, callee+"(")
		deeper := strings.Count(content, ", depth + 1 )")
		if calls != 1 || !strings.Contains(content, callee+"( ") {
			t.Fatalf("badElementKindContent should call %s once, got %d:\n%s", callee, calls, content)
		}
		if deeper != 4 {
			t.Fatalf("badElementKindContent passes depth + 1 on %d of its 4 structural calls (a union-in-union chain is unbounded otherwise):\n%s", deeper, content)
		}
	}
	if strings.Contains(content, "badElementKindUnion( r, depth )") {
		t.Fatalf("badElementKindContent hands a union arm the same depth:\n%s", content)
	}
}

func TestCppMessageSeenSetIsOffTheFrame(t *testing.T) {
	t.Parallel()
	src := resourceBoundsEmitted(t)
	if strings.Contains(src, "kTableMessageSeenBits") {
		t.Fatal("the emitted C++ still declares a per-body seen bitmap (kTableMessageSeenBits)")
	}
	walks := map[string]string{
		"TableMessageSkipBody":  "inline bool TableMessageSkipBody( TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits, int depth )",
		"TableMessageSkip":      "inline bool TableMessageSkip( TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits, const TableMessageEntry & entry, int depth )",
		"TableMessageSkipKeyed": "inline bool TableMessageSkipKeyed(",
		"RootLoadMessageBody":   "inline bool RootLoadMessageBody(",
		"InnerLoadMessageBody":  "inline bool InnerLoadMessageBody(",
	}
	for name, head := range walks {
		body := cppFunctionText(t, src, head)
		if strings.Contains(body, "seen[") || strings.Contains(body, "seen_keys[") || strings.Contains(body, "uint64_t seen") {
			t.Fatalf("%s carries a seen array on its frame:\n%s", name, body)
		}
	}
	for _, name := range []string{"TableMessageSkipBody", "TableMessageSkipKeyed", "RootLoadMessageBody", "InnerLoadMessageBody"} {
		if !strings.Contains(cppFunctionText(t, src, walks[name]), "const TableMessageSeenBody ") {
			t.Fatalf("%s opens no level of the repeat check", name)
		}
	}
	// the keyed field's own key set is a level too
	root := cppFunctionText(t, src, walks["RootLoadMessageBody"])
	if !strings.Contains(root, "const TableMessageSeenBody seen_keys") {
		t.Fatalf("the keyed field's key set opens no level:\n%s", root)
	}
	// a naming is constant work: no scan of the vocabulary for the id's first
	// slot, which AnnounceRead resolves once
	once := cppFunctionText(t, src, "inline bool TableMessageSeenOnce(")
	if strings.Contains(once, "for (") {
		t.Fatalf("TableMessageSeenOnce scans:\n%s", once)
	}
	if !strings.Contains(cppFunctionText(t, src, "inline bool AnnounceReadOnce( TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * to )"), "parsed.seen_first = first;") {
		t.Fatal("AnnounceRead does not resolve each id's first slot")
	}
}

func TestCppNamedRangedReadHoldsTheAnnouncedDomain(t *testing.T) {
	t.Parallel()
	src := resourceBoundsEmitted(t)
	skip := cppFunctionText(t, src, "inline bool TableMessageSkipRanged(")
	if !strings.Contains(skip, "return TableMessageRangedHolds( kind, lo, hi, base_lo, base_hi );") {
		t.Fatalf("the skip path does not take the shared domain test:\n%s", skip)
	}
	root := cppFunctionText(t, src, "inline bool RootLoadMessageBody(")
	for _, want := range []string{
		"TableMessageRangedHolds( entry.kind, raw",      // a 64-bit storage (position, kind 23; small)
		"TableMessageRangedHolds( entry.kind, lo_v",     // a 128-bit kind (reach, kind 24)
		"TableMessageRangedHolds( entry.elem_kind, raw", // an element (samples, axes)
	} {
		if !strings.Contains(root, want) {
			t.Fatalf("RootLoadMessageBody has no %q: a named ranged read must hold base plus offset to the announced kind's domain", want)
		}
	}
	// a value past the domain refuses out_of_range (§3.3), never damage, and
	// the sum is formed only when the test held: no ranged add is unguarded
	if !strings.Contains(root, "report->refuse_reason = out_of_range;") {
		t.Fatal("RootLoadMessageBody never refuses out_of_range")
	}
	for _, add := range []string{"decoded_wide", "raw_v"} {
		if strings.Contains(root, "== 1 ) { "+add) {
			t.Fatalf("a ranged sum into %s is formed whether or not the announced domain holds it:\n%s", add, root)
		}
	}
	guarded := 0
	for _, line := range strings.Split(root, "\n") {
		sum := strings.Contains(line, "packing == 1") && strings.Contains(line, "+") &&
			(strings.Contains(line, "{ decoded_wide") || strings.Contains(line, "{ raw_v"))
		if !sum {
			continue
		}
		if !strings.Contains(line, "packing == 1 && !past") {
			t.Fatalf("a ranged sum is formed with no domain guard: %q", line)
		}
		guarded++
	}
	if guarded < 3 {
		t.Fatalf("expected a guarded ranged sum on every path (64-bit, 128-bit, widened), found %d", guarded)
	}
}

func TestCppJsonRepeatedKeyIsAnIndexLookup(t *testing.T) {
	t.Parallel()
	src := resourceBoundsEmitted(t)
	track := cppFunctionText(t, src, "inline bool TableJsonKeyTrack(")
	if strings.Contains(track, "for ( int64_t i = in.key_mark; i < in.key_count; i++ )") {
		t.Fatalf("TableJsonKeyTrack scans every earlier key of the object:\n%s", track)
	}
	if !strings.Contains(track, "TableJsonKeyHash(") || !strings.Contains(track, "TableJsonKeyIndexPlace(") {
		t.Fatalf("TableJsonKeyTrack does not look the key up in the index:\n%s", track)
	}
	scope := cppFunctionText(t, src, "~TableJsonKeyScope()")
	if !strings.Contains(scope, "TableJsonKeysCut( in, count );") {
		t.Fatalf("a closing object does not take its keys out of the index:\n%s", scope)
	}
}

// THE ACROSS-LEGS CELLS this leg owned (review 2026-11-21, verify-message.md
// and verify-values.md), held on the emitted text beside the findings.

// A union FIELD skipped at a root body's own level refuses an explicit arm 0
// exactly as a nested body's does (§3.3, ruling message-one-encoding).
func TestCppFieldSkipRefusesUnionArmZero(t *testing.T) {
	t.Parallel()
	src := resourceBoundsEmitted(t)
	field := cppFunctionText(t, src, "inline bool TableMessageSkipField(")
	if !strings.Contains(field, "|| arm == 0 ) { return false; }") {
		t.Fatalf("TableMessageSkipField admits an explicit arm 0 at a field:\n%s", field)
	}
	body := cppFunctionText(t, src, "inline bool TableMessageSkipBody( TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits, int depth )")
	root := cppFunctionText(t, src, "inline bool RootLoadMessageBody(")
	for name, text := range map[string]string{"TableMessageSkipBody": body, "RootLoadMessageBody": root} {
		if strings.Contains(text, "TableMessageSkip( r, vocabulary, index_bits, entry") {
			t.Fatalf("%s skips a field through the element skip, which admits arm 0:\n%s", name, text)
		}
		if !strings.Contains(text, "TableMessageSkipField( r, vocabulary, index_bits, entry") {
			t.Fatalf("%s does not skip its fields through TableMessageSkipField", name)
		}
	}
}

// A bytes(N) count rides as count - min, as every kind-6 array's does (§3.3).
func TestCppMessageBytesCountAddsTheAnnouncedMin(t *testing.T) {
	t.Parallel()
	root := cppFunctionText(t, resourceBoundsEmitted(t), "inline bool RootLoadMessageBody(")
	if !strings.Contains(root, "+= (uint64_t) entry.min;") {
		t.Fatalf("the bytes(N) read drops the announced min from its count:\n%s", root)
	}
}

// A compressed float's -0 lands as +0 on every read and is stored as +0 by
// every writer (§3.3; ruling fixed-compressed-negzero), and a float32 range in
// JSON is compared at float32 (SPEC.md §4.3).
func TestCppCompressedFloatNegZeroAndFloat32Range(t *testing.T) {
	t.Parallel()
	src := resourceBoundsEmitted(t)
	// variable read, message read (heading, and the gains element), JSON read
	if n := strings.Count(src, "-0 lands as +0"); n < 3 {
		t.Fatalf("a compressed float's -0 is folded on %d read paths, want the variable, message and JSON reads", n)
	}
	// the variable writer (heading, gains) and the fixed writer
	if n := strings.Count(src, "-0 is stored as +0"); n < 3 {
		t.Fatalf("a compressed float's -0 is stored as +0 by %d write sites, want the variable and fixed writers", n)
	}
	if !strings.Contains(src, "single ? (double) (float) f->range_min : f->range_min") {
		t.Fatal("the JSON read compares a float32 against its unrounded double bound")
	}
}

// THE VARIABLE SAVE'S WRITE CONTRACT on a field that is not `required`
// (docs/SPEC.md §5.1 rows 1 and 2): a debug-only schema_assert, as the fixed
// and packet writers hold it, and nothing a release build runs. Row 5 (an
// enum outside its set) already refuses in every build through TableEnumRef.
func TestCppVariableSaveAssertsTheWriteContract(t *testing.T) {
	t.Parallel()
	files, err := Generate(cppUnitFrom(t, `package contractfast

table Root
{
    small int8 | min = -100, max = 100
    mask  bits(13)
}
`))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var sb strings.Builder
	for _, text := range files {
		sb.Write(text)
	}
	save := cppFunctionText(t, sb.String(), "inline bool RootSaveBodyFields(")
	for _, want := range []string{
		"schema_assert( value.small >= ", // row 1, a ranged field
		"schema_assert( value.small <= ",
		"schema_assert( value.mask <= 8191ull );", // row 2, bits(13)
	} {
		if !strings.Contains(save, want) {
			t.Fatalf("RootSaveBodyFields has no %q:\n%s", want, save)
		}
	}
	if strings.Contains(save, "if ( value.small < -100") {
		t.Fatal("a field that is not required refuses its range in every build; the assert leg asserts it")
	}
}
