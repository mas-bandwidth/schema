package cpptable

// TestCPPJsonBase64Canonical pins docs/SPEC-TABLES.md §16.2 and §16.8 for the
// C++ JSON read of a bytes value (docs/rulings.json `json-base64-canonical`):
// the body is CANONICAL base64, the text the writer writes — the standard
// alphabet, padded, pad bits zero — and any other spelling that decodes is
// refused. The compiler's own engine (internal/tabletext) is the reference:
// every row's verdict is checked against it before the generated C++ is built,
// and the accepted rows are checked on the bytes they land.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cppJsonBase64CanonicalDir = "../../../testdata/wire/json-base64-canonical"

type cppBase64Case struct {
	name string
	text string
	ok   bool
	data []byte
}

// cppBase64VectorCases reads the landed vector manifest and asks the engine
// for each row's verdict and bytes. A row whose engine verdict disagrees with
// the manifest refutes the reference; it is not a product-code failure.
func cppBase64VectorCases(t *testing.T, model *tabletext.Model) []cppBase64Case {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(cppJsonBase64CanonicalDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []cppBase64Case
	for line := range strings.SplitSeq(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 5 {
			t.Fatalf("manifest row %q has %d columns", line, len(cols))
		}
		name, root, jsonFile, verdict := cols[0], cols[2], cols[3], cols[4]
		body, err := os.ReadFile(filepath.Join(cppJsonBase64CanonicalDir, jsonFile))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		st := model.Lookup(root)
		if st == nil {
			t.Fatalf("%s: no table %s", name, root)
		}
		inst := model.New(st)
		var report tabletext.Report
		ok := model.Read(inst, body, &report)
		want := verdict == "read"
		if ok != want || report.Refused != !want {
			t.Fatalf("refuted: engine %s on %s: ok=%t report=%+v; manifest says %s",
				name, jsonFile, ok, report, verdict)
		}
		c := cppBase64Case{name: name, text: string(body), ok: ok}
		if ok {
			fv, found := inst.FieldByKey("data")
			if !found {
				t.Fatalf("%s: no data field", name)
			}
			c.data = append([]byte(nil), fv.Cell.Str...)
			if name == "canonical-two-bytes-read" && !bytes.Equal(c.data, []byte("AB")) {
				t.Fatalf("refuted: engine %s read %x, want 41 42", name, c.data)
			}
		}
		cases = append(cases, c)
	}
	if len(cases) == 0 {
		t.Fatal("the vector manifest names no row")
	}
	return cases
}

// cppBase64EscapeCases asks the engine for the two canonical spellings written
// with JSON escapes (internal/tabletext/base64escape_test.go): escapes are
// undone before the alphabet sees the text, and the decoded bytes are what a
// canonical body spells.
func cppBase64EscapeCases(t *testing.T, model *tabletext.Model) []cppBase64Case {
	t.Helper()
	st := model.Lookup("ProfileConfig")
	if st == nil {
		t.Fatal("no ProfileConfig table")
	}
	cases := []cppBase64Case{
		{name: "escaped-solidus", text: `{ "icon": "\/\/\/\/" }`, ok: true, data: []byte{0xff, 0xff, 0xff}},
		{name: "escaped-letter", text: `{ "icon": "\u0041Q==" }`, ok: true, data: []byte{0x01}},
	}
	for i := range cases {
		c := &cases[i]
		inst := model.New(st)
		var report tabletext.Report
		ok := model.Read(inst, []byte(c.text), &report)
		if !ok || report.Refused {
			t.Fatalf("refuted: engine %s: ok=%t report=%+v", c.name, ok, report)
		}
		fv, found := inst.FieldByKey("icon")
		if !found {
			t.Fatalf("%s: no icon field", c.name)
		}
		if got := fv.Cell.Str; !bytes.Equal(got, c.data) {
			t.Fatalf("refuted: engine %s read %x, want %x", c.name, got, c.data)
		}
	}
	return cases
}

func cppWantArray(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return fmt.Sprintf("static const uint8_t want[] = { %s };", cppBytes(data))
}

func cppChunkCase(c cppBase64Case) string {
	var b strings.Builder
	fmt.Fprintf(&b, `
    {
        static const uint8_t text[] = { %s };
        ChunkBuilder builder; TableReport report;
        bool ok = ChunkFromJson( builder, (const char *) text, (int64_t) sizeof(text), &report );
        CHECK(ok == %t, "%s verdict");
`, cppBytes([]byte(c.text)), c.ok, c.name)
	if c.ok {
		b.WriteString(cppWantArray(c.data))
		fmt.Fprintf(&b, `
        CHECK(builder.GetRoot() != NULL && builder.GetRoot()->data_length == %d, "%s length");
        if (builder.GetRoot() != NULL && builder.GetRoot()->data_length == %d) { CHECK(memcmp(builder.GetRoot()->data, want, (size_t) %d) == 0, "%s bytes"); }
`, len(c.data), c.name, len(c.data), len(c.data), c.name)
	}
	b.WriteString("    }\n")
	return b.String()
}

func cppProfileCase(c cppBase64Case) string {
	var b strings.Builder
	fmt.Fprintf(&b, `
    {
        static const uint8_t text[] = { %s };
        ProfileConfig value; ProfileConfigReset( value ); TableReport report;
        bool ok = ProfileConfigFromJson( value, (const char *) text, (int64_t) sizeof(text), &report );
        CHECK(ok == %t, "%s verdict");
`, cppBytes([]byte(c.text)), c.ok, c.name)
	if c.ok {
		b.WriteString(cppWantArray(c.data))
		fmt.Fprintf(&b, `
        CHECK(value.icon_length == %d, "%s length");
        if (value.icon_length == %d) { CHECK(memcmp(value.icon, want, (size_t) %d) == 0, "%s bytes"); }
`, len(c.data), c.name, len(c.data), len(c.data), c.name)
	}
	b.WriteString("    }\n")
	return b.String()
}

func runCPPBase64CanonicalProbe(t *testing.T, u *ir.Unit, cases []cppBase64Case, emit func(cppBase64Case) string) {
	t.Helper()
	var body strings.Builder
	body.WriteString(`
    int failures = 0;
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`)
	for _, c := range cases {
		body.WriteString(emit(c))
	}
	body.WriteString("    if (failures != 0) return 1;\n")
	out, err := runCPPCountRefusalProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("base64 canonical probe: %v\n%s", err, out)
	}
}

func TestCPPJsonBase64Canonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (c++)")
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not found: %v", cxx, err)
		}
		t.Skipf("generated C++ execution requires %s: %v", cxx, err)
	}

	stream := loadUnit(t, "../../../tables/stream/Stream.schema")
	streamModel := tabletext.NewModel(stream)
	vectors := cppBase64VectorCases(t, streamModel)

	escaped := loadUnitFromSource(t, cppJsonBase64CanonicalEscapeSchema)
	escapes := cppBase64EscapeCases(t, tabletext.NewModel(escaped))

	runCPPBase64CanonicalProbe(t, stream, vectors, cppChunkCase)
	runCPPBase64CanonicalProbe(t, escaped, escapes, cppProfileCase)
}

const cppJsonBase64CanonicalEscapeSchema = `package probe
fixed table ProfileConfig { icon bytes(16) }
`
