package darttable

// THE WEB-INT PIN FOR THE WIDE WIRE HELPERS.
//
// The VM / dart2js differential (make/schema-dart-vm-dart2js-differential.mk)
// runs the generated codec on both runtimes. This unit test pins the emitted
// spelling that keeps it equal: dart2js computes << and >> as 32-bit
// operators, so the old putU64/readU64 lost every u64 from 2^32 up (2^32+5
// wrote as 5) and _signExtend turned a negative int32 into zero. The failing
// lines are kept in test/dart-differential/webints.dart; this test reds if the
// generator regresses to a shift of 32 or more.
import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDartWireWideIntHelpersAreWebSafe(t *testing.T) {
	t.Parallel()

	u := loadUnit(t, filepath.Join("..", "..", "..", "tables", "examples", "Ranges.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate refused the Ranges unit: %v", err)
	}
	src := string(files["TabledemoWire.dart"])
	if src == "" {
		t.Fatal("Generate emitted no TabledemoWire.dart for tables/examples/Ranges.schema")
	}
	for _, want := range []string{
		"final m = 0x100000000;",
		"if (hi >= 0x80000000) {",
		"return raw >= 0x80000000 ? raw - 0x100000000 : raw;",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the wide-integer helpers are not web-safe: the emitted code lacks %q", want)
		}
	}
	if strings.Contains(src, "return (raw << shift) >> shift;") {
		t.Error("_signExtend still shifts by 64-width*8: dart2js computes a shift of 32 or more with its 32-bit operator and every negative int32 reads as zero")
	}

	driver, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "dart-differential", "webints.dart"))
	if err != nil {
		t.Fatalf("read the runtime lane driver: %v", err)
	}
	for _, want := range []string{"4294967301", "9007199254740991"} {
		if !strings.Contains(string(driver), want) {
			t.Errorf("webints.dart lost the web-exact value %s that was red at BASE", want)
		}
	}
}
