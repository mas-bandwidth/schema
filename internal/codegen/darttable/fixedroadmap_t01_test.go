package darttable

// THE DART LEG'S FIXED-TABLE FRAMING ROWS (docs/roadmap.sexp node
// `fixed-tables`, rows file-envelope, batch-capacity, plan-selection). Each
// subtest is one roadmap task id and implements the page sentence its comment
// quotes: docs/FIXED-FORM-ALGORITHM.md §5.3 (the load, in order) and its
// refusal table, SPEC-TABLES §3.4.
//
// The tasks held by an existing test are named there and add nothing here:
// dart/F7 by TestFixedFormUnder20Bytes, dart/F8 by TestFixedFormRaggedTail.
// dart/F12's law is a STREAM's re-announcement; this leg has no stream
// carrier, so it is reported unknown rather than decided here.

import (
	"path/filepath"
	"testing"
)

// roadmapPrelude is the Dart every probe body starts from: the corpus file
// (one record under its own hash), its framing numbers, a poisoned destination
// and a `refused` helper that holds one refusal to §5.3's whole answer.
//
// "REFUSE is total: no counter moves, nothing is decoded, and not one
// destination byte is written" and "`refused` plus `reason` is one answer;
// `malformed` is the other; they are NEVER both set."
const roadmapPrelude = `  final data = File(FILE).readAsBytesSync();
  final head = ByteData.sublistView(data);
  final layoutBytes = head.getUint32(t.TableFixedLimits.headerBytes, Endian.little);
  final at0 = t.TableFixedLimits.layoutAt + layoutBytes;
  final recordBytes = data.length - at0;
  check(recordBytes > 8, 'the corpus file carries one record: $recordBytes');
  void poison() {
    plan.image.fillRange(0, plan.image.length, 0x5A);
    for (final v in values) {
      v.x = 0x5A5A5A5A;
      v.y = 0x5A5A5A5A;
      v.z = 0x5A5A5A5A;
      v.w = 0x5A5A5A5A;
    }
  }
  bool untouched() {
    for (var i = 0; i < plan.image.length; i++) {
      if (plan.image[i] != 0x5A) return false;
    }
    for (final v in values) {
      if (v.x != 0x5A5A5A5A || v.y != 0x5A5A5A5A || v.z != 0x5A5A5A5A || v.w != 0x5A5A5A5A) {
        return false;
      }
    }
    return true;
  }
  bool quiet(t.TableFixedReport r) =>
      r.unknown == 0 && r.kindMismatch == 0 && r.clamped == 0 && r.widened == 0 && r.duplicate == 0;
  // one refusal, whole: the name, -1, never malformed too, no counter, no byte
  t.TableFixedReport refused(Uint8List file, int capacity, int want, String what, [int? len]) {
    poison();
    final rep = t.TableFixedReport();
    final n = LOAD(values, capacity, file, len ?? file.length, plan, rep);
    check(n == -1, '$what: owes -1, not $n: ${why(rep)}');
    check(rep.refused == want, '$what: owes ${name(want)}, not ${name(rep.refused)}');
    check(!rep.malformed, '$what: a refusal by name never sets malformed too: ${why(rep)}');
    check(quiet(rep), '$what: REFUSE is total, no counter moves: ${why(rep)}');
    check(untouched(), '$what: REFUSE writes not one destination byte');
    return rep;
  }
  // a file of n records: the corpus file's header and layout, then its one record n times
  Uint8List records(int n) {
    final out = Uint8List(at0 + n * recordBytes);
    out.setRange(0, at0, data);
    for (var k = 0; k < n; k++) {
      out.setRange(at0 + k * recordBytes, at0 + (k + 1) * recordBytes, data, at0);
    }
    return out;
  }
`

type roadmapRun struct {
	reader string
	older  []string
	file   string
	body   string
}

func TestFixedRoadmapT01Framing(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	own := filepath.Join(corpus, "new_field_append.bin")
	ownRun := func(body string) []roadmapRun {
		return []roadmapRun{{"VNEW_field_append", nil, own, body}}
	}

	rows := []struct {
		id   string
		runs []roadmapRun
	}{
		// dart/F4 "layout_malformed, truncated": "`L := LE(4, b+16)`; if
		// `20 + L > bytes`, `REFUSE layout_malformed`". The file names the true
		// layout and is handed to the reader ONE BYTE SHORT of 20+L, so no later
		// branch (length or byte compare) can be the one that refuses.
		{"dart/F4", ownRun(roadmapPrelude + `
  final whole = LOAD(values, values.length, data, data.length, plan, report);
  check(whole == 1 && report.refused == 0 && !report.malformed,
      'the whole file must read first: n=$whole ${why(report)}');
  final cut = t.TableFixedLimits.layoutAt + layoutBytes - 1;
  final rep = refused(data, values.length, t.TableFixedRefusal.layoutMalformed,
      'F4 a file one byte short of 20+L', cut);
  check(rep.layoutHash == 0 && rep.hash == 0, 'F4 a truncated layout reports no hash: ${why(rep)}');
  for (var len = t.TableFixedLimits.layoutAt; len < cut; len++) {
    refused(data, values.length, t.TableFixedRefusal.layoutMalformed, 'F4 a $len-byte prefix', len);
  }`)},
	}

	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			for _, r := range row.runs {
				out, err := runVersionProbe(t, dartBin, r.reader, r.older, 0, r.file, r.body)
				if err != nil {
					t.Fatalf("%s: %v\n%s", row.id, err, out)
				}
			}
		})
	}
}
