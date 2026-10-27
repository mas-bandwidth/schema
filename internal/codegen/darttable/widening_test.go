package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestWideningReasonsDart pins the Dart leg's half of "widening on read, and
// the refusal reasons" (docs/SPEC-TABLES.md §3, §4, §7, §19.2) against the
// reference leg:
//
//   - a DIRECT WIDENING of every narrower kind into every wider field on its
//     own ladder (i8..i32 into int16..int64, u8..u32 into uint16..uint64, f32
//     into float64), each value and its widened count equal to the Go engine's
//     read of the same bytes (internal/tablewire);
//   - the wire report's REASON: unknown_form for a form byte this reader does
//     not carry, wire_damaged for framing it cannot follow, ok for a read that
//     kept its verdict (widened or not);
//   - the cook's and the block's Open REASON, one forgery per clause, each the
//     first failing clause;
//   - and the vocabulary itself: the generated TableRefuseReason words, in
//     order, are internal/codegen/gotable/refusal.go's TableRefuse* Error()
//     strings, so no leg invents a word.
//
// Every expected word is read out of the Go leg's refusal.go, never typed here.
func TestWideningReasonsDart(t *testing.T) {
	t.Parallel()
	words := goRefuseWords(t)
	word := func(name string) string {
		t.Helper()
		w, ok := words[name]
		if !ok {
			t.Fatalf("refusal.go has no TableRefuse%s", name)
		}
		return w
	}

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
				got := dartRefuseWords(string(src))
				if strings.Join(got, ",") != strings.Join(goRefuseOrder(t), ",") {
					t.Fatalf("the Dart TableRefuseReason words are not the reference leg's, in order:\n dart %v\n   go %v", got, goRefuseOrder(t))
				}
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
			want        string // value widened reason, as the probe prints it
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
			if !strings.HasSuffix(c.want, " "+word("Ok")) || strings.Fields(c.want)[1] != "1" {
				t.Fatalf("%s: the Go engine's own read is not one widening: %q", c.name, c.want)
			}
		}

		// THE WIRE REASON: the form byte refusals, framing damage, and ok
		truncated := oneFieldWire(wideField(t, root, "i64"), ir.TableKindI16, []byte{0xFE, 0xFF})[:4]
		reasons := []struct {
			name string
			wire []byte
			want string
		}{
			{"form byte 0", []byte{0}, word("UnknownForm")},
			{"form byte 6", []byte{6, 0, 0, 0, 0, 0, 0, 0, 0}, word("UnknownForm")},
			{"form byte 0xff", []byte{0xff}, word("UnknownForm")},
			{"a body cut inside its payload", truncated, word("WireDamaged")},
			{"a widened read", cases[0].wire, word("Ok")},
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
    stdout.writeln('$value ${r.widened} ${r.reason}');
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
		for i, c := range cases {
			if got[i] != c.want {
				t.Errorf("%s: Dart read %q, the Go engine %q", c.name, got[i], c.want)
			}
		}
		for i, r := range reasons {
			fields := strings.Fields(got[len(cases)+i])
			if reason := fields[len(fields)-1]; reason != r.want {
				t.Errorf("%s: the report's reason is %q, the reference leg's is %q", r.name, reason, r.want)
			}
		}
	})

	t.Run("cook and block open reasons", func(t *testing.T) {
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
		got := map[string]string{}
		for _, line := range runProbe(t, dart, dir, graph, block) {
			name, reason, _ := strings.Cut(line, " ")
			got[name] = reason
		}
		want := map[string]string{
			"cook_ok":              word("Ok"),
			"cook_null":            word("UnalignedBase"),
			"cook_short_header":    word("Truncated"),
			"cook_magic_byte":      word("NotACook"),
			"cook_magic_reversed":  word("ForeignOrder"),
			"cook_byte_order":      word("NotACook"),
			"cook_build":           word("WrongBuildVersion"),
			"cook_reserved":        word("ReservedNotZero"),
			"cook_alignment":       word("BadAlignment"),
			"cook_data_long":       word("Truncated"),
			"cook_one_short":       word("Truncated"),
			"cook_build_and_short": word("WrongBuildVersion"),
			"cook_lead":            word("UnalignedBase"),
			"block_ok":             word("Ok"),
			"block_null":           word("UnalignedBase"),
			"block_short":          word("Truncated"),
			"block_magic_byte":     word("NotACook"),
			"block_magic_swapped":  word("ForeignOrder"),
			"block_byte_order":     word("NotACook"),
			"block_build":          word("WrongBuildVersion"),
			"block_pitch":          word("BadLayout"),
			"block_padding":        word("Truncated"),
			"block_lead":           word("UnalignedBase"),
		}
		for name, w := range want {
			if got[name] != w {
				t.Errorf("%s: Open's reason is %q, the reference leg's is %q", name, got[name], w)
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
  stdout.writeln('$name ${r.$2}');
}

void block(String name, Uint8List? bytes, [int base = 0, int? extent]) {
  final r = blk.RenderFrameBlock.openWithReason(bytes, base, extent ?? (bytes == null ? 0 : bytes.length - base));
  stdout.writeln('$name ${r.$2}');
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

// engineLine is the Go engine's read of one wire, spelled as the probe spells
// the Dart read: the field's value as a Dart int or double, the widened count,
// and the reason the verdict maps to.
func engineLine(t *testing.T, m *tabletext.Model, st *ir.Struct, wire []byte, field string) string {
	t.Helper()
	inst := m.New(st)
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
		t.Fatalf("the Go engine refused the widening wire: %v", err)
	}
	reason := "ok"
	if report.Refused {
		reason = "unknown_form"
	} else if report.Malformed {
		reason = "wire_damaged"
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
		return fmt.Sprintf("%s %d %s", value, report.Widened, reason)
	}
	t.Fatalf("the engine's instance has no field %s", field)
	return ""
}

func runProbe(t *testing.T, dart, dir string, args ...string) []string {
	t.Helper()
	cmd := exec.Command(dart, append([]string{"--enable-asserts", "probe.dart"}, args...)...)
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

var goRefuseCase = regexp.MustCompile(`case TableRefuse(\w+):return "(\w+)"`)

// goRefuseWords reads the reference leg's vocabulary out of its own source:
// each TableRefuse* constant's Error() string, keyed by the constant's name.
func goRefuseWords(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range goRefuseCase.FindAllStringSubmatch(goRefusalSource(t), -1) {
		out[m[1]] = m[2]
	}
	if len(out) == 0 {
		t.Fatal("no TableRefuse* Error() strings in internal/codegen/gotable/refusal.go")
	}
	return out
}

// goRefuseOrder is the same vocabulary in declaration order.
func goRefuseOrder(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, m := range goRefuseCase.FindAllStringSubmatch(goRefusalSource(t), -1) {
		out = append(out, m[2])
	}
	return out
}

func goRefusalSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "gotable", "refusal.go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

var dartRefuseCase = regexp.MustCompile(`(?m)^  \w+\('(\w+)'\)[,;]$`)

// dartRefuseWords is the generated enum's words, in declaration order.
func dartRefuseWords(src string) []string {
	start := strings.Index(src, "enum TableRefuseReason {")
	end := strings.Index(src[start:], "\n}\n")
	var out []string
	for _, m := range dartRefuseCase.FindAllStringSubmatch(src[start:start+end], -1) {
		out = append(out, m[1])
	}
	return out
}
