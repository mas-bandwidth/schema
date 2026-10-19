package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"maps"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func loadInlineDartUnit(t *testing.T, name, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse(name, []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", name, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  name,
		Name:  name,
		Base:  strings.TrimSuffix(filepath.Base(name), ".schema"),
		Bytes: []byte(src),
		AST:   f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", name, cerrs[0])
	}
	return u
}

var webSafeIntPat = regexp.MustCompile(`0x[0-9a-fA-F]{14,}|-?\b\d{15,}\b`)

func foldWebSafeDartSource(src string) string {
	const safe = (1 << 53) - 1
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		code, comment, hasComment := strings.Cut(line, "//")
		isBound := strings.Contains(code, "report.clamped++") ||
			strings.Contains(code, "_jsonIntField(") ||
			strings.Contains(code, "clamp(")
		code = webSafeIntPat.ReplaceAllStringFunc(code, func(tok string) string {
			if strings.HasPrefix(tok, "0x") || strings.HasPrefix(tok, "0X") {
				u, uerr := strconv.ParseUint(tok[2:], 16, 64)
				if uerr != nil || u <= safe {
					return tok
				}
				return strconv.FormatUint(u&0x7fffffff, 10)
			}
			v, err := strconv.ParseInt(tok, 10, 64)
			if err != nil {
				u, uerr := strconv.ParseUint(tok, 10, 64)
				if uerr != nil {
					return tok
				}
				if u <= safe {
					return tok
				}
				if isBound {
					return strconv.FormatInt(safe, 10)
				}
				return strconv.FormatUint(u&0x7fffffff, 10)
			}
			if v >= -safe && v <= safe {
				return tok
			}
			if isBound {
				if v > 0 {
					return strconv.FormatInt(safe, 10)
				}
				return strconv.FormatInt(-safe, 10)
			}
			return strconv.FormatInt(int64(uint64(v)&0x7fffffff), 10)
		})
		if hasComment {
			lines[i] = code + "//" + comment
		} else {
			lines[i] = code
		}
	}
	return strings.Join(lines, "\n")
}

func findNodeBinary(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("node"); err == nil {
		return p
	}
	for _, cand := range []string{
		"/home/glenn/sdk/node-v26.7.0/bin/node",
		"/home/glenn/sdk/bin/node",
	} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "1" {
		t.Fatal("tool missing: node (SCHEMA_REQUIRE_CORPUS=1)")
	}
	t.Skip("tool missing: node")
	return ""
}

// TestDartEmittedSitesAreWebExact pins the emitted Dart table codecs across
// wireunion.go, fixedruntime.go, blockruntime.go, cookruntime.go, wireform.go,
// and retainwire.go so no 64-bit integer, count, length, tag, or float32-NaN
// widening uses a 32-bit-truncating shift or Uint64 ByteData accessor under
// dart2js (docs/SPEC-TABLES.md §15). It also runs the generated code on both
// the Dart VM and dart2js (node) over values in 2^32..2^53-1 and verifies that
// each site's reversed witness goes red under dart2js.
func TestDartEmittedSitesAreWebExact(t *testing.T) {
	t.Parallel()

	const probeSchema = `package webprobe

union Payload {
    wide int64 | min = -9007199254740991, max = 9007199254740991
    uwide uint64 | min = 0, max = 9007199254740991
    narrow int32
    items [..4]int64 | min = -9007199254740991, max = 9007199254740991
}

table Envelope {
    choice Payload
    counts [..4]uint64 | min = 0, max = 9007199254740991
    metric float64
    tag int64 | min = -9007199254740991, max = 9007199254740991
}

fixed table Sample {
    signal float32
    widened float64
    stamp uint64 | min = 0, max = 9007199254740991
}
`
	u := loadInlineDartUnit(t, "WebProbe.schema", probeSchema)
	pktFiles, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("dart.Generate(WebProbe): %v", err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate(WebProbe): %v", err)
	}
	maps.Copy(files, pktFiles)
	wireSrc := string(files["WebProbeWire.dart"])
	fixedSrc := string(files["WebProbeFixed.dart"])
	blockSrc := string(files["WebProbeBlock.dart"])
	cookSrc := string(files["WebProbeCook.dart"])
	if wireSrc == "" || fixedSrc == "" || blockSrc == "" || cookSrc == "" {
		t.Fatalf("missing generated files: wire=%d fixed=%d block=%d cook=%d",
			len(wireSrc), len(fixedSrc), len(blockSrc), len(cookSrc))
	}

	// (a) wireunion.go: union scalar arm and union array element reads.
	for _, bad := range []string{
		"for (var b = 0; b < width; b++) { raw |= p.readByte() << (8 * b); }",
		"for (var b = 0; b < 8; b++) { raw |= p.readByte() << (8 * b); }",
	} {
		if strings.Contains(wireSrc, bad) {
			t.Errorf("site (a) wireunion.go still emits 32-bit-truncating shift %q", bad)
		}
	}
	for _, want := range []string{
		"if (width == 8) { raw = readU64(p.data, p.pos); p.pos += 8; } else { var scale = 1; for (var b = 0; b < width; b++) { raw += p.readByte() * scale; scale *= 256; } }",
		"raw = readU64(p.data, p.pos); p.pos += 8;",
	} {
		if !strings.Contains(wireSrc, want) {
			t.Errorf("site (a) wireunion.go missing web-exact form %q", want)
		}
	}

	// (b) blockruntime.go & cookruntime.go: 64-bit prologue/header word reads.
	if strings.Contains(blockSrc, "value = (value << 8) | view.getUint8(at + i);") {
		t.Error("site (b) blockruntime.go still shifts 64-bit word with (value << 8) | view.getUint8")
	}
	if strings.Contains(cookSrc, "value = (value << 8) | view.getUint8(at + i);") {
		t.Error("site (b) cookruntime.go still shifts 64-bit word with (value << 8) | view.getUint8")
	}
	for _, src := range []struct {
		name, text string
	}{
		{"WebProbeBlock.dart", blockSrc},
		{"WebProbeCook.dart", cookSrc},
	} {
		if !strings.Contains(src.text, "return (hi - 0x100000000) * 0x100000000 + lo;") {
			t.Errorf("site (b) %s missing 32-bit half-word reconstruction", src.name)
		}
	}

	// (c) fixedruntime.go: widenFloat NaN payload widening, tableFixedTagAt, and enum read loop.
	for _, bad := range []string{
		"final sign = (bits & 0x80000000) << 32;",
		"conv.setUint64(0, sign | exp | mant, Endian.little);",
		"v |= source[at + i] << (8 * i);",
		"raw |= source[s + k] << (8 * k);",
	} {
		if strings.Contains(fixedSrc, bad) {
			t.Errorf("site (c) fixedruntime.go still emits web-unsafe construct %q", bad)
		}
	}
	for _, want := range []string{
		"conv.setUint32(0, lo, Endian.little);",
		"conv.setUint32(4, hi, Endian.little);",
		"v += source[at + i] * scale;",
		"raw += source[s + k] * scale;",
	} {
		if !strings.Contains(fixedSrc, want) {
			t.Errorf("site (c) fixedruntime.go missing web-exact construct %q", want)
		}
	}

	// (d) wireform.go & retainwire.go: putLeb, readLeb, _f64Bits/_f64FromBits, _u64le, _lebOut, _RetainCursor.u64, _retainEmitPayload.
	for _, bad := range []string{
		"v |= (b & 0x7F) << (7 * i);",
		"len |= (b & 0x7F) << shift;",
		"v |= b[at + i] << (8 * i);",
		"return bd.getUint64(0, Endian.little);",
		"bd.setUint64(0, b, Endian.little);",
	} {
		if strings.Contains(wireSrc, bad) {
			t.Errorf("site (d) wireform.go / retainwire.go still emits web-unsafe construct %q", bad)
		}
	}
	for _, want := range []string{
		"v += (b & 0x7F) * scale;",
		"len += (b & 0x7F) * scale;",
		"bd.setUint32(0, readU32(d, off), Endian.little);",
		"bd.setUint32(4, readU32(d, off + 4), Endian.little);",
	} {
		if !strings.Contains(wireSrc, want) {
			t.Errorf("site (d) wireform.go / retainwire.go missing web-exact construct %q", want)
		}
	}

	// Check SPEC-TABLES.md documents the 2^53 - 1 rule.
	specBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "SPEC-TABLES.md"))
	if err != nil {
		t.Fatalf("read docs/SPEC-TABLES.md: %v", err)
	}
	if !strings.Contains(string(specBytes), "2^53 - 1") {
		t.Error("docs/SPEC-TABLES.md does not state the 2^53 - 1 web exactness and refusal rule")
	}

	// Functional VM vs dart2js differential + reversed witnesses per site.
	dartBin := dartBinary(t)
	nodeBin := findNodeBinary(t)
	dir := t.TempDir()

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(foldWebSafeDartSource(string(content))), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	const driverSrc = `import 'dart:typed_data';
import 'WebProbeWire.dart';
import 'WebProbeFixed.dart' as fixed;
import 'WebProbeBlock.dart' as block;
import 'WebProbeCook.dart' as cook;

String hex(List<int> bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

int combineLoHi(int lo, int hi) {
  if (hi >= 0x80000000) {
    return (hi - 0x100000000) * 0x100000000 + lo;
  }
  return hi * 0x100000000 + lo;
}

void main() {
  final bad = <String>[];
  void sameInt(String label, int want, int got) {
    if (want != got) bad.add('$label=$got(want $want)');
  }

  // Site (a): union int64 arm and int64[] array arm across 2^32+5, 2^40+3, 2^53-1, -(2^32+5).
  final cases = <int>[4294967301, 1099511627779, 9007199254740991, -4294967301];
  final unionOut = <int>[];
  for (final want in cases) {
    final env = Envelope();
    env.choice = Payload()..armTag = 1..wide = want;
    env.counts = <int>[4294967301, 9007199254740991];
    env.metric = 1.5000000000000002; // low mantissa bit set
    env.tag = want;
    final wire = envelopeWireSave(env);
    final rep = TableWireReport();
    final back = envelopeWireLoad(wire, rep);
    final got = back.choice.wide;
    unionOut.add(got);
    sameInt('union_i64[$want]', want, got);
    sameInt('union_clamped[$want]', 0, rep.clamped);
    sameInt('counts[0]', 4294967301, back.counts[0]);
    sameInt('counts[1]', 9007199254740991, back.counts[1]);
    if (back.metric != env.metric) {
      bad.add('f64_roundtrip=${back.metric}(want ${env.metric})');
    }
  }

  // Union array arm (wireunion.go:724 emitGetElem).
  final arrEnv = Envelope();
  arrEnv.choice = Payload()..armTag = 4..items = List<int>.from(cases);
  final arrWire = envelopeWireSave(arrEnv);
  final arrRep = TableWireReport();
  final arrBack = envelopeWireLoad(arrWire, arrRep);
  final gotItems = arrBack.choice.items;
  for (var i = 0; i < cases.length; i++) {
    sameInt('union_arr[$i]', cases[i], i < gotItems.length ? gotItems[i] : 0);
  }

  // Site (b): blockruntime.go tableBlockRead64, cookruntime.go tableCookRead64, and lo/hi 32-bit combine.
  final bbuf = Uint8List(16);
  final bview = ByteData.sublistView(bbuf);
  // 4294967301 = 0x00000001_00000005
  bview.setUint32(0, 5, Endian.little);
  bview.setUint32(4, 1, Endian.little);
  // 9007199254740991 = 0x001fffff_ffffffff
  bview.setUint32(8, 0xffffffff, Endian.little);
  bview.setUint32(12, 0x001fffff, Endian.little);
  final b0 = block.tableBlockRead64(bview, 0);
  final b1 = block.tableBlockRead64(bview, 8);
  final c0 = cook.tableCookRead64(bview, 0);
  final c1 = cook.tableCookRead64(bview, 8);
  sameInt('block_read64[0]', 4294967301, b0);
  sameInt('block_read64[1]', 9007199254740991, b1);
  sameInt('cook_read64[0]', 4294967301, c0);
  sameInt('cook_read64[1]', 9007199254740991, c1);
  sameInt('combine_lohi[0]', 4294967301, combineLoHi(5, 1));
  sameInt('combine_lohi[1]', 9007199254740991, combineLoHi(0xffffffff, 0x001fffff));

  // Site (c): fixedruntime.go widenFloat on negative signaling f32 NaN 0xff800001 -> f64 0xfff00000_20000000,
  // plus tableFixedTagAt at width 8 on 4294967301 and 9007199254740991.
  final tag0 = fixed.tableFixedTagAt(bbuf, 0, 8);
  final tag1 = fixed.tableFixedTagAt(bbuf, 8, 8);
  sameInt('fixed_tag_at[0]', 4294967301, tag0);
  sameInt('fixed_tag_at[1]', 9007199254740991, tag1);

  final srcImg = Uint8List(32);
  final srcView = ByteData.sublistView(srcImg);
  srcView.setUint32(0, 0xff800001, Endian.little); // negative sNaN, bit 31 set, payload=1
  final dstImg = Uint8List(32);
  final convScratch = ByteData(8);
  final plan = Int32List.fromList(<int>[
    fixed.TableFixedOp.widenFloat, 0, 0, 4, 0, -1, 0, 0, 1, -1, 0, 1, 0, 0,
  ]);
  final frep = fixed.TableFixedReport();
  fixed.tableFixedRun(
    plan,
    1,
    srcImg,
    srcView,
    0,
    dstImg,
    ByteData.sublistView(dstImg),
    Int32List(0),
    convScratch,
    frep,
  );
  final dstView = ByteData.sublistView(dstImg);
  final nanLo = dstView.getUint32(0, Endian.little);
  final nanHi = dstView.getUint32(4, Endian.little);
  sameInt('widen_f32_nan_lo', 0x20000000, nanLo);
  sameInt('widen_f32_nan_hi', 0xfff00000, nanHi);

  // Site (d): putLeb / readLeb on unsigned LEB128 values up to 2^53-1.
  final lebCases = <int>[0, 127, 128, 4294967301, 1099511627779, 9007199254740991];
  final w = TableWireWriter(WireState());
  for (final v in lebCases) {
    w.putLeb(v);
  }
  final wBytes = Uint8List.fromList(w.buf);
  final r = TableWireReader(wBytes, wBytes.length);
  for (final want in lebCases) {
    final got = r.readLeb() ?? 0;
    sameInt('leb[$want]', want, got);
  }

  print('union=${unionOut.join(",")}');
  print('arr=${gotItems.join(",")}');
  print('block=$b0,$b1');
  print('cook=$c0,$c1');
  print('tag=$tag0,$tag1');
  print('nan=${nanHi.toRadixString(16)}_${nanLo.toRadixString(16)}');
  print('roundtrip=${bad.isEmpty ? "ok" : "BAD:${bad.join(";")}"}');
}
`
	driverPath := filepath.Join(dir, "main.dart")
	if err := os.WriteFile(driverPath, []byte(driverSrc), 0o644); err != nil {
		t.Fatalf("write main.dart: %v", err)
	}

	runBoth := func(workDir string) (vmOut, jsOut string, err error) {
		vmCmd := exec.Command(dartBin, filepath.Join(workDir, "main.dart"))
		vb, verr := vmCmd.CombinedOutput()
		if verr != nil {
			return string(vb), "", verr
		}
		jsPath := filepath.Join(workDir, "main.js")
		compCmd := exec.Command(dartBin, "compile", "js", "-O2", "-o", jsPath, filepath.Join(workDir, "main.dart"))
		if cb, cerr := compCmd.CombinedOutput(); cerr != nil {
			t.Fatalf("dart compile js failed: %v\n%s", cerr, cb)
		}
		nodeCmd := exec.Command(nodeBin, jsPath)
		jb, jerr := nodeCmd.CombinedOutput()
		if jerr != nil {
			return string(vb), string(jb), jerr
		}
		return string(vb), string(jb), nil
	}

	vmOut, jsOut, err := runBoth(dir)
	if err != nil {
		t.Fatalf("runBoth failed: %v\nvm:\n%s\njs:\n%s", err, vmOut, jsOut)
	}
	if vmOut != jsOut {
		t.Fatalf("VM and dart2js transcripts diverged:\n--- VM ---\n%s--- dart2js ---\n%s", vmOut, jsOut)
	}
	if !strings.Contains(jsOut, "roundtrip=ok\n") {
		t.Fatalf("roundtrip not ok on dart2js:\n%s", jsOut)
	}

	// Reversed witnesses per site: planting the old 32-bit shift at each site
	// must turn the dart2js transcript RED naming the corrupted value.
	witnesses := []struct {
		name      string
		file      string
		oldText   string
		plantText string
		wantBad   string
	}{
		{
			name:      "site_a_wireunion_arm",
			file:      "WebProbeWire.dart",
			oldText:   "if (width == 8) { raw = readU64(p.data, p.pos); p.pos += 8; } else { var scale = 1; for (var b = 0; b < width; b++) { raw += p.readByte() * scale; scale *= 256; } }",
			plantText: "for (var b = 0; b < width; b++) { raw |= p.readByte() << (8 * b); }",
			wantBad:   "union_i64[4294967301]=5(want 4294967301)",
		},
		{
			name:      "site_a_wireunion_elem",
			file:      "WebProbeWire.dart",
			oldText:   "raw = readU64(p.data, p.pos); p.pos += 8;",
			plantText: "for (var b = 0; b < 8; b++) { raw |= p.readByte() << (8 * b); }",
			wantBad:   "union_arr[0]=5(want 4294967301)",
		},
		{
			name:    "site_b_block_cook_read64",
			file:    "WebProbeBlock.dart",
			oldText: "var lo = 0;\n  var hi = 0;\n  for (var i = 0; i < 4; i++) {\n    lo |= view.getUint8(at + i) << (8 * i);\n    hi |= view.getUint8(at + 4 + i) << (8 * i);\n  }\n  if (hi >= 0x80000000) {\n    return (hi - 0x100000000) * 0x100000000 + lo;\n  }\n  return hi * 0x100000000 + lo;",
			plantText: "var value = 0;\n  for (var i = 7; i >= 0; i--) {\n    value = (value << 8) | view.getUint8(at + i);\n  }\n  return value;",
			wantBad: "block_read64[0]=5(want 4294967301)",
		},
		{
			name:    "site_c_fixed_widen_float_nan",
			file:    "WebProbeFixed.dart",
			oldText: "final lo = (bits & 0x7) << 29; // 23 bits up to 52\n          final hi = (bits & 0x80000000) | 0x7ff00000 | ((bits & 0x007fffff) >>> 3);\n          conv.setUint32(0, lo, Endian.little);\n          conv.setUint32(4, hi, Endian.little);",
			plantText: "final sign = (bits & 0x80000000) << 32;\n          final signHi = (sign ~/ 0x100000000) & 0x80000000;\n          final lo = (bits & 0x7) << 29;\n          final hi = signHi | 0x7ff00000 | ((bits & 0x007fffff) >>> 3);\n          conv.setUint32(0, lo, Endian.little);\n          conv.setUint32(4, hi, Endian.little);",
			wantBad: "widen_f32_nan_hi=2146435072(want 4293918720)",
		},
		{
			name:    "site_d_wireform_readleb",
			file:    "WebProbeWire.dart",
			oldText: "if (i == 9) {\n        v |= (b & 0x7F) << 63;\n      } else {\n        v += (b & 0x7F) * scale;\n      }",
			plantText: "v |= (b & 0x7F) << (7 * i);",
			wantBad: "leb[4294967301]=",
		},
	}

	for _, w := range witnesses {
		pdir := t.TempDir()
		for name, content := range files {
			text := foldWebSafeDartSource(string(content))
			if name == w.file {
				if !strings.Contains(text, w.oldText) {
					t.Fatalf("witness %s: oldText not found in %s", w.name, w.file)
				}
				text = strings.ReplaceAll(text, w.oldText, w.plantText)
			}
			if err := os.WriteFile(filepath.Join(pdir, name), []byte(text), 0o644); err != nil {
				t.Fatalf("witness %s write %s: %v", w.name, name, err)
			}
		}
		if err := os.WriteFile(filepath.Join(pdir, "main.dart"), []byte(driverSrc), 0o644); err != nil {
			t.Fatalf("witness %s write main.dart: %v", w.name, err)
		}
		_, pjs, err := runBoth(pdir)
		if err != nil {
			t.Fatalf("witness %s runBoth error: %v", w.name, err)
		}
		if !strings.Contains(pjs, w.wantBad) {
			t.Fatalf("witness %s did not go red with %q on dart2js:\n%s", w.name, w.wantBad, pjs)
		}
	}
}
