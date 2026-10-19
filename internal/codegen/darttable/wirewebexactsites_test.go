package darttable

// THE WEB-EXACT PIN FOR THE REMAINING 64-BIT SITES (docs/SPEC-TABLES.md, THE
// WEB TARGET).
//
// Under dart2js an int is a double and the shift and bit operators are 32-bit,
// so a shift of 32 or more, or a bit operation on a value past 2^32, silently
// loses the high bits. The putU64/readU64/_signExtend helpers are pinned by
// TestDartWireWideIntHelpersAreWebSafe; this test pins the other sites the
// generator emits on the web-capable variable form, and the fixed form's
// float32 widening:
//
//   (a) the union integer arm read (wireunion.go) and its 8-byte array element
//       read: readU64's exact arithmetic for width 8, with the web refusal;
//   (c) the fixed runtime's widenFloat: the f64 laid as two 32-bit words;
//   (d) readLeb and the retain cursor's id and framed length: exact scaling,
//       with the web refusal past 2^53 - 1.
//
// The runtime proof is the VM-versus-dart2js lane
// (make tables-dart-vm-dart2js-differential, test/dart-differential/webints.dart);
// this is the pure-Go text check that reds at BASE on each site.
import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func generateDart(t *testing.T, rel ...string) map[string][]byte {
	t.Helper()
	u := loadUnit(t, filepath.Join(append([]string{"..", "..", ".."}, rel...)...))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate refused %s: %v", filepath.Join(rel...), err)
	}
	return files
}

// dartFunc returns the emitted Dart function or method whose header line
// starts with header, through its closing brace at the header's indent.
func dartFunc(src, header string) string {
	i := strings.Index(src, header)
	if i < 0 {
		return ""
	}
	lineStart := strings.LastIndex(src[:i], "\n") + 1
	indent := src[lineStart:i]
	rest := src[i:]
	end := strings.Index(rest, "\n"+indent+"}\n")
	if end < 0 {
		return rest
	}
	return src[lineStart : i+end+len(indent)+3]
}

func TestDartEmittedSitesAreWebExact(t *testing.T) {
	t.Parallel()

	// (a) THE UNION INT64 ARM: test/tables/A2.schema's union Value carries
	// `a int64`, read through wireunion.go's integer arm.
	a2 := string(generateDart(t, "test", "tables", "A2.schema")["Tbla2Wire.dart"])
	if a2 == "" {
		t.Fatal("Generate emitted no Tbla2Wire.dart for test/tables/A2.schema")
	}
	arm := dartFunc(a2, "bool _valueArm1(")
	if arm == "" {
		t.Fatal("Tbla2Wire.dart has no _valueArm1 (the int64 arm of union Value)")
	}
	for _, want := range []string{
		"if (width == 8) {",
		"if (_webInexact64(p.data, p.pos, true)) { p.report.refused = true; p.pos = p.limit; return true; }",
		"raw = readU64(p.data, p.pos);",
	} {
		if !strings.Contains(arm, want) {
			t.Errorf("site (a) wireunion.go union int arm: the emitted read lacks %q; an 8-byte arm loses bytes 4..7 under dart2js:\n%s", want, arm)
		}
	}
	// the byte loop may stand only in the else branch, for widths 1, 2 and 4
	loop := "for (var b = 0; b < width; b++) { raw |= p.readByte() << (8 * b); }"
	if j := strings.Index(arm, loop); j >= 0 && !strings.Contains(arm[:j], "} else {") {
		t.Errorf("site (a): the shift loop reads every width, 8 included:\n%s", arm)
	}
	if regexp.MustCompile(`for \(var b = 0; b < 8; b\+\+\) \{ raw \|=`).MatchString(a2) {
		t.Error("site (a) wireunion.go array element: an 8-byte element is still assembled by shifts up to 56")
	}

	// (d) readLeb and the retain cursor, in the same emitted runtime.
	leb := dartFunc(a2, "int? readLeb() {")
	if leb == "" {
		t.Fatal("Tbla2Wire.dart has no readLeb")
	}
	if strings.Contains(leb, "<< (7 * i)") {
		t.Errorf("site (d) readLeb: a group is still shifted by 7*i (32-bit under dart2js from i=5):\n%s", leb)
	}
	for _, want := range []string{"v += (b & 0x7F) * scale;", "if (_webInts && v > 9007199254740991) return null;"} {
		if !strings.Contains(leb, want) {
			t.Errorf("site (d) readLeb lacks %q", want)
		}
	}
	u64 := dartFunc(a2, "int? u64() {")
	if u64 == "" {
		t.Fatal("Tbla2Wire.dart has no _RetainCursor.u64")
	}
	if strings.Contains(u64, "for (var i = 0; i < 8; i++)") {
		t.Errorf("site (d) _RetainCursor.u64 still shifts bytes 4..7 by 32..56:\n%s", u64)
	}
	if !strings.Contains(u64, "if (_webInts && hi > 0x1FFFFF) return null;") {
		t.Error("site (d) _RetainCursor.u64 lacks the web refusal past 2^53 - 1")
	}
	if strings.Contains(a2, "len |= (b & 0x7F) << shift;") {
		t.Error("site (d) retained framed length: a LEB group is still shifted")
	}
	for _, want := range []string{
		"const bool _webInts = identical(0, 0.0);",
		"bool _webInexact64(Uint8List d, int off, bool signed) {",
	} {
		if !strings.Contains(a2, want) {
			t.Errorf("the web range helper is not emitted: lacks %q", want)
		}
	}

	// (c) THE FIXED FORM'S FLOAT32 WIDENING, in the wide unit's fixed runtime.
	var fixed string
	for name, body := range generateDart(t, "examples-wide", "Caption.schema") {
		if strings.HasSuffix(name, "Fixed.dart") && strings.Contains(string(body), "case TableFixedOp.widenFloat:") {
			fixed = string(body)
		}
	}
	if fixed == "" {
		t.Fatal("no emitted *Fixed.dart for examples-wide/Caption.schema carries the widenFloat op")
	}
	if strings.Contains(fixed, "(bits & 0x80000000) << 32") {
		t.Error("site (c) fixedruntime.go widenFloat: the sign is still shifted by 32 (0 under dart2js)")
	}
	if !strings.Contains(fixed, "(bits & 0x80000000) | 0x7ff00000 | (mant >>> 3),") {
		t.Error("site (c) fixedruntime.go widenFloat: the f64's high word is not laid as 32 bits")
	}

	// THE RUNTIME LANE carries the values that were red at BASE.
	driver, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "dart-differential", "webints.dart"))
	if err != nil {
		t.Fatalf("read the runtime lane driver: %v", err)
	}
	for _, want := range []string{"4294967301", "1099511627779", "-4294967301", "unionRefused"} {
		if !strings.Contains(string(driver), want) {
			t.Errorf("webints.dart lost the union lane value %s", want)
		}
	}
}
