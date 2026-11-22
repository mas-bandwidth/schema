package javatable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestJavaCompressedFloatStoresAreGuarded is the fast twin of
// TestJavaCompressedNegZeroCorpus (docs/SPEC-TABLES.md §3.3, docs/rulings.json
// fixed-compressed-negzero): it reads the generated Java for the corpus's
// Probe.schema rather than compiling it. Every float store of the fixed
// writer and the JSON writer writes a compressed float through the +0
// normalisation, `(x == 0.0f ? 0.0f : x)`, which is plain code and so holds
// without -ea; the variable writer's putF32In and putF64In carry the same
// test; the message form's elision compares a compressed float by value;
// every compressed field's cook descriptor is `.ranged()`, which the cook's
// float store turns into +0. The plain control `p` is stored, elided and
// cooked by its bits, so its -0 rides.
func TestJavaCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(javaNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	squeeze := func(s string) string { return strings.Join(strings.Fields(s), "") }
	text := map[string]string{}
	var all strings.Builder
	for _, name := range sortedNames(files) {
		text[name] = squeeze(string(files[name]))
		all.WriteString(text[name])
	}
	isPlain := regexp.MustCompile(`^v\.p$`)
	const normalised = "==0.0f?0.0f:"

	// THE FIXED AND JSON WRITERS: every float operand is normalised, except
	// the plain control's
	writers := map[string]*regexp.Regexp{
		"fixed": regexp.MustCompile(`TableFixed\.put32\([^;]*?\.floatToRawIntBits\((.*?)\)\);`),
		"json":  regexp.MustCompile(`TableJson\.writeFloat\(b,(.*?),true\);`),
	}
	for writer, re := range writers {
		guarded, plain := 0, 0
		for _, m := range re.FindAllStringSubmatch(all.String(), -1) {
			operand := m[1]
			switch {
			case isPlain.MatchString(operand):
				plain++
			case strings.Contains(operand, normalised):
				guarded++
			default:
				t.Errorf("the %s writer stores a compressed float as held: %s", writer, m[0])
			}
		}
		if guarded == 0 || plain == 0 {
			t.Errorf("the %s writer has %d normalised compressed stores and %d plain ones; want both", writer, guarded, plain)
		}
	}

	// THE VARIABLE WRITER: a compressed float is put through putF32In, whose
	// runtime body normalises, and the plain control through putF32
	src2 := all.String()
	for _, want := range []string{
		"putF32(v==0.0f?0.0f:v);",
		"putF64(v==0.0?0.0:v);",
		"w.putF32(v.p);",
	} {
		if !strings.Contains(src2, want) {
			t.Errorf("the generated variable writer lacks %s", want)
		}
	}
	if n := strings.Count(src2, ".putF32In("); n == 0 {
		t.Error("the generated variable writer puts no compressed float through putF32In")
	}

	// THE MESSAGE FORM: a float elided by its bits is the plain control's
	bitsElided := regexp.MustCompile(`java\.lang\.Float\.floatToRawIntBits\(([^()]*?)\)!=`)
	plainElided := 0
	for _, m := range bitsElided.FindAllStringSubmatch(text["FixMessage.java"], -1) {
		if !isPlain.MatchString(m[1]) {
			t.Errorf("the message form elides a compressed float by its bits: %s", m[0])
			continue
		}
		plainElided++
	}
	if plainElided == 0 {
		t.Error("the message form elides the plain control by no bit test")
	}

	// THE COOK: every compressed field's descriptor is ranged, the plain
	// control's and the integer arm's are not, and the store honours it
	ranged := map[string]bool{"a": true, "z": true, "xs": true, "ys": true, "ks": true, "v": true, "p": false, "n": false, "pick": false}
	info := regexp.MustCompile(`TableCookFieldInfo\.of\("(\w+)"`)
	seen := map[string]int{}
	for name, body := range text {
		for _, at := range info.FindAllStringSubmatchIndex(body, -1) {
			field := body[at[2]:at[3]]
			want, ok := ranged[field]
			if !ok {
				continue
			}
			end := strings.IndexByte(body[at[0]:], ';')
			if end < 0 {
				t.Fatalf("%s: an unterminated cook descriptor for %s", name, field)
			}
			if got := strings.Contains(body[at[0]:at[0]+end], ".ranged()"); got != want {
				t.Errorf("%s: the cook descriptor of %s is ranged=%v, want %v", name, field, got, want)
			}
			seen[field]++
		}
	}
	for field := range ranged {
		if seen[field] == 0 {
			t.Errorf("no cook descriptor names %s", field)
		}
	}
	for _, want := range []string{
		"if(ranged&&raw==(width==4?0x80000000L:0x8000000000000000L)){return0;}",
		"bits(source,f.storage,f.elemSize,f.ranged)",
	} {
		if !strings.Contains(src2, want) {
			t.Errorf("the generated cook runtime lacks %s", want)
		}
	}
}
