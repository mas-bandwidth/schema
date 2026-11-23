package darttable

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE VARIABLE SAVE HOLDS THE WRITE CONTRACT IN A DEBUG RUN (docs/SPEC.md
// §5.1 rows 1 to 6 and 10) on every field, arm payload and element, required
// or not; the C leg's TestCWriteContractDebug is the pattern. Dart is an
// assert leg: a row the declaration does not admit throws AssertionError
// under --enable-asserts; rows the leg refuses in every build (6 and 7, and
// 1 and 10 on a required field) throw StateError in both runs; and without
// asserts no AssertionError is ever thrown. Every admitted twin saves in
// both runs.
const review1121WriteSchema = `package probe

enum Color { red, green }

union Pick
{
	a uint8 | min = 0, max = 9
	b uint8
	c float32 | min = -1.0, max = 1.0, resolution = 0.01
}

table Root
{
	r   uint8 = 1 | min = 1, max = 9
	bt  bits(4)
	ea  [..4]uint8 | min = 0, max = 9
	arr [2..4]uint8
	s   string(4)
	bs  bytes(4)
	c   Color
	p   Pick
	f   float32 = 0.5 | min = -1.0, max = 1.0, resolution = 0.01
	fe  [..4]float32 | min = -1.0, max = 1.0, resolution = 0.01
	rq  uint8 | min = 1, max = 9, required
	rf  float32 | min = -1.0, max = 1.0, resolution = 0.01, required
	g   float32 | min = 0.0, max = 0.1, resolution = 0.001
	rg  float32 | min = 0.0, max = 0.1, resolution = 0.001, required
}
`

// review1121WriteRows maps each row to the Dart statements that set it on a
// fresh value whose required fields are already lawful.
var review1121WriteRows = []struct{ name, set string }{
	{"row1_field", "v.r = 0;"},
	{"row1_field_twin", "v.r = 9;"},
	{"row1_arm", "v.p.armTag = 1; v.p.a = 10;"},
	{"row1_arm_twin", "v.p.armTag = 1; v.p.a = 9;"},
	{"row1_element", "v.ea = <int>[1, 10];"},
	{"row1_element_twin", "v.ea = <int>[1, 9];"},
	{"row1_required", "v.rq = 0;"},
	{"row2_bits", "v.bt = 0x1f;"},
	{"row2_bits_twin", "v.bt = 0x0f;"},
	{"row3_below_min", "v.arr = <int>[1];"},
	{"row3_past_max", "v.arr = <int>[1, 2, 3, 4, 5];"},
	{"row3_twin", "v.arr = <int>[1, 2, 3, 4];"},
	{"row4_string", "v.s = 'abcde';"},
	{"row4_bytes", "v.bs = 'abcde';"},
	{"row4_twin", "v.s = 'abcd'; v.bs = 'abcd';"},
	{"row5_enum", "v.c = Color(7);"},
	{"row5_twin", "v.c = Color.green;"},
	{"row6_union", "v.p.armTag = 7;"},
	{"row6_twin", "v.p.armTag = 2; v.p.b = 200;"},
	{"row10_field", "v.f = 1.5;"},
	{"row10_arm", "v.p.armTag = 3; v.p.c = 1.5;"},
	{"row10_element", "v.fe = <double>[0.5, 1.5];"},
	{"row10_required", "v.rf = 1.5;"},
	{"row10_twin", "v.f = 1.0; v.p.armTag = 3; v.p.c = -1.0; v.fe = <double>[0.5, 1.0]; v.rf = -1.0;"},
	// The float32 top step a writer stores for max = 0.1 is admitted: the
	// range is held at float32 (SPEC.md §4.6), on a field and a required one.
	{"row10_top_step_twin", "v.g = 0.10000000149011612; v.rg = 0.10000000149011612;"},
	{"row10_past_top_step", "v.g = 0.10000001639127731;"},
}

const review1121WriteProbe = `import 'ProbeWire.dart';
import 'Probe.dart';

String outcome(void Function(Root v) set) {
  final v = Root();
  v.rq = 1;
  v.arr = <int>[0, 0]; // at Min whatever birth gives, so each row stands alone
  try {
    set(v);
    rootWireSave(v);
    return 'saved';
  } on AssertionError {
    return 'assert';
  } on StateError {
    return 'refused';
  }
}

void main() {
%s}
`

func runDartWriteContract(t *testing.T, asserts bool) map[string]string {
	t.Helper()
	u := unitFrom(t, review1121WriteSchema)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var body strings.Builder
	for _, row := range review1121WriteRows {
		fmt.Fprintf(&body, "  print('ROW %s ' + outcome((v) { %s }));\n", row.name, row.set)
	}
	files["review1121_write.dart"] = []byte(fmt.Sprintf(review1121WriteProbe, body.String()))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	args := []string{"review1121_write.dart"}
	if asserts {
		args = append([]string{"--enable-asserts"}, args...)
	}
	cmd := testproc.Command(t, dartBinary(t), args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the write-contract probe did not run (asserts=%v): %v\n%s", asserts, err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if f := strings.Fields(line); len(f) == 3 && f[0] == "ROW" {
			got[f[1]] = f[2]
		}
	}
	return got
}

func TestDartWriteContractDebug(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart (it runs the generated variable save with and without asserts)")
	debug := runDartWriteContract(t, true)
	release := runDartWriteContract(t, false)
	// Debug: the assert, or the every-build refusal a required field gets first.
	wantDebug := map[string]string{
		"row1_field": "assert", "row1_arm": "assert", "row1_element": "assert", "row1_required": "refused",
		"row2_bits": "assert", "row3_below_min": "assert", "row3_past_max": "assert",
		"row4_string": "assert", "row4_bytes": "assert", "row5_enum": "assert", "row6_union": "assert",
		"row10_field": "assert", "row10_arm": "assert", "row10_element": "assert", "row10_required": "refused",
		"row10_past_top_step": "assert",
	}
	// Without asserts: no AssertionError; a required field's rows 1 and 10
	// and row 6 are refused in every build.
	wantRelease := map[string]string{"row1_required": "refused", "row6_union": "refused", "row10_required": "refused"}
	for _, row := range review1121WriteRows {
		want, ok := wantDebug[row.name]
		if !ok {
			want = "saved"
		}
		if got := debug[row.name]; got != want {
			t.Errorf("%s (asserts on): %q, want %q", row.name, got, want)
		}
		got := release[row.name]
		if strings.HasSuffix(row.name, "_twin") {
			if got != "saved" {
				t.Errorf("%s (asserts off): %q, want saved", row.name, got)
			}
			continue
		}
		if got == "assert" || got == "" {
			t.Errorf("%s (asserts off): %q: an assert leg carries no release write assert", row.name, got)
		}
		if w, ok := wantRelease[row.name]; ok && got != w {
			t.Errorf("%s (asserts off): %q, want %q", row.name, got, w)
		}
	}
}
