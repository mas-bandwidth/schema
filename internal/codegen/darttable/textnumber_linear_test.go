package darttable

// A NUMBER TOKEN COSTS TIME IN ITS OWN LENGTH (docs/SPEC-TABLES.md §16.2,
// §16.12). The text read decides a numeric field's range from the token's
// significant digits and decimal point before it parses, so a token the
// field cannot hold refuses without super-linear big-integer work, and the
// token scanner is one pass over RFC 8259's number production rather than a
// regular expression.
//
// The functional row runs the same texts against the generated Dart and the
// Go engine (internal/tabletext), and requires each verdict and value to be
// the engine's. The short row pins the same cases at thirty digits, where
// time plays no part.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const dartTextNumberLinearSchema = `package probe
table Root {
    u uint64
    w uint128
    g fixed(48, 16) | min = -30000, max = 30000
    f float64
}
`

type dartTextNumberLinearCase struct {
	name  string
	field string
	token string
}

// dartTextNumberLinearCases builds the same shapes at any digit run.
func dartTextNumberLinearCases(long, floatLen int) []dartTextNumberLinearCase {
	nines := strings.Repeat("9", long)
	return []dartTextNumberLinearCase{
		{"uint64-past-range", "u", nines},
		{"uint128-past-range", "w", nines},
		{"fixed-past-range", "g", nines},
		{"leading-zeros", "g", "0." + strings.Repeat("0", long-2) + "1"},
		{"zeros-after-point", "g", "1." + strings.Repeat("0", long-1)},
		{"float-long-token", "f", "0." + strings.Repeat("0", floatLen-2) + "1"},
		// a comment is layout between tokens (§16.2): a slash right after a
		// number opens one, and a slash that opens none is refused
		{"uint64-block-comment", "u", "7/*c*/"},
		{"uint64-line-comment", "u", "7//c\n"},
		{"fixed-block-comment", "g", "1.5/**/"},
		{"float-line-comment", "f", "2e3//c\n"},
		{"long-block-comment", "u", nines + "/*c*/"},
		{"lone-slash", "u", "7/"},
		{"slash-not-comment", "u", "7/c"},
	}
}

func dartTextNumberLinearRun(t *testing.T, cases []dartTextNumberLinearCase) []string {
	t.Helper()
	u := unitFrom(t, dartTextNumberLinearSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	var args []string
	for i, tc := range cases {
		name := fmt.Sprintf("case%d.json", i)
		text := `{"` + tc.field + `":` + tc.token + `}`
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, name)
	}
	const probe = `import 'dart:io';
import 'ProbeWire.dart' as w;
void main(List<String> args) {
  for (final path in args) {
    final r = w.TableTextReport();
    final w.Root v;
    try {
      v = w.rootFromJson(File(path).readAsStringSync(), r);
    } on FormatException {
      // a text that is not JSON in this dialect throws: malformed
      print("1 0 malformed");
      continue;
    }
    final hex = w.rootWireSave(v).map((b) => b.toRadixString(16).padLeft(2, '0')).join();
    print('${r.refused ? 1 : 0} ${r.reason == 'kind_mismatch' ? 1 : 0} $hex');
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), append([]string{"--enable-asserts", "probe.dart"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Dart probe: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(got) != len(cases) {
		t.Fatalf("the probe answered %d lines for %d texts: %q", len(got), len(cases), got)
	}
	return got
}

func dartTextNumberLinearOracle(t *testing.T, cases []dartTextNumberLinearCase) []string {
	t.Helper()
	u := unitFrom(t, dartTextNumberLinearSchema)
	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("the probe unit declares no Root")
	}
	var want []string
	for _, tc := range cases {
		inst := model.New(def)
		var rep tabletext.Report
		ok := model.Read(inst, []byte(`{"`+tc.field+`":`+tc.token+`}`), &rep)
		refused, kind := "0", "0"
		if !ok {
			refused = "1"
		}
		if strings.HasPrefix(rep.Reason, "malformed") {
			want = append(want, "1 0 malformed")
			continue
		}
		if strings.HasPrefix(rep.Reason, "kind_mismatch") {
			kind = "1"
		}
		wire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s: the Go engine's wire: %v", tc.name, err)
		}
		want = append(want, refused+" "+kind+" "+fmt.Sprintf("%x", wire))
	}
	return want
}

func dartTextNumberLinearCheck(t *testing.T, got, want []string, cases []dartTextNumberLinearCase) {
	t.Helper()
	for i := range cases {
		if got[i] != want[i] {
			t.Errorf("%s: Dart answers %s, the Go engine %s", cases[i].name, got[i], want[i])
		}
	}
}

func TestDartTextLongNumberTokenFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	cases := dartTextNumberLinearCases(4_000_000, 10_000_000)
	got := dartTextNumberLinearRun(t, cases)
	want := dartTextNumberLinearOracle(t, cases)
	dartTextNumberLinearCheck(t, got, want, cases)
}

func TestDartTextNumberTokenVerdicts(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	cases := dartTextNumberLinearCases(30, 30)
	got := dartTextNumberLinearRun(t, cases)
	want := dartTextNumberLinearOracle(t, cases)
	dartTextNumberLinearCheck(t, got, want, cases)
}
