package cstable

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestTimestampCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	// The same RFC3339 boundaries pinned by internal/rfc3339 and the generated
	// Go reader. Fractions past nanoseconds truncate; valid offsets reach23:59.
	values := []string{
		"1970-01-01T00:00:00Z", "2024-02-29T12:34:05Z", "2000-02-29T00:00:00Z",
		"1969-12-31T23:59:59.999999999Z", "2026-10-17t12:34:05z", "2026-10-17t12:34:05.5z",
		"2026-10-17T12:34:05.123456789Z", "2026-10-17T12:34:05.1234567899Z",
		"2026-10-17T12:34:05." + strings.Repeat("1234567890", 100) + "Z", "2026-10-17T12:34:05.0Z",
		"2026-10-17T18:04:05.123456789+05:30", "2026-10-17T05:34:05-07:00",
		"2026-10-18T12:33:05+23:59", "2026-10-16T12:35:05-23:59", "1970-01-01T00:00:00-00:00",
		"1677-09-21T00:12:43.145224192Z", "2262-04-11T23:47:16.854775807Z",
		"2026-10-17T12:34:05+24:00", "2026-10-17T12:34:05+05:60", "2026-10-17T12:34:05,5Z",
		"2026-10-17T12:34:05-24:00", "2026-10-17T12:34:05-05:60", "2016-12-31T23:59:60Z",
		"2016-12-31T23:59:60.5Z", "2026-00-17T12:34:05Z", "2026-13-17T12:34:05Z",
		"2026-10-00T12:34:05Z", "2026-10-32T12:34:05Z", "2026-02-30T12:34:05Z",
		"2026-04-31T12:34:05Z", "2025-02-29T12:34:05Z", "1900-02-29T12:34:05Z",
		"2026-10-17T24:00:00Z", "2026-10-17T12:60:05Z", "2026-10-17T12:34:05.Z",
		"2026-10-17T12:34:05.1.2Z", "2026-10-17T12:34:05", "2026-10-17T12:34:05+0530",
		"2026-10-17T12:34:05+05", "2026-10-17T12:34:05+05:30:00", "", "2026-10-17",
		"2026-10-17T12:34Z", " 2026-10-17T12:34:05Z", "2026-10-17T12:34:05Z ",
		"2026-10-17T12:34:05Z\n", "2026-10-17T12:34:05Zx", "x2026-10-17T12:34:05Z",
		"2026-10-17 12:34:05Z", "20x6-10-17T12:34:05Z", "2026-10-17T1x:34:05Z",
		"0000-01-01T00:00:00Z", "9999-12-31T23:59:59Z", "1677-09-21T00:12:43.145224191Z",
		"2262-04-11T23:47:16.854775808Z",
	}
	var inputs []string
	for _, value := range values {
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, `{"at":`+string(b)+`}`)
	}
	inputs = append(inputs, `{"at":"1970-01-01\u005400:00:00\u005a"}`, `{"at":42}`, `{"at":null}`)
	inputs = append(inputs, `{"at":"1970-01-01T00:00:00\q"}`, "{\"at\":\"1970-01-01T00:00:00Z\n\"}", `{"at":"1970-01-01T00:00:00\uD800"}`)
	runCarryCS(t, `package probe
fixed table Root {
 at timestamp
}
`, inputs, `long[] counts = { 0L, 1740746045000000000L, -1L, long.MinValue, long.MaxValue };
 var vocabulary = new TableVocabulary(); var annReport = new TableReport();
 Schema.AnnounceRead(vocabulary, Schema.Announce(), annReport);
 if (!vocabulary.Announced || annReport.Malformed || annReport.Refused) throw new Exception("timestamp announcement");
 if (Schema.RootTableType().Fields[0].Kind != 36) throw new Exception("timestamp descriptor kind");
 foreach (long count in counts) {
  var value = new Root { At = count };
  byte[] wire = new byte[Schema.RootMeasure(value)];
  if (Schema.RootSave(value, wire) != wire.Length) throw new Exception("timestamp file save");
  if (count != 0) {
   if (wire[2] != 36 || System.Buffers.Binary.BinaryPrimitives.ReadInt64LittleEndian(wire.AsSpan(3,8)) != count) throw new Exception("timestamp kind or width");
   byte[] mismatch = (byte[])wire.Clone(); mismatch[2] = 5;
   var skipped = new Root(); var mismatchReport = new TableReport();
   Schema.RootLoadVerdict(skipped, mismatch, mismatchReport);
   if (skipped.At != 0 || mismatchReport.KindMismatch != 1 || mismatchReport.Widened != 0 || mismatchReport.Malformed) throw new Exception("timestamp must not widen from int64");
  }
  byte[] fixedWire = new byte[Schema.RootFixedMeasure(1)];
  if (Schema.RootFixedSave(value, fixedWire) != fixedWire.Length) throw new Exception("timestamp fixed save");
  var fixedBack = new Root(); var fixedReport = new TableReport();
  if (Schema.RootFixedLoad(fixedBack, fixedWire, new TableFixedEntry[32], fixedReport) != 1 || fixedBack.At != count || fixedReport.Clamped != 0) throw new Exception("timestamp fixed load");
  var values = new Root[] { value };
  byte[] message = new byte[Schema.RootMeasureMessages(values)];
  if (Schema.RootSaveMessages(values, message) != message.Length) throw new Exception("timestamp message save");
  var back = new Root[] { new Root() }; var read = new TableReport();
  Schema.RootLoadMessages(back, message, vocabulary, read, out int n);
  if (n != 1 || read.Malformed || read.Refused || read.Clamped != 0 || back[0].At != count) throw new Exception("timestamp message load: " + read.Reason);
 }`)
}
