package gotable

import (
	"testing"
)

// timestampLengthSchema is the VARIABLE class (the form this card's red
// reproduces on): a table with one timestamp field, read through a builder.
const timestampLengthSchema = `package probe
table Root
{
	at timestamp
}
`

// TestGoTimestampTextReadsAnyValidLength pins the Go timestamp text read to the
// engine's rule (docs/SPEC-TABLES.md §16.2): a valid RFC 3339 spelling of any
// length reads, a seconds-fraction longer than nine digits TRUNCATES to
// nanoseconds, a spelling longer than the 255-byte bound is refused BY NAME and
// never clamped, and garbage keeps `timestamp_not_rfc3339: <field>` with no
// clamped count. Red at BASE: `tableJsonReadTimestamp` capped the string at 63
// bytes, so the 79-byte spelling below refused AND counted a clamp.
func TestGoTimestampTextReadsAnyValidLength(t *testing.T) {
	t.Parallel()

	runGenerated(t, timestampLengthSchema, `package probe
import ("strings";"testing")

func readCount(t *testing.T, text string) (count int64, ok bool, r TableReport) {
	t.Helper()
	var b RootBuilder
	if !b.Init() { t.Fatal("builder init") }
	defer b.Shutdown()
	ok = RootFromJson(&b, []byte(text), &r)
	if ok {
		if !b.Lock() { t.Fatal("lock") }
		count = b.AsConst().At
	}
	return
}

func TestTimestampAnyLength(t *testing.T) {
	// the count a nine-digit fraction lands; every longer spelling must land the same
	base := "2026-10-17T12:34:05.123456789Z"
	want, ok, r := readCount(t, "{\"at\":\""+base+"\"}")
	if !ok || r != (TableReport{}) { t.Fatalf("base refused %+v", r) }

	for _, frac := range []string{
		"1234567891234567891234567891234567891234567891234567891234", // 79 bytes total, the red at BASE
		"1234567890",
		"123456789012",
		"123456789" + strings.Repeat("0", 51), // 60 fraction digits
	} {
		got, ok, r := readCount(t, "{\"at\":\"2026-10-17T12:34:05."+frac+"Z\"}")
		if !ok || r != (TableReport{}) { t.Fatalf("%d digits refused %+v", len(frac), r) }
		if got != want { t.Fatalf("%d digits: %d vs %d - truncation mismatch", len(frac), got, want) }
	}

	// a 10000-byte string of digits after the dot reads or is refused, never clamped
	long := "{\"at\":\"2026-10-17T12:34:05." + strings.Repeat("9", 10000) + "Z\"}"
	_, ok, r = readCount(t, long)
	if ok {
		if r.Clamped != 0 { t.Fatalf("10000 bytes clamped: %+v", r) }
	} else {
		if !r.Malformed || !strings.HasPrefix(r.Reason, "timestamp_not_rfc3339: at") { t.Fatalf("10000 bytes: unnamed refusal %+v", r) }
		if r.Clamped != 0 { t.Fatalf("10000 bytes clamped on refusal: %+v", r) }
	}

	// garbage keeps the named refusal, never clamped
	_, ok, r = readCount(t, "{\"at\":\"garbage\"}")
	if ok { t.Fatal("garbage accepted") }
	if !r.Malformed || r.Reason != "timestamp_not_rfc3339: at" { t.Fatalf("garbage: unnamed refusal %+v", r) }
	if r.Clamped != 0 { t.Fatalf("garbage clamped: %+v", r) }
}
`)
}
