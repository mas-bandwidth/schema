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
	"strings"
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
	old := filepath.Join(corpus, "old_field_append.bin")
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

		// dart/F9 "batch_too_large": "If `rest / record_bytes` passes the
		// caller's capacity, `REFUSE batch_too_large`". The boundary reads: n ==
		// capacity is whole, n == capacity + 1 refuses.
		{"dart/F9", ownRun(roadmapPrelude + `
  final three = records(3);
  poison();
  final fit = t.TableFixedReport();
  final n = LOAD(values, 3, three, three.length, plan, fit);
  check(n == 3 && fit.refused == 0 && !fit.malformed, 'F9 n == capacity reads whole: n=$n ${why(fit)}');
  refused(three, 2, t.TableFixedRefusal.batchTooLarge, 'F9 three records into capacity two');
  refused(three, 0, t.TableFixedRefusal.batchTooLarge, 'F9 three records into capacity zero');
  refused(records(2), 1, t.TableFixedRefusal.batchTooLarge, 'F9 two records into capacity one');`)},

		// dart/F10 "no_layout": "per record: if `LE(8, at) != h`, `REFUSE
		// no_layout`". Every record is held to it, the last one included, and the
		// refusal lands BEFORE any record is decoded.
		{"dart/F10", ownRun(roadmapPrelude + `
  for (var k = 0; k < 3; k++) {
    final bad = records(3);
    bad[at0 + k * recordBytes] ^= 0xff;
    refused(bad, 3, t.TableFixedRefusal.noLayout, 'F10 record $k carries a hash that is not the layout\'s');
  }`)},

		// dart/R7 "the identity lane is an index comparison, never a recomputed
		// hash": "a runtime NEVER computes a hash from layout bytes it holds". The
		// header hash is taken as given: this file's layout bytes are its own and
		// intact, one hash bit is not, and the read refuses layout_newer with the
		// file's hash — a reader that re-derived the hash from the bytes would
		// accept it. The static half is asserted over the generated load below.
		{"dart/R7", ownRun(roadmapPrelude + `
  final hashAt = t.TableFixedLimits.hashAt;
  final own = head.getUint64(hashAt, Endian.little);
  for (var bit = 0; bit < 64; bit += 9) {
    final forged = Uint8List.fromList(data);
    final forgedHash = own ^ (1 << bit);
    ByteData.sublistView(forged).setUint64(hashAt, forgedHash, Endian.little);
    final rep = refused(forged, values.length, t.TableFixedRefusal.layoutNewer, 'R7 header hash bit $bit flipped');
    check(rep.layoutHash == forgedHash, 'R7 reports the file\'s hash, not the layout\'s: ${why(rep)}');
  }
  poison();
  final good = t.TableFixedReport();
  check(LOAD(values, values.length, data, data.length, plan, good) == 1 && good.refused == 0 && !good.malformed,
      'R7 the unforged file takes the identity lane: ${why(good)}');
  check(good.hash == own, 'R7 the report carries the header\'s own hash');`)},

		// dart/R8 "a hash in no lineage entry → layout_newer, reporting the file's
		// hash AND NOTHING ELSE": the name, layoutHash == the file's hash, every
		// other report field untouched and not one destination byte written.
		{"dart/R8", ownRun(roadmapPrelude + `
  for (final stranger in <int>[0x0EADBEEFCAFEF00D, 0x7FFFFFFFFFFFFFFF, 1, -1, -0x7FFFFFFFFFFFFFFF]) {
    final forged = Uint8List.fromList(data);
    ByteData.sublistView(forged).setUint64(t.TableFixedLimits.hashAt, stranger, Endian.little);
    final rep = refused(forged, values.length, t.TableFixedRefusal.layoutNewer, 'R8 stranger hash $stranger');
    check(rep.layoutHash == stranger, 'R8 reports the file\'s hash: ${rep.layoutHash} want $stranger');
    check(rep.hash == 0, 'R8 and NOTHING ELSE: the report\'s own hash stays zero: ${rep.hash}');
  }`)},

		// dart/R9 "a known hash with a different layout length or bytes →
		// layout_malformed; the seven §1.1 malformations under a known hash all
		// come back as this one name". One forged file per §1.1 rule, each under
		// the held hash, plus a different length; none is walked, all are one name
		// with nothing set, and the held file still reads afterwards.
		{"dart/R9", ownRun(roadmapPrelude + `
  final entry1 = t.TableFixedLimits.layoutAt + 4 + 17;
  final count = head.getUint32(t.TableFixedLimits.layoutAt, Endian.little);
  check(count >= 2, 'R9 needs a root and a field: $count entries');
  Uint8List forge(void Function(Uint8List, ByteData) edit) {
    final f = Uint8List.fromList(data);
    edit(f, ByteData.sublistView(f));
    return f;
  }
  final cases = <String, Uint8List>{
    'rule 1 count_mismatch': forge((f, v) => v.setUint32(t.TableFixedLimits.layoutAt, count + 1, Endian.little)),
    'rule 2 kind_unknown': forge((f, v) => f[entry1 + 8] = 31),
    'rule 3 size_mismatch': forge((f, v) => v.setUint32(entry1 + 9, v.getUint32(entry1 + 9, Endian.little) + 1, Endian.little)),
    'rule 4 kind_invalid': forge((f, v) => f[t.TableFixedLimits.layoutAt + 4 + 8] = 2),
    'rule 5 tree_unclosed': forge((f, v) => v.setUint32(t.TableFixedLimits.layoutAt + 4 + 13, 1000, Endian.little)),
    'rule 6 record_too_large': forge((f, v) => v.setUint32(t.TableFixedLimits.layoutAt + 4 + 9, 70000, Endian.little)),
  };
  // rule 7, nesting past the walk bound: a chain of sixty-six single-child
  // tables is a different layout LENGTH under the same held hash
  {
    final chain = BytesBuilder();
    final cnt = ByteData(4)..setUint32(0, 66, Endian.little);
    chain.add(cnt.buffer.asUint8List());
    for (var i = 0; i < 66; i++) {
      final e = ByteData(17);
      e.setUint64(0, i + 1, Endian.little);
      e.setUint8(8, 13);
      e.setUint32(9, 8, Endian.little);
      e.setUint32(13, i == 65 ? 0 : 1, Endian.little);
      chain.add(e.buffer.asUint8List());
    }
    final lay = chain.toBytes();
    final f = BytesBuilder();
    f.add(data.sublist(0, t.TableFixedLimits.layoutAt));
    f.add(lay);
    f.add(data.sublist(at0));
    final out = f.toBytes();
    ByteData.sublistView(out).setUint32(t.TableFixedLimits.headerBytes, lay.length, Endian.little);
    cases['rule 7 too_deep'] = out;
  }
  // a different layout length with every byte of the old layout still in place
  cases['longer by one byte'] = Uint8List.fromList(
      [...data.sublist(0, at0), 0, ...data.sublist(at0)])
    ..buffer.asByteData().setUint32(t.TableFixedLimits.headerBytes, layoutBytes + 1, Endian.little);
  cases['shorter by one byte'] = forge((f, v) => v.setUint32(t.TableFixedLimits.headerBytes, layoutBytes - 1, Endian.little));
  cases['second layout, one byte flipped in the last entry'] = forge((f, v) => f[at0 - 1] ^= 0xff);
  for (final c in cases.entries) {
    final rep = refused(c.value, values.length, t.TableFixedRefusal.layoutMalformed, 'R9 ${c.key}');
    check(rep.layoutHash == 0 && rep.hash == 0, 'R9 ${c.key}: one name and nothing reported: ${why(rep)}');
  }
  poison();
  final after = t.TableFixedReport();
  check(LOAD(values, values.length, data, data.length, plan, after) == 1 && after.refused == 0 && !after.malformed,
      'R9 the refusals changed nothing: the held file still reads: ${why(after)}');`)},

		// dart/R13 "REFUSE is total: refused+reason and malformed are never both
		// set, every counter stays zero, and not one destination byte is written".
		// Every refusal the load owes, on the identity lane and on a compiled
		// (older-entry) lane, including a bad LATER record behind good ones —
		// the earlier records must not be decoded into the destination — and a
		// malformed read, which sets malformed and no reason.
		{"dart/R13", []roadmapRun{
			{"VNEW_field_append", nil, own, roadmapPrelude + `
  final bad = records(3);
  bad[at0 + 2 * recordBytes] ^= 0xff;
  refused(bad, 3, t.TableFixedRefusal.noLayout, 'R13 identity lane, record 2 forged behind two good ones');
  refused(records(3), 2, t.TableFixedRefusal.batchTooLarge, 'R13 identity lane, batch past capacity');
  final newer = Uint8List.fromList(data)..[0] = 9;
  refused(newer, 1, t.TableFixedRefusal.newerForm, 'R13 a form byte this reader does not carry');
  final alien = Uint8List.fromList(data);
  ByteData.sublistView(alien).setUint64(t.TableFixedLimits.hashAt, 0x0EADBEEFCAFEF00D, Endian.little);
  refused(alien, 1, t.TableFixedRefusal.layoutNewer, 'R13 a hash in no lineage entry');
  final lie = Uint8List.fromList(data)..[at0 - 1] ^= 0xff;
  refused(lie, 1, t.TableFixedRefusal.layoutMalformed, 'R13 a known hash over other layout bytes');
  poison();
  final ragged = t.TableFixedReport();
  final cut = Uint8List.fromList(data.sublist(0, data.length - 1));
  final n = LOAD(values, 1, cut, cut.length, plan, ragged);
  check(n == -1 && ragged.malformed && ragged.refused == 0, 'R13 a malformed read is malformed and names no reason: ${why(ragged)}');
  check(quiet(ragged) && untouched(), 'R13 a malformed read moves no counter and writes no byte: ${why(ragged)}');`},
			{"VNEW_field_append", []string{"VOLD_field_append"}, old, roadmapPrelude + `
  final bad = records(3);
  bad[at0 + 2 * recordBytes] ^= 0xff;
  refused(bad, 3, t.TableFixedRefusal.noLayout, 'R13 compiled lane, record 2 forged behind two good ones');
  refused(records(3), 2, t.TableFixedRefusal.batchTooLarge, 'R13 compiled lane, batch past capacity');`},
		}},
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

	// dart/R7, the static half: "the compiler hands every reader its own wire
	// hash and every known hash as CONSTANTS ... and a runtime NEVER computes a
	// hash from layout bytes it holds". The generated load compares the header
	// hash to those constants and calls no hash function.
	t.Run("dart/R7/static", func(t *testing.T) {
		t.Parallel()
		u := versionSchema(t, "VNEW_field_append")
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		var src string
		for name, data := range files {
			if strings.HasSuffix(name, "Fixed.dart") && strings.Contains(string(data), "FixedLoad(") {
				src = string(data)
			}
		}
		at := strings.Index(src, "FixedLoad(")
		if at < 0 {
			t.Fatal("no generated FixedLoad in the fixed library")
		}
		body := src[at:]
		if end := strings.Index(body, "\n}\n"); end >= 0 {
			body = body[:end]
		}
		if !strings.Contains(body, "if (hash != ") {
			t.Fatal("the identity lane is not an index/constant comparison against the handed own hash")
		}
		for _, banned := range []string{"hashOf(", "0x100000001b3", "fnv"} {
			if strings.Contains(body, banned) {
				t.Fatalf("the load computes a hash from layout bytes (%q): a runtime never derives one", banned)
			}
		}
	})
}
