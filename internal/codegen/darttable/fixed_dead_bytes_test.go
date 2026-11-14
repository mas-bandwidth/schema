package darttable

import "testing"

// EVERY DEAD BYTE IS ZERO (docs/SPEC-TABLES.md §3.4, THE SLACK RULE; the
// owner's ruling of 2026-11-10, "Every dead byte", "Be strict";
// docs/rulings.json fixed-dead-bytes-zero). One record of every slack class
// is written clean and read back, then one byte of each class's slack is
// stained in turn and the identity read must refuse wire_damaged, landing
// nothing. The classes: an absent optional's payload, a counted array's tail
// past its live count, string(N) and bytes(N) past the used length, a union's
// arm bytes under tag 0 and behind a narrower arm, a payload-free arm's
// whole arm span, and a text inside a LIVE array element (slack nested in live
// data). A ranged `min = 1` field inside slack reads zero and does not refuse:
// slack is checked for zero and for nothing else.
//
// Layout of T (offsets in the body):
//
//	0  o ?Inner           present 0, payload 1..4 (Inner{ r int8 | min = 1, max = 100; s int16 })
//	4  c [..2]int16       count 4..7, elements 8..11
//	12 s string(4)        used 12..15, units 16..19
//	20 b bytes(3)         used 20..23, units 24..26
//	27 u Shot             tag 27, arms 28..35 (small int8, big int64, ping)
//	36 w [..2]Item        count 36..39, elements 40.. (Item{ n string(2) } = 6 bytes)
const deadBytesSchema = `package probe

type Inner
{
    r int8 = 1 | min = 1, max = 100
    s int16
}

type Item
{
    n string(2)
}

union Shot
{
    small int8
    big   int64
    ping
}

fixed table T
{
    o ?Inner
    c [..2]int16
    s string(4)
    b bytes(3)
    u Shot
    w [..2]Item
}
`

func TestFixedDeadBytesRefusedDart(t *testing.T) {
	t.Parallel()
	t12Capability(t, deadBytesSchema, `
  final v = values[0];
  v.oPresent = false;
  v.cCount = 1;
  v.c[0] = 7;
  v.sLength = 1;
  v.s[0] = 0x41;
  v.bLength = 1;
  v.b[0] = 9;
  v.u.type = 1;
  v.u.small = 3;
  v.wCount = 1;
  v.w[0].nLength = 1;
  v.w[0].n[0] = 0x42;
  final wrote = t.tFixedSave(values, 1, bytes);
  check(wrote == bytes.length, 'the writer wrote $wrote of ${bytes.length}');
  final body = t.tFixedHeaderBytes + 8;
  final plan = t.tFixedNewPlan();
  final rep = t.TableFixedReport();
  final back = [t.T()];
  t.initT(back[0]);
  check(t.tFixedLoad(back, 1, bytes, bytes.length, plan, rep) == 1 &&
      rep.refused == 0, 'the clean record reads: ${rep.refused}');
  check(back[0].cCount == 1 && back[0].u.small == 3 && back[0].wCount == 1,
      'the clean record landed');
  void stained(String what, int at) {
    final saved = bytes[at];
    bytes[at] = 0x04;
    t.initT(back[0]);
    final n = t.tFixedLoad(back, 1, bytes, bytes.length, plan, rep);
    check(n == -1 && rep.refused == t.TableFixedRefusal.damaged,
        '$what (body+${at - body}) must refuse wire_damaged: n=$n ${rep.refused}');
    check(back[0].cCount == 0, '$what: nothing landed');
    bytes[at] = saved;
  }
  stained('an absent optional payload, the ranged field', body + 1);
  stained('an absent optional payload, the last byte', body + 3);
  stained('a counted array tail', body + 10);
  stained('a string past its used length', body + 17);
  stained('bytes past the used length', body + 26);
  stained('the bytes behind a narrower arm', body + 29);
  stained('the last byte behind a narrower arm', body + 35);
  stained('a text inside a live element', body + 45);
  stained('a whole dead element', body + 47);
  // UNDER TAG 0, and under the payload-free arm, EVERY arm byte is slack
  bytes[body + 27] = 0;
  bytes[body + 28] = 0;
  check(t.tFixedLoad(back, 1, bytes, bytes.length, plan, rep) == 1,
      'tag 0 with zero arms reads: ${rep.refused}');
  stained('an arm byte under tag 0', body + 28);
  bytes[body + 27] = 3;
  check(t.tFixedLoad(back, 1, bytes, bytes.length, plan, rep) == 1,
      'the payload-free arm with zero arms reads: ${rep.refused}');
  stained('an arm byte under the payload-free arm', body + 28);
  // AND THE WIDEST ARM HAS NO SLACK: every one of its bytes is live
  bytes[body + 27] = 2;
  bytes[body + 35] = 0x7f;
  check(t.tFixedLoad(back, 1, bytes, bytes.length, plan, rep) == 1,
      'the widest arm\'s last byte is live data: ${rep.refused}');
`, 1)
}
