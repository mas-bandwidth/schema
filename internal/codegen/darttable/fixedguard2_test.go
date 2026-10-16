package darttable

// dart/W16: "nested union answers outer tag" (docs/FIXED-FORM-ALGORITHM.md
// §4.1, §5.2, §5.8 row 6; docs/SPEC-TABLES.md §3.4).
//
// `guard2` / `arg2` / `argw2` are the OUTER tag an arm inside an arm also
// answers to, CONJOINED with `guard` / `arg` / `argw`: the entry runs when BOTH
// tags hold their ordinal, and `NO_GUARD` on `guard2` means no second
// condition.

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
)

func TestFixedNestedUnionAnswersTheOuterTagDart(t *testing.T) {
	t.Parallel()
	t.Run("dart/W16", func(t *testing.T) {
		t.Parallel()

		u := t12Schema(t, "FG1")
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("W16: Generate(FG1): %v", err)
		}
		src := t02Source(t, files)

		// 1. Static plan shape: 12 lanes per entry (op, src, dst, size, aux,
		// guard, arg, meta, argW, guard2, arg2, argW2), and every guarded
		// entry of FG1's FhRoot answers to the outer union tag at byte 5 — on
		// lane 1 for Outer's own entries and on lane 2 (guard2/arg2/argW2) for
		// Inner's arm entries (whose lane 1 guard is the inner tag at byte 6).
		for _, want := range []string{
			"static const int guard2 = 9;",
			"static const int arg2 = 10;",
			"static const int argW2 = 11;",
			"static const int lanes = 12;",
		} {
			if !strings.Contains(fixedRuntime, want) {
				t.Errorf("W16: fixedRuntime does not declare %q", want)
			}
		}

		rows := t02Rows(t, src, "final Int32List fhRootFixedIdentity")
		if len(rows) == 0 {
			t.Fatal("W16: fhRootFixedIdentity has no rows")
		}
		const (
			outerTagAt = 5
			innerTagAt = 6
		)
		sawInnerTag := false
		sawInnerArm1 := false
		sawInnerArm2 := false
		for idx, r := range rows {
			if len(r) != 12 {
				t.Fatalf("W16: fhRootFixedIdentity row %d has %d lanes, want 12", idx, len(r))
			}
			guard, arg, argw := r[5], r[6], r[8]
			guard2, arg2, argw2 := r[9], r[10], r[11]
			if guard == fixedNoGuard {
				if guard2 != fixedNoGuard {
					t.Errorf("W16: unguarded row %d carries guard2=%d", idx, guard2)
				}
				continue
			}
			if r[1] == innerTagAt && r[3] == 1 && guard == outerTagAt && guard2 == fixedNoGuard {
				sawInnerTag = true
			}
			if guard == innerTagAt {
				if guard2 != outerTagAt || arg2 != 1 || argw2 != 1 || argw != 1 {
					t.Errorf("W16: inner arm row %d must conjoin inner guard=6 with outer guard2=5/arg2=1/argw2=1; got guard=%d arg=%d argw=%d guard2=%d arg2=%d argw2=%d",
						idx, guard, arg, argw, guard2, arg2, argw2)
				}
				switch arg {
				case 1:
					sawInnerArm1 = true
				case 2:
					sawInnerArm2 = true
				}
			} else if guard != outerTagAt {
				t.Errorf("W16: guarded row %d has unexpected guard=%d guard2=%d", idx, guard, guard2)
			}
		}
		if !sawInnerTag {
			t.Error("W16: inner union's tag copy is not guarded by outer tag at offset 5")
		}
		if !sawInnerArm1 || !sawInnerArm2 {
			t.Errorf("W16: expected both inner arms guarded by guard=6 and guard2=5 (arm1=%v arm2=%v)", sawInnerArm1, sawInnerArm2)
		}

		// 2. Functional probe on both the identity plan and a lineage-compiled
		// plan (an older hash with the unit's own layout).
		self, ok := FixedLineageOf(u, "FhRoot")
		if !ok {
			t.Fatal("W16: no lineage entry for FhRoot")
		}
		otherHash := self.Wire ^ 0x00ff00ff00ff00ff
		lineage := map[string][]FixedLineageEntry{
			"FhRoot": {{Wire: otherHash, Layout: self.Layout, Record: self.Record}},
		}
		pktFiles, err := dart.Generate(u)
		if err != nil {
			t.Fatalf("W16: dart.Generate: %v", err)
		}
		tblFiles, err := GenerateLineage(u, lineage)
		if err != nil {
			t.Fatalf("W16: GenerateLineage: %v", err)
		}
		maps.Copy(pktFiles, tblFiles)

		codec := ""
		typefile := ""
		for name, data := range pktFiles {
			if strings.HasSuffix(name, "Fixed.dart") && bytes.Contains(data, []byte("fhRootFixedLoad")) {
				codec = name
			}
			if strings.HasSuffix(name, "Fixed.dart") && bytes.Contains(data, []byte("class FhRoot")) {
				typefile = name
			}
		}
		if codec == "" || typefile == "" {
			t.Fatalf("W16: missing codec/type file (codec=%q type=%q)", codec, typefile)
		}
		dir := t.TempDir()
		for name, data := range pktFiles {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}

		probe := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import %q as c;
import %q as t;
import './FG1.dart' as d;

void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    exitCode = 1;
  }
}

void setHash(ByteData view, int at, int lo, int hi) {
  view.setUint32(at, lo, Endian.little);
  view.setUint32(at + 4, hi, Endian.little);
}

void main() {
  const otherLo = 0x%08x;
  const otherHi = 0x%08x;

  // (d) In a compiled plan, the None entry of the inner union carries the
  // OUTER guard (5, arg 1) at the OUTER tag's width (1), with guard2 = -1,
  // while the inner arms carry guard = 6 and guard2 = 5, arg2 = 1, argW2 = 1.
  {
    final cplan = c.fhRootFixedNewPlan();
    final crep = t.TableFixedReport();
    check(
      cplan.theirs.parse(
        ByteData.sublistView(c.fhRootFixedLayout),
        0,
        c.fhRootFixedLayout.length,
      ),
      'W16(d): fhRootFixedLayout parses',
    );
    final made = t.TableFixedCompiler.compile(
      cplan,
      c.fhRootFixedLayout,
      ByteData.sublistView(c.fhRootFixedLayout),
      c.fhRootFixedDst,
      c.fhRootFixedCover,
      c.fhRootFixedCoverCount,
      crep,
    );
    check(made > 0, 'W16(d): compile produced $made entries');
    var sawInnerNone = false;
    var sawCompiledInnerArm1 = false;
    var sawCompiledInnerArm2 = false;
    for (var i = 0; i < made; i++) {
      final b = i * t.TableFixedLane.lanes;
      final op = cplan.entries[b + t.TableFixedLane.op];
      final src = cplan.entries[b + t.TableFixedLane.src];
      final dst = cplan.entries[b + t.TableFixedLane.dst];
      final aux = cplan.entries[b + t.TableFixedLane.aux];
      final guard = cplan.entries[b + t.TableFixedLane.guard];
      final arg = cplan.entries[b + t.TableFixedLane.arg];
      final argW = cplan.entries[b + t.TableFixedLane.argW];
      final guard2 = cplan.entries[b + t.TableFixedLane.guard2];
      final arg2 = cplan.entries[b + t.TableFixedLane.arg2];
      final argW2 = cplan.entries[b + t.TableFixedLane.argW2];
      if (op == t.TableFixedOp.constant && dst == 6 && aux == 0) {
        sawInnerNone = true;
        check(
          guard == 5 && arg == 1 && argW == 1 && guard2 == t.tableFixedNoGuard,
          'W16(d): inner None entry must carry outer guard=5/arg=1/argW=1 and guard2=NO_GUARD, '
          'got guard=$guard arg=$arg argW=$argW guard2=$guard2',
        );
      }
      if (op == t.TableFixedOp.copy && src == 7 && dst == 7) {
        check(
          guard == 6 && argW == 1 && guard2 == 5 && arg2 == 1 && argW2 == 1,
          'W16(d): compiled inner arm copy must carry guard=6 and guard2=5/arg2=1/argW2=1, '
          'got guard=$guard arg=$arg argW=$argW guard2=$guard2 arg2=$arg2 argW2=$argW2',
        );
        if (arg == 1) sawCompiledInnerArm1 = true;
        if (arg == 2) sawCompiledInnerArm2 = true;
      }
    }
    check(sawInnerNone, 'W16(d): compiled plan carries inner union None entry');
    check(
      sawCompiledInnerArm1 && sawCompiledInnerArm2,
      'W16(d): compiled plan carries both inner arm copies ($sawCompiledInnerArm1, $sawCompiledInnerArm2)',
    );
  }

  // Run (a), (b), (c) across both path 0 (identity plan) and path 1
  // (lineage-compiled plan selected by the older hash).
  for (var path = 0; path < 2; path++) {
    final label = path == 0 ? 'identity' : 'compiled';

    // (a) & (b): Outer selects B (arm 2, which does NOT hold Inner).
    // Set m = 0x22334401 so byte 6 (the inner tag's offset) is 1 (Inner.leaf),
    // and forge byte 10 (the high byte of Inner's payload at 7..11, past
    // OuterB.m's 6..10) to 0x99. Stain plan.image[10] with 0x5A and stain
    // caller's back[0].pick.a.inner.leaf.n with 0x5A5A5A5A.
    {
      final one = t.FhRoot();
      t.initFhRoot(one);
      one.tier = d.Tier.silver;
      one.head = 77;
      one.pick.type = 2;
      one.pick.b.m = 0x22334401;
      final wire = Uint8List(c.fhRootFixedMeasure(1));
      check(
        c.fhRootFixedSave(<t.FhRoot>[one], 1, wire) == wire.length,
        'W16($label): B record saves',
      );
      final bodyAt = c.fhRootFixedHeaderBytes + 8;
      check(wire[bodyAt + 5] == 2, 'W16($label): outer tag is 2 (OuterB)');
      check(wire[bodyAt + 6] == 1, 'W16($label): byte 6 is 1 (forged inner tag Inner.leaf)');
      wire[bodyAt + 10] = 0x99;
      if (path == 1) {
        final wv = ByteData.sublistView(wire);
        setHash(wv, t.TableFixedLimits.hashAt, otherLo, otherHi);
        setHash(wv, c.fhRootFixedHeaderBytes, otherLo, otherHi);
      }

      final plan = c.fhRootFixedNewPlan();
      plan.image.fillRange(0, plan.image.length, 0x5A);
      final back = <t.FhRoot>[t.FhRoot()];
      t.initFhRoot(back[0]);
      back[0].pick.a.inner.type = 2;
      back[0].pick.a.inner.leaf.n = 0x5A5A5A5A;
      back[0].pick.a.inner.other.k = 0x5A5A5A5A;
      final rep = t.TableFixedReport();
      final n = c.fhRootFixedLoad(back, 1, wire, wire.length, plan, rep);
      check(
        n == 1 && rep.refused == 0 && !rep.malformed && rep.clamped == 0,
        'W16($label): B record loads cleanly (n=$n refused=${rep.refused} malformed=${rep.malformed})',
      );
      // (a) the other arm reads back whole
      check(
        back[0].pick.type == 2 &&
            back[0].pick.b.m == 0x22334401 &&
            back[0].tier == d.Tier.silver &&
            back[0].head == 77,
        'W16($label)(a): OuterB reads back whole (type=${back[0].pick.type} m=0x${back[0].pick.b.m.toRadixString(16)})',
      );
      // (b) no inner-arm entry ran: plan.image[10] (owned by Inner's arm at
      // 7..11, outside OuterB.m at 6..10) was not overwritten with 0x99, and
      // caller storage for Inner is untouched.
      check(
        plan.image[10] == 0x5A,
        'W16($label)(b): inner arm entry ran under OuterB and overwrote image[10]=0x${plan.image[10].toRadixString(16)}',
      );
      check(
        back[0].pick.a.inner.leaf.n == 0x5A5A5A5A &&
            back[0].pick.a.inner.other.k == 0x5A5A5A5A,
        'W16($label)(b): caller inner storage was touched (${back[0].pick.a.inner.leaf.n}, ${back[0].pick.a.inner.other.k})',
      );
    }

    // (c) The reverse: Outer selects OuterA (arm 1) and Inner selects other
    // (arm 2); only the inner arm named by the inner tag runs.
    {
      final one = t.FhRoot();
      t.initFhRoot(one);
      one.tier = d.Tier.bronze;
      one.head = 11;
      one.pick.type = 1;
      one.pick.a.edge = 22;
      one.pick.a.inner.type = 2;
      one.pick.a.inner.other.k = 33;
      final wire = Uint8List(c.fhRootFixedMeasure(1));
      check(
        c.fhRootFixedSave(<t.FhRoot>[one], 1, wire) == wire.length,
        'W16($label)(c): A record saves',
      );
      if (path == 1) {
        final wv = ByteData.sublistView(wire);
        setHash(wv, t.TableFixedLimits.hashAt, otherLo, otherHi);
        setHash(wv, c.fhRootFixedHeaderBytes, otherLo, otherHi);
      }

      final plan = c.fhRootFixedNewPlan();
      final back = <t.FhRoot>[t.FhRoot()];
      t.initFhRoot(back[0]);
      back[0].pick.a.inner.leaf.n = 0x5A5A5A5A;
      final rep = t.TableFixedReport();
      final n = c.fhRootFixedLoad(back, 1, wire, wire.length, plan, rep);
      check(
        n == 1 && rep.refused == 0 && !rep.malformed && rep.clamped == 0,
        'W16($label)(c): A record loads cleanly (n=$n)',
      );
      check(
        back[0].pick.type == 1 &&
            back[0].pick.a.edge == 22 &&
            back[0].pick.a.inner.type == 2 &&
            back[0].pick.a.inner.other.k == 33,
        'W16($label)(c): OuterA and Inner.other land whole',
      );
      check(
        back[0].pick.a.inner.leaf.n == 0x5A5A5A5A,
        'W16($label)(c): unselected inner arm leaf.n was touched (${back[0].pick.a.inner.leaf.n})',
      );
    }
  }

  if (exitCode == 0) {
    stdout.writeln('ok');
  }
}
`, "./"+codec, "./"+typefile, uint32(otherHash), uint32(otherHash>>32))

		out := t12RunProbe(t, dir, "w16_probe.dart", probe)
		if !strings.Contains(out, "ok") {
			t.Fatalf("W16: probe did not print ok:\n%s", out)
		}
	})
}
