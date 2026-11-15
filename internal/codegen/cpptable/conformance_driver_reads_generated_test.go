package cpptable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cppDriverWalkNames are the names a driver-side wire walk was spelled with:
// the strict form-1 verdict belongs to the generated read (TableWireStrict,
// wirestrict.go), so none of them may stand in the driver.
var cppDriverWalkNames = []string{
	"framing_strict_ok",
	"FramingWalk",
	"FramingRecord",
	"framing_u64le",
	"framing_name_id",
	"framing_leb",
	"framing_fixed_kind",
	"framing_kind_width",
	"framing_scalar",
	"framing_text_valid",
	"framing_body",
	"framing_payload",
	"framing_content",
	"framing_array_header",
	"framing_array",
	"framing_keyed",
	"framing_union",
	"framing_scan_records",
	"framing_graph",
}

// cppDriverFunction answers the body of one top-level C++ function of the
// driver, from its signature's line to the brace that closes it at column 0.
func cppDriverFunction(t *testing.T, src, name string) string {
	t.Helper()
	at := regexp.MustCompile(`(?m)^static [^\n(]*\b` + regexp.QuoteMeta(name) + `\(`).FindStringIndex(src)
	if at == nil {
		t.Fatalf("test/conformance/cpp/main.cpp defines no %s", name)
	}
	rest := src[at[0]:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("%s in test/conformance/cpp/main.cpp has no closing brace at column 0", name)
	}
	return rest[:end+3]
}

// TestCppConformanceDriverReadsThroughGeneratedReader holds the C++
// conformance driver to the one-read rule (test/conformance/README.md;
// docs/SPEC-TABLES.md §6.5): no function in it decides a form-1 wire's
// verdict before or instead of the generated load. No wire walk of its own
// stands in the file, and read_answer's verdict is the generated load's
// return and its report, with nothing else that sets it.
func TestCppConformanceDriverReadsThroughGeneratedReader(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "cpp", "main.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, name := range cppDriverWalkNames {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(src) {
			t.Errorf("test/conformance/cpp/main.cpp spells %s: a wire walk in the driver decides the verdict the generated read owes", name)
		}
	}

	read := cppDriverFunction(t, src, "read_answer")
	// the verdict: exactly the two generated loads' answers, each taken
	// with the load's own report
	verdicts := regexp.MustCompile(`(?m)^\s*ok\s*=([^;]*);`).FindAllStringSubmatch(read, -1)
	want := map[string]bool{
		"root != NULL && !report.refused": false,
		"( form1 ? codec->load_form1 : codec->load )( value, wire.data(), (int64_t) wire.size(), &report ) && !report.refused": false,
	}
	for _, v := range verdicts {
		got := strings.TrimSpace(v[1])
		if _, ok := want[got]; !ok {
			t.Errorf("read_answer sets its verdict from %q: the verdict is the generated load's return and its report alone", got)
			continue
		}
		want[got] = true
	}
	for v, seen := range want {
		if !seen {
			t.Errorf("read_answer never sets its verdict from %q", v)
		}
	}
	if !regexp.MustCompile(`const void \* root = variable->load\( region, wire\.data\(\), \(int64_t\) wire\.size\(\), &report \);`).MatchString(read) {
		t.Error("read_answer's region root is not the generated load's own answer: it must be `const void * root = variable->load( region, wire.data(), (int64_t) wire.size(), &report );`")
	}
	// nothing in read_answer writes the report the verdict is read from
	if m := regexp.MustCompile(`report\.[a-z_]+\s*=[^=]`).FindString(read); m != "" {
		t.Errorf("read_answer writes the load's report itself (%q): a refusal is the generated read's to make", m)
	}
}
