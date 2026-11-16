package darttable

// JSON numeric text is held to the Go engine (docs/SPEC-TABLES.md §16.2).
// Floats preserve negative zero. Wide integers read exact decimal strings,
// including values beyond binary64 precision; bare numbers and invalid or
// out-of-range strings refuse. The controls remove negative-zero handling or
// route wide decimal strings through binary64 and must change their rows.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// textInt64 holds valid string-encoded values a binary64 conversion rounds.
var textInt64 = []string{
	`{"u": "9007199254740993"}`,
	`{"u": "9223372036854775807"}`,
	`{"s": "-9007199254740993"}`,
	`{"u": "18446744073709551615"}`,
	`{"s": "9223372036854775807"}`,
}

// Keep the old number-token and out-of-domain values as refusal cases, and
// exercise the same out-of-domain digits in their required string form.
var textWideRefusal = []string{
	`{"u": 18446744073709551616}`,
	`{"u": -1}`,
	`{"s": 9223372036854775808}`,
	`{"s": -9223372036854775809}`,
	`{"s": 99999999999999999999999}`,
	`{"s": -99999999999999999999999}`,
	`{"w": 18446744073709551615}`,
	`{"u": -1e3}`,
	`{"u": 2e19}`,
	`{"u": "18446744073709551616"}`,
	`{"u": "-1"}`,
	`{"s": "9223372036854775808"}`,
	`{"s": "-9223372036854775809"}`,
	`{"s": "99999999999999999999999"}`,
	`{"s": "-99999999999999999999999"}`,
	`{"u": "-1e3"}`,
	`{"u": "2e19"}`,
}

var textNumberSteady = []string{
	`{"d": -0.0}`,
	`{"f": 0}`,
	`{"s": -0, "u": -0, "w": -0}`,
	`{"u": "9223372036854775808"}`,
	`{"s": "-9223372036854775808"}`,
	`{"w": 4294967295}`,
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
	all = append(all, textWideRefusal...)
	all = append(all, textNumberSteady...)
	return all
}

// textNumberRun generates the unit, lets edit plant a reversal in the
// generated wire module, and answers one line per text:
// `read <saved bytes>` or `refused <reason>`.
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
    if (r.refused) {
      print('refused ${r.reason}');
    } else {
      final hex = w.rootWireSave(v).map((b) => b.toRadixString(16).padLeft(2, '0')).join();
      print('read $hex');
    }
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
			reason, _, _ := strings.Cut(rep.Reason, ":")
			if reason != "kind_mismatch" && reason != "out_of_range" {
				t.Fatalf("%s: unexpected Go refusal (%+v)", text, rep)
			}
			want = append(want, "refused "+reason)
			continue
		}
		if slices.Contains(textWideRefusal, text) {
			t.Fatalf("%s: wide out-of-contract text was accepted", text)
		}
		wire, err := tablewire.EncodeVariable(model, inst)
		if err != nil {
			t.Fatalf("%s: the Go engine's wire: %v", text, err)
		}
		want = append(want, fmt.Sprintf("read %x", wire))
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

// textInt64DoubleCompare plants a lossy binary64 conversion in the emitted
// wide-string reader while retaining its token and range checks.
const textInt64DoubleCompare = `int _jsonWideIntField(dynamic val, TableTextReport r, int? lo, int? hi, bool unsigned, [int def = 0]) {
  if (val is! String || !_jsonIntegerToken(val)) {
    r.refuse('kind_mismatch');
    return def;
  }
  if (_jsonIntegerDigits(val) > 20) {
    r.refuse('out_of_range');
    return def;
  }
  final rounded = double.parse(val);
  return _jsonPlaceInteger(BigInt.from(rounded), r, lo, hi, unsigned, def);
}

int _jsonWideIntFieldExact(`

func TestDartTextInt64DoubleCompareNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()
	got := textNumberRun(t, func(src string) string {
		return plant(t, src, "int _jsonWideIntField(", textInt64DoubleCompare)
	})
	red := textNumberRed(got, textNumberOracle(t))
	wantRed := append(append([]string{}, textInt64...), `{"s": "-9223372036854775809"}`)
	if strings.Join(red, " | ") != strings.Join(wantRed, " | ") {
		t.Fatalf("the double compare is red on %q, want exactly %q", red, wantRed)
	}
}
