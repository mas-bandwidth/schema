package darttable

// THE BLOCK FILL PATH ON THE DART LEG (docs/SPEC-TABLES.md §19.1): `begin`
// writes the prologue and the triples and NOTHING else, and its image is the
// reference's — the same three prologue words at the same offsets, each
// triple's offset_of rounded up to the array's own alignment, its count and
// its pitch, and the used extent rounded to §19.1's 64 bytes.
//
// THE IMAGE IS CHECKED TWICE, from two directions that share no code with the
// emitter: against §19.1's layout facts recomputed HERE out of ir.Blocks — the
// same facts the reference leg's builder (internal/codegen/gotable/blockbuild.go)
// is emitted from — and against the leg's OWN reader, `open`, which refuses an
// image whose triples or extent it disagrees with. A refusal is named: the
// array, the count and the declared maximum, through TableBlockRefusal, and a
// refused begin leaves the caller's bytes untouched.
//
// THE COOK WRITE HALF IS NOT HERE YET. Cook and CookMeasure in the generated
// cook runtime, byte-identical to `schema cook` in both byte orders and with
// the reference leg's TableRefuse* name for a write past the measured size, is
// the second half of this card and is reported not done.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const blockBuildSchema = `package probe
fixed table Row { value uint64
 flag bool }
fixed table Root { stamp uint64
 rows [..65]Row
 other [..3]Row }
`

const blockBuildProbe = `import 'dart:io';
import 'dart:typed_data';

import './ProbeBlock.dart' as b;

void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    exitCode = 1;
  }
}

String image(Uint8List bytes, int length) {
  final out = StringBuffer();
  for (final x in Uint8List.sublistView(bytes, 0, length)) {
    out.write(x.toRadixString(16).padLeft(2, '0'));
  }
  return out.toString();
}

void main() {
  final bytes = Uint8List(b.RootBlock.blockMaxBytes);
  bytes.fillRange(0, bytes.length, 0xa5);
  final refusal = b.TableBlockRefusal();
  final block = b.RootBlock.begin(bytes, 0, rows: 17, other: 2, refusal: refusal);
  check(block != null, 'begin refused a count inside every declared maximum');
  if (block == null) {
    return;
  }
  check(refusal.empty, 'a clean begin named a refusal: ${refusal.array}');
  stdout.writeln('extent ${block.length}');
  stdout.writeln('triples ${block.rowsOffsetOf} ${block.rowsCount} ${block.rowsPitch} '
      '${block.otherOffsetOf} ${block.otherCount} ${block.otherPitch}');
  stdout.writeln('image ${image(bytes, block.length)}');
  // THE ROWS ARE THE CALLER'S: begin touched neither a row nor the padding.
  check(bytes[b.RootBlock.projectionSize] == 0xa5,
      'begin wrote a projection field');
  check(bytes[block.rowsOffsetOf] == 0xa5, 'begin wrote a row byte');

  // THE MAXIMUM REFUSAL, BY NAME, AND IT WRITES NOTHING (§19.1).
  final before = Uint8List.fromList(bytes);
  check(
    b.RootBlock.begin(bytes, 0, rows: 66, other: 2, refusal: refusal) == null &&
        refusal.array == 'rows' &&
        refusal.count == 66 &&
        refusal.maximum == 65,
    'the maximum refusal: ${refusal.array} ${refusal.count} ${refusal.maximum}',
  );
  check(
    b.RootBlock.begin(bytes, 0, rows: 17, other: -1, refusal: refusal) == null &&
        refusal.array == 'other' && refusal.count == -1,
    'the negative count refusal: ${refusal.array} ${refusal.count}',
  );
  check(bytes.equalsAll(before), 'a refused begin changed the caller bytes');

  // THE LEG'S OWN READER agrees with what the fill path wrote: open refuses an
  // image whose triples, pitch, count or extent it disagrees with.
  final opened = b.RootBlock.open(bytes, 0, block.length);
  check(opened != null, 'open refused the image begin wrote');
  if (opened != null) {
    check(opened.length == block.length,
        'open measured ${opened.length}, begin measured ${block.length}');
    check(opened.rowsOffsetOf == block.rowsOffsetOf &&
        opened.rowsCount == 17 && opened.rowsPitch == b.RootBlock.rowsStride,
        'open read back a triple begin did not write');
    final row = opened.rowsAt(3, opened.rowsCursor());
    check(row.value == 0xa5a5a5a5a5a5a5a5, 'a row begin left alone moved');
  }
  if (exitCode == 0) {
    stdout.writeln('ok');
  }
}

extension on Uint8List {
  bool equalsAll(Uint8List other) {
    for (var i = 0; i < length; i++) {
      if (this[i] != other[i]) {
        return false;
      }
    }
    return true;
  }
}
`

// TestBlockBuildAndCookWriteDart pins the Dart block builder's image against
// §19.1's layout facts, recomputed here out of the same IR the reference leg's
// builder is emitted from, and against the leg's own reader.
func TestBlockBuildAndCookWriteDart(t *testing.T) {
	t.Parallel()

	u := unitFrom(t, blockBuildSchema)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate tables: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// NOT `probe.dart`: this filesystem is case-insensitive and the packet
	// library is `Probe.dart`, so the two names are one file.
	if err := os.WriteFile(filepath.Join(dir, "blockbuild_probe.dart"),
		[]byte(blockBuildProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "blockbuild_probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the block fill probe did not run green: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "ok") {
		t.Fatalf("the block fill probe printed no ok:\n%s", text)
	}

	// THE IMAGE, recomputed from §19.1's facts over the same 0xa5-filled
	// extent the probe began with (docs/SPEC-TABLES.md §19.1).
	bl := ir.Blocks(u).Block("Root")
	if bl == nil {
		t.Fatal("Root has no block form")
	}
	counts := map[string]int64{"rows": 17, "other": 2}
	want := bytes.Repeat([]byte{0xa5}, int(bl.MaxBytes))
	binary.LittleEndian.PutUint64(want[0:], 0x4b4c42414d484353)
	binary.LittleEndian.PutUint64(want[8:], ir.BuildVersion(u))
	binary.LittleEndian.PutUint64(want[16:], 1)
	offset := bl.Projection.Size
	for _, a := range bl.Arrays {
		alignment := int64(ir.BlockAlign)
		if a.ElemAlign() > alignment {
			alignment = a.ElemAlign()
		}
		offset = (offset + alignment - 1) &^ (alignment - 1)
		count := counts[a.Field.Name]
		binary.LittleEndian.PutUint64(want[a.OffsetOfOffset:], uint64(offset))
		binary.LittleEndian.PutUint32(want[a.CountOffset:], uint32(count))
		binary.LittleEndian.PutUint32(want[a.StrideOffset:], uint32(a.Stride))
		offset += count * a.Stride
	}
	used := (offset + 63) &^ 63
	if got := fieldOf(t, text, "extent"); got != fmt.Sprintf("%d", used) {
		t.Fatalf("begin measured %s bytes, §19.1's layout says %d", got, used)
	}
	got, err := hex.DecodeString(fieldOf(t, text, "image"))
	if err != nil {
		t.Fatalf("the probe's image is not hex: %v\n%s", err, text)
	}
	if !bytes.Equal(got, want[:used]) {
		t.Fatalf("the Dart builder's image is not §19.1's:\n got %x\nwant %x", got, want[:used])
	}
}

// fieldOf reads one `name value` line out of the probe's output.
func fieldOf(t *testing.T, text, name string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(line, name+" "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("the probe printed no %s line:\n%s", name, text)
	return ""
}
