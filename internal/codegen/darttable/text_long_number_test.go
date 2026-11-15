package darttable

// A NUMBER TOKEN OF ANY LENGTH IS READ IN ONE PASS (docs/SPEC-TABLES.md
// §16.2): the scan runs over the token's code units, so a long digit run costs
// bounded stack and linear time, and the read answers a value or a refusal,
// never an Error. Each row's verdict, unknown count and `precision` value are
// the Go engine's (internal/tabletext) over the same text. Running the Dart SDK
// makes this a functional test, gated by slowtest.
//
// The rows: a 10 MB float token for `precision` reads; the same token under an
// unknown key reads with one unknown; a 10 MB integer for `experience` is
// refused out of range; and `1.` followed by 600 zeros is the float 1.0
// (control). The texts are built here, never committed.

import (
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// dartJsonLongNumberDigits is the 10 MB run of digits each long row spans.
const dartJsonLongNumberDigits = 10 << 20

type dartJsonLongNumberCase struct {
	name string
	text string
}

func dartJsonLongNumberCases() []dartJsonLongNumberCase {
	floatToken := "1." + strings.Repeat("9", dartJsonLongNumberDigits)
	intToken := strings.Repeat("9", dartJsonLongNumberDigits)
	return []dartJsonLongNumberCase{
		{"precision-long-float", `{"precision": ` + floatToken + `}`},
		{"unknown-long-float", `{"nope": ` + floatToken + `}`},
		{"experience-long-integer", `{"experience": ` + intToken + `}`},
		{"precision-600-zeros", `{"precision": 1.` + strings.Repeat("0", 600) + `}`},
	}
}

func dartJsonLongNumberUnit(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	u := loadUnit(t, "../../../tables/examples/Tables.schema")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire := ""
	for name, src := range files {
		if strings.HasSuffix(name, "Wire.dart") && strings.Contains(string(src), "ProfileConfig profileConfigFromJson(") {
			wire = name
		}
	}
	if wire == "" {
		t.Fatal("Generate emitted no library declaring profileConfigFromJson")
	}
	maps.Copy(files, packetFiles(t, u))
	return files, wire
}

// dartJsonLongNumberRun answers one line per row from the generated Dart text
// read: `refused`, or `read <unknown> <precision bits in hex>`.
func dartJsonLongNumberRun(t *testing.T) []string {
	t.Helper()
	cases := dartJsonLongNumberCases()
	files, wire := dartJsonLongNumberUnit(t)
	dir := t.TempDir()
	writeFiles(t, dir, files)
	var args []string
	for _, tc := range cases {
		name := tc.name + ".json"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(tc.text), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, name)
	}
	probe := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import '%s';

void main(List<String> args) {
  for (final path in args) {
    final r = TableTextReport();
    try {
      final v = profileConfigFromJson(File(path).readAsStringSync(), r);
      if (r.refused) {
        stdout.writeln('refused');
      } else {
        final bits = (ByteData(8)..setFloat64(0, v.precision, Endian.big)).getUint64(0, Endian.big);
        stdout.writeln('read ${r.unknown} ${bits.toRadixString(16)}');
      }
    } on FormatException {
      stdout.writeln('refused');
    }
  }
}
`, wire)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), append([]string{"--enable-asserts", "probe.dart"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart text read: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("the probe answered %d lines for %d rows: %q", len(lines), len(cases), out)
	}
	return lines
}

// dartJsonLongNumberOracle answers the Go engine's verdict, unknown count and
// `precision` value for each row.
func dartJsonLongNumberOracle(t *testing.T) []string {
	t.Helper()
	cases := dartJsonLongNumberCases()
	u := loadUnit(t, "../../../tables/examples/Tables.schema")
	model := tabletext.NewModel(u)
	def := model.Lookup("ProfileConfig")
	if def == nil {
		t.Fatal("the unit declares no ProfileConfig")
	}
	var want []string
	for _, tc := range cases {
		inst := model.New(def)
		var rep tabletext.Report
		if !model.Read(inst, []byte(tc.text), &rep) {
			want = append(want, "refused")
			continue
		}
		at := -1
		for i := range inst.Fields {
			if inst.Fields[i].Def.Name == "precision" {
				at = i
				break
			}
		}
		if at < 0 {
			t.Fatal("ProfileConfig declares no precision field")
		}
		want = append(want, fmt.Sprintf("read %d %x", rep.Unknown, math.Float64bits(inst.Fields[at].Cell.F)))
	}
	return want
}

func TestDartJsonLongNumberTokenReadsWithoutError(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	got, want := dartJsonLongNumberRun(t), dartJsonLongNumberOracle(t)
	for i, tc := range dartJsonLongNumberCases() {
		if got[i] != want[i] {
			t.Errorf("%s: Dart answered %q, the Go engine %q", tc.name, got[i], want[i])
		}
	}
}
