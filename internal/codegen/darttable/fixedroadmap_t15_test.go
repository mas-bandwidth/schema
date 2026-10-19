package darttable

// TestFixedRoadmapDartT15GuardChain is the dart leg's row of the card
// the-guard-chain-answers-the-outer-tag: docs/roadmap.sexp dart/W16 ("nested
// union answers outer tag") and dart/R28 ("the guard chain: the plan entry
// carries offset+count into a pool of (guard, arg, argw) links, outermost
// first, the only bound the layout's 64; an arm inside an arm answers to the
// OUTER tag"). Law: docs/FIXED-FORM-ALGORITHM.md §4.1.
//
// THE FIXTURE IS THE FIVE LEGS' ONE (t15GuardChainSchema, the same text in
// gotable, cstable, javatable and jstable): a union whose arm is a union whose
// arm is a union whose arm is a table, three tags deep on one path. The
// generated Dart runs under the leg's own dart: the deepest leaf must land as
// written and must NOT land while any one of the three tags names another arm,
// on the IDENTITY plan and on the COMPILED plan a lineage entry selects — the
// same layout under another hash, so nothing but the plan moves. The OUTERMOST
// poison is the clause only the chain carries: the guard lane holds the
// innermost tag, so a plan without the pool's links lands the leaf there.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// t15GuardChainSchema is THE fixture of the five legs' T15GuardChain tests.
// The record body is [0] Outer's tag, [1] Middle's, [2] Inner's, [3..7) x.
const t15GuardChainSchema = `package probe

type LeafA { x int32 = 0 }
type LeafB { y int32 = 0 }
union Inner
{
    a LeafA
    b LeafB
}
union Middle
{
    i Inner
    n LeafB
}
union Outer
{
    m Middle
    p LeafB
}
fixed table Root
{
    o Outer
    mid Middle
    inn Inner
}
`

// t15OtherHash masks the lineage entry's hash: this build's own layout under a
// hash that is not this build's, so a file carrying it reads through the plan
// COMPILED from the lock's bytes (§4.2).
const t15OtherHash = 0x00ff00ff00ff00ff

func TestFixedRoadmapDartT15GuardChain(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Dart SDK (it runs the generated library)")
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"dart/W16", func(t *testing.T) { t15RunDart(t, false) }},
		{"dart/R28", func(t *testing.T) { t15RunDart(t, true) }},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// t15RunDart generates the packet library and the fixed library with a lineage
// of the same layout under another hash and runs the probe on both plans,
// clean and with each tag poisoned. chain also holds the runtime to the chain's
// shape: two lanes of offset+count into the plan's pool, laid outermost first.
func t15RunDart(t *testing.T, chain bool) {
	t.Helper()
	dartBin := t08DartBinary(t)
	if chain {
		for _, want := range []string{
			"static const int guards = 9;",
			"static const int guardCount = 10;",
			"e[b + TableFixedLane.guards] = plan.chainAt;",
			"e[b + TableFixedLane.guardCount] = plan.chainLen;",
		} {
			if !strings.Contains(fixedRuntime, want) {
				t.Fatalf("R28: the dart runtime does not carry the guard chain's offset+count: no %q", want)
			}
		}
	}
	u := unitFrom(t, t15GuardChainSchema)
	self, ok := FixedLineageOf(u, "Root")
	if !ok {
		t.Fatal("no lineage entry for Root")
	}
	other := self.Wire ^ t15OtherHash
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"Root": {{Wire: other, Layout: self.Layout, Record: self.Record}}})
	if err != nil {
		t.Fatalf("generate fixed: %v", err)
	}
	pkt, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	maps.Copy(files, pkt)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var hash [8]byte
	binary.LittleEndian.PutUint64(hash[:], other)
	var hb []string
	for _, b := range hash {
		hb = append(hb, fmt.Sprint(b))
	}
	probe := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';

import './ProbeFixed.dart' as t;

void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    exitCode = 1;
  }
}

void main() {
  final v = t.Root();
  t.initRoot(v);
  v.o.type = 1;
  v.o.m.type = 1;
  v.o.m.i.type = 1;
  v.o.m.i.a.x = 42;
  final bytes = Uint8List(t.rootFixedMeasure(1));
  check(t.rootFixedSave([v], 1, bytes) == bytes.length, 'the save');
  final rec = bytes.length - t.rootFixedRecordBytes + 8;
  check(bytes[rec] == 1 && bytes[rec + 1] == 1 && bytes[rec + 2] == 1,
      'the writer did not put the three tags where the chain says');
  const other = <int>[%s];
  for (var path = 0; path < 2; path++) {
    for (var poison = 0; poison <= 3; poison++) {
      final file = Uint8List.fromList(bytes);
      if (path == 1) {
        for (var k = 0; k < 8; k++) {
          file[8 + k] = other[k];
          file[rec - 8 + k] = other[k];
        }
      }
      if (poison > 0) {
        file[rec + poison - 1] = 2;
      }
      final out = [t.Root()];
      t.initRoot(out[0]);
      final report = t.TableFixedReport();
      final n = t.rootFixedLoad(out, 1, file, file.length, t.rootFixedNewPlan(), report);
      final which = path == 0 ? 'identity' : 'compiled';
      if (n != 1 || report.refused != 0 || report.malformed) {
        check(false, '$which poison $poison: n=$n refused=${report.refused}');
        continue;
      }
      final x = out[0].o.m.i.a.x;
      check(poison != 0 || x == 42, '$which: the record as written did not land its deepest leaf');
      check(poison == 0 || x != 42,
          '$which: the deepest leaf landed while tag ${poison - 1} names another arm');
    }
  }
  if (exitCode == 0) {
    stdout.writeln('PROBE OK');
  }
}
`, strings.Join(hb, ", "))
	if err := os.WriteFile(filepath.Join(dir, "main_probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBin, "--enable-asserts", "main_probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PROBE OK") {
		t.Fatalf("the driven reader failed (%v):\n%s", err, out)
	}
}
