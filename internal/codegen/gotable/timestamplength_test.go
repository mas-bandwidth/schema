package gotable

import (
	"testing"
)

// TestGoTimestampTextReadsAnyValidLength pins the text form's timestamp read on
// any valid RFC 3339 length (docs/SPEC-TABLES.md §16.2): the generated Go walk
// scans the WHOLE string token before parsing, so a spelling whose fraction is
// longer than nine digits truncates to nanoseconds exactly as the engine's read
// does, silently and with no clamp counted. A string longer than the 128-byte
// bound §16.2 declares is refused by name — `timestamp_not_rfc3339: <field>`
// — with no clamped count, instead of being cut at a byte cap, and garbage
// keeps the same named refusal. The generated unit is compiled and run in a
// t.TempDir() by runGenerated, the package's house pattern.
//
// At BASE the generated walk scans into a 64 byte buffer (`text[:63]`), so the
// 79-byte spelling below was refused as malformed while the engine read it.
func TestGoTimestampTextReadsAnyValidLength(t *testing.T) {
	t.Parallel()

	runGenerated(t, `package probe
fixed table Root
{
	at timestamp
}
`, `package probe
import ("strings";"testing")

func TestTimestampTextReadsAnyValidLength(t *testing.T) {
	// nine fraction digits land the count, and the reference for every longer
	// spelling: the fraction past nine digits truncates, never rounds
	const base = "2026-10-17T12:34:05."
	var want int64
	{
		var r Root
		var rep TableReport
		text := "{\"at\":\"" + base + "123456789Z\"}"
		if !RootFromJson(&r, []byte(text), &rep) || rep != (TableReport{}) {
			t.Fatalf("nine digits: %+v", rep)
		}
		want = r.At
	}
	// the 79-byte spelling, and 10, 12 and 60 fraction digits, all read to the
	// same nanosecond count as nine digits, silently. Every fraction opens with
	// the reference "123456789", so its truncation is the same count.
	fractions := []string{
		"1234567891234567891234567891234567891234567891234567891234",
		"1234567890",
		"123456789012",
	}
	fractions = append(fractions, "123456789"+strings.Repeat("0", 51)) // 60 digits
	for _, frac := range fractions {
		var r Root
		var rep TableReport
		text := "{\"at\":\"" + base + frac + "Z\"}"
		if !RootFromJson(&r, []byte(text), &rep) || rep != (TableReport{}) {
			t.Fatalf("fraction length %d: %+v", len(frac), rep)
		}
		if r.At != want {
			t.Fatalf("fraction length %d: count %d want %d", len(frac), r.At, want)
		}
	}
	// a 10000-byte string of digits after the dot exceeds the 128-byte bound
	// §16.2 declares: refused by name, no clamp counted, nothing proportional to
	// its length allocated
	{
		var r Root
		var rep TableReport
		text := "{\"at\":\"" + base + strings.Repeat("1", 10000) + "Z\"}"
		if RootFromJson(&r, []byte(text), &rep) {
			t.Fatal("10000-byte fraction accepted")
		}
		if !rep.Malformed || !strings.HasPrefix(rep.Reason, "timestamp_not_rfc3339: at") || rep.Clamped != 0 {
			t.Fatalf("10000-byte fraction: %+v", rep)
		}
	}
	// garbage keeps the named refusal, with no clamp counted
	{
		var r Root
		var rep TableReport
		if RootFromJson(&r, []byte("{\"at\":\"garbage\"}"), &rep) {
			t.Fatal("garbage accepted")
		}
		if !rep.Malformed || !strings.HasPrefix(rep.Reason, "timestamp_not_rfc3339: at") || rep.Clamped != 0 {
			t.Fatalf("garbage: %+v", rep)
		}
	}
}
`)
}
