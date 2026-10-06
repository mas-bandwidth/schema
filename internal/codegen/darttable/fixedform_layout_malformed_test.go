package darttable

import (
	"path/filepath"
	"testing"
)

// TestFixedFormLayoutMalformedTruncated is the row fixed_form_layout_malformed,
// item F4 of schema#876: a GAP on all nine legs (matrix row GGGGGGGGG). A file
// that carries a KNOWN hash but TRUNCATED layout bytes is layout_malformed:
// "a lie about a known version". The hash matches, so it is not refused by
// name; the layout bytes are wrong, so it is malformed and the counters are
// asserted zero by NAME, and no destination byte is written.
func TestFixedFormLayoutMalformedTruncated(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)

	file := filepath.Join(corpus, "new_field_append.bin")

	body := `  final data = File(FILE).readAsBytesSync();
  final head = ByteData.sublistView(data);
  final layoutBytes = head.getUint32(t.TableFixedLimits.headerBytes, Endian.little);
  check(layoutBytes > 8, 'layout bytes = ${layoutBytes}');
  final rep = t.TableFixedReport();
  values[0].x = 0x5A5A5A5A;
  values[0].y = 0x5A5A5A5A;
  values[0].z = 0x5A5A5A5A;
  values[0].w = 0x5A5A5A5A;
  final truncated = Uint8List(data.length);
  truncated.setRange(0, data.length, data);
  final layoutStart = t.TableFixedLimits.headerBytes + t.TableFixedLimits.layoutAt;
  final layoutEnd = layoutStart + layoutBytes;
  check(layoutEnd < truncated.length, 'layout bytes fit');
  final k = (layoutBytes / 2).toInt();
  final cut = layoutStart + k;
  truncated[cut] ^= 0xFF;
  final n = LOAD(values, values.length, truncated, truncated.length, plan, rep);
  check(n == -1, 'truncated layout owes -1, not $n: ${why(rep)}');
  check(rep.refused == t.TableFixedRefusal.none,
      'layout_malformed is a refusal by name, not the joint answer: ${name(rep.refused)}');
  check(!rep.malformed, 'layout_malformed does not set malformed too');
  check(rep.clamped == 0 && rep.unknown == 0 && rep.kindMismatch == 0 &&
      rep.widened == 0 && rep.duplicate == 0 && rep.hash == 0,
      'layout_malformed moves no counter: ${why(rep)}');
  check(values[0].x == 0x5A5A5A5A && values[0].y == 0x5A5A5A5A &&
      values[0].z == 0x5A5A5A5A && values[0].w == 0x5A5A5A5A,
      'truncated layout wrote a destination byte');`

	out, err := runVersionProbe(t, dartBin, "VNEW_field_append", nil, 0, file, body)
	if err != nil {
		t.Fatalf("layout_malformed (truncated): %v\n%s", err, out)
	}
}
