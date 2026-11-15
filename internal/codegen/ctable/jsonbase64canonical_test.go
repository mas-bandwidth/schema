// Canonical base64 in the C JSON read of a bytes field (docs/SPEC-TABLES.md §16.2, §16.8).
package ctable

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// base64Row is one JSON text read by the engine and by the C leg. want is
// the verdict the standard states ("read" or "refused"); bytes, when set, is
// the value a read must hold.
type base64Row struct {
	name  string
	text  []byte
	want  string
	bytes []byte
}

// base64Probe is one table the rows are read into: the schema, its root
// table, the generated C read of that root, and the C expression that names
// the bytes the read holds.
type base64Probe struct {
	schema string
	root   string
	field  string
	read   string
	view   string
}

// TestCJsonBase64Canonical holds the C JSON read of a `bytes(N)` and a
// `*bytes` value to canonical base64 of the string's DECODED value: the
// standard alphabet, padded, pad bits zero. The JSON layer decides first, so
// the canonical spelling written with JSON escapes reads, and every other
// spelling that decodes is refused. The rows are every vector under
// testdata/wire/json-base64-canonical/ plus escaped spellings; each is first
// held to the verdict the standard states by the compiler's own engine
// (internal/tabletext), then the C read is compared with the engine on the
// verdict, and on the bytes of a read.
//
// Compiling and running the C makes this a functional test: it skips by name
// without SCHEMA_SLOW, and fails by name under SCHEMA_REQUIRE_CORPUS=1 when no
// C compiler is found.
func TestCJsonBase64Canonical(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := os.ReadFile(filepath.Join(root, "tables/stream/Stream.schema"))
	if err != nil {
		t.Fatal(err)
	}
	vectors := base64CanonicalVectors(t, root)

	// the manifest's rows, on the table it names, and the canonical
	// spellings written with JSON escapes
	chunk := append([]base64Row{}, vectors...)
	chunk = append(chunk,
		base64Row{name: "escaped-solidus-read", text: []byte(`{ "data": "\/\/\/\/" }`), want: "read", bytes: []byte{0xff, 0xff, 0xff}},
		base64Row{name: "escaped-letter-read", text: []byte(`{ "data": "\u0041Q==" }`), want: "read", bytes: []byte{0x01}},
		base64Row{name: "escaped-pad-read", text: []byte(`{ "data": "AQ\u003d\u003d" }`), want: "read", bytes: []byte{0x01}},
		base64Row{name: "escaped-pad-bits-set-refused", text: []byte(`{ "data": "\u0041R==" }`), want: "refused"},
		base64Row{name: "escaped-unpadded-refused", text: []byte(`{ "data": "\u0041Q" }`), want: "refused"},
	)

	// the same bodies in a *bytes slot and a second bytes(N) table
	bodies := []struct {
		name string
		body string
		want string
		val  []byte
	}{
		{"canonical", `QUI=`, "read", []byte("AB")},
		{"canonical-group", `AQID`, "read", []byte{1, 2, 3}},
		{"empty", ``, "read", []byte{}},
		{"one-byte", `AQ==`, "read", []byte{0x01}},
		{"pad-bits-set", `AR==`, "refused", nil},
		{"pad-bits-set-one-pad", `QUJ=`, "refused", nil},
		{"unpadded-two", `AR`, "refused", nil},
		{"unpadded-three", `QUI`, "refused", nil},
		{"one-pad-where-two-belong", `QQ=`, "refused", nil},
		{"pad-in-the-middle", `Q=Q=`, "refused", nil},
		{"not-base64", `$$$$`, "refused", nil},
		{"escaped-solidus", `\/\/\/\/`, "read", []byte{0xff, 0xff, 0xff}},
		{"escaped-letter", `\u0041Q==`, "read", []byte{0x01}},
		{"escaped-pad-bits-set", `\u0041R==`, "refused", nil},
		{"escaped-unpadded", `\u0041Q`, "refused", nil},
		{"escaped-newline", `\n`, "refused", nil},
	}
	var bounded, pointer []base64Row
	for _, b := range bodies {
		bounded = append(bounded, base64Row{name: "bounded-" + b.name, text: []byte(`{"bounded":"` + b.body + `"}`), want: b.want, bytes: b.val})
		pointer = append(pointer, base64Row{name: "pointer-" + b.name, text: []byte(`{"pointer":"` + b.body + `"}`), want: b.want, bytes: b.val})
	}
	const blobSchema = `package probe

table Blob
{
    bounded bytes(8)
    pointer *bytes
}
`

	probes := []struct {
		name  string
		probe base64Probe
		rows  []base64Row
	}{
		{"stream-chunk", base64Probe{
			schema: string(stream), root: "Chunk", field: "data",
			read: "chunk", view: cBoundedView("data"),
		}, chunk},
		{"blob-bounded", base64Probe{
			schema: blobSchema, root: "Blob", field: "bounded",
			read: "blob", view: cBoundedView("bounded"),
		}, bounded},
		{"blob-pointer", base64Probe{
			schema: blobSchema, root: "Blob", field: "pointer",
			read: "blob", view: cPointerView("pointer"),
		}, pointer},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			u := cUnitOf(t, p.probe.schema)
			engine := base64EngineVerdicts(t, u, p.probe, p.rows)
			leg := base64CVerdicts(t, u, p.probe, p.rows)
			for i, row := range p.rows {
				if leg[i] != engine[i] {
					t.Errorf("%s: C read %q, the engine reads %q, text %s", row.name, leg[i], engine[i], row.text)
				}
			}
		})
	}
}

// base64CanonicalVectors reads the vectors the standard landed, each with
// the verdict its manifest states.
func base64CanonicalVectors(t *testing.T, root string) []base64Row {
	t.Helper()
	dir := filepath.Join(root, "testdata/wire/json-base64-canonical")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []base64Row
	scanner := bufio.NewScanner(bytes.NewReader(manifest))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 6 {
			t.Fatalf("MANIFEST.tsv: %d columns in %q, want 6", len(cols), line)
		}
		if cols[1] != "tables/stream/Stream.schema" || cols[2] != "Chunk" {
			t.Fatalf("MANIFEST.tsv: row %s names %s %s; this test reads Chunk in tables/stream/Stream.schema", cols[0], cols[1], cols[2])
		}
		if cols[4] != "read" && cols[4] != "refused" {
			t.Fatalf("MANIFEST.tsv: row %s verdict %q", cols[0], cols[4])
		}
		text, err := os.ReadFile(filepath.Join(dir, cols[3]))
		if err != nil {
			t.Fatal(err)
		}
		row := base64Row{name: cols[0], text: bytes.TrimRight(text, "\n"), want: cols[4]}
		if cols[0] == "canonical-two-bytes-read" {
			row.bytes = []byte("AB")
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	return rows
}

// base64EngineVerdicts reads every row with the compiler's own engine and
// holds it to the verdict (and the bytes) the row states first: the
// reference is confirmed before the C leg is compared with it. A read is
// "read <hex>", a refusal "refused".
func base64EngineVerdicts(t *testing.T, u *ir.Unit, p base64Probe, rows []base64Row) []string {
	t.Helper()
	model := tabletext.NewModel(u)
	st := model.Lookup(p.root)
	if st == nil {
		t.Fatalf("no %s table", p.root)
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		inst := model.New(st)
		var report tabletext.Report
		if !model.Read(inst, row.text, &report) {
			out[i] = "refused"
		} else {
			fv, found := inst.FieldByKey(p.field)
			if !found {
				t.Fatalf("no %s field", p.field)
			}
			got := fv.Cell.Str
			if fv.Cell.Blob != nil {
				got = fv.Cell.Blob.Data
			}
			out[i] = strings.TrimSpace("read " + hex.EncodeToString(got))
		}
		verdict, _, _ := strings.Cut(out[i], " ")
		if verdict != row.want {
			t.Fatalf("the engine's verdict on %s is %q, the standard states %q: text %s", row.name, out[i], row.want, row.text)
		}
		if row.bytes != nil && out[i] != strings.TrimSpace("read "+hex.EncodeToString(row.bytes)) {
			t.Fatalf("the engine reads %s as %q, the standard states %x", row.name, out[i], row.bytes)
		}
	}
	return out
}

// cBoundedView is the body that views a bytes(N) field: its fixed buffer and
// the used length beside it.
func cBoundedView(field string) string {
	return fmt.Sprintf("    (void) b;\n    v.data = r->%[1]s;\n    v.length = r->%[1]s_length;", field)
}

// cPointerView is the body that views a *bytes field: the blob its
// reference names in the builder's arena.
func cPointerView(field string) string {
	return fmt.Sprintf(`    TableCtx ctx;
    TableBytesView blob;
    ctx.arena = &b->arena;
    blob = table_bytes_at( &ctx, &r->%[1]s );
    v.data = blob.data;
    v.length = blob.length;`, field)
}

// base64CVerdicts compiles the generated C with a probe that reads every
// row through the root's JSON read and prints one verdict per row, in the
// engine's spelling.
func base64CVerdicts(t *testing.T, u *ir.Unit, p base64Probe, rows []base64Row) []string {
	t.Helper()
	slowtest.Gate(t, "the C compiler (it compiles and runs the generated unit)")
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	if _, err := exec.LookPath(cc); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not found: %v", cc, err)
		}
		t.Skipf("%s: %v", cc, err)
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("c.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	var probe strings.Builder
	fmt.Fprintf(&probe, `#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include "ProbeTable.h"

typedef struct Row {
    const char * text;
    int64_t length;
} Row;

`)
	for i, row := range rows {
		fmt.Fprintf(&probe, "static const char row%d[] = { ", i)
		for _, b := range row.text {
			fmt.Fprintf(&probe, "0x%02x, ", b)
		}
		fmt.Fprintf(&probe, "0x00 };\n")
	}
	fmt.Fprintf(&probe, "static const Row rows[] = {\n")
	for i, row := range rows {
		fmt.Fprintf(&probe, "    { row%d, %d },\n", i, len(row.text))
	}
	fmt.Fprintf(&probe, `};

typedef struct View {
    const uint8_t * data;
    int64_t length;
} View;

static View field_view( %[2]sBuilder * b, %[2]s * r )
{
    View v;
%[3]s
    return v;
}

int main( void )
{
    size_t i;
    for ( i = 0; i < sizeof( rows ) / sizeof( rows[0] ); i++ )
    {
        %[2]sBuilder b;
        TableReport report;
        int ok;
        if ( !%[1]s_builder_init( &b ) ) { fprintf( stderr, "builder_init failed\n" ); return 1; }
        memset( &report, 0, sizeof( report ) );
        ok = %[1]s_from_json( &b, rows[i].text, rows[i].length, &report );
        if ( !ok || report.refused )
        {
            printf( "refused\n" );
        }
        else
        {
            %[2]s * r = %[1]s_builder_root( &b );
            View v = field_view( &b, r );
            int64_t k;
            printf( "read " );
            for ( k = 0; k < v.length; k++ ) { printf( "%%02x", v.data[k] ); }
            printf( "\n" );
        }
        %[1]s_builder_shutdown( &b );
    }
    return 0;
}
`, p.read, p.root, p.view)
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(probe.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := append([]string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I", dir, main}, sources...)
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin).CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(got) != len(rows) {
		t.Fatalf("probe printed %d verdicts for %d rows:\n%s", len(got), len(rows), out)
	}
	for i := range got {
		got[i] = strings.TrimSpace(got[i])
	}
	return got
}
