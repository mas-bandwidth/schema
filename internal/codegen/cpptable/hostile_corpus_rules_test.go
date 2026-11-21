package cpptable

// THE RULES THE SHARED HOSTILE CORPUS HOLDS THE C++ TEXT READ TO
// (internal/ci/hostilecorpus, make schema-cpp-runs-hostile-corpus), held on
// the emitted text in the fast lane: no compiler, no files. Each test names
// the property; the corpus rows are the run of the same rules.
//
//   - a node label is followed by its object's , or } and nothing else;
//   - in a skipped value a bare label refers to a closed definition, and a
//     label with fields defines a new one, open until the object closes;
//   - a union's arm value is two levels below the union's holder;
//   - a map's counted text key is read into a buffer of its declared bound,
//     and a key new to the map is placed without a scan of the map;
//   - a counted array [A..B]T is born at A, elides at its birth value, and a
//     count below A is refused on every read.

import (
	"sort"
	"strings"
	"testing"
)

func hostileRulesEmitted(t *testing.T, schema string) string {
	t.Helper()
	files, err := Generate(cppUnitFrom(t, schema))
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

const hostileRulesGraphSchema = `package hostilegraph

table Node
{
    value int32
    next  *Node
}

table Inner
{
    a int32
}

union Pick
{
    inner Inner
    plain int32
}

table Root
{
    head *Node
    pick Pick
    names map[string(300)]int32
    ids   map[int32]int32
}
`

func TestCppJsonLabelIsFollowedByCommaOrClose(t *testing.T) {
	t.Parallel()
	src := hostileRulesEmitted(t, hostileRulesGraphSchema)
	scan := hostileFunction(t, src, "inline bool TableJsonScanLabel(")
	if !strings.Contains(scan, "next != ',' && next != '}'") {
		t.Fatalf("a label may be followed by something other than , or }:\n%s", scan)
	}
}

func TestCppSkippedLabelFollowsTheLabelRules(t *testing.T) {
	t.Parallel()
	src := hostileRulesEmitted(t, hostileRulesGraphSchema)
	skipped := hostileFunction(t, src, "inline bool TableJsonSkippedAmpersand( TableJsonIn & in, const char * key, int32_t key_length, int32_t, bool & reference, uint64_t & opened )")
	for _, want := range []string{
		"if ( entry == NULL || entry->open != 0 )", // a bare label names a closed definition
		"if ( entry != NULL )",                     // a label with fields is new
		"entry->open = 1;",                         // and open until its object closes
		"opened = label;",
	} {
		if !strings.Contains(skipped, want) {
			t.Fatalf("the skipped-label read has no %q:\n%s", want, skipped)
		}
	}
	container := hostileFunction(t, src, "inline bool TableJsonSkipContainer(")
	if n := strings.Count(container, "TableJsonSkippedClose( in, opened );"); n != 3 {
		t.Fatalf("a skipped object closes the definition it opened at %d of its 3 closes:\n%s", n, container)
	}
	if !strings.Contains(container, "if ( reference ) { return true; }") {
		t.Fatalf("a skipped reference is not consumed whole:\n%s", container)
	}
}

func TestCppUnionArmValueIsTwoLevelsDown(t *testing.T) {
	t.Parallel()
	src := hostileRulesEmitted(t, hostileRulesGraphSchema)
	scalar := hostileFunction(t, src, "inline bool TableJsonReadScalar(")
	union, _, found := strings.Cut(scalar, "a second key: a one-of with two arms is not a value")
	if !found {
		t.Fatalf("the scalar read has no union branch:\n%s", scalar)
	}
	if strings.Contains(union, "TableJsonSkipValue( in, depth + 1 )") {
		t.Fatalf("a union's arm value is skipped one level down, not two:\n%s", union)
	}
	if n := strings.Count(union, "TableJsonSkipValue( in, depth + 2 )"); n != 4 {
		t.Fatalf("%d of the union's 4 skips are at depth + 2", n)
	}
	if !strings.Contains(union, "TableJsonReadTable( in, payload, arms->arms[tag].table, depth + 2 )") {
		t.Fatal("a table arm's object is read one level down, not two")
	}
}

func TestCppMapReadHoldsTheKeyBoundAndPlacesWithoutAScan(t *testing.T) {
	t.Parallel()
	src := hostileRulesEmitted(t, hostileRulesGraphSchema)
	read := hostileFunction(t, src, "inline bool TableJsonReadMap( TableJsonIn & in, void * slot, const TableFieldInfo * f, int32_t depth )")
	for _, want := range []string{
		"key_capacity = (int32_t) key->array_bound;", // a string(300) key is held to 300, not 255
		"TableJsonMapKeysAdd( held,",                 // the read's key set
		"key_value, fresh )",                         // a fresh key skips the find
	} {
		if !strings.Contains(read, want) {
			t.Fatalf("the map read has no %q:\n%s", want, read)
		}
	}
	place := hostileFunction(t, src, "inline Entry * TableMapPlace(")
	if !strings.Contains(place, "fresh ? NULL : TableMapScan(") {
		t.Fatalf("the map place scans for a key the read already knows is new:\n%s", place)
	}
}

const hostileRulesBirthSchema = `package hostilebirth

fixed table Rec
{
    a int32
    b uint8
}

table Bounds
{
    octets [2..8]uint8
    recs   [1..4]Rec
    plain  [..4]uint8
}

fixed table FixedBounds
{
    octets [2..8]uint8
    recs   [1..4]Rec
}
`

func TestCppCountedArrayIsBornAtItsMinimum(t *testing.T) {
	t.Parallel()
	src := hostileRulesEmitted(t, hostileRulesBirthSchema)
	for _, want := range []string{
		"int32_t octets_count = 2; // born at its declared minimum",
		"int32_t recs_count = 1; // born at its declared minimum",
		"int32_t plain_count = 0;",
		"value.octets_count = 2; // born at its declared minimum",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("the storage is not born at the declared minimum: no %q", want)
		}
	}
	// the descriptor carries the minimum for the text read
	if !strings.Contains(src, "int32_t array_min;") {
		t.Fatal("TableFieldInfo has no array_min column")
	}
}

func TestCppCountedArrayElidesItsBirthValue(t *testing.T) {
	t.Parallel()
	src := hostileRulesEmitted(t, hostileRulesBirthSchema)
	for _, head := range []string{
		"inline int64_t BoundsMeasureBody(",
		"inline bool BoundsSaveBodyFields(",
		"inline int64_t FixedBoundsMeasureMessageBody(",
		"inline bool FixedBoundsSaveMessageBody(",
	} {
		body := hostileFunction(t, src, head)
		for _, want := range []string{
			"bool birth_rides_octets = value.octets_count != 2;",
			"bool birth_rides_recs = value.recs_count != 1;",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s does not elide the birth value: no %q:\n%s", head, want, body)
			}
		}
	}
}

func TestCppCountedArrayBelowItsMinimumIsRefused(t *testing.T) {
	t.Parallel()
	src := hostileRulesEmitted(t, hostileRulesBirthSchema)
	// the file read and the message read
	if n := strings.Count(src, "< 2 ) // a count below the declared Min (§5.1 row 3)"); n < 2 {
		t.Fatalf("a count below Min is refused on %d of the file and message reads", n)
	}
	// the fixed read's bounds pass, its check and its write check
	if n := strings.Count(src, "value.octets_count < 2"); n < 3 {
		t.Fatalf("the fixed form holds a stored count to Min at %d of its clamp, check and write check", n)
	}
	// the text read
	if n := strings.Count(src, "if ( f->array_min > 0 && placed < f->array_min )"); n != 2 {
		t.Fatalf("the text read refuses a count below Min on %d of its 2 counted-array paths", n)
	}
	// the writers' contract, debug only
	if !strings.Contains(src, "schema_assert( value.octets_count >= 2 ); // a count below the declared Min, debug only") {
		t.Fatal("the variable writer does not assert row 3's lower half")
	}
}

func TestCppTextWriteRefusesACountBelowItsMinimum(t *testing.T) {
	t.Parallel()
	src := hostileRulesEmitted(t, hostileRulesBirthSchema)
	body := hostileFunction(t, src, "inline bool TableJsonCountInContract(")
	if !strings.Contains(body, "count >= f->array_min && count <= f->array_bound") {
		t.Fatalf("the text write does not hold a stored count to its declared Min (§5.1 row 3):\n%s", body)
	}
}

const hostileRulesFloatSchema = `package hostilefloat

table Ranged
{
    tenth float32 = 0 | min = 0, max = 0.1, resolution = 0.01
}

fixed table FixedRanged
{
    tenth float32 = 0 | min = 0, max = 0.1, resolution = 0.01
}
`

// Row 10 of the write contract (docs/SPEC.md §5.1): a ranged float off its
// range is the caller's bug on every writer, and no writer quantizes or
// clamps it in the build where its check is active. C++ is an assert leg, so
// it is a debug-only schema_assert, and a float32's ends are compared at
// float32.
func TestCppWritersAssertARangedFloatInsideItsRange(t *testing.T) {
	t.Parallel()
	src := hostileRulesEmitted(t, hostileRulesFloatSchema)
	const assert = "schema_assert( !( value.tenth < 0.0f ) && !( value.tenth > 0.1f ) ); // the write contract, debug only (docs/SPEC.md §5.1 row 10)"
	for _, head := range []string{"inline int64_t RangedMeasureBody(", "inline bool RangedSaveBodyFields(", "inline int64_t FixedRangedMeasureMessageBody(", "inline bool FixedRangedSaveMessageBody("} {
		if body := hostileFunction(t, src, head); !strings.Contains(body, assert) {
			t.Fatalf("%s does not assert row 10: no %q in\n%s", head, assert, body)
		}
	}
	admits := hostileFunction(t, src, "inline bool FixedRangedFixedWriteAdmits(")
	const check = "bad |= (int) ( value.tenth < 0.0f ) | (int) ( value.tenth > 0.1f );"
	if !strings.Contains(admits, check) {
		t.Fatalf("the fixed write check does not hold row 10: no %q in\n%s", check, admits)
	}
}

const hostileRulesFloatDefaultSchema = `package hostiledefault

table Born
{
    seventh float32 = 0.7 | min = 0.7, max = 1, resolution = 0.01
}

fixed table FixedBorn
{
    seventh float32 = 0.7 | min = 0.7, max = 1, resolution = 0.01
}
`

// A compressed float's default test against a default that is not zero is
// spelled with the ordered comparisons, never == or !=: 0.7f is not an exact
// literal, and clang's -Wfloat-equal reds the comparison in the consumer's
// build (make tables-cpp-clean). The truth table is !='s, a NaN included.
func TestCppCompressedFloatDefaultTestIsOrdered(t *testing.T) {
	t.Parallel()
	src := hostileRulesEmitted(t, hostileRulesFloatDefaultSchema)
	for _, bad := range []string{"!= 0.7f", "== 0.7f"} {
		if at := strings.Index(src, bad); at >= 0 {
			from := strings.LastIndex(src[:at], "\n") + 1
			end := at + strings.Index(src[at:], "\n")
			t.Fatalf("a float equality against the inexact default: %s", src[from:end])
		}
	}
	if !strings.Contains(src, "!( value.seventh >= 0.7f && value.seventh <= 0.7f )") {
		t.Fatal("the elision test of a compressed float born at 0.7 is not the ordered spelling")
	}
}

// hostileFunction is the text of the first DEFINITION whose head contains
// head, up to the closing brace in column one: these runtime functions carry
// '{' and '}' as character literals, which a brace count would misread.
func hostileFunction(t *testing.T, src, head string) string {
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
		end := strings.Index(src[at:], "\n}\n")
		if end < 0 {
			t.Fatalf("head %q: no closing brace in column one", head)
		}
		return src[at : at+end+2]
	}
}
