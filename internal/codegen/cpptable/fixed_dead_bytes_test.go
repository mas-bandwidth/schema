package cpptable

// EVERY DEAD BYTE IS ZERO (Glenn, 2026-11-10; docs/SPEC-TABLES.md §3.4, "ZERO
// ON READ, OR THE RECORD IS REFUSED"), the C++ twin of ctable's
// fixed_dead_bytes_test.go. One probe per read the C++ fixed form has, the
// IDENTITY read and the LINEAGE read (an older writer's layout, found by the
// VOLD_/VNEW_ sibling convention and compiled at run time), each over the four
// classes of slack and their controls:
//
//   - an absent optional's payload;
//   - a counted array's tail past its live count;
//   - string(N) and bytes(N) past the used length;
//   - a union's arm bytes under tag 0, behind a narrower arm up to the widest
//     arm, and all of them behind a payload-free arm;
//   - slack nested in live data (a present optional's string tail);
//   - and, NOT slack, a deprecated field's slot, which keeps its current
//     handling and reads.
//
// Each case puts ONE lawful-looking byte in a dead span of the writer's own
// clean record; the read must refuse the whole record as wire_damaged before
// anything lands. The live cases beside them must still read. The offsets are
// the writer's, written out by hand from §3.4's constant-size table, and the
// probe checks two anchors (lead and trail) before it trusts them.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The writer's body, packed (§3.4):
//
//	lead   0..4    uint32 = 1
//	opt    4       present byte, then Leaf 5..15: v 5..9, t length 9..13, t bytes 13..15
//	xs     15..19  count, then four int32 at 19..35
//	s      35..39  length, then eight bytes at 39..47
//	blob   47..51  count, then six bytes at 51..57
//	u      57      tag (1 wide, 2 narrow, 3 ping), then the widest arm's 8 bytes at 58..66
//	note   66..70  length, then four bytes at 70..74 (deprecated)
//	trail  74..78  uint32 = 2
//
// The newer reader appends `extra` after trail.
const cppDeadBody = `
type Leaf
{
    v int32 = 0
    t string(2)
}

type Wide
{
    a int32 = 0
    b int32 = 0
}

type Narrow
{
    c int16 = 0
}

union U
{
    wide   Wide
    narrow Narrow
    ping
}

fixed table Dead
{
    lead  uint32 = 1
    opt   ?Leaf
    xs    [..4]int32
    s     string(8)
    blob  bytes(6)
    u     U
    note  string(4) | deprecated
    trail uint32 = 2
`

const cppDeadWriterSchema = "package deadold\n" + cppDeadBody + "}\n"

const cppDeadReaderSchema = "package deadnew\n" + cppDeadBody + "    extra int32 = 5\n}\n"

type cppDeadPoke struct {
	at int
	v  byte
}

type cppDeadCase struct {
	name   string
	pokes  []cppDeadPoke
	refuse bool
}

// cppDeadLive is the clean record's live data, laid over the writer's reset
// image: one array element, "ab" in s, two bytes in blob, the narrow arm.
var cppDeadLive = []cppDeadPoke{{15, 1}, {19, 7}, {35, 2}, {39, 'a'}, {40, 'b'}, {47, 2}, {51, 1}, {52, 2}, {57, 2}, {58, 3}}

var cppDeadCases = []cppDeadCase{
	{"clean: the live record reads", nil, false},
	{"absent optional: a byte of its payload", []cppDeadPoke{{7, 1}}, true},
	{"counted array: a byte past the live count", []cppDeadPoke{{23, 1}}, true},
	{"string(N): a byte past the used length", []cppDeadPoke{{41, 'x'}}, true},
	{"bytes(N): a byte past the used length", []cppDeadPoke{{53, 1}}, true},
	{"union: a byte behind the narrower arm", []cppDeadPoke{{60, 1}}, true},
	{"union: the widest arm is all live", []cppDeadPoke{{57, 1}, {58, 1}, {59, 2}, {60, 3}, {61, 4}, {62, 5}, {63, 6}, {64, 7}, {65, 8}}, false},
	{"union: tag 0 over zero arm bytes reads", []cppDeadPoke{{57, 0}, {58, 0}}, false},
	{"union: an arm byte under tag 0", []cppDeadPoke{{57, 0}, {58, 0}, {65, 1}}, true},
	{"union: a payload-free arm over zero arm bytes reads", []cppDeadPoke{{57, 3}, {58, 0}}, false},
	{"union: an arm byte behind a payload-free arm", []cppDeadPoke{{57, 3}}, true},
	{"deprecated: its slot is not slack", []cppDeadPoke{{66, 1}, {70, 'q'}, {72, 'z'}}, false},
	{"nested: a present optional's live text reads", []cppDeadPoke{{4, 1}, {9, 1}, {13, 'k'}}, false},
	{"nested: a present optional's text tail", []cppDeadPoke{{4, 1}, {9, 1}, {13, 'k'}, {14, 'z'}}, true},
}

func cppDeadCasesSource() string {
	var b strings.Builder
	for _, c := range cppDeadCases {
		at := []string{"-1"}
		val := []string{"0"}
		for _, p := range c.pokes {
			at = append(at, fmt.Sprint(p.at))
			val = append(val, fmt.Sprint(p.v))
		}
		refuse := "false"
		if c.refuse {
			refuse = "true"
		}
		fmt.Fprintf(&b, "    {\n        static const int at[] = { %s };\n        static const int val[] = { %s };\n        DeadCase( %q, len, body, at + 1, val + 1, %d, %s );\n    }\n",
			strings.Join(at, ", "), strings.Join(val, ", "), c.name, len(c.pokes), refuse)
	}
	return b.String()
}

func cppDeadLiveSource() string {
	var b strings.Builder
	for _, p := range cppDeadLive {
		fmt.Fprintf(&b, "    clean[body + %d] = %d;\n", p.at, p.v)
	}
	return b.String()
}

// cppDeadBuild writes the given schema files into a fresh directory, loads
// `main` (the VOLD_ sibling beside it, when given, is its lineage), generates
// its C++ there and compiles `source` against it.
func cppDeadBuild(t *testing.T, schemas map[string]string, main string, edit func(map[string][]byte), source func(header, pkg string) string) (string, []byte, error) {
	t.Helper()
	sdir := t.TempDir()
	for name, src := range schemas {
		if err := os.WriteFile(filepath.Join(sdir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	u := loadUnit(t, filepath.Join(sdir, main))
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(source(header, u.Package)), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	exe := filepath.Join(dir, "probe")
	out, err := exec.Command(cxx, "-std=c++17", "-O1", "-I"+dir, filepath.Join(dir, "probe.cpp"), "-o", exe).CombinedOutput()
	return exe, out, err
}

// cppDeadRun writes the writer's reset record to a file, then reads it with
// the reader through every case. With `lineage` the reader is VNEW_dead beside
// its VOLD_dead writer; without it the writer IS the reader (identity).
func cppDeadRun(t *testing.T, lineage bool, edit func(map[string][]byte)) ([]byte, error) {
	slowtest.Gate(t, "an external toolchain")
	t.Helper()
	writer := cppDeadReaderSchema
	if lineage {
		writer = cppDeadWriterSchema
	}
	wexe, out, err := cppDeadBuild(t, map[string]string{"VOLD_dead.schema": writer}, "VOLD_dead.schema", nil, func(header, pkg string) string {
		return fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include "%s"

using namespace %s;

int main( int argc, char ** argv )
{
    static uint8_t out[1 << 16];
    if ( argc != 2 ) { return 2; }
    Dead v;
    DeadReset( v );
    const int64_t n = DeadFixedSave( &v, 1, out, (int64_t) sizeof( out ) );
    if ( n < 0 ) { std::printf( "the writer's save failed\n" ); return 1; }
    FILE * f = std::fopen( argv[1], "wb" );
    if ( f == NULL ) { return 1; }
    const bool ok = std::fwrite( out, 1, (size_t) n, f ) == (size_t) n;
    std::fclose( f );
    return ok ? 0 : 1;
}
`, header, pkg)
	})
	if err != nil {
		return out, fmt.Errorf("the writer did not compile: %w", err)
	}
	file := filepath.Join(t.TempDir(), "dead.bin")
	if out, err := exec.Command(wexe, file).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the writer did not run: %w", err)
	}
	schemas := map[string]string{"VNEW_dead.schema": cppDeadReaderSchema}
	if lineage {
		schemas["VOLD_dead.schema"] = cppDeadWriterSchema
	}
	rexe, out, err := cppDeadBuild(t, schemas, "VNEW_dead.schema", edit, func(header, pkg string) string {
		return fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include "%s"

using namespace %s;

static uint8_t clean[1 << 16];
static uint8_t data[1 << 16];
static TableFixedEntry plan[4096];
static int failures = 0;

static void DeadCase( const char * name, int64_t len, int64_t body, const int * at, const int * val, int k, bool want_refuse )
{
    std::memcpy( data, clean, (size_t) len );
    for ( int i = 0; i < k; ++i ) { data[body + at[i]] = (uint8_t) val[i]; }
    Dead back;
    DeadReset( back );
    TableReport r;
    const int64_t n = DeadFixedLoad( &back, 1, data, len, plan, 4096, NULL, &r );
    if ( want_refuse )
    {
        if ( n != -1 || !r.refused || r.reason != wire_damaged )
        {
            std::printf( "%%s: want the whole record refused as wire_damaged, got n=%%lld refused=%%d reason=%%d\n", name, (long long) n, (int) r.refused, (int) r.reason );
            failures++;
        }
        else if ( back.xs_count != 0 || back.s_length != 0 )
        {
            std::printf( "%%s: a refused record landed bytes\n", name );
            failures++;
        }
    }
    else if ( n != 1 || r.refused )
    {
        std::printf( "%%s: want a clean read, got n=%%lld refused=%%d reason=%%d\n", name, (long long) n, (int) r.refused, (int) r.reason );
        failures++;
    }
}

int main( int argc, char ** argv )
{
    if ( argc != 2 ) { return 2; }
    FILE * f = std::fopen( argv[1], "rb" );
    if ( f == NULL ) { std::printf( "the writer's file is missing\n" ); return 1; }
    const int64_t len = (int64_t) std::fread( clean, 1, sizeof( clean ), f );
    std::fclose( f );
    uint32_t layout;
    std::memcpy( &layout, clean + kTableFixedHeaderBytes, 4 );
    const int64_t body = (int64_t) kTableFixedHeaderBytes + 4 + (int64_t) layout + 8;
    if ( body + 78 > len || clean[body + 0] != 1 || clean[body + 74] != 2 )
    {
        std::printf( "the writer's layout is not the one the offsets were written for\n" );
        return 1;
    }
%s%s    return failures != 0;
}
`, header, pkg, cppDeadLiveSource(), cppDeadCasesSource())
	})
	if err != nil {
		return out, fmt.Errorf("the reader did not compile: %w", err)
	}
	return exec.Command(rexe, file).CombinedOutput()
}

func TestFixedDeadBytesCpp(t *testing.T) {
	t.Parallel()
	for _, mode := range []struct {
		name    string
		lineage bool
	}{{"identity", false}, {"lineage", true}} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			if out, err := cppDeadRun(t, mode.lineage, nil); err != nil {
				t.Fatalf("the %s read: %v\n%s", mode.name, err, out)
			}
		})
	}
}

// TestFixedDeadBytesCppNegativeControl plants the fault in the real artifact:
// the emitted runtime's refusal scan no longer looks at the dead entries.
// Both reads must go red on the refusal cases.
func TestFixedDeadBytesCppNegativeControl(t *testing.T) {
	t.Parallel()
	const check = "else if ( p.op == kTableFixedDead && TableFixedDead( p, src ) )"
	edit := func(files map[string][]byte) {
		found := false
		for name, data := range files {
			if strings.HasSuffix(name, "Table.h") {
				text := string(data)
				if strings.Count(text, check) != 1 {
					t.Fatalf("%s: the scan's dead check is not there exactly once", name)
				}
				files[name] = []byte(strings.ReplaceAll(text, check, "else if ( false )"))
				found = true
			}
		}
		if !found {
			t.Fatal("no Table.h carried the scan's dead check")
		}
	}
	for _, mode := range []struct {
		name    string
		lineage bool
	}{{"identity", false}, {"lineage", true}} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			out, err := cppDeadRun(t, mode.lineage, edit)
			if err == nil {
				t.Fatalf("with the dead check disabled the probe still passed:\n%s", out)
			}
			if !strings.Contains(string(out), "want the whole record refused as wire_damaged") {
				t.Fatalf("the probe went red, but not on an unrefused dead byte:\n%s", out)
			}
		})
	}
}
