package darttable

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
)

// dartUnlawfulAnswer is the Dart leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful).
type dartUnlawfulAnswer struct {
	// poison sets row v of the NEW type to values that are not its declared
	// defaults, so a load that leaves a row alone, or lands the file in it, is
	// seen.
	poison string
	// defaults is true of row v exactly when every field the poison touched
	// holds its declared default (read off the VNEW schema: lead = 1,
	// trail = 2, every unlisted field its type's default).
	defaults string
	// values, for the LAWFUL control only, is a Dart expression over
	// values[0] that formats the loaded record as the case's Values line.
	values string
}

var dartUnlawfulAnswers = map[string]dartUnlawfulAnswer{
	"unknown_census": {
		poison:   `v.lead = 99; v.trail = 88; for (var j = 0; j < v.items.length; j++) { v.items[j].a = 5 + j; }`,
		defaults: `v.lead == 1 && v.trail == 2 && v.items.every((e) => e.a == 0)`,
	},
	"unknown_census_many": {
		poison:   `v.lead = 99; v.trail = 88; for (var j = 0; j < v.items.length; j++) { v.items[j].a = 5 + j; }`,
		defaults: `v.lead == 1 && v.trail == 2 && v.items.every((e) => e.a == 0)`,
	},
	"kind_moved": {
		poison:   `v.lead = 99; v.v = 5.5; v.trail = 88;`,
		defaults: `v.lead == 1 && v.v == 0.0 && v.trail == 2`,
	},
	"arm_removed": {
		poison:   `v.lead = 99; v.pick.type = 2 as dynamic; /* PickType.gamma: the packet library is not re-exported */ v.pick.gamma.p = 77; v.trail = 88;`,
		defaults: `v.lead == 1 && v.pick.type == 0 /* PickType.none */ && v.trail == 2`,
	},
	"variant_removed": {
		poison:   `v.lead = 99; v.tier = 2 as dynamic; /* Tier.gold */ v.trail = 88;`,
		defaults: `v.lead == 1 && v.tier == 0 /* Tier.none */ && v.trail == 2`,
	},
	"key_removed": {
		poison:   `v.lead = 99; for (var j = 0; j < v.slots.length; j++) { v.slots[j] = 70 + j; } v.trail = 88;`,
		defaults: `v.lead == 1 && v.slots.every((e) => e == 0) && v.trail == 2`,
	},
	"array_shrunk": {
		poison:   `v.lead = 99; for (var j = 0; j < v.vals.length; j++) { v.vals[j] = 70 + j; } v.trail = 88;`,
		defaults: `v.lead == 1 && v.vals.every((e) => e == 0) && v.trail == 2`,
	},
	"counted_shrunk": {
		poison:   `v.lead = 99; v.valsCount = 2; v.vals[0] = 70; v.vals[1] = 71; v.trail = 88;`,
		defaults: `v.lead == 1 && v.valsCount == 0 && v.vals.every((e) => e == 0) && v.trail == 2`,
	},
	"text_narrowed": {
		poison:   `v.lead = 99; v.text[0] = 0x7a; v.text[1] = 0x7a; v.textLength = 2; v.trail = 88;`,
		defaults: `v.lead == 1 && v.textLength == 0 && v.text.every((e) => e == 0) && v.trail == 2`,
	},
	"lawful_control": {
		poison: `v.lead = 99; v.v = 5; v.trail = 88;`,
		values: `"lead=${values[0].lead} v=${values[0].v} trail=${values[0].trail}"`,
	},
	"bytes_narrowed": {
		poison:   `v.lead = 99; v.blob[0] = 0x5A; v.blobLength = 1; v.trail = 88;`,
		defaults: `v.lead == 1 && v.blobLength == 0 && v.blob.every((e) => e == 0) && v.trail == 2`,
	},
}

// TestDartFixedUnlawfulCorpus is the Dart leg's driver for the shared
// unlawful-lineage corpus. The probe prints its verdict on a line of its own
// ("VERDICT: ..."), and the driver answers what the probe printed: the LAWFUL
// control's loaded values, or "refused" once every check below held. Per
// UNLAWFUL case: the NEW build, handed the OLD layout as a lineage entry,
// refuses the OLD file before any record lands, every counter zero, every
// poisoned row back at its declared defaults (both by the answer's own check
// and by fresh(), the probe's whole-value comparison), on a first load and a
// second through the same plan (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT,
// §5.4, §5.9 #30; SPEC-TABLES §6.5; the Go reference's
// TestFixedUnlawfulCorpus). The LAWFUL control loads, and its verdict is the
// values the load landed in record 0.
//
// The verdict is the shared one (refusal-verdict-only). Separately, and as
// this leg's own assertion, the refusal's NAME is pinned: Dart answers an
// unlawful entry layout_damaged (TableFixedRefusal.layoutDamaged), as it does
// for kind_moved and unknown_census. The refusal is a runtime check in every
// build, never an assert: the probe runs with --enable-asserts, but nothing
// it checks rides on one.
func TestDartFixedUnlawfulCorpus(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	fixedunlawful.Run(t, "darttable", dartUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a dartUnlawfulAnswer) string {
		t.Parallel()
		body := strings.NewReplacer("POISON", a.poison, "DEFAULTS", a.defaults, "CASE", c.Name).Replace(`  final data = File(FILE).readAsBytesSync();
  for (var pass = 0; pass < 2; pass++) {
    for (final v in values) {
      POISON
    }
    report.reset();
    final n = LOAD(values, values.length, data, data.length, plan, report);
    // THE VERDICT (shared): refused, before any record lands.
    check(n < 0, 'CASE load $pass: the UNLAWFUL entry loaded $n records: ${why(report)}');
    // THE NAME (this leg's own): layout_damaged.
    check(report.refused == t.TableFixedRefusal.layoutDamaged,
        'CASE load $pass: refused, but not layout_damaged: ${why(report)}');
    check(report.unknown == 0 && report.widened == 0,
        'CASE load $pass: a refusal moves no counter: ${why(report)}');
    for (var k = 0; k < values.length; k++) {
      final v = values[k];
      check(DEFAULTS, 'CASE load $pass: row $k is off its declared defaults after the refusal');
      check(fresh(v), 'CASE load $pass: row $k is not a fresh value after the refusal');
    }
  }
  if (exitCode == 0) {
    stdout.writeln('VERDICT: refused');
  }
`)
		if c.Lawful {
			body = strings.NewReplacer("POISON", a.poison, "VALUES", a.values, "CASE", c.Name).Replace(`  final data = File(FILE).readAsBytesSync();
  for (final v in values) {
    POISON
  }
  report.reset();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n >= 0 && report.refused == 0, 'CASE: the LAWFUL control refused: n=$n ${why(report)}');
  if (exitCode == 0) {
    stdout.writeln('VERDICT: loaded $n: ' + VALUES);
  }
`)
		}
		out, err := runVersionProbe(t, dartBin, c.New, []string{c.Old}, 0, filepath.Join(corpus, c.File), body)
		if err != nil {
			t.Fatalf("%s: %v\n%s", c.Name, err, out)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if verdict, ok := strings.CutPrefix(strings.TrimSpace(line), "VERDICT: "); ok {
				return verdict
			}
		}
		t.Fatalf("%s: the probe printed no verdict:\n%s", c.Name, out)
		return ""
	})
}
