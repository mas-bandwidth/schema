package darttable

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const dartTextHostileSchema = `package probe

enum Level {
	Easy
	Hard
}

table Child {
	label string(16)
}

table Sub {
	x int32 = 10
	y int32 = 20
}

union Choice {
	a int32
	b
	c *Child
	d Sub
}

table Root {
	version int32 = 42
	val int32
	f32 float32
	f64 float64
	flag bool = true
	tag string(16)
	wide1 wstring(2)
	wide2 wstring(2)
	keyed [Level]int32
	entries map[int32]int32
	choice Choice
	sub Sub
	items []Sub
}
`

func TestDartTextHostile(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()

	u := buildUnit(t, dartTextHostileSchema)
	for _, st := range u.Tables {
		for _, f := range st.Fields {
			if f.Name == "wide1" {
				f.Type.Size = 1
			}
		}
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wireCode, ok := files["ProbeWire.dart"]
	if !ok || len(wireCode) == 0 {
		t.Fatal("Generate emitted no ProbeWire.dart")
	}

	cases := []struct {
		name      string
		json      string
		wantExtra string
	}{
		{
			name:      "arm-scalar-null",
			json:      `{"choice": {"a": null}}`,
			wantExtra: "arm=0",
		},
		{
			name:      "arm-scalar-string",
			json:      `{"choice": {"a": "bad"}}`,
			wantExtra: "arm=0",
		},
		{
			name:      "arm-void-not-null",
			json:      `{"choice": {"b": 123}}`,
			wantExtra: "arm=0",
		},
		{
			name:      "arm-table-wrong-shape",
			json:      `{"choice": {"d": 123}}`,
			wantExtra: "arm=0",
		},
		{
			name:      "arm-pointer-null",
			json:      `{"choice": {"c": null}}`,
			wantExtra: "arm=3",
		},
		{
			name:      "dup-key-int-underflow-second",
			json:      `{"val": 7, "val": 5e-324}`,
			wantExtra: "val=7",
		},
		{
			name:      "dup-key-int-string-second",
			json:      `{"val": 7, "val": "bad"}`,
			wantExtra: "val=7",
		},
		{
			name:      "dup-key-float-overflow-second",
			json:      `{"f64": 2.5, "f64": 1e400}`,
			wantExtra: "f64=2.5",
		},
		{
			name:      "dup-key-table-replaces-whole",
			json:      `{"sub": {"x": 99, "y": 88}, "sub": {"x": 55}}`,
			wantExtra: "sub=55/20",
		},
		{
			name:      "dup-key-table-bad-second-preserves-first",
			json:      `{"sub": {"x": 99, "y": 88}, "sub": "not-an-object"}`,
			wantExtra: "sub=99/88",
		},
		{
			name:      "dup-key-array-elem",
			json:      `{"items": [{"x": 11, "x": "bad"}]}`,
			wantExtra: "item0=11/20",
		},
		{
			name:      "keyed-array-dup-key",
			json:      `{"keyed": {"Hard": 64, "Hard": 0.1}}`,
			wantExtra: "hard=64",
		},
		{
			name:      "map-dup-key",
			json:      `{"entries": {"7": 100, "7": 200}}`,
			wantExtra: "map=1:7->200",
		},
		{
			name:      "null-scalar-preserves-defaults",
			json:      `{"version": null, "flag": null}`,
			wantExtra: "ver=42,flag=true",
		},
		{
			name:      "repeated-unknown-keys-count-unknown-not-duplicate",
			json:      `{"nope": 1, "nope": 2, "keyed": {"NoSuch": 1, "NoSuch": 2}}`,
			wantExtra: "unk=4,dup=0",
		},
		{
			name:      "float32-subnormal",
			json:      `{"f32": 7.0064923216240853547e-46}`,
			wantExtra: "f32bits=00000001",
		},
		{
			name:      "float32-upper-band",
			json:      `{"f32": 340282356779733661637539395458142568447}`,
			wantExtra: "f32bits=7f7fffff",
		},
		{
			name:      "float32-midpoint-overflow-preserves-prior",
			json:      `{"f32": 1.5, "f32": 340282356779733661637539395458142568448}`,
			wantExtra: "f32bits=3fc00000",
		},
		{
			name:      "float32-overflow",
			json:      `{"f32": 1e39}`,
			wantExtra: "f32bits=00000000",
		},
		{
			name:      "float32-single-round-integer-midpoint-neighbor",
			json:      `{"f32": 18446745173231108097}`,
			wantExtra: "f32bits=5f800001",
		},
		{
			name:      "wstring-surrogate-pair-clamp-bound-1",
			json:      `{"wide1": "\uD83D\uDE00"}`,
			wantExtra: "wide1=,clamped=1",
		},
		{
			name:      "wstring-surrogate-pair-clamp-bound-2",
			json:      `{"wide2": "a\uD83D\uDE00"}`,
			wantExtra: "wide2=a,clamped=1",
		},
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ProbeWire.dart"), wireCode, 0o600); err != nil {
		t.Fatal(err)
	}

	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("missing Root in model")
	}

	var args []string
	type oracleExpected struct {
		report   string
		wireHex  string
		canonHex string
	}
	expected := make([]oracleExpected, len(cases))

	for i, tc := range cases {
		inFile := fmt.Sprintf("case_%02d.json", i)
		if err := os.WriteFile(filepath.Join(dir, inFile), []byte(tc.json), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, inFile)

		inst := model.New(def)
		var rep tabletext.Report
		if !model.Read(inst, []byte(tc.json), &rep) {
			t.Fatalf("%s: Go oracle unexpectedly refused input %s", tc.name, tc.json)
		}
		wireBytes, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s: Go oracle tablewire.Encode failed: %v", tc.name, err)
		}
		canonBytes, err := model.Write(inst)
		if err != nil {
			t.Fatalf("%s: Go oracle model.Write failed: %v", tc.name, err)
		}
		expected[i] = oracleExpected{
			report: fmt.Sprintf("u=%d,km=%d,c=%d,d=%d",
				rep.Unknown, rep.KindMismatch, rep.Clamped, rep.Duplicate),
			wireHex:  hex.EncodeToString(wireBytes),
			canonHex: hex.EncodeToString(canonBytes),
		}
	}

	probe := `import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart';

String toHex(List<int> bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

String f32Hex(double d) {
  final bd = ByteData(4)..setFloat32(0, d, Endian.big);
  return bd.getUint32(0, Endian.big).toRadixString(16).padLeft(8, '0');
}

String extraFor(int idx, Root v, TableTextReport r) {
  switch (idx) {
    case 0:
    case 1:
    case 2:
    case 3:
    case 4:
      return 'arm=${v.choice.armTag}';
    case 5:
    case 6:
      return 'val=${v.val}';
    case 7:
      return 'f64=${v.f64}';
    case 8:
    case 9:
      return 'sub=${v.sub.x}/${v.sub.y}';
    case 10:
      return 'item0=${v.items[0].x}/${v.items[0].y}';
    case 11:
      return 'hard=${v.keyed[1]}';
    case 12:
      return 'map=${v.entries.length}:${v.entries[0].key}->${v.entries[0].value}';
    case 13:
      return 'ver=${v.version},flag=${v.flag}';
    case 14:
      return 'unk=${r.unknown},dup=${r.duplicate}';
    case 15:
    case 16:
    case 17:
    case 18:
    case 19:
      return 'f32bits=${f32Hex(v.f32)}';
    case 20:
      return 'wide1=${v.wide1},clamped=${r.clamped}';
    case 21:
      return 'wide2=${v.wide2},clamped=${r.clamped}';
    default:
      return '';
  }
}

void main(List<String> args) {
  for (var i = 0; i < args.length; i++) {
    final text = File(args[i]).readAsStringSync();
    final r = TableTextReport();
    final v = rootFromJson(text, r);
    final wire = rootWireSave(v);
    final canon = utf8.encode(rootToJson(v).trimRight());
    final rep = 'u=${r.unknown},km=${r.kindMismatch},c=${r.clamped},d=${r.duplicate}';
    stdout.writeln('$rep|${toHex(wire)}|${toHex(canon)}|${extraFor(i, v, r)}');
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
		t.Fatalf("dart probe failed: %v\n%s", err, out)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d output lines, want %d:\n%s", len(lines), len(cases), out)
	}

	for i, tc := range cases {
		parts := strings.Split(lines[i], "|")
		if len(parts) != 4 {
			t.Fatalf("%s: malformed probe line %q", tc.name, lines[i])
		}
		gotRep, gotWire, gotCanon, gotExtra := parts[0], parts[1], parts[2], parts[3]
		want := expected[i]
		if gotRep != want.report {
			t.Errorf("%s: report mismatch: got %s, want %s", tc.name, gotRep, want.report)
		}
		if gotWire != want.wireHex {
			t.Errorf("%s: wire mismatch:\n got %s\nwant %s", tc.name, gotWire, want.wireHex)
		}
		if gotCanon != want.canonHex {
			t.Errorf("%s: canonical text mismatch:\n got %s\nwant %s", tc.name, gotCanon, want.canonHex)
		}
		if gotExtra != tc.wantExtra {
			t.Errorf("%s: extra check mismatch: got %q, want %q", tc.name, gotExtra, tc.wantExtra)
		}
	}
}
