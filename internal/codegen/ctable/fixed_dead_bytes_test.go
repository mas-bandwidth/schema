package ctable

// EVERY DEAD BYTE IS ZERO (Glenn, 2026-11-10; docs/SPEC-TABLES.md §3.4, "ZERO
// ON READ, OR THE RECORD IS REFUSED"). One probe per read the C fixed form
// has, the IDENTITY read (the file's layout is the reader's own) and the
// LINEAGE read (an older writer's layout, compiled at run time), each over
// the four classes of slack and their controls:
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
// Each case puts ONE lawful-looking byte (a 1, a printable letter) in a dead
// span of the writer's own clean record; the read must refuse the whole
// record as wire_damaged. The live cases beside them must still read, so the
// check is the dead span and not the field. The offsets are written out by
// hand from §3.4's constant-size table for the WRITER's layout below, and the
// probe checks two anchors (lead and trail) before it trusts them.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
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
// The newer reader appends `extra` after trail, so the lineage read compiles
// the writer's layout and every offset above is the writer's.
const cDeadWriterSchema = `package deadold

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
}
`

const cDeadReaderSchema = `package deadnew

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
    extra int32 = 5
}
`

type deadPoke struct {
	at int
	v  byte
}

type deadCase struct {
	name   string
	pokes  []deadPoke
	refuse bool
}

// deadLive is the clean record's live data, laid over the writer's reset
// image: one array element, "ab" in s, two bytes in blob, the narrow arm.
var deadLive = []deadPoke{{15, 1}, {19, 7}, {35, 2}, {39, 'a'}, {40, 'b'}, {47, 2}, {51, 1}, {52, 2}, {57, 2}, {58, 3}}

var deadCases = []deadCase{
	{"clean: the live record reads", nil, false},
	{"absent optional: a byte of its payload", []deadPoke{{7, 1}}, true},
	{"counted array: a byte past the live count", []deadPoke{{23, 1}}, true},
	{"string(N): a byte past the used length", []deadPoke{{41, 'x'}}, true},
	{"bytes(N): a byte past the used length", []deadPoke{{53, 1}}, true},
	{"union: a byte behind the narrower arm", []deadPoke{{60, 1}}, true},
	{"union: the widest arm is all live", []deadPoke{{57, 1}, {58, 1}, {59, 2}, {60, 3}, {61, 4}, {62, 5}, {63, 6}, {64, 7}, {65, 8}}, false},
	{"union: tag 0 over zero arm bytes reads", []deadPoke{{57, 0}, {58, 0}}, false},
	{"union: an arm byte under tag 0", []deadPoke{{57, 0}, {58, 0}, {65, 1}}, true},
	{"union: a payload-free arm over zero arm bytes reads", []deadPoke{{57, 3}, {58, 0}}, false},
	{"union: an arm byte behind a payload-free arm", []deadPoke{{57, 3}}, true},
	{"deprecated: its slot is not slack", []deadPoke{{66, 1}, {70, 'q'}, {72, 'z'}}, false},
	{"nested: a present optional's live text reads", []deadPoke{{4, 1}, {9, 1}, {13, 'k'}}, false},
	{"nested: a present optional's text tail", []deadPoke{{4, 1}, {9, 1}, {13, 'k'}, {14, 'z'}}, true},
}

// deadCasesC prints the case table as C calls of dead_case.
func deadCasesC() string {
	var b strings.Builder
	for _, c := range deadCases {
		at := []string{"-1"}
		val := []string{"0"}
		for _, p := range c.pokes {
			at = append(at, fmt.Sprint(p.at))
			val = append(val, fmt.Sprint(p.v))
		}
		refuse := 0
		if c.refuse {
			refuse = 1
		}
		fmt.Fprintf(&b, "    {\n        static const int at[] = { %s };\n        static const int val[] = { %s };\n        dead_case( %q, len, body, at + 1, val + 1, %d, %d );\n    }\n",
			strings.Join(at, ", "), strings.Join(val, ", "), c.name, len(c.pokes), refuse)
	}
	return b.String()
}

func deadLiveC() string {
	var b strings.Builder
	for _, p := range deadLive {
		fmt.Fprintf(&b, "    clean[body + %d] = %d;\n", p.at, p.v)
	}
	return b.String()
}

// cDeadBuild generates `src` as C (with `older` as the root's lineage) into a
// fresh directory and returns it with the Table.h name and the root's C names.
func cDeadBuild(t *testing.T, src string, older []string, edit func(map[string][]byte)) (dir, header, root, snake string) {
	t.Helper()
	u := cUnitOf(t, src)
	lineage := map[string][]FixedLineageEntry{}
	for _, o := range older {
		ou := cUnitOf(t, o)
		for _, st := range ir.TableFixedRoots(ou) {
			e, ok := FixedLineageOf(ou, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir = t.TempDir()
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
	root = cFixedRootName(t, u)
	return dir, header, root, ir.RustSnake(root)
}

func cDeadCompile(t *testing.T, dir, source string) (string, []byte, error) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	exe := filepath.Join(dir, "probe")
	args := append([]string{"-std=c99", "-O1", "-Wall", "-Wextra", "-Werror", "-I" + dir}, gccFortifySilence(cc)...)
	args = append(args, filepath.Join(dir, "probe.c"), "-o", exe, "-lm")
	out, err := exec.Command(cc, args...).CombinedOutput()
	return exe, out, err
}

// cDeadRun writes the writer's reset record to a file, then reads it with
// the reader through every case. `older` is the reader's lineage (nil for the
// identity read, where the writer IS the reader).
func cDeadRun(t *testing.T, writer, reader string, older []string, edit func(map[string][]byte)) ([]byte, error) {
	slowtest.Gate(t, "an external toolchain")
	t.Helper()
	wdir, wheader, wroot, wsnake := cDeadBuild(t, writer, nil, nil)
	wexe, out, err := cDeadCompile(t, wdir, fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include "%s"

int main( int argc, char ** argv )
{
    static uint8_t out[1 << 16];
    %s v;
    int64_t n;
    FILE * f;
    if ( argc != 2 ) { return 2; }
    memset( &v, 0, sizeof( v ) );
    %s_reset( &v );
    n = %s_fixed_save( &v, 1, out, (int64_t) sizeof( out ) );
    if ( n < 0 ) { printf( "the writer's save failed\n" ); return 1; }
    f = fopen( argv[1], "wb" );
    if ( f == NULL ) { return 1; }
    if ( fwrite( out, 1, (size_t) n, f ) != (size_t) n ) { fclose( f ); return 1; }
    fclose( f );
    return 0;
}
`, wheader, wroot, wsnake, wsnake))
	if err != nil {
		return out, fmt.Errorf("the writer did not compile: %w", err)
	}
	file := filepath.Join(wdir, "dead.bin")
	if out, err := exec.Command(wexe, file).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the writer did not run: %w", err)
	}
	rdir, rheader, rroot, rsnake := cDeadBuild(t, reader, older, edit)
	rexe, out, err := cDeadCompile(t, rdir, fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include "%s"

static uint8_t clean[1 << 16];
static uint8_t data[1 << 16];
static TableFixedEntry plan[4096];
static int failures = 0;

static void dead_case( const char * name, int64_t len, int64_t body, const int * at, const int * val, int k, int want_refuse )
{
    %s back;
    TableReport r;
    int64_t n;
    int i;
    memcpy( data, clean, (size_t) len );
    for ( i = 0; i < k; ++i ) { data[body + at[i]] = (uint8_t) val[i]; }
    memset( &back, 0, sizeof( back ) );
    %s_reset( &back );
    memset( &r, 0, sizeof( r ) );
    n = %s_fixed_load( &back, 1, data, len, plan, 4096, NULL, &r );
    if ( want_refuse )
    {
        if ( n != -1 || !r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED )
        {
            printf( "%%s: want the whole record refused as wire_damaged, got n=%%lld refused=%%d reason=%%d\n", name, (long long) n, (int) r.refused, (int) r.refuse_reason );
            failures++;
        }
    }
    else if ( n != 1 || r.refused )
    {
        printf( "%%s: want a clean read, got n=%%lld refused=%%d reason=%%d\n", name, (long long) n, (int) r.refused, (int) r.refuse_reason );
        failures++;
    }
}

int main( int argc, char ** argv )
{
    FILE * f;
    int64_t len;
    uint32_t layout;
    int64_t body;
    if ( argc != 2 ) { return 2; }
    f = fopen( argv[1], "rb" );
    if ( f == NULL ) { printf( "the writer's file is missing\n" ); return 1; }
    len = (int64_t) fread( clean, 1, sizeof( clean ), f );
    fclose( f );
    memcpy( &layout, clean + kTableFixedHeaderBytes, 4 );
    body = (int64_t) kTableFixedHeaderBytes + 4 + (int64_t) layout + 8;
    if ( body + 78 > len || clean[body + 0] != 1 || clean[body + 74] != 2 )
    {
        printf( "the writer's layout is not the one the offsets were written for\n" );
        return 1;
    }
%s%s    return failures != 0;
}
`, rheader, rroot, rsnake, rsnake, deadLiveC(), deadCasesC()))
	if err != nil {
		return out, fmt.Errorf("the reader did not compile: %w", err)
	}
	return exec.Command(rexe, file).CombinedOutput()
}

func TestFixedDeadBytesC(t *testing.T) {
	t.Parallel()
	t.Run("identity", func(t *testing.T) {
		t.Parallel()
		if out, err := cDeadRun(t, cDeadReaderSchema, cDeadReaderSchema, nil, nil); err != nil {
			t.Fatalf("the identity read: %v\n%s", err, out)
		}
	})
	t.Run("lineage", func(t *testing.T) {
		t.Parallel()
		if out, err := cDeadRun(t, cDeadWriterSchema, cDeadReaderSchema, []string{cDeadWriterSchema}, nil); err != nil {
			t.Fatalf("the lineage read: %v\n%s", err, out)
		}
	})
}

// TestFixedDeadBytesCNegativeControl plants the fault in the real artifact:
// the emitted runtime's dead entry no longer counts damage. Both reads must go
// red on the refusal cases.
func TestFixedDeadBytesCNegativeControl(t *testing.T) {
	t.Parallel()
	const check = "if ( table_fixed_dead( p, src ) ) { (*damaged)++; }"
	edit := func(files map[string][]byte) {
		found := false
		for name, data := range files {
			if strings.HasSuffix(name, "Table.h") {
				text := string(data)
				if strings.Count(text, check) != 1 {
					t.Fatalf("%s: the dead entry's check is not there exactly once", name)
				}
				files[name] = []byte(strings.ReplaceAll(text, check, "(void) src;"))
				found = true
			}
		}
		if !found {
			t.Fatal("no Table.h carried the dead entry's check")
		}
	}
	for _, mode := range []struct {
		name  string
		write string
		older []string
	}{{"identity", cDeadReaderSchema, nil}, {"lineage", cDeadWriterSchema, []string{cDeadWriterSchema}}} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			out, err := cDeadRun(t, mode.write, cDeadReaderSchema, mode.older, edit)
			if err == nil {
				t.Fatalf("with the dead check disabled the probe still passed:\n%s", out)
			}
			if !strings.Contains(string(out), "want the whole record refused as wire_damaged") {
				t.Fatalf("the probe went red, but not on an unrefused dead byte:\n%s", out)
			}
		})
	}
}
