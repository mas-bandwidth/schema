package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestWideningReasonsDart pins the Dart leg's half of "widening on read, and
// the refusal verdicts" (docs/SPEC-TABLES.md §3, §4, §7, §19.2) against the
// reference leg:
//
//   - a DIRECT WIDENING of every narrower kind into every wider field on its
//     own ladder (i8..i32 into int16..int64, u8..u32 into uint16..uint64, f32
//     into float64), each value and its widened count equal to the Go engine's
//     read of the same bytes (internal/tablewire);
//   - the wire report's VERDICT: refused for a form byte this reader does not
//     carry and for framing it cannot follow, read for a read that kept its
//     verdict (widened or not), each the Go engine's verdict on the same bytes;
//   - the cook's and the block's Open VERDICT, one forgery per clause: every
//     forgery refuses, the unforged cook and block open;
//   - and the reason enum's home: one generated library defines
//     TableRefuseReason.
//
// Why a read refused is per-language diagnostic detail (docs/rulings.json
// refusal-verdict-only): the probes print Dart's reason beside each verdict
// for the failure message, and no Dart reason or word list is compared with
// the reference leg's.
func TestWideningReasonsDart(t *testing.T) {
	t.Parallel()

	t.Run("vocabulary", func(t *testing.T) {
		t.Parallel()
		files, err := Generate(loadUnit(t, unitSchemas(t, "tables/block")...))
		if err != nil {
			t.Fatal(err)
		}
		var runtime string
		for name, src := range files {
			if strings.Contains(string(src), "enum TableRefuseReason {") {
				if runtime != "" {
					t.Fatalf("TableRefuseReason is defined twice in one unit (%s and %s): one accelerator home carries it (§20.7)", runtime, name)
				}
				runtime = name
			}
		}
		if runtime == "" {
			t.Fatal("no generated library defines TableRefuseReason: Open has no reason beside its null")
		}
	})

	dart := dartBinary(t)

	t.Run("widening and the wire reason", func(t *testing.T) {
		t.Parallel()
		u := buildUnit(t, wideningDecl)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		packet := packetFiles(t, u)
		wireLib := ""
		for name := range files {
			if strings.HasSuffix(name, "Wire.dart") {
				wireLib = name
			}
		}
		if wireLib == "" {
			t.Fatal("Generate emitted no form-1 wire library for the widening table")
		}
		root := u.Tables["Root"]
		model := tabletext.NewModel(u)

		type wcase struct {
			name, field string
			wire        []byte
			want        string // value widened verdict, as the probe prints it
		}
		var cases []wcase
		ladders := []struct {
			signed bool
			kinds  []int
			fields map[int]string
		}{
			{true, []int{ir.TableKindI8, ir.TableKindI16, ir.TableKindI32, ir.TableKindI64},
				map[int]string{ir.TableKindI16: "i16", ir.TableKindI32: "i32", ir.TableKindI64: "i64"}},
			{false, []int{ir.TableKindU8, ir.TableKindU16, ir.TableKindU32, ir.TableKindU64},
				map[int]string{ir.TableKindU16: "u16", ir.TableKindU32: "u32", ir.TableKindU64: "u64"}},
		}
		for _, l := range ladders {
			for di, decl := range l.kinds {
				fieldName, ok := l.fields[decl]
				if !ok {
					continue
				}
				for _, narrow := range l.kinds[:di] {
					// the narrow kind's extreme payload: -2 sign-extends, the
					// unsigned maximum zero-extends (§4)
					payload := make([]byte, ir.TableKindWidth(narrow))
					for i := range payload {
						payload[i] = 0xFF
					}
					if l.signed {
						payload[0] = 0xFE
					}
					wire := oneFieldWire(wideField(t, root, fieldName), narrow, payload)
					cases = append(cases, wcase{
						name:  fmt.Sprintf("kind %d into %s", narrow, fieldName),
						field: fieldName,
						wire:  wire,
						want:  engineLine(t, model, root, wire, fieldName),
					})
				}
			}
		}
		f32 := binary.LittleEndian.AppendUint32(nil, math.Float32bits(1.5))
		f32wire := oneFieldWire(wideField(t, root, "f64"), ir.TableKindF32, f32)
		cases = append(cases, wcase{"kind 10 into f64", "f64", f32wire, engineLine(t, model, root, f32wire, "f64")})
		for _, c := range cases {
			if !strings.HasSuffix(c.want, " read") || strings.Fields(c.want)[1] != "1" {
				t.Fatalf("%s: the Go engine's own read is not one widening: %q", c.name, c.want)
			}
		}

		// THE WIRE VERDICT: the form byte refusals, framing damage, and a
		// read. The want is the Go engine's verdict on the same bytes, held
		// to the verdict this row exists for so the oracle cannot drift.
		truncated := oneFieldWire(wideField(t, root, "i64"), ir.TableKindI16, []byte{0xFE, 0xFF})[:4]
		reasons := []struct {
			name string
			wire []byte
			want string
		}{
			{"form byte 0", []byte{0}, "refused"},
			{"form byte 6", []byte{6, 0, 0, 0, 0, 0, 0, 0, 0}, "refused"},
			{"form byte 0xff", []byte{0xff}, "refused"},
			{"a body cut inside its payload", truncated, "refused"},
			{"a widened read", cases[0].wire, "read"},
		}
		for _, r := range reasons {
			if got := engineVerdict(t, model, root, r.wire); got != r.want {
				t.Fatalf("%s: the Go engine's verdict is %s, the row exists for %s", r.name, got, r.want)
			}
		}

		dir := t.TempDir()
		writeFiles(t, dir, packet)
		writeFiles(t, dir, files)
		var in strings.Builder
		for _, c := range cases {
			fmt.Fprintf(&in, "%s %s\n", c.field, hex.EncodeToString(c.wire))
		}
		for _, r := range reasons {
			fmt.Fprintf(&in, "reason %s\n", hex.EncodeToString(r.wire))
		}
		probe := `import 'dart:io';
import 'dart:typed_data';
import '` + wireLib + `';

Uint8List unhex(String s) {
  final out = Uint8List(s.length ~/ 2);
  for (var i = 0; i < out.length; i++) {
    out[i] = int.parse(s.substring(2 * i, 2 * i + 2), radix: 16);
  }
  return out;
}

void main(List<String> args) {
  for (final line in File(args[0]).readAsLinesSync()) {
    final f = line.split(' ');
    final r = TableWireReport();
    final v = rootWireLoad(unhex(f[1]), r);
    final Object value = switch (f[0]) {
      'i16' => v.i16,
      'i32' => v.i32,
      'i64' => v.i64,
      'u16' => v.u16,
      'u32' => v.u32,
      'u64' => v.u64,
      'f64' => v.f64,
      _ => '-',
    };
    stdout.writeln('$value ${r.widened} ${r.refused ? 'refused' : 'read'} ${r.reason}');
  }
}
`
		if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cases.txt"), []byte(in.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		got := runProbe(t, dart, dir, "cases.txt")
		if len(got) != len(cases)+len(reasons) {
			t.Fatalf("probe answered %d lines for %d cases:\n%s", len(got), len(cases)+len(reasons), strings.Join(got, "\n"))
		}
		// Each line is value, widened count, verdict and Dart's own reason:
		// the reason is diagnostic and is never compared.
		for i, c := range cases {
			if f := strings.Fields(got[i]); len(f) < 3 || strings.Join(f[:3], " ") != c.want {
				t.Errorf("%s: Dart read %q, the Go engine %q", c.name, got[i], c.want)
			}
		}
		for i, r := range reasons {
			if f := strings.Fields(got[len(cases)+i]); len(f) < 3 || f[2] != r.want {
				t.Errorf("%s: Dart's verdict is %q, the Go engine's is %s", r.name, got[len(cases)+i], r.want)
			}
		}
	})

	t.Run("report surface carries the strict counters", func(t *testing.T) {
		t.Parallel()
		src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "dart", "main.dart"))
		if err != nil {
			t.Fatal(err)
		}
		line, ok := dartFunctionSpan(string(src), "String _reportLine(")
		if !ok {
			t.Fatal("the Dart driver has no report line")
		}
		for _, retired := range []string{"clamped", "duplicate", "malformed"} {
			if strings.Contains(line, retired) {
				t.Errorf("the report line still carries the retired %s counter", retired)
			}
		}
		for _, survivor := range []string{"$unknown", "$widened", "$retained", "$retainLost"} {
			if !strings.Contains(line, survivor) {
				t.Errorf("a successful read does not report %s", survivor)
			}
		}
		if !strings.Contains(line, `'refused\n'`) {
			t.Error("a refused read must answer the refusal alone")
		}
		load, ok := dartFunctionSpan(string(src), "String? loadReport(")
		if !ok {
			t.Fatal("the Dart driver has no loadReport")
		}
		for _, survivor := range []string{"rep.retained", "rep.retainLost"} {
			if !strings.Contains(load, survivor) {
				t.Errorf("the report surface does not carry %s", survivor)
			}
		}
		for _, sig := range []string{"int surfaceReport(", "int surfaceFraming("} {
			body, ok := dartFunctionSpan(string(src), sig)
			if !ok {
				t.Fatalf("the Dart driver has no %s", sig)
			}
			for _, retired := range []string{"clamped", "duplicate", "malformed"} {
				if strings.Contains(body, retired) {
					t.Errorf("%s still carries the retired %s counter", sig, retired)
				}
			}
		}
	})

	t.Run("cook and block open verdicts", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		for _, unit := range []string{"tables/pointers", "tables/block"} {
			u := loadUnit(t, unitSchemas(t, unit)...)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			writeFiles(t, dir, packetFiles(t, u))
			writeFiles(t, dir, files)
		}
		graph, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "graph_tree.bin"))
		if err != nil {
			t.Fatal(err)
		}
		block, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "block_render.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(openReasonProbe), 0o600); err != nil {
			t.Fatal(err)
		}
		// Each line is the forgery, Open's verdict and Dart's own reason;
		// the reason is diagnostic and is never compared.
		got := map[string]string{}
		lines := map[string]string{}
		for _, line := range runProbe(t, dart, dir, graph, block) {
			name, rest, _ := strings.Cut(line, " ")
			verdict, _, _ := strings.Cut(rest, " ")
			got[name] = verdict
			lines[name] = line
		}
		want := map[string]string{
			"cook_ok":              "open",
			"cook_null":            "refused",
			"cook_short_header":    "refused",
			"cook_magic_byte":      "refused",
			"cook_magic_reversed":  "refused",
			"cook_byte_order":      "refused",
			"cook_build":           "refused",
			"cook_reserved":        "refused",
			"cook_alignment":       "refused",
			"cook_data_long":       "refused",
			"cook_one_short":       "refused",
			"cook_build_and_short": "refused",
			"cook_lead":            "refused",
			"block_ok":             "open",
			"block_null":           "refused",
			"block_short":          "refused",
			"block_magic_byte":     "refused",
			"block_magic_swapped":  "refused",
			"block_byte_order":     "refused",
			"block_build":          "refused",
			"block_pitch":          "refused",
			"block_padding":        "refused",
			"block_lead":           "refused",
		}
		for name, w := range want {
			if got[name] != w {
				t.Errorf("%s: Open's verdict is %q (%q), want %s", name, got[name], lines[name], w)
			}
		}
		if len(got) != len(want) {
			t.Errorf("probe answered %d forgeries, want %d: %v", len(got), len(want), got)
		}
	})
}

// wideningDecl is one field per wider kind (docs/SPEC-TABLES.md §4): each
// receives every narrower kind on its own signed or unsigned ladder, and the
// float64 receives a float32.
const wideningDecl = `package widen

table Root {
	i16 int16
	i32 int32
	i64 int64
	u16 uint16
	u32 uint32
	u64 uint64
	f64 float64
}
`

// openReasonProbe forges one header clause per line, on a cook the leg itself
// writes from the committed graph_tree wire and on the committed render block,
// at the offsets testdata/conformance/tables/MANIFEST.txt's forgery rows name.
const openReasonProbe = `import 'dart:io';
import 'dart:typed_data';
import 'GraphCook.dart' as ck;
import 'GraphdemoWire.dart' as graphdemo;
import 'RenderBlock.dart' as blk;

Uint8List forge(Uint8List base, int at, int value) {
  final out = Uint8List.fromList(base);
  ByteData.view(out.buffer).setUint64(at, value, Endian.little);
  return out;
}

Uint8List lead(Uint8List base, int n) {
  final out = Uint8List(base.length + n);
  out.setRange(n, n + base.length, base);
  return out;
}

void cook(String name, Uint8List? bytes, [int at = 0, int? length]) {
  final r = ck.SceneCook.openWithReason(bytes, at, length ?? (bytes == null ? 0 : bytes.length - at));
  stdout.writeln('$name ${r.$1 == null ? 'refused' : 'open'} ${r.$2}');
}

void block(String name, Uint8List? bytes, [int base = 0, int? extent]) {
  final r = blk.RenderFrameBlock.openWithReason(bytes, base, extent ?? (bytes == null ? 0 : bytes.length - base));
  stdout.writeln('$name ${r.$1 == null ? 'refused' : 'open'} ${r.$2}');
}

void main(List<String> args) {
  final value = graphdemo.sceneWireLoad(File(args[0]).readAsBytesSync());
  final c = Uint8List(ck.SceneCook.cookMeasure(value));
  if (!ck.SceneCook.cook(value, c, Endian.little)) {
    stderr.writeln('the leg could not cook graph_tree');
    exit(1);
  }
  final view = ByteData.view(c.buffer);
  cook('cook_ok', c);
  cook('cook_null', null);
  cook('cook_short_header', c, 0, 63);
  cook('cook_magic_byte', forge(c, 0, view.getUint64(0, Endian.little) ^ 0xff));
  cook('cook_magic_reversed', forge(c, 0, 0x5343484d434f4f4b));
  cook('cook_byte_order', forge(c, 16, 2));
  cook('cook_build', forge(c, 8, 0));
  cook('cook_reserved', forge(c, 48, 1));
  cook('cook_alignment', forge(c, 40, 3));
  cook('cook_data_long', forge(c, 24, view.getUint64(24, Endian.little) + 1));
  cook('cook_one_short', c, 0, c.length - 1);
  cook('cook_build_and_short', forge(c, 8, 0), 0, c.length - 1);
  cook('cook_lead', lead(c, 1), 1);

  final b = File(args[1]).readAsBytesSync();
  block('block_ok', b);
  block('block_null', null);
  block('block_short', b, 0, 8);
  block('block_magic_byte', forge(b, 0, 0));
  block('block_magic_swapped', forge(b, 0, 0x5343484d41424c4b));
  block('block_byte_order', forge(b, 16, 2));
  block('block_build', forge(b, 8, 0));
  final pitch = Uint8List.fromList(b);
  ByteData.view(pitch.buffer).setUint32(0x3c, 0x60, Endian.little);
  block('block_pitch', pitch);
  block('block_padding', b, 0, b.length - 1);
  block('block_lead', lead(b, 1), 1);
}
`

// oneFieldWire is a form-1 file whose root body carries ONE field at the given
// kind (docs/SPEC-TABLES.md §3): the form byte, ref 1, the kind and its
// payload, the terminator, and a one-entry id table.
func oneFieldWire(f *ir.Field, kind int, payload []byte) []byte {
	wire := []byte{1, 1, byte(kind)}
	wire = append(wire, payload...)
	wire = append(wire, 0)
	wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(f))
	return binary.LittleEndian.AppendUint64(wire, 1)
}

func wideField(t *testing.T, st *ir.Struct, name string) *ir.Field {
	t.Helper()
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("%s has no field %s", st.Name, name)
	return nil
}

// engineVerdict is the Go engine's verdict on one wire: refused for a read's
// refusal, read otherwise. An error that is not a read's refusal is the
// oracle failing, never a verdict.
func engineVerdict(t *testing.T, m *tabletext.Model, st *ir.Struct, wire []byte) string {
	t.Helper()
	var report tabletext.Report
	ok, err := tablewire.Decode(m, m.New(st), wire, &report)
	if err != nil && !tablewire.ReadRefusal(err) {
		t.Fatalf("the Go engine failed without a refusal: %v", err)
	}
	if !ok || report.Refused {
		return "refused"
	}
	return "read"
}

// engineLine is the Go engine's read of one wire, spelled as the probe spells
// the Dart read: the field's value as a Dart int or double, the widened count,
// and the verdict.
func engineLine(t *testing.T, m *tabletext.Model, st *ir.Struct, wire []byte, field string) string {
	t.Helper()
	inst := m.New(st)
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
		t.Fatalf("the Go engine refused the widening wire: %v", err)
	}
	verdict := "read"
	if report.Refused {
		verdict = "refused"
	}
	for i := range inst.Fields {
		f := &inst.Fields[i]
		if f.Def.Name != field {
			continue
		}
		var value string
		switch f.Def.Type.Kind {
		case ir.TFloat64:
			value = strconv.FormatFloat(f.Cell.F, 'f', -1, 64)
			if !strings.Contains(value, ".") {
				value += ".0"
			}
		default:
			if f.Def.Type.Signed {
				value = strconv.FormatInt(f.Cell.I, 10)
			} else {
				value = strconv.FormatInt(int64(f.Cell.U), 10)
			}
		}
		return fmt.Sprintf("%s %d %s", value, report.Widened, verdict)
	}
	t.Fatalf("the engine's instance has no field %s", field)
	return ""
}

func runProbe(t *testing.T, dart, dir string, args ...string) []string {
	slowtest.Gate(t, "an external toolchain")

	t.Helper()
	cmd := testproc.Command(t, dart, append([]string{"--enable-asserts", "probe.dart"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe over the generated library: %v\n%s", err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func unitSchemas(t *testing.T, unit string) []string {
	t.Helper()
	schemas, err := filepath.Glob(filepath.Join("..", "..", "..", unit, "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schema files under %s: %v", unit, err)
	}
	return schemas
}

// dartFunctionSpan is one function's text, from its signature to the brace
// that closes its body.
func dartFunctionSpan(src, sig string) (string, bool) {
	i := strings.Index(src, sig)
	if i < 0 {
		return "", false
	}
	open := strings.Index(src[i:], ") {")
	if open < 0 {
		return "", false
	}
	open += i + 2
	depth := 0
	for k := open; k < len(src); k++ {
		switch src[k] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[i : k+1], true
			}
		}
	}
	return "", false
}
