package darttable

// dart/R28 "the guard chain" (docs/FIXED-FORM-ALGORITHM.md §4.1, §5.8 row 6;
// docs/SPEC-TABLES.md §3.4): "the plan entry carries offset+count into a pool
// of (guard, arg, argw) links, outermost first, the only bound the layout's
// 64; an arm inside an arm answers to the OUTER tag".
//
// THE TWO INLINE LANES ARE THE FIRST LINKS OF THE CHAIN. `guard`/`arg`/`argW`
// is the innermost tag and `guard2`/`arg2`/`argW2` the one around it, exactly
// as dart/W16 left them; a third tag and every one past it rides the POOL, and
// `poolAt`/`poolCount` name that run, outermost first. A count of 0 is an
// entry nested two deep or less, and it reads exactly as it did before the
// pool existed.
//
// The fixture is testdata/FG3.schema: three nested unions, the shape two lanes
// cannot carry. At the base this test was written against, the third tag
// overwrote the second on lane 2, so Leaf's entry ran under Mid's OTHER arm.

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// r28Unit parses and checks a schema held in memory, so the deep fixtures the
// chain's bound needs are written by the test and never touch the disk.
func r28Unit(t *testing.T, base, src string) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse(base+".schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", base, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: base + ".schema", Name: base + ".schema", Base: base, Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", base, cerrs[0])
	}
	return u
}

// r28Nested is `depth` unions, each the arm of a type that is the arm of the
// union around it, the innermost holding Leaf. Every tag is one byte, so union
// U<i>'s tag sits at body byte 4+i and Leaf.n at 4+depth.
func r28Nested(depth int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package tblr28n%d\n\n", depth)
	b.WriteString("type Leaf\n{\n    n int32 = 0\n}\n\n")
	b.WriteString("type Alt\n{\n    k int8 = 0\n}\n\n")
	for i := depth - 1; i >= 0; i-- {
		if i == depth-1 {
			fmt.Fprintf(&b, "union U%d\n{\n    leaf Leaf\n    alt  Alt\n}\n\n", i)
		} else {
			fmt.Fprintf(&b, "union U%d\n{\n    w   W%d\n    alt Alt\n}\n\n", i, i+1)
		}
		if i > 0 {
			fmt.Fprintf(&b, "type W%d\n{\n    u U%d\n}\n\n", i, i)
		}
	}
	b.WriteString("fixed table Deep\n{\n    head int32 = 1\n    pick U0\n}\n\n")
	// a sibling the form IS carried for, so the library is written and the
	// refusal line has somewhere to stand
	b.WriteString("fixed table Shallow\n{\n    a int32 = 0\n}\n")
	return b.String()
}

// r28Plan is the identity plan of the fixed root `name`, walked whether or not
// the table is refused, so the chain rule can be asked of the plan itself.
func r28Plan(t *testing.T, u *ir.Unit, name string) []fixedPlanEntry {
	t.Helper()
	for _, st := range u.Tables {
		if st.Name == name {
			plan, _ := fixedIdentityPlan(st)
			return plan
		}
	}
	t.Fatalf("no table %s", name)
	return nil
}

func TestFixedGuardChainDart(t *testing.T) {
	t.Parallel()
	t.Run("dart/R28", func(t *testing.T) {
		t.Parallel()

		u := versionSchemaPath(t, filepath.Join("testdata", "FG3.schema"), "FG3")
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("R28: Generate(FG3): %v", err)
		}
		src := t02Source(t, files)

		// (a) THE STATIC PLAN: fourteen lanes an entry, the last two the pool
		// offset and count, and a pool of three-lane links beside the plan.
		for _, want := range []string{
			"static const int lanes = 14;",
			"static const int poolAt = 12;",
			"static const int poolCount = 13;",
			"static const int linkLanes = 3;",
			"static const int linkGuard = 0;",
			"static const int linkArg = 1;",
			"static const int linkArgW = 2;",
		} {
			if !strings.Contains(fixedRuntime, want) {
				t.Errorf("R28(a): fixedRuntime does not declare %q", want)
			}
		}
		rows := t02Rows(t, src, "final Int32List fkRootFixedIdentity")
		pool := t02Rows(t, src, "final Int32List fkRootFixedIdentityPool")
		for i, l := range pool {
			if len(l) != 3 {
				t.Fatalf("R28(a): fkRootFixedIdentityPool link %d has %d lanes, want 3", i, len(l))
			}
		}
		const (
			outerTagAt = 4
			midTagAt   = 5
			innerTagAt = 6
		)
		sawLeaf, sawOther, sawMidArm, sawOuterArm := false, false, false, false
		for idx, r := range rows {
			if len(r) != 14 {
				t.Fatalf("R28(a): fkRootFixedIdentity row %d has %d lanes, want 14", idx, len(r))
			}
			guard, arg, guard2, arg2 := r[5], r[6], r[9], r[10]
			at, count := r[12], r[13]
			switch guard {
			case fixedNoGuard:
				if guard2 != fixedNoGuard || at != 0 || count != 0 {
					t.Errorf("R28(a): unguarded row %d carries guard2=%d poolAt=%d poolCount=%d", idx, guard2, at, count)
				}
			case outerTagAt:
				sawOuterArm = true
				if guard2 != fixedNoGuard || count != 0 {
					t.Errorf("R28(a): Outer's arm row %d is one deep and carries guard2=%d poolCount=%d", idx, guard2, count)
				}
			case midTagAt:
				sawMidArm = true
				if guard2 != outerTagAt || arg2 != 1 || count != 0 || at != 0 {
					t.Errorf("R28(a): Mid's arm row %d is two deep: want guard2=4/arg2=1 and no pool, got guard2=%d arg2=%d poolAt=%d poolCount=%d", idx, guard2, arg2, at, count)
				}
			case innerTagAt:
				// THE THIRD TAG: inline lanes Inner then Mid, the pool Outer.
				if guard2 != midTagAt || arg2 != 1 || count != 1 {
					t.Fatalf("R28(a): Inner's arm row %d must carry guard=6, guard2=5/arg2=1 and one pooled link; got guard2=%d arg2=%d poolCount=%d", idx, guard2, arg2, count)
				}
				if at < 0 || int(at) >= len(pool) {
					t.Fatalf("R28(a): Inner's arm row %d names pool link %d of %d", idx, at, len(pool))
				}
				if l := pool[at]; l[0] != outerTagAt || l[1] != 1 || l[2] != 1 {
					t.Errorf("R28(a): Inner's arm row %d's pooled link is %v, want Outer's tag (4, 1, 1)", idx, l)
				}
				switch arg {
				case 1:
					sawLeaf = true
				case 2:
					sawOther = true
				}
			default:
				t.Errorf("R28(a): row %d has an unexpected guard %d", idx, guard)
			}
		}
		if !sawLeaf || !sawOther || !sawMidArm || !sawOuterArm {
			t.Errorf("R28(a): the plan misses an arm (leaf=%v other=%v mid=%v outer=%v)", sawLeaf, sawOther, sawMidArm, sawOuterArm)
		}

		// (a) OUTERMOST FIRST, read where it can be seen: four nested unions
		// put TWO links in the pool, and the run must read U0 then U1.
		u4 := r28Unit(t, "R28N4", r28Nested(4))
		files4, err := Generate(u4)
		if err != nil {
			t.Fatalf("R28: Generate(four deep): %v", err)
		}
		src4 := t02Source(t, files4)
		rows4 := t02Rows(t, src4, "final Int32List deepFixedIdentity")
		pool4 := t02Rows(t, src4, "final Int32List deepFixedIdentityPool")
		sawDeepLeaf := false
		for idx, r := range rows4 {
			if r[5] != 7 || r[6] != 1 {
				continue // not U3's leaf arm
			}
			sawDeepLeaf = true
			if r[9] != 6 || r[13] != 2 {
				t.Fatalf("R28(a): four deep, leaf row %d wants guard2=6 and two pooled links, got guard2=%d poolCount=%d", idx, r[9], r[13])
			}
			at := int(r[12])
			if at+2 > len(pool4) {
				t.Fatalf("R28(a): four deep, leaf row %d names links %d..%d of %d", idx, at, at+2, len(pool4))
			}
			if g0, g1 := pool4[at][0], pool4[at+1][0]; g0 != 4 || g1 != 5 {
				t.Errorf("R28(a): four deep, the pooled run reads guards %d then %d; OUTERMOST FIRST is 4 then 5", g0, g1)
			}
		}
		if !sawDeepLeaf {
			t.Error("R28(a): four deep, no row is U3's leaf arm")
		}

		// (c) THE CHAIN'S ONLY BOUND IS THE LAYOUT'S 64. FG3's chain is three,
		// well inside it; a chain of 65 is refused BY NAME at generate time.
		// Such a table is past the layout's own depth bound first, so the
		// emitted library names that refusal and lays no plan down; the chain
		// rule is the second fence, asked of the plan itself.
		if why := fixedPlanChainRefusal(r28Plan(t, u, "FkRoot")); why != "" {
			t.Errorf("R28(c): FG3's three-link chain is refused: %s", why)
		}
		deep := r28Unit(t, "R28N65", r28Nested(65))
		deepFiles, err := Generate(deep)
		if err != nil {
			t.Fatalf("R28(c): Generate(65 deep): %v", err)
		}
		deepSrc := t02Source(t, deepFiles)
		if !strings.Contains(deepSrc, "table Deep has NO FIXED FORM in Dart") {
			t.Error("R28(c): a 65-deep chain is not refused by name at generate time")
		}
		if strings.Contains(deepSrc, "deepFixedIdentity") {
			t.Error("R28(c): a 65-deep table still lays a plan down")
		}
		why := fixedPlanChainRefusal(r28Plan(t, deep, "Deep"))
		if !strings.Contains(why, "guard chain") || !strings.Contains(why, "65") || !strings.Contains(why, "64") {
			t.Errorf("R28(c): the chain rule does not name a 65-link chain past 64: %q", why)
		}
		if !strings.Contains(fixedRuntime, "count + 3 > TableFixedLimits.maxDepth") {
			t.Error("R28(c): the compiled walk's conjoin does not hold the chain to the layout's 64")
		}

		// (d) COUNT 0 IS THE TWO-LANE PLAN, UNCHANGED: FG1 (two nested unions)
		// lays down exactly the twelve lanes it did before the pool, and 0, 0.
		u1 := t12Schema(t, "FG1")
		files1, err := Generate(u1)
		if err != nil {
			t.Fatalf("R28(d): Generate(FG1): %v", err)
		}
		before := [][]int64{
			{0, 0, 0, 6, 0, -1, 0, 0, 1, -1, 0, 1},
			{0, 6, 6, 1, 0, 5, 1, 0, 1, -1, 0, 1},
			{0, 7, 7, 4, 0, 6, 1, 0, 1, 5, 1, 1},
			{0, 7, 7, 4, 0, 6, 2, 0, 1, 5, 1, 1},
			{0, 11, 11, 4, 0, 5, 1, 0, 1, -1, 0, 1},
			{0, 6, 6, 4, 0, 5, 2, 0, 1, -1, 0, 1},
		}
		rows1 := t02Rows(t, t02Source(t, files1), "final Int32List fhRootFixedIdentity")
		if len(rows1) != len(before) {
			t.Fatalf("R28(d): FG1's identity plan has %d rows, %d before the pool", len(rows1), len(before))
		}
		for i, r := range rows1 {
			if len(r) != 14 || !slices.Equal(r[:12], before[i]) || r[12] != 0 || r[13] != 0 {
				t.Errorf("R28(d): FG1 row %d is %v; before the pool it was %v, and its pool lanes must be 0, 0", i, r, before[i])
			}
		}

		// (b) and (d), RUN: identity and lineage-compiled plans both.
		r28Probe(t, u)
	})
}

// r28Probe is the functional half: the Dart SDK runs it, so it skips by name
// without one (dartBinary) and fails by name under SCHEMA_REQUIRE_CORPUS=1.
func r28Probe(t *testing.T, u *ir.Unit) {
	t.Helper()
	self, ok := FixedLineageOf(u, "FkRoot")
	if !ok {
		t.Fatal("R28: no lineage entry for FkRoot")
	}
	otherHash := self.Wire ^ 0x00ff00ff00ff00ff
	lineage := map[string][]FixedLineageEntry{
		"FkRoot": {{Wire: otherHash, Layout: self.Layout, Record: self.Record}},
	}
	pktFiles, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("R28: dart.Generate: %v", err)
	}
	tblFiles, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("R28: GenerateLineage: %v", err)
	}
	maps.Copy(pktFiles, tblFiles)
	codec, typefile := "", ""
	for name, data := range pktFiles {
		if strings.HasSuffix(name, "Fixed.dart") && bytes.Contains(data, []byte("fkRootFixedLoad")) {
			codec = name
		}
		if strings.HasSuffix(name, "Fixed.dart") && bytes.Contains(data, []byte("class FkRoot")) {
			typefile = name
		}
	}
	if codec == "" || typefile == "" {
		t.Fatalf("R28: missing codec/type file (codec=%q type=%q)", codec, typefile)
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

const int stain = 0x5A;

// One FkRoot whose three tags all select the arm that holds Leaf, saved, with
// tag byte 'forge' of the body (or none) set to the non-selecting value 2, and
// the record re-stamped with the other hash on the compiled path.
Uint8List record(int path, int forge, int otherLo, int otherHi) {
  final one = t.FkRoot();
  t.initFkRoot(one);
  one.head = 77;
  one.pick.type = 1;
  one.pick.a.rim = 66;
  one.pick.a.mid.type = 1;
  one.pick.a.mid.a.edge = 55;
  one.pick.a.mid.a.inner.type = 1;
  one.pick.a.mid.a.inner.leaf.n = 0x99332211;
  final wire = Uint8List(c.fkRootFixedMeasure(1));
  check(c.fkRootFixedSave(<t.FkRoot>[one], 1, wire) == wire.length, 'R28: the record saves');
  final bodyAt = c.fkRootFixedHeaderBytes + 8;
  check(wire[bodyAt + 4] == 1 && wire[bodyAt + 5] == 1 && wire[bodyAt + 6] == 1,
      'R28: the three tags sit at body bytes 4, 5 and 6');
  check(wire[bodyAt + 10] == 0x99, 'R28: Leaf.n high byte is body byte 10');
  if (forge >= 0) {
    wire[bodyAt + forge] = 2;
  }
  if (path == 1) {
    final wv = ByteData.sublistView(wire);
    setHash(wv, t.TableFixedLimits.hashAt, otherLo, otherHi);
    setHash(wv, c.fkRootFixedHeaderBytes, otherLo, otherHi);
  }
  return wire;
}

void main() {
  const otherLo = 0x%08x;
  const otherHi = 0x%08x;

  // (a), compiled: the plan compiled from a layout lays the SAME chain down —
  // Leaf's copy carries Inner then Mid inline and Outer as one pooled link, and
  // an entry two deep carries no pool at all (d).
  {
    final cplan = c.fkRootFixedNewPlan();
    final crep = t.TableFixedReport();
    check(
      cplan.theirs.parse(
        ByteData.sublistView(c.fkRootFixedLayout),
        0,
        c.fkRootFixedLayout.length,
      ),
      'R28(a): fkRootFixedLayout parses',
    );
    final made = t.TableFixedCompiler.compile(
      cplan,
      c.fkRootFixedLayout,
      ByteData.sublistView(c.fkRootFixedLayout),
      c.fkRootFixedDst,
      c.fkRootFixedCover,
      c.fkRootFixedCoverCount,
      crep,
    );
    check(made > 0, 'R28(a): compile produced $made entries');
    var sawLeaf = false;
    var sawMid = false;
    for (var i = 0; i < made; i++) {
      final b = i * t.TableFixedLane.lanes;
      final e = cplan.entries;
      final op = e[b + t.TableFixedLane.op];
      final guard = e[b + t.TableFixedLane.guard];
      final guard2 = e[b + t.TableFixedLane.guard2];
      final at = e[b + t.TableFixedLane.poolAt];
      final count = e[b + t.TableFixedLane.poolCount];
      if (guard == 5) {
        sawMid = true;
        check(guard2 == 4 && count == 0,
            'R28(d): a compiled entry two deep carries guard2=4 and no pool, got guard2=$guard2 poolCount=$count');
      }
      if (op == t.TableFixedOp.copy &&
          e[b + t.TableFixedLane.src] == 7 &&
          e[b + t.TableFixedLane.size] == 4) {
        sawLeaf = true;
        final l = at * t.TableFixedLane.linkLanes;
        check(
          guard == 6 &&
              e[b + t.TableFixedLane.arg] == 1 &&
              guard2 == 5 &&
              e[b + t.TableFixedLane.arg2] == 1 &&
              count == 1 &&
              at + count <= cplan.poolUsed &&
              cplan.pool[l + t.TableFixedLane.linkGuard] == 4 &&
              cplan.pool[l + t.TableFixedLane.linkArg] == 1 &&
              cplan.pool[l + t.TableFixedLane.linkArgW] == 1,
          'R28(a): the compiled Leaf copy must carry guard=6, guard2=5 and the pooled link (4, 1, 1); '
          'got guard=$guard guard2=$guard2 poolAt=$at poolCount=$count',
        );
      }
    }
    check(sawLeaf, 'R28(a): the compiled plan carries the Leaf copy');
    check(sawMid, 'R28(a): the compiled plan carries an entry under Mid');
  }

  for (var path = 0; path < 2; path++) {
    final label = path == 0 ? 'identity' : 'compiled';
    // forge -1 is every tag holding; 4, 5 and 6 forge Outer, Mid and Inner.
    for (final forge in const <int>[-1, 4, 5, 6]) {
      final wire = record(path, forge, otherLo, otherHi);
      final plan = c.fkRootFixedNewPlan();
      plan.image.fillRange(0, plan.image.length, stain);
      final back = <t.FkRoot>[t.FkRoot()];
      t.initFkRoot(back[0]);
      back[0].pick.a.mid.a.inner.leaf.n = 0x5A5A5A5A;
      final rep = t.TableFixedReport();
      final n = c.fkRootFixedLoad(back, 1, wire, wire.length, plan, rep);
      check(
        n == 1 && rep.refused == 0 && !rep.malformed && rep.clamped == 0,
        'R28($label, forge $forge): the record loads cleanly (n=$n refused=${rep.refused} malformed=${rep.malformed} clamped=${rep.clamped})',
      );
      if (forge < 0) {
        // ALL THREE TAGS HOLD: Leaf's entry runs and the value lands whole.
        check(
          plan.image[10] == 0x99 &&
              back[0].pick.type == 1 &&
              back[0].pick.a.mid.type == 1 &&
              back[0].pick.a.mid.a.inner.type == 1 &&
              (back[0].pick.a.mid.a.inner.leaf.n & 0xffffffff) == 0x99332211 &&
              back[0].pick.a.mid.a.edge == 55 &&
              back[0].pick.a.rim == 66 &&
              back[0].head == 77,
          'R28($label)(b): with every tag holding, Leaf lands whole (image[10]=0x${plan.image[10].toRadixString(16)})',
        );
      } else {
        // ONE TAG NAMES ITS OTHER ARM: Leaf's entry must not run, so the one
        // byte only it writes stays as the caller stained it.
        check(
          plan.image[10] == stain,
          'R28($label)(b): with tag byte $forge forged to 2, Leaf ran anyway and wrote image[10]=0x${plan.image[10].toRadixString(16)}',
        );
        check(
          back[0].pick.a.mid.a.inner.leaf.n == 0x5A5A5A5A,
          'R28($label)(b): with tag byte $forge forged, the caller Leaf storage was touched',
        );
      }
    }
  }

  if (exitCode == 0) {
    stdout.writeln('ok');
  }
}
`, "./"+codec, "./"+typefile, uint32(otherHash), uint32(otherHash>>32))

	out := t12RunProbe(t, dir, "r28_probe.dart", probe)
	if !strings.Contains(out, "ok") {
		t.Fatalf("R28: probe did not print ok:\n%s", out)
	}
}
