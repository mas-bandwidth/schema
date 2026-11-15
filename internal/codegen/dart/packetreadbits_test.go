package dart

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// packetReadBitsSchema has two packet types whose wire sizes are not whole
// bytes, so a read that reported bytes, or rounded, would miss: Pair is a
// fixed 11 bits (count 7, flag 1, level 3) and Path is 2 bits of count then 3
// bits per cell then 1 bit of done — 9 bits with two cells.
const packetReadBitsSchema = `package framing

type Pair
{
    count int32 | min = 0, max = 100
    flag bool
    level int32 | min = -3, max = 3
}

type Cell
{
    v int32 | min = 0, max = 7
}

type Path
{
    cells [..3]Cell
    done bool
}
`

// packetReadBitsDriver writes each type, reads it back through the framing
// entry and checks the bits consumed against the wire sizes above; then lays
// Pair, Path and Pair back to back at bit offsets 0, 11 and 20 and reads them
// in turn; then refuses a count above its range and a truncated packet.
const packetReadBitsDriver = `import 'dart:io';
import 'dart:typed_data';
import 'Framing.dart';

void fail(String why) {
  stderr.writeln(why);
  exit(1);
}

// copyBits appends the first count bits of src to dst at bit offset at.
void copyBits(ByteData src, int count, ByteData dst, int at) {
  for (var i = 0; i < count; i++) {
    final bit = (src.getUint8(i >>> 3) >>> (i & 7)) & 1;
    final o = at + i;
    dst.setUint8(o >>> 3, dst.getUint8(o >>> 3) | (bit << (o & 7)));
  }
}

void main() {
  final pair = Pair()
    ..count = 77
    ..flag = true
    ..level = -2;
  final pairBuf = ByteData(pairMaxBytes);
  final pairBytes = writePair(pair, pairBuf);

  final path = Path();
  path.cells[0].v = 5;
  path.cells[1].v = 2;
  path.cellsCount = 2;
  path.done = true;
  final pathBuf = ByteData(pathMaxBytes);
  final pathBytes = writePath(path, pathBuf);

  final p1 = Pair();
  final pairBits = readPairBits(p1, pairBuf, pairBytes * 8, 0);
  if (pairBits != 11) {
    fail('Pair: bits consumed $pairBits, want 11');
  }
  if (p1.count != 77 || !p1.flag || p1.level != -2) {
    fail('Pair: wrong values');
  }
  final q1 = Path();
  final pathBits = readPathBits(q1, pathBuf, pathBytes * 8, 0);
  if (pathBits != 9) {
    fail('Path: bits consumed $pathBits, want 9');
  }
  if (q1.cellsCount != 2 || q1.cells[0].v != 5 || q1.cells[1].v != 2 || !q1.done) {
    fail('Path: wrong values');
  }

  // back to back: Pair at 0, Path at 11, Pair at 20; 31 bits in all
  final joined = ByteData(8);
  copyBits(pairBuf, 11, joined, 0);
  copyBits(pathBuf, 9, joined, 11);
  copyBits(pairBuf, 11, joined, 20);
  final total = 31;
  var at = 0;
  final a = Pair();
  var n = readPairBits(a, joined, total, at);
  if (n != 11) {
    fail('back to back: first Pair consumed $n, want 11');
  }
  at += n;
  final b = Path();
  n = readPathBits(b, joined, total, at);
  if (n != 9) {
    fail('back to back: Path consumed $n, want 9');
  }
  at += n;
  final c = Pair();
  n = readPairBits(c, joined, total, at);
  if (n != 11) {
    fail('back to back: second Pair consumed $n, want 11');
  }
  at += n;
  if (at != total) {
    fail('back to back: ended at $at, want $total');
  }
  if (a.count != 77 || !a.flag || a.level != -2) {
    fail('back to back: first Pair wrong values');
  }
  if (b.cellsCount != 2 || b.cells[0].v != 5 || b.cells[1].v != 2 || !b.done) {
    fail('back to back: Path wrong values');
  }
  if (c.count != 77 || !c.flag || c.level != -2) {
    fail('back to back: second Pair wrong values');
  }

  // damaged: count 127 is above max = 100 (the low 7 bits of byte 0)
  final bad = ByteData(pairBytes);
  for (var i = 0; i < pairBytes; i++) {
    bad.setUint8(i, pairBuf.getUint8(i));
  }
  bad.setUint8(0, bad.getUint8(0) | 0x7f);
  if (readPairBits(Pair(), bad, pairBytes * 8, 0) != -1) {
    fail('damaged: a count above its range was not refused');
  }
  if (readPair(Pair(), bad, pairBytes * 8)) {
    fail('damaged: readPair accepted what readPairBits refused');
  }
  // damaged: the packet ends one bit short of the third Pair
  if (readPairBits(Pair(), joined, total - 1, 20) != -1) {
    fail('damaged: a truncated packet was not refused');
  }
  // the start must lie inside the bits the caller vouches for
  if (readPairBits(Pair(), joined, total, total + 1) != -1) {
    fail('an offset past numBits was not refused');
  }
  if (readPairBits(Pair(), joined, total, -1) != -1) {
    fail('a negative offset was not refused');
  }
  stdout.writeln('ok');
}
`

// TestDartPacketReadReportsBitsConsumed holds SPEC §5 and §6.3 for the Dart
// leg: every packet read reports the bits it consumed. read<T>Bits answers
// exactly the bits the writer wrote for two packet types, reads several
// objects in turn from one buffer by bit offset (it is the framing entry and
// runs no end-of-packet check), and refuses a damaged packet as read<T> does.
func TestDartPacketReadReportsBitsConsumed(t *testing.T) {
	t.Parallel()
	f, perrs := parser.Parse("Framing.schema", []byte(packetReadBitsSchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Framing.schema", Name: "Framing.schema", Base: "Framing",
		Bytes: []byte(packetReadBitsSchema), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	slowtest.Gate(t, "the Dart toolchain (it compiles and runs the generated unit)")
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.dart"), []byte(packetReadBitsDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("dart", "--enable-asserts", "main.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Dart framing read: %v\n%s", err, out)
	}
}
