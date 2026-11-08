package darttable

// A NUMBER TOKEN LANDS EXACTLY AS THE ENGINE LANDS IT (docs/SPEC-TABLES.md
// §16.2). Two token classes are pinned here, each row's clamped count and
// saved bytes the Go engine's (internal/tabletext, internal/tablewire) over
// the same unit:
//
//   - `-0` is a float field's negative zero. The tolerant read keeps the token
//     a _JNum rather than an int, because an int has no negative zero.
//   - an integer token at or past +-2^63, a negative token in an unsigned
//     field, and an integral float lands by its exact sign and magnitude,
//     saturated at 2^64-1 and counted, then the declared range, then the
//     64-bit storage domain, each move counted.
//
// Each class has a reversed witness that puts the old read back and holds
// that it is red on exactly that class's rows.
//
// Running the Dart SDK makes this a functional test: it is gated by
// slowtest, as every Dart probe here is.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const textNumberSchema = `package probe
fixed table Root {
    u uint64
    s int64
    w uint32
    f float32
    d float64
}
`

// textNegativeZero are the rows the -0 token decides.
var textNegativeZero = []string{
	`{"f": -0}`,
	`{"d": -0}`,
}

// textInt64 are the rows the exact 64-bit placement decides.
var textInt64 = []string{
	`{"u": 18446744073709551616}`,
	`{"u": -1}`,
	`{"s": 9223372036854775808}`,
	`{"s": -9223372036854775809}`,
	`{"s": 99999999999999999999999}`,
	`{"s": -99999999999999999999999}`,
	`{"w": 18446744073709551615}`,
	`{"u": -1e3}`,
	`{"u": 2e19}`,
}

// textNumberSteady are rows both reads have always landed as the engine does.
var textNumberSteady = []string{
	`{"d": -0.0}`,
	`{"f": 0}`,
	`{"s": -0, "u": -0, "w": -0}`,
	`{"u": 9223372036854775807}`,
	`{"u": 9223372036854775808}`,
	`{"u": 18446744073709551615}`,
	`{"s": 9223372036854775807, "w": 4294967295}`,
	`{"s": -9223372036854775808}`,
	`{"s": 1e19}`,
	`{"s": -1e19}`,
	`{"w": 1e10}`,
	`{"w": 4294967296}`,
	`{"w": -99999999999999999999999}`,
	`{"s": 2.5}`,
}

func textNumberTexts() []string {
	var all []string
	all = append(all, textNegativeZero...)
	all = append(all, textInt64...)
	all = append(all, textNumberSteady...)
	return all
}

// textNumberRun generates the unit, lets edit plant a reversal in the
// generated wire module, and answers one line per text:
// `<clamped> <kind mismatch> <saved bytes in hex>`.
func textNumberRun(t *testing.T, edit func(string) string) []string {
	t.Helper()
	u := unitFrom(t, textNumberSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	if edit != nil {
		files["ProbeWire.dart"] = []byte(edit(string(files["ProbeWire.dart"])))
	}
	dir := t.TempDir()
	writeFiles(t, dir, files)
	texts := textNumberTexts()
	if err := os.WriteFile(filepath.Join(dir, "texts.txt"), []byte(strings.Join(texts, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	const probe = `import 'dart:io';
import 'ProbeWire.dart' as w;
void main() {
  for (final text in File('texts.txt').readAsStringSync().split('\n')) {
    final r = w.TableTextReport();
    final v = w.rootFromJson(text, r);
    final hex = w.rootWireSave(v).map((b) => b.toRadixString(16).padLeft(2, '0')).join();
    print('${r.refused ? 1 : 0} ${r.reason == 'kind_mismatch' ? 1 : 0} $hex');
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Dart probe: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(got) != len(texts) {
		t.Fatalf("the probe answered %d lines for %d texts: %q", len(got), len(texts), got)
	}
	return got
}

func textNumberOracle(t *testing.T) []string {
	t.Helper()
	u := unitFrom(t, textNumberSchema)
	model := tabletext.NewModel(u)
	var want []string
	for _, text := range textNumberTexts() {
		inst := model.New(u.Tables["Root"])
		var rep tabletext.Report
		if !model.Read(inst, []byte(text), &rep) {
			t.Fatalf("%s: the Go engine refused the text (%+v)", text, rep)
		}
		wire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s: the Go engine's wire: %v", text, err)
		}
		want = append(want, fmt.Sprintf("%s %s %x", strconv.Itoa(0), strconv.Itoa(0), wire))
	}
	return want
}

// textNumberRed is every text whose line differs from the engine's.
func textNumberRed(got, want []string) []string {
	texts := textNumberTexts()
	var red []string
	for i := range want {
		if got[i] != want[i] {
			red = append(red, texts[i])
		}
	}
	return red
}

func TestDartTextNumberTokensLandAsTheEngine(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()
	got, want := textNumberRun(t, nil), textNumberOracle(t)
	texts := textNumberTexts()
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: Dart lands %s, the Go engine %s", texts[i], got[i], want[i])
		}
	}
}

// plant replaces anchor, which must stand once in src, with planted.
func plant(t *testing.T, src, anchor, planted string) string {
	t.Helper()
	if n := strings.Count(src, anchor); n != 1 {
		t.Fatalf("the plant's anchor stands %d times in ProbeWire.dart, want once: %q", n, anchor)
	}
	return strings.Replace(src, anchor, planted, 1)
}

func TestDartTextNegativeZeroNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()
	got := textNumberRun(t, func(src string) string {
		return plant(t, src, "if (token != '-0' && !token.contains('.')", "if (!token.contains('.')")
	})
	red := textNumberRed(got, textNumberOracle(t))
	if strings.Join(red, " | ") != strings.Join(textNegativeZero, " | ") {
		t.Fatalf("the int -0 is red on %q, want exactly %q", red, textNegativeZero)
	}
}

// textInt64DoubleCompare is the read that compared a token past an int by its
// double, the reversed witness's body.
const textInt64DoubleCompare = `int _jsonIntField(dynamic val, TableTextReport r, int? lo, int? hi, [bool unsigned = false, int def = 0]) {
  int clamp(int v) {
    if (unsigned) {
      if (v < 0 && hi != null && hi >= 0) { v = lo ?? 0; }
      if (lo != null && _u64Below(v, lo)) { v = lo; }
      if (hi != null && _u64Above(v, hi)) { v = hi; }
    } else {
      if (lo != null && v < lo) { v = lo; }
      if (hi != null && v > hi) { v = hi; }
    }
    return v;
  }
  if (val is int) return clamp(val);
  if (unsigned && val is _JNum && (hi == null || hi < 0)) {
    final wide = BigInt.tryParse(val.token);
    if (wide != null && wide >= BigInt.zero && wide <= BigInt.parse('18446744073709551615')) {
      final signed = wide > BigInt.from(9223372036854775807)
          ? wide - (BigInt.one << 64) : wide;
      return clamp(signed.toInt());
    }
  }
  if (val is _JNum && val.d.isFinite && val.d == val.d.truncateToDouble()) {
    if (val.d > 9223372036854775807.0) { return clamp(9223372036854775807); }
    if (val.d < -9223372036854775808.0) { return clamp(-9223372036854775808); }
    return clamp(val.d.toInt());
  }
  if (val is num && val.isFinite && val.toDouble() == val.toDouble().truncateToDouble()) {
    if (val.toDouble() > 9223372036854775807.0) { return clamp(9223372036854775807); }
    if (val.toDouble() < -9223372036854775808.0) { return clamp(-9223372036854775808); }
    return clamp(val.toInt());
  }
  return def;
}

int _jsonIntFieldExact(`

func TestDartTextInt64DoubleCompareNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()
	got := textNumberRun(t, func(src string) string {
		return plant(t, src, "int _jsonIntField(", textInt64DoubleCompare)
	})
	red := textNumberRed(got, textNumberOracle(t))
	if strings.Join(red, " | ") != strings.Join(textInt64, " | ") {
		t.Fatalf("the double compare is red on %q, want exactly %q", red, textInt64)
	}
}
