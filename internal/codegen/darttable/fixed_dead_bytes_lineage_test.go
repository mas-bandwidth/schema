package darttable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// EVERY DEAD BYTE IS ZERO ON A LINEAGE READ TOO (docs/SPEC-TABLES.md §3.4,
// THE SLACK RULE; docs/rulings.json fixed-dead-bytes-zero). The OLD build
// writes one record; the NEW build reads it through its lineage, so the plan
// is COMPILED, and the slack it checks is the WRITER's: the bytes past the
// writer's used length, past the writer's live count, and under the writer's
// tag 0. A clean record reads; one stained slack byte refuses wire_damaged and
// lands nothing; a stained byte of LIVE data is data and reads.
type deadLineageRow struct {
	base  string // the pair, VOLD_<base> written and VNEW_<base> reading
	write string // Dart lines filling values[0] before the OLD build saves it
	read  string // Dart lines run against the record at `body` in `data`
}

var deadLineageRows = []deadLineageRow{
	{
		// StringGrow: lead 0..3, text used 4..7 units 8..15, trail 16..19
		base: "string_grow",
		write: `  values[0].textLength = 2;
  values[0].text[0] = 0x41;
  values[0].text[1] = 0x42;`,
		read: `  stained('the writer\'s text past its used length', body + 10);
  stained('the writer\'s last text unit', body + 15);
  live('a used text unit', body + 9);`,
	},
	{
		// ArrayBoundedGrow: lead 0..3, count 4..7, elements 8..23, trail 24..27
		base: "array_bounded_grow",
		write: `  values[0].valsCount = 1;
  values[0].vals[0] = 5;`,
		read: `  stained('the writer\'s counted tail', body + 12);
  stained('the writer\'s last tail byte', body + 23);
  live('the live element', body + 9);`,
	},
	{
		// Lineage (union_append): tag 0, arm 1..4, seq 5..8
		base:  "union_append",
		write: `  values[0].seq = 3;`,
		read: `  stained('an arm byte under the writer\'s tag 0', body + 2);
  data[body] = 1;
  live('the alpha arm, which is the writer\'s whole arm span', body + 2);`,
	},
}

func TestFixedDeadBytesLineageRefusedDart(t *testing.T) {
	dartBin := dartBinary(t)
	for _, row := range deadLineageRows {
		t.Run(row.base, func(t *testing.T) {
			old := "VOLD_" + row.base
			st := versionRoot(t, versionSchema(t, old))
			lower := lowerFirst(st.Name)
			file := filepath.Join(t.TempDir(), "old_"+row.base+".bin")
			write := fmt.Sprintf(`%s
  final out = Uint8List(t.%sFixedMeasure(1));
  final wrote = t.%sFixedSave(values, 1, out);
  check(wrote == out.length, 'the old build wrote $wrote of ${out.length}');
  File(FILE).writeAsBytesSync(out);`, row.write, lower, lower)
			if out, err := runVersionProbe(t, dartBin, old, nil, 0, file, write); err != nil {
				t.Fatalf("the old build's write: %v\n%s", err, out)
			}
			read := fmt.Sprintf(`  final data = File(FILE).readAsBytesSync();
  final body = data.length - %d;
  check(LOAD(values, 1, data, data.length, plan, report) == 1,
      'the clean record reads: ${why(report)}');
  void stained(String what, int at) {
    final saved = data[at];
    data[at] = 0x04;
    t.init%s(values[0]);
    final n = LOAD(values, 1, data, data.length, plan, report);
    check(n == -1 && report.refused == t.TableFixedRefusal.damaged,
        '$what (body+${at - body}) must refuse wire_damaged: n=$n ${why(report)}');
    check(fresh(values[0]), '$what: nothing landed');
    data[at] = saved;
  }
  void live(String what, int at) {
    final saved = data[at];
    data[at] = 0x04;
    final n = LOAD(values, 1, data, data.length, plan, report);
    check(n == 1, '$what is live data and reads: n=$n ${why(report)}');
    data[at] = saved;
  }
%s`, fixedTypeBytes(st), st.Name, row.read)
			if out, err := runVersionProbe(t, dartBin, "VNEW_"+row.base, []string{old}, 0, file, read); err != nil {
				t.Fatalf("the new build's lineage read: %v\n%s", err, out)
			}
		})
	}
}
