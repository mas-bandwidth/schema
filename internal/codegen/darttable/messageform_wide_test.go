package darttable

// THE MESSAGE FORM'S 128-BIT SCALARS ON THE DART LEG (docs/SPEC-TABLES.md §3.3).
//
// A 128-bit value crosses the message form's bit lane as its LOW SIXTY-FOUR
// BITS AND THEN THE REST, and a ranged one crosses as its offset from the
// declared base taken at the full 128-bit width (the reference's
// tableMessageWriteScalar lo/hi pair). The Dart leg's TableBitWriter.put and
// TableBitReader.get carry at most 64 bits in an int, so the emitter used to
// print `w.put(value.wideKey, 128)` and `value.flux = r.bits.get(102)`, which do
// not compile, and it printed a base past int64 as `0`. That was the
// generated/bench/paired/dart red that kept the Dart bench runner from
// building (BenchMixed.wide_key uint128, BenchMixed.flux int128 | min = -2^100).
//
// No row of testdata/conformance/tables/MANIFEST.txt carries a 128-bit field,
// so the C++ reference's bytes are produced here: the same schema is generated
// into C++ by internal/codegen/cpptable, compiled with c++ against the sibling
// serialize runtime, and the probe prints its announcement and its message
// bytes. The Dart probe then (1) emits the same announcement, (2) reads the C++
// announcement, (3) LoadMessages the C++ bytes and gets every value back
// exactly, and (4) SaveMessages and MeasureMessages reproduce the C++ bytes. A
// missing toolchain or sibling is a skip that names itself, never a green; the
// text clause at the end needs no toolchain and was red at BASE everywhere.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
)

// wideSchema has every 128-bit message-form path: a bare uint128 (128 bits,
// both lanes full), an int128 ranged over +-2^126 (127 bits, a negative value,
// a base whose high half is 0xc000...), the bench's own ranged int128
// (102 bits, its base -2^100 past int64, the value's offset crossing into the
// high lane), and a ranged int128 whose base 2^65 is past uint64 but whose
// span fits ten bits (the low lane only). A uint128 takes no range (SPEC §4.3).
//
// Carrier is a packet type with 128-bit storage, as the paired bench unit has
// (Bench.schema): internal/codegen/dart writes Int128.dart only when a PACKET
// declaration needs 128 bits (unitNeeds128), so a unit whose only 128-bit
// fields are in fixed tables imports an Int128.dart nothing emits. That gap is
// the packet emitter's and outside this test's package; Carrier keeps the probe
// on the shape the bench unit really has.
const wideSchema = `package wideprobe

type Carrier
{
    k uint128
}

fixed table Wide
{
    bare uint128
    neg  int128  | min = -85070591730234615865843651857942052864, max = 85070591730234615865843651857942052864
    flux int128  | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376
    high int128 = 36893488147419103232 | min = 36893488147419103232, max = 36893488147419104232
    tag  int32
}
`

// wideValue is one field's value as its two's-complement 128-bit halves.
type wideValue struct {
	field  string
	signed bool
	hi, lo uint64
}

func wideHalves(t *testing.T, field string, signed bool, dec string) wideValue {
	t.Helper()
	v, ok := new(big.Int).SetString(dec, 10)
	if !ok {
		t.Fatalf("bad literal %s", dec)
	}
	v.Mod(v, new(big.Int).Lsh(big.NewInt(1), 128))
	lo := new(big.Int).And(v, new(big.Int).SetUint64(^uint64(0))).Uint64()
	hi := new(big.Int).Rsh(v, 64).Uint64()
	return wideValue{field: field, signed: signed, hi: hi, lo: lo}
}

func wideValues(t *testing.T) []wideValue {
	t.Helper()
	return []wideValue{
		// hi 0x0123456789abcdef, lo 0xfedcba9876543210
		wideHalves(t, "bare", false, "1512366075204170929049582354406559215"),
		// -(2^100) - 12345
		wideHalves(t, "neg", true, "-1267650600228229401496703217721"),
		// -(2^100) + 2^64 + 5: the offset 2^64 + 5 needs the high lane
		wideHalves(t, "flux", true, "-1267650600209782657423011653627"),
		// 2^65 + 777: base 2^65, offset 777 in ten bits
		wideHalves(t, "high", true, "36893488147419104009"),
	}
}

// cppWideReference generates wideSchema into C++, compiles a probe that fills
// one Wide with values and prints the announcement and the one-body batch as
// hex, and returns both.
func cppWideReference(t *testing.T, values []wideValue) (announce, message []byte) {
	t.Helper()
	ser, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ser, "serialize.h")); err != nil {
		t.Skipf("named as not run: the sibling serialize runtime is not staged at %s, so the C++ reference's 128-bit storage type does not resolve", ser)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		t.Skipf("named as not run: no C++ compiler (%s) for the reference", cxx)
	}
	u := unitFrom(t, wideSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("generate C++ packet: %v", err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatalf("generate C++ table: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	header := ""
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("cpptable generated no Table.h")
	}
	var set strings.Builder
	for _, v := range values {
		fmt.Fprintf(&set, "    v.%s = decltype( v.%s )( ( (unsigned __int128) 0x%016xull << 64 ) | (unsigned __int128) 0x%016xull );\n",
			v.field, v.field, v.hi, v.lo)
	}
	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdint.h>
#include "%s"

using namespace wideprobe;

static void hexline( const char * tag, const uint8_t * p, int64_t n )
{
    printf( "%%s ", tag );
    for ( int64_t i = 0; i < n; i++ ) printf( "%%02x", p[i] );
    printf( "\n" );
}

int main( void )
{
    static uint8_t buf[1 << 16];
    Wide v;
%s    v.tag = -7;
    TableReport report{};
    int64_t measure = WideMeasureMessages( &v, 1, &report );
    int64_t wrote = WideSaveMessages( &v, 1, buf, (int64_t) sizeof( buf ), &report );
    if ( measure <= 0 || wrote != measure ) { printf( "save measure=%%lld wrote=%%lld\n", (long long) measure, (long long) wrote ); return 1; }
    hexline( "message", buf, wrote );
    int64_t a = Announce( buf, (int64_t) sizeof( buf ) );
    if ( a != AnnounceMeasure() ) { printf( "announce %%lld\n", (long long) a ); return 1; }
    hexline( "announce", buf, a );
    return 0;
}
`, header, set.String())
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	if out, err := exec.Command(cxx, "-std=c++17", "-O1", "-I"+dir, "-I"+ser, filepath.Join(dir, "probe.cpp"), "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("the C++ reference probe did not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the C++ reference probe failed: %v\n%s", err, out)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		tag, hx, _ := strings.Cut(line, " ")
		b, err := hex.DecodeString(hx)
		if err != nil {
			t.Fatalf("the C++ reference printed %q: %v", line, err)
		}
		switch tag {
		case "message":
			message = b
		case "announce":
			announce = b
		}
	}
	if message == nil || announce == nil {
		t.Fatalf("the C++ reference printed no message or no announcement:\n%s", out)
	}
	return announce, message
}

func dartWideLiteral(v wideValue) string {
	cls := "UInt128"
	if v.signed {
		cls = "Int128"
	}
	return fmt.Sprintf("%s(0x%016x, 0x%016x)", cls, v.hi, v.lo)
}

// TestMessageFormWideScalarsMatchTheCppReferenceDart pins the 128-bit
// message-form bytes against the C++ reference, both directions.
func TestMessageFormWideScalarsMatchTheCppReferenceDart(t *testing.T) {
	t.Parallel()

	values := wideValues(t)

	// "A 128-bit value crosses the bit lane as its low sixty-four bits and then
	// the rest": the C++ reference's bytes, read and written by Dart.
	t.Run("bytes_match_cpp", func(t *testing.T) {
		t.Parallel()
		dartBin := announceDartBinary(t)
		announce, message := cppWideReference(t, values)

		u := unitFrom(t, wideSchema)
		files, err := dart.Generate(u)
		if err != nil {
			t.Fatalf("generate packet: %v", err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("generate table: %v", err)
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
		if _, ok := files["Int128.dart"]; !ok {
			t.Fatal("the Dart leg emitted no Int128.dart for a unit with 128-bit fields")
		}
		var set, cmp strings.Builder
		for _, v := range values {
			fmt.Fprintf(&set, "  want.%s = %s;\n", v.field, dartWideLiteral(v))
			fmt.Fprintf(&cmp, "  check(got.%s == %s, '%s: ${got.%s.hi.toRadixString(16)}:${got.%s.lo.toRadixString(16)}');\n",
				v.field, dartWideLiteral(v), v.field, v.field, v.field)
		}
		home := runtimeHome(u) + "Fixed.dart"
		rootLib := fixedTableBase(u, "Wide") + "Fixed.dart"
		probe := fmt.Sprintf(`import 'dart:typed_data';

import './Int128.dart';
import './%s' as h;
import './%s' as r;

void check(bool ok, String what) {
  if (!ok) {
    throw StateError(what);
  }
}

bool same(Uint8List a, Uint8List b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

void main() {
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  // (1) the announcement is the reference's, byte for byte.
  check(same(h.tableAnnouncement, ann), 'the Dart announcement differs from the C++ reference');
  // (2) the reference's announcement reads.
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep), 'the C++ announcement is refused: ' + annRep.reason);
  // (3) LoadMessages of the reference's bytes gives every 128-bit value back.
  final got = h.Wide();
  h.initWide(got);
  final loadRep = h.TableReport();
  final count = r.wideLoadMessages(<h.Wide>[got], msg, vocab, loadRep);
  check(count == 1 && !loadRep.refused && !loadRep.malformed,
      'LoadMessages count=$count refused=${loadRep.refused} malformed=${loadRep.malformed} reason=${loadRep.reason}');
%s  check(got.tag == -7, 'tag: ${got.tag}');
  // (4) SaveMessages and MeasureMessages reproduce the reference's bytes.
  final want = h.Wide();
  h.initWide(want);
%s  want.tag = -7;
  final values = <h.Wide>[want];
  final measure = r.wideMeasureMessages(values, h.TableReport());
  check(measure == msg.length, 'MeasureMessages=$measure want=${msg.length}');
  final buffer = Uint8List(measure);
  final wrote = r.wideSaveMessages(values, buffer, h.TableReport());
  check(wrote == msg.length, 'SaveMessages=$wrote want=${msg.length}');
  check(same(buffer, msg), 'the 128-bit message bytes differ from the C++ reference: $buffer want $msg');
  print('ok');
}
`, home, rootLib, dartBytes(announce), dartBytes(message), cmp.String(), set.String())
		if err := os.WriteFile(filepath.Join(dir, "wide_probe.dart"), []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dartBin, "--enable-asserts", "wide_probe.dart")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil || !bytes.Contains(out, []byte("ok")) {
			t.Fatalf("the Dart leg's 128-bit message form does not match the C++ reference: %v\n%s", err, out)
		}
	})

	// The red at BASE, toolchain or not: a 128-bit value never goes through
	// the int-only 64-bit bit lane in one call, and a base past int64 is never
	// printed as 0.
	t.Run("never_the_64_bit_lane", func(t *testing.T) {
		t.Parallel()
		files, err := Generate(unitFrom(t, wideSchema))
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		var all strings.Builder
		for _, data := range files {
			all.Write(data)
		}
		src := all.String()
		for _, bad := range []string{
			"w.put(value.bare, 128)", "w.put(value.neg - 0, 128)",
			"w.put(value.flux - 0, 102)", "w.put(value.flux, 102)",
			"value.bare = r.bits.get(128)", "value.neg = r.bits.get(128)",
			"value.flux = r.bits.get(102)", "value.flux += 0",
			"value.high = r.bits.get(10)",
		} {
			if strings.Contains(src, bad) {
				t.Fatalf("the message form hands a 128-bit value to the 64-bit bit lane: %q (docs/SPEC-TABLES.md §3.3)", bad)
			}
		}
		for _, want := range []string{
			"w.put(v.lo, 64);", "w.put(v.hi, 64);", "w.put(v.hi, 38);", "w.put(v.lo, 10);",
			"final lo = r.bits.get(10);",
			"(value.neg - Int128(0xc000000000000000, 0))",
			"value.neg = Int128(hi, lo) + Int128(0xc000000000000000, 0);",
			"(value.flux - Int128(0xfffffff000000000, 0))",
			"value.flux = Int128(hi, lo) + Int128(0xfffffff000000000, 0);",
		} {
			if !strings.Contains(src, want) {
				t.Fatalf("the 128-bit message form does not cross the lane as low 64 then the rest, base at full width: missing %q", want)
			}
		}
	})
}
