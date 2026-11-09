package ctable

import (
	"bytes"
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
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vtCellsInternalScopedFiles lists the variable-table C test files in
// internal/codegen/ctable/ that recite the C variable cells under the strict
// read (docs/SPEC-TABLES.md §4, §6.5).
var vtCellsInternalScopedFiles = []string{
	"jsonhostile_test.go",
	"textlist_test.go",
	"map_repeated_key_widen_test.go",
	"unboundedtext_test.go",
	"arrayroot_test.go",
	"onesided_and_optionalmap_test.go",
	"timestamp_test.go",
	"count_refusal_test.go",
	"required_test.go",
	"discard_test.go",
}

// vtCellsEmitterAnchors pins the strict-read refusal emission sites in
// internal/codegen/ctable/ so any regression in the emitter fails the unit
// check immediately without needing a compiler subprocess.
var vtCellsEmitterAnchors = []struct {
	file    string
	anchors []string
}{
	{
		file: "cook.go",
		anchors: []string{
			"static SCHEMA_UNUSED int table_report_finish( TableReport * report, int decoded )",
			"else if ( report->missing_required > 0 ) { reason = SCHEMA_TABLE_REFUSE_MISSING_REQUIRED; }",
			"else if ( report->blob_shared > 0 ) { reason = SCHEMA_TABLE_REFUSE_BLOB_SHARED; }",
			"else if ( !decoded || report->refused ) { reason = SCHEMA_TABLE_REFUSE_WIRE_DAMAGED; }",
			"return report->refuse_reason != SCHEMA_TABLE_REFUSE_OK;",
		},
	},
	{
		file: "wire_read.go",
		anchors: []string{
			"if ( table_report_finish( report, whole ) ) { %s( value ); return 0; } /* a refusal discards the whole load (§4) */",
			"r->report->refused=1, r->report->refuse_reason=SCHEMA_TABLE_REFUSE_OUT_OF_RANGE;",
			"r->report->refused=1, r->report->refuse_reason=SCHEMA_TABLE_REFUSE_KIND_MISMATCH;",
			"if ( ref <= 4096 ) { if ( seen_ref[(ref-1)>>3] & (uint8_t)(1u << ((ref-1) & 7)) ) { r->report->refused=1, r->report->refuse_reason=SCHEMA_TABLE_REFUSE_WIRE_DAMAGED; return 0; }",
		},
	},
	{
		file: "graph.go",
		anchors: []string{
			"if(!%s(&reader,(%s *)(void *)region,&nodes,directory,&sink) || table_report_finish(report,1)) { report->refused=1; memset(region,0,(size_t)total);",
			"if(!ok || table_report_finish(report,ok)) { report->refused=1; %s(root); return 0; } /* a refusal discards the whole load (§4) */",
		},
	},
	{
		file: "json.go",
		anchors: []string{
			"if ( !table_report_finish( report, decoded ) ) { return 1; }",
			"in->report->refused=1, in->report->refuse_reason=SCHEMA_TABLE_REFUSE_OUT_OF_RANGE;",
			"in->report->refused=1, in->report->refuse_reason=SCHEMA_TABLE_REFUSE_KIND_MISMATCH;",
			"if ( ( seen[index >> 6] & bit ) != 0 ) { in->report->refused=1, in->report->refuse_reason=SCHEMA_TABLE_REFUSE_WIRE_DAMAGED; }",
		},
	},
}

const vtCellsStrictSchema = `package probe

table Inner
{
    n int32 = 0
}

table Graph
{
    a     int32 = 0
    score int32 | min = 0, max = 10
    name  string(8)
    inner Inner
    next  *Inner
    tags  map[int32]int32
}
`

const vtCellsWriterSchema = `package probe

table Inner
{
    n int32 = 0
}

table Graph
{
    a     int32 = 0
    score int32 = 0
    name  string(8)
    inner Inner
    next  *Inner
    tags  map[int32]int32
}
`

func vtCellsProbeBody(clean, damaged, over []byte) string {
	return fmt.Sprintf(`
    static const uint8_t clean[] = { %s };
    static const uint8_t damaged[] = { %s };
    static const uint8_t over[] = { %s };
    SCHEMA_C_ALIGN16 static uint8_t region[8192];
    TableReport r;
    GraphBuilder b;
    const Graph * root;
    Graph * broot;

    /* 1. Clean wire reads whole through region and builder entry points. */
    memset( &r, 0, sizeof( r ) );
    root = graph_load( region, sizeof( region ), clean, (int64_t) sizeof( clean ), &r );
    if ( root == NULL || r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_OK || root->a != 5 || root->score != 7 || root->inner.n != 3 )
    { fprintf( stderr, "clean region read failed\n" ); return 1; }

    if ( !graph_builder_init( &b ) ) { return 1; }
    memset( &r, 0, sizeof( r ) );
    if ( !graph_load_builder( &b, clean, (int64_t) sizeof( clean ), &r ) || r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_OK )
    { fprintf( stderr, "clean builder read failed\n" ); graph_builder_shutdown( &b ); return 1; }
    broot = graph_builder_root( &b );
    if ( broot == NULL || broot->a != 5 || broot->score != 7 || broot->inner.n != 3 )
    { fprintf( stderr, "clean builder root mismatch\n" ); graph_builder_shutdown( &b ); return 1; }
    graph_builder_shutdown( &b );

    /* 2. Damaged wire refuses whole and lands nothing. */
    memset( &r, 0, sizeof( r ) );
    if ( graph_load( region, sizeof( region ), damaged, (int64_t) sizeof( damaged ), &r ) != NULL || !r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED )
    { fprintf( stderr, "damaged region read was not refused whole\n" ); return 1; }

    if ( !graph_builder_init( &b ) ) { return 1; }
    memset( &r, 0, sizeof( r ) );
    if ( graph_load_builder( &b, damaged, (int64_t) sizeof( damaged ), &r ) || !r.refused )
    { fprintf( stderr, "damaged builder read was not refused whole\n" ); graph_builder_shutdown( &b ); return 1; }
    broot = graph_builder_root( &b );
    if ( broot != NULL && ( broot->a != 0 || broot->score != 0 ) )
    { fprintf( stderr, "damaged builder read landed partial state\n" ); graph_builder_shutdown( &b ); return 1; }
    graph_builder_shutdown( &b );

    /* 3. Out-of-range wire refuses whole with SCHEMA_TABLE_REFUSE_OUT_OF_RANGE. */
    memset( &r, 0, sizeof( r ) );
    if ( graph_load( region, sizeof( region ), over, (int64_t) sizeof( over ), &r ) != NULL || !r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_OUT_OF_RANGE )
    { fprintf( stderr, "out-of-range region read was not refused whole\n" ); return 1; }

    if ( !graph_builder_init( &b ) ) { return 1; }
    memset( &r, 0, sizeof( r ) );
    if ( graph_load_builder( &b, over, (int64_t) sizeof( over ), &r ) || !r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_OUT_OF_RANGE )
    { fprintf( stderr, "out-of-range builder read was not refused whole\n" ); graph_builder_shutdown( &b ); return 1; }
    broot = graph_builder_root( &b );
    if ( broot != NULL && ( broot->a != 0 || broot->score != 0 ) )
    { fprintf( stderr, "out-of-range builder read landed partial state\n" ); graph_builder_shutdown( &b ); return 1; }
    graph_builder_shutdown( &b );

    /* 4. Truncated wire refuses whole. */
    memset( &r, 0, sizeof( r ) );
    if ( graph_load( region, sizeof( region ), clean, (int64_t) sizeof( clean ) - 3, &r ) != NULL || !r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED )
    { fprintf( stderr, "truncated region read was not refused whole\n" ); return 1; }

    /* 5. JSON strict read: clean accepts, out-of-range, duplicate map key, and lone surrogate refuse whole. */
    {
        const char * good_json = "{\"a\":5,\"score\":7,\"name\":\"ok\",\"tags\":{\"1\":10}}";
        const char * over_json = "{\"a\":5,\"score\":99}";
        const char * dup_json  = "{\"a\":5,\"tags\":{\"1\":10,\"1\":20}}";
        const char * surr_json = "{\"a\":5,\"name\":\"\\ud800A\"}";

        if ( !graph_builder_init( &b ) ) { return 1; }
        memset( &r, 0, sizeof( r ) );
        if ( !graph_from_json( &b, good_json, (int64_t) strlen( good_json ), &r ) || r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_OK )
        { fprintf( stderr, "clean JSON read refused\n" ); graph_builder_shutdown( &b ); return 1; }
        broot = graph_builder_root( &b );
        if ( broot == NULL || broot->a != 5 || broot->score != 7 || broot->tags.count != 1 )
        { fprintf( stderr, "clean JSON root mismatch\n" ); graph_builder_shutdown( &b ); return 1; }
        graph_builder_shutdown( &b );

        if ( !graph_builder_init( &b ) ) { return 1; }
        memset( &r, 0, sizeof( r ) );
        if ( graph_from_json( &b, over_json, (int64_t) strlen( over_json ), &r ) || !r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_OUT_OF_RANGE )
        { fprintf( stderr, "out-of-range JSON read was not refused whole\n" ); graph_builder_shutdown( &b ); return 1; }
        if ( graph_builder_root( &b )->a != 0 || graph_builder_root( &b )->score != 0 )
        { fprintf( stderr, "out-of-range JSON read landed partial state\n" ); graph_builder_shutdown( &b ); return 1; }
        graph_builder_shutdown( &b );

        if ( !graph_builder_init( &b ) ) { return 1; }
        memset( &r, 0, sizeof( r ) );
        if ( graph_from_json( &b, dup_json, (int64_t) strlen( dup_json ), &r ) || !r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED )
        { fprintf( stderr, "duplicate map key JSON read was not refused whole\n" ); graph_builder_shutdown( &b ); return 1; }
        if ( graph_builder_root( &b )->a != 0 || graph_builder_root( &b )->tags.count != 0 )
        { fprintf( stderr, "duplicate map key JSON read landed partial state\n" ); graph_builder_shutdown( &b ); return 1; }
        graph_builder_shutdown( &b );

        if ( !graph_builder_init( &b ) ) { return 1; }
        memset( &r, 0, sizeof( r ) );
        if ( graph_from_json( &b, surr_json, (int64_t) strlen( surr_json ), &r ) || !r.refused || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED )
        { fprintf( stderr, "lone surrogate JSON read was not refused whole\n" ); graph_builder_shutdown( &b ); return 1; }
        if ( graph_builder_root( &b )->a != 0 || graph_builder_root( &b )->name_length != 0 )
        { fprintf( stderr, "lone surrogate JSON read landed partial state\n" ); graph_builder_shutdown( &b ); return 1; }
        graph_builder_shutdown( &b );
    }
`, cBytes(clean), cBytes(damaged), cBytes(over))
}

func runVtCellsFilesProbe(t *testing.T, files map[string][]byte, body string) (string, error) {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Fatalf("functional C probe requires cc on PATH")
	}
	dir := t.TempDir()
	var headers []string
	var sources []string
	for name, data := range files {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			headers = append(headers, name)
		}
		if strings.HasSuffix(name, ".c") {
			sources = append(sources, full)
		}
	}
	var src strings.Builder
	for _, h := range headers {
		fmt.Fprintf(&src, "#include %q\n", h)
	}
	src.WriteString("#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\nint main(void) {\n")
	src.WriteString(body)
	src.WriteString("\n    return 0;\n}\n")
	mainC := filepath.Join(dir, "main.c")
	if err := os.WriteFile(mainC, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-I", dir, mainC)
	args = append(args, sources...)
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		return string(out), fmt.Errorf("compile: %w", err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestCVariableCellsStrict recites the C variable-table cells under the strict
// read (docs/SPEC-TABLES.md §4, §6.5). The unit checks inspect the generated C
// artifacts, emitter anchors, and scoped C variable-table test sources in
// memory with no subprocesses, sockets, or network; the compiled C probe is a
// functional test gated by slowtest.Gate that compiles and runs the real
// generated C code under AddressSanitizer and UndefinedBehaviorSanitizer.
func TestCVariableCellsStrict(t *testing.T) {
	t.Parallel()

	// 1. Unit check: generated C variable-table header and source carry the
	// plain strict entry points and none of the retired tolerant spellings.
	files := generate(t, vtCellsStrictSchema)
	var header strings.Builder
	for name, data := range files {
		for _, f := range vtStrictFindings("generated "+name, string(data)) {
			t.Error(f)
		}
		if strings.HasSuffix(name, "Table.h") {
			header.Write(data)
		}
	}
	for _, want := range []string{
		"graph_load(",
		"graph_load_builder(",
		"graph_load_retain(",
		"graph_from_json(",
		"table_report_finish(",
	} {
		if !strings.Contains(header.String(), want) {
			t.Errorf("generated variable header is missing strict symbol %q", want)
		}
	}

	// 2. Unit check: emitter sources in internal/codegen/ctable/ hold all
	// strict-read refusal anchors.
	for _, group := range vtCellsEmitterAnchors {
		data, err := os.ReadFile(group.file)
		if err != nil {
			t.Fatalf("read %s: %v", group.file, err)
		}
		src := string(data)
		for _, anchor := range group.anchors {
			if !strings.Contains(src, anchor) {
				t.Errorf("%s: missing strict-read anchor %q", group.file, anchor)
			}
		}
	}

	// 3. Unit check: scoped compiler C variable-table tests and internal/codegen/ctable
	// variable-table tests contain no retired tolerant spellings or report fields.
	for _, name := range vtStrictScopedFiles {
		path := filepath.Join("..", "..", "..", "compiler", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range vtStrictFindings(path, string(data)) {
			t.Error(f)
		}
	}
	for _, name := range vtCellsInternalScopedFiles {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range vtStrictFindings(name, string(data)) {
			t.Error(f)
		}
	}

	// The fixed-form generator red (vt/c/generator-panic): R2.schema's
	// payload-free pong arm has a nil field (fixedCollectTypes used to panic)
	// and A2.schema's direct string arm used to emit table_fixed_put0.
	r2Files := vtFixedArtifact(t, filepath.Join("..", "..", "..", "test", "tables", "R2.schema"))
	a2Files := vtFixedArtifact(t, filepath.Join("..", "..", "..", "test", "tables", "A2.schema"))
	if strings.Contains(string(r2Files["ProbeTable.h"]), "table_fixed_put0") || strings.Contains(string(a2Files["ProbeTable.h"]), "table_fixed_put0") {
		t.Fatal("fixed-form header emits table_fixed_put0")
	}

	// 4. Functional test (requires the C compiler): compile and run the
	// generated C variable-table strict read against clean, damaged,
	// out-of-range, truncated, duplicate-key, and lone-surrogate inputs.
	// The same gate runs the fixed-form probes for the generator red.
	slowtest.Gate(t, "functional test: the C compiler (cc)")
	reader := unitFrom(t, vtCellsStrictSchema)
	rm := tabletext.NewModel(reader)
	clean := vtStrictWire(t, vtCellsStrictSchema, `{"a":5,"score":7,"name":"ok","inner":{"n":3},"next":{"n":9},"tags":{"1":11}}`)
	over := vtStrictWire(t, vtCellsWriterSchema, `{"a":5,"score":19,"inner":{"n":3},"next":{"n":9}}`)
	var overRep tabletext.Report
	if ok, err := tablewire.Decode(rm, rm.New(rm.Lookup("Graph")), over, &overRep); ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("oracle did not refuse out-of-range wire: ok=%v err=%v %+v", ok, err, overRep)
	}
	damaged := bytes.Clone(clean)
	at := bytes.Index(damaged, []byte{byte(ir.TableKindI32), 3, 0, 0, 0, 0})
	if at < 0 {
		t.Fatal("Inner body is not where this test expects it")
	}
	damaged[at] = 37

	allFiles, err := cgen.Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(allFiles, files)
	if out, err := runVtCellsFilesProbe(t, allFiles, vtCellsProbeBody(clean, damaged, over)); err != nil {
		t.Fatalf("functional C strict-read probe failed: %v\n%s", err, out)
	}

	// vt/c/generator-panic: R2.schema nil-arm and A2.schema direct string arm.
	if out, err := runFixedHeaderProbe(t, r2Files, vtGeneratorPongProbe); err != nil {
		t.Fatalf("generator-panic clean pong probe failed: %v\n%s", err, out)
	}
	if out, err := runFixedHeaderProbe(t, a2Files, vtGeneratorStringProbe); err != nil {
		t.Fatalf("generator-panic clean string probe failed: %v\n%s", err, out)
	}
}

// TestCVariableCellsStrictNegativeControl plants faults into copies of the real
// artifacts — retired spellings in the generated header and scoped test files,
// and a sabotaged refusal guard in the real generated C header — and verifies
// that both the unit check and the functional C probe fail when the strict-read
// invariant is broken.
func TestCVariableCellsStrictNegativeControl(t *testing.T) {
	t.Parallel()

	// 1. Unit negative control: planting retired spellings into the real
	// generated header and scoped test files must be caught by vtStrictFindings.
	files := generate(t, vtCellsStrictSchema)
	rawHeader, ok := files["ProbeTable.h"]
	if !ok {
		t.Fatal("generated files missing ProbeTable.h")
	}
	if got := vtStrictFindings("ProbeTable.h", string(rawHeader)); len(got) != 0 {
		t.Fatalf("unplanted ProbeTable.h already fails: %v", got)
	}
	for _, fault := range []string{
		"graph_load_tolerant(",
		"report.clamped",
		"report.kind_mismatch",
		"report.malformed",
		"report.duplicate",
		"SCHEMA_TABLE_REFUSE_CLAMPED",
	} {
		planted := string(rawHeader) + "\n/* " + fault + " */\n"
		if got := vtStrictFindings("ProbeTable.h", planted); len(got) == 0 {
			t.Errorf("negative control failed: planting %q in ProbeTable.h was not flagged", fault)
		}
	}

	realInternal, err := os.ReadFile("jsonhostile_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := vtStrictFindings("jsonhostile_test.go", string(realInternal)); len(got) != 0 {
		t.Fatalf("unplanted jsonhostile_test.go already fails: %v", got)
	}
	if got := vtStrictFindings("jsonhostile_test.go", string(realInternal)+"\n// r.clamped\n"); len(got) == 0 {
		t.Fatal("negative control failed: planting r.clamped in jsonhostile_test.go was not flagged")
	}

	// 2. Functional negative control (requires the C compiler): sabotage the
	// strict refusal guard in the real generated ProbeTable.h so out_of_range
	// is forgiven, and verify the compiled C probe fails.
	slowtest.Gate(t, "functional test: the C compiler (cc)")
	reader := unitFrom(t, vtCellsStrictSchema)
	clean := vtStrictWire(t, vtCellsStrictSchema, `{"a":5,"score":7,"name":"ok","inner":{"n":3},"next":{"n":9},"tags":{"1":11}}`)
	over := vtStrictWire(t, vtCellsWriterSchema, `{"a":5,"score":19,"inner":{"n":3},"next":{"n":9}}`)
	damaged := bytes.Clone(clean)
	at := bytes.Index(damaged, []byte{byte(ir.TableKindI32), 3, 0, 0, 0, 0})
	if at < 0 {
		t.Fatal("Inner body is not where this test expects it")
	}
	damaged[at] = 37

	allFiles, err := cgen.Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(allFiles, files)
	const target = "return report->refuse_reason != SCHEMA_TABLE_REFUSE_OK;"
	const replacement = "return report->refuse_reason != SCHEMA_TABLE_REFUSE_OK && report->refuse_reason != SCHEMA_TABLE_REFUSE_OUT_OF_RANGE;"
	headerStr := string(allFiles["ProbeTable.h"])
	if !strings.Contains(headerStr, target) {
		t.Fatalf("ProbeTable.h missing target anchor %q", target)
	}
	allFiles["ProbeTable.h"] = []byte(strings.Replace(headerStr, target, replacement, 1))
	out, err := runVtCellsFilesProbe(t, allFiles, vtCellsProbeBody(clean, damaged, over))
	if err == nil {
		t.Fatalf("functional negative control passed with the out-of-range guard turned off:\n%s", out)
	}
	if strings.Contains(err.Error(), "compile:") {
		t.Fatalf("negative control did not run the probe (the planted header failed to compile): %v\n%s", err, out)
	}

	// Generator-red plants, in copies of the generated headers. A compile
	// error is not the failure: the probe must run and disagree.
	r2Files := vtFixedArtifact(t, filepath.Join("..", "..", "..", "test", "tables", "R2.schema"))
	r2Header := string(r2Files["ProbeTable.h"])
	r2Planted, ok := plantPongWritesBoost(r2Header)
	if !ok {
		t.Fatal("pong plant did not find the empty fixed-form pong case and the boost store")
	}
	r2Files["ProbeTable.h"] = []byte(r2Planted)
	out, err = runFixedHeaderProbe(t, r2Files, vtGeneratorPongProbe)
	if err == nil {
		t.Fatalf("generator-panic pong plant passed; the probe did not go red:\n%s", out)
	}
	if strings.Contains(err.Error(), "compile:") {
		t.Fatalf("pong plant did not run the probe (the planted header failed to compile): %v\n%s", err, out)
	}

	a2Files := vtFixedArtifact(t, filepath.Join("..", "..", "..", "test", "tables", "A2.schema"))
	a2Header := string(a2Files["ProbeTable.h"])
	a2Planted, ok := plantDirectStringCopiesNothing(a2Header)
	if !ok {
		t.Fatal("string plant did not find the direct arm memcpy")
	}
	a2Files["ProbeTable.h"] = []byte(a2Planted)
	out, err = runFixedHeaderProbe(t, a2Files, vtGeneratorStringProbe)
	if err == nil {
		t.Fatalf("generator-panic string plant passed; the probe did not go red:\n%s", out)
	}
	if strings.Contains(err.Error(), "compile:") {
		t.Fatalf("string plant did not run the probe (the planted header failed to compile): %v\n%s", err, out)
	}
}

// vtGeneratorPongProbe is the clean fixed-form probe for R2.schema. The
// payload-free pong arm must not write the boost residue that shares its
// storage, and the neighbours (grade, buff) must survive the round trip.
const vtGeneratorPongProbe = `
    static uint8_t buf_a[1 << 16];
    static uint8_t buf_b[1 << 16];
    static TableFixedEntry plan[4096];
    Cfg v;
    Cfg back;
    TableReport r;
    int64_t n_a;
    int64_t n_b;

    memset( &v, 0, sizeof( v ) );
    v.grade = GRADE_GOLD;
    v.effect.type = EFFECT_TYPE_PONG;
    v.buff.mult = 3.5f;
    n_a = cfg_fixed_save( &v, 1, buf_a, (int64_t) sizeof( buf_a ) );
    if ( n_a < 0 ) { fprintf( stderr, "pong save failed\n" ); return 1; }

    memset( &v, 0, sizeof( v ) );
    v.grade = GRADE_GOLD;
    v.effect.type = EFFECT_TYPE_PONG;
    v.effect.as.boost.power = 123456;
    v.buff.mult = 3.5f;
    n_b = cfg_fixed_save( &v, 1, buf_b, (int64_t) sizeof( buf_b ) );
    if ( n_b < 0 ) { fprintf( stderr, "pong residue save failed\n" ); return 1; }
    if ( n_a != n_b || memcmp( buf_a, buf_b, (size_t) n_a ) != 0 )
    { fprintf( stderr, "pong arm wrote boost residue\n" ); return 1; }

    memset( &back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    if ( cfg_fixed_load( &back, 1, buf_a, n_a, plan, 4096, NULL, &r ) != 1 )
    { fprintf( stderr, "pong load refused\n" ); return 1; }
    if ( back.effect.type != EFFECT_TYPE_PONG || back.grade != GRADE_GOLD || back.buff.mult != 3.5f )
    { fprintf( stderr, "pong neighbours moved type=%d grade=%d\n", (int) back.effect.type, (int) back.grade ); return 1; }
`

// vtGeneratorStringProbe is the clean fixed-form probe for A2.schema's direct
// string arm: the bytes and the neighbours both come back. A zero-width store
// that still compiles fails this probe.
const vtGeneratorStringProbe = `
    static uint8_t buf[1 << 16];
    static TableFixedEntry plan[4096];
    Root v;
    Root back;
    TableReport r;
    int64_t n;
    static const char want[] = "12345678";

    memset( &v, 0, sizeof( v ) );
    v.id = 7;
    v.value.type = VALUE_TYPE_C;
    memcpy( v.value.as.c.value, want, 8 );
    v.value.as.c.value_length = 8;
    v.tail = 0xA1B2C3D4u;
    n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
    if ( n < 0 ) { fprintf( stderr, "direct string save failed\n" ); return 1; }
    memset( &back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    if ( root_fixed_load( &back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
    { fprintf( stderr, "direct string load refused\n" ); return 1; }
    if ( back.value.type != VALUE_TYPE_C || back.value.as.c.value_length != 8 || memcmp( back.value.as.c.value, want, 8 ) != 0 )
    { fprintf( stderr, "direct string arm did not round-trip\n" ); return 1; }
    if ( back.id != 7 || back.tail != 0xA1B2C3D4u )
    { fprintf( stderr, "direct string arm moved a neighbour\n" ); return 1; }
`

func vtFixedArtifact(t *testing.T, schemaPath string) map[string][]byte {
	t.Helper()
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(data))
	var files map[string][]byte
	var genErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("fixed-form generator panicked on %s: %v", schemaPath, r)
			}
		}()
		packet, err := cgen.Generate(u)
		if err != nil {
			genErr = err
			return
		}
		lineage, err := GenerateLineage(u, nil)
		if err != nil {
			genErr = err
			return
		}
		maps.Copy(packet, lineage)
		files = packet
	}()
	if genErr != nil {
		t.Fatalf("generate %s: %v", schemaPath, genErr)
	}
	if files == nil || files["ProbeTable.h"] == nil {
		t.Fatalf("generate %s: no ProbeTable.h", schemaPath)
	}
	return files
}

func plantPongWritesBoost(header string) (string, bool) {
	const empty = "        case EFFECT_TYPE_PONG:\n        {\n            break;\n        }"
	if strings.Count(header, empty) != 1 {
		return "", false
	}
	const marker = "schema_tblr2_boost_fixed_write_body_( b +"
	i := strings.Index(header, marker)
	if i < 0 {
		return "", false
	}
	lineStart := strings.LastIndex(header[:i], "\n") + 1
	relEnd := strings.Index(header[i:], "\n")
	if relEnd < 0 {
		return "", false
	}
	call := strings.TrimSpace(header[lineStart : i+relEnd])
	if strings.HasPrefix(call, "static ") || !strings.Contains(call, "&value->effect.as.boost") {
		return "", false
	}
	planted := "        case EFFECT_TYPE_PONG:\n        {\n            " + call + "\n            break;\n        }"
	return strings.Replace(header, empty, planted, 1), true
}

func plantDirectStringCopiesNothing(header string) (string, bool) {
	const real = "memcpy( b + 9, value->value.as.c.value, (size_t) ( value->value.as.c.value_length ) );"
	const fault = "memcpy( b + 9, value->value.as.c.value, (size_t) 0 );"
	if strings.Count(header, real) != 1 {
		return "", false
	}
	return strings.Replace(header, real, fault, 1), true
}

func runFixedHeaderProbe(t *testing.T, files map[string][]byte, body string) (string, error) {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Fatalf("functional C probe requires cc on PATH")
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src := "#include <stdio.h>\n#include <stdint.h>\n#include <string.h>\n#include \"ProbeTable.h\"\nint main( void )\n{\n" + body + "\n    return 0;\n}\n"
	mainC := filepath.Join(dir, "probe.c")
	if err := os.WriteFile(mainC, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-I", dir, mainC, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		return string(out), fmt.Errorf("compile: %w", err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
