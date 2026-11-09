package ctable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cqualify"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var cFixedFormSourceFiles = []string{
	"fixedform.h",
	"fixedform_main.c",
	"fixedform_fx1.c",
	"fixedform_fx2.c",
	"fixedform_layout.c",
	"fixedform_v1.c",
	"fixedform_v2.c",
	"fixedform_ut1.c",
	"fixedform_ut2.c",
	"fixedform_fu1.c",
	"fixedform_fu2.c",
}

var cFixedFormReaderUnits = []string{
	"fixedform_fx1.c",
	"fixedform_fx2.c",
	"fixedform_layout.c",
	"fixedform_v1.c",
	"fixedform_v2.c",
	"fixedform_ut1.c",
	"fixedform_ut2.c",
	"fixedform_fu1.c",
	"fixedform_fu2.c",
}

var cFixedFormForbiddenTokens = []string{
	"_fixed_load_tolerant",
	"_TABLE_REFUSE_NONE",
	".clamped",
	"->clamped",
	".kind_mismatch",
	"->kind_mismatch",
	".duplicate",
	"->duplicate",
	".malformed",
	"->malformed",
}

func checkCFixedFormSourcesStrict(sources map[string]string) error {
	for _, name := range cFixedFormSourceFiles {
		src, ok := sources[name]
		if !ok || len(strings.TrimSpace(src)) == 0 {
			return fmt.Errorf("missing or empty C fixed-form source %s", name)
		}
		for _, bad := range cFixedFormForbiddenTokens {
			if strings.Contains(src, bad) {
				return fmt.Errorf("%s still contains retired tolerant token %q", name, bad)
			}
		}
	}
	for _, name := range cFixedFormReaderUnits {
		src := sources[name]
		if !strings.Contains(src, "_fixed_load(") && !strings.Contains(src, "_fixed_load (") {
			return fmt.Errorf("%s never calls strict *_fixed_load", name)
		}
	}
	return nil
}

func loadCFixedFormSourcesFromDisk(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "test", "c-tables")
	out := make(map[string]string, len(cFixedFormSourceFiles))
	for _, name := range cFixedFormSourceFiles {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out[name] = string(b)
	}
	return out
}

var (
	cStrictCorpusOnce sync.Once
	cStrictCorpusDir  string
	cStrictCorpusErr  error
)

func cStrictCorpus(t *testing.T) string {
	t.Helper()
	primary, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(primary, "old_field_append.bin")); err == nil {
		return primary
	}
	cached, err := filepath.Abs("../../../build/fixedform-corpus-strict")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cached, "old_field_append.bin")); err == nil {
		return cached
	}
	cStrictCorpusOnce.Do(func() {
		cmd := exec.Command("make", "-C", "../../..", "tables-fixedform-corpus")
		out, err := cmd.CombinedOutput()
		if err != nil {
			cStrictCorpusErr = fmt.Errorf("make tables-fixedform-corpus: %w\n%s", err, out)
			return
		}
		_ = os.RemoveAll(cached)
		if err := os.Rename(primary, cached); err != nil {
			cStrictCorpusErr = fmt.Errorf("stage fixedform-corpus-strict: %w", err)
			return
		}
		cStrictCorpusDir = cached
	})
	if cStrictCorpusErr != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and corpus build failed: %v", cStrictCorpusErr)
		}
		t.Skipf("fixedform corpus unavailable: %v", cStrictCorpusErr)
	}
	return cStrictCorpusDir
}

func cUnitNamed(t *testing.T, base, src string) *ir.Unit {
	t.Helper()
	fname := base + ".schema"
	f, perrs := parser.Parse(fname, []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", fname, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: fname, Name: fname, Base: base, Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", fname, cerrs[0])
	}
	return u
}

type cHarnessMutation struct {
	unitFile string
	target   string
	replace  string
}

func buildAndRunCFixedFormHarness(t *testing.T, mut *cHarnessMutation) ([]byte, error) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Skipf("cc: %v", err)
	}
	dir := t.TempDir()
	schemas := []string{"FX1", "FX2", "V1", "V2", "UT1", "UT2", "FU1", "FU2"}
	var genSources []string
	var incFlags []string
	mutApplied := false
	for _, s := range schemas {
		lower := strings.ToLower(s)
		sub := filepath.Join(dir, lower)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		raw := cReadSchema(t, s)
		u := cUnitNamed(t, s, raw)
		files, err := cgen.Generate(u)
		if err != nil {
			t.Fatalf("cgen.Generate(%s): %v", s, err)
		}
		tfiles, err := Generate(u)
		if err != nil {
			t.Fatalf("ctable.Generate(%s): %v", s, err)
		}
		maps.Copy(files, tfiles)
		files, err = cqualify.Qualify(files, u.Package)
		if err != nil {
			t.Fatalf("cqualify.Qualify(%s): %v", s, err)
		}
		for name, data := range files {
			if strings.Contains(name, "/") {
				continue
			}
			rel := lower + "/" + name
			if mut != nil && rel == mut.unitFile {
				text := string(data)
				if !strings.Contains(text, mut.target) {
					t.Fatalf("mutation target %q not found in %s", mut.target, rel)
				}
				data = []byte(strings.ReplaceAll(text, mut.target, mut.replace))
				mutApplied = true
			}
			if err := os.WriteFile(filepath.Join(sub, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		genSources = append(genSources, filepath.Join(sub, s+"Table.c"))
		incFlags = append(incFlags, "-I"+sub)
	}
	cTablesDir, err := filepath.Abs("../../../test/c-tables")
	if err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(dir, "c-tables")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var cSources []string
	for _, name := range cFixedFormSourceFiles {
		b, err := os.ReadFile(filepath.Join(cTablesDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if mut != nil && name == mut.unitFile {
			text := string(b)
			if !strings.Contains(text, mut.target) {
				t.Fatalf("mutation target %q not found in %s", mut.target, name)
			}
			b = []byte(strings.Replace(text, mut.target, mut.replace, 1))
			mutApplied = true
		}
		dst := filepath.Join(srcDir, name)
		if err := os.WriteFile(dst, b, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") {
			cSources = append(cSources, dst)
		}
	}
	if mut != nil && !mutApplied {
		t.Fatalf("mutation on %s was never applied", mut.unitFile)
	}
	serialize, err := filepath.Abs("../../../../serialize.c")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "schema_test_c_fixedform")
	baseFlags := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-ffp-contract=off"}
	baseFlags = append(baseFlags, gccFortifySilence(cc)...)
	baseFlags = append(baseFlags, "-I"+srcDir, "-I"+serialize)
	baseFlags = append(baseFlags, incFlags...)
	allSrcs := append(append([]string{}, cSources...), genSources...)
	objs := make([]string, len(allSrcs))
	var wg sync.WaitGroup
	var firstErr error
	var firstOut []byte
	var mu sync.Mutex
	for i, src := range allSrcs {
		obj := fmt.Sprintf("%s.%d.o", src, i)
		objs[i] = obj
		wg.Add(1)
		go func(src, obj string) {
			defer wg.Done()
			args := append(append([]string{}, baseFlags...), "-c", src, "-o", obj)
			if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("compile %s: %w", filepath.Base(src), err)
					firstOut = out
				}
				mu.Unlock()
			}
		}(src, obj)
	}
	wg.Wait()
	if firstErr != nil {
		return firstOut, firstErr
	}
	linkArgs := append(append([]string{}, objs...), "-o", bin, "-lm")
	if out, err := exec.Command(cc, linkArgs...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("link harness: %w", err)
	}
	return exec.Command(bin).CombinedOutput()
}

const cStrictT01Macros = `
#define RESET_BACK() do { int z_; \
    memset( &fresh, 0, sizeof( fresh ) ); memset( back, 0x5A, sizeof( back ) ); \
    @SNAKE@_reset( &fresh ); for ( z_ = 0; z_ < 8; ++z_ ) { @SNAKE@_reset( &back[z_] ); } \
    memset( &r, 0, sizeof( r ) ); } while ( 0 )
#define CHECK_POISON( WHO ) do { int z_; \
    for ( z_ = 0; z_ < 8; ++z_ ) { if ( memcmp( &back[z_], &fresh, sizeof( fresh ) ) != 0 ) \
        { printf( "%s: destination slot %d was written before refusal\n", (WHO), z_ ); return 1; } } } while ( 0 )
#define EXPECT_REFUSED( WHO, REASON, HASH ) do { TableReport e_; \
    memset( &e_, 0, sizeof( e_ ) ); e_.refused = 1; e_.reason = (REASON); e_.layout_hash = (HASH); \
    if ( n != -1 ) { printf( "%s: n=%lld, not -1\n", (WHO), (long long) n ); return 1; } \
    if ( r.refused != 1 || r.reason != (REASON) || r.refuse_reason != 0 ) { printf( "%s: refused=%d reason=%d refuse_reason=%d, not reason %d\n", (WHO), r.refused, r.reason, r.refuse_reason, (int) (REASON) ); return 1; } \
    if ( memcmp( &r, &e_, sizeof( e_ ) ) != 0 ) { printf( "%s: the report is not exactly refused+reason(+layout_hash): u=%d w=%d rr=%d layout_hash=%llx\n", (WHO), r.unknown, r.widened, r.refuse_reason, (unsigned long long) r.layout_hash ); return 1; } \
    CHECK_POISON( WHO ); } while ( 0 )
#define EXPECT_WIRE_DAMAGED( WHO ) do { TableReport e_; \
    memset( &e_, 0, sizeof( e_ ) ); e_.refused = 1; e_.refuse_reason = SCHEMA_TABLE_REFUSE_WIRE_DAMAGED; \
    if ( n != -1 ) { printf( "%s: n=%lld, not -1\n", (WHO), (long long) n ); return 1; } \
    if ( r.refused != 1 || r.reason != 0 || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED ) { printf( "%s: refused=%d reason=%d refuse_reason=%d beside wire_damaged\n", (WHO), r.refused, r.reason, r.refuse_reason ); return 1; } \
    if ( memcmp( &r, &e_, sizeof( e_ ) ) != 0 ) { printf( "%s: the report is not exactly wire_damaged\n", (WHO) ); return 1; } \
    CHECK_POISON( WHO ); } while ( 0 )
`

func cStrictT01Probe(t *testing.T, reader string, older []string, retire int, file, body string) {
	t.Helper()
	snake := ir.RustSnake(cFixedRootName(t, cUnitOf(t, reader)))
	expanded := strings.NewReplacer("@LOAD@", snake+"_fixed_load", "@SNAKE@", snake).Replace(cStrictT01Macros + body)
	out, err := cRunVersionProbe(t, reader, older, retire, file, expanded, "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestCFixedFormSourcesStrict(t *testing.T) {
	t.Run("sources", func(t *testing.T) {
		sources := loadCFixedFormSourcesFromDisk(t)
		if err := checkCFixedFormSourcesStrict(sources); err != nil {
			t.Fatal(err)
		}
	})

	slowtest.Gate(t, "functional test: invokes the C compiler (cc) and runs compiled C fixed-form binaries")
	corpus := cStrictCorpus(t)

	// c/F1, c/F2, c/F3 and the 8 valid-data rows (fixed-arrays, bool-values,
	// ranged-integers, utf8-values, nested-types, nested-fixed-tables,
	// counted-arrays, union-values) across test/c-tables/fixedform_*.c.
	t.Run("c_fixedform_harness", func(t *testing.T) {
		t.Parallel()
		out, err := buildAndRunCFixedFormHarness(t, nil)
		if err != nil {
			t.Fatalf("c_fixedform_harness failed: %v\n%s", err, out)
		}
		if !bytes.Contains(out, []byte("the fixed form reads BACKWARD, on the C leg")) {
			t.Fatalf("missing completion banner in harness output:\n%s", out)
		}
	})

	// c/F7: every length from 0 through kTableFixedHeaderBytes + 4 - 1 refuses wire_damaged.
	t.Run("c/F7_under_20_bytes", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_field_append")
		root := cFixedRootName(t, cUnitOf(t, newer))
		snake := ir.RustSnake(root)
		body := fmt.Sprintf(`
    {
        int64_t k;
        for ( k = 0; k < kTableFixedHeaderBytes + 4; ++k )
        {
            memset( &fresh, 0, sizeof( fresh ) );
            memset( back, 0, sizeof( back ) );
            %s_reset( &fresh );
            %s_reset( &back[0] );
            memset( &r, 0, sizeof( r ) );
            n = %s_fixed_load( back, 8, data, k, plan, 4096, NULL, &r );
            if ( n != -1 )
                { printf( "len=%%lld: n=%%lld, want -1\n", (long long) k, (long long) n ); return 1; }
            if ( !r.refused || r.reason != 0 || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED )
                { printf( "len=%%lld: refused=%%d reason=%%d refuse_reason=%%d\n", (long long) k, r.refused, r.reason, r.refuse_reason ); return 1; }
            if ( r.widened != 0 || r.unknown != 0 || r.layout_hash != 0 )
                { printf( "len=%%lld: counters moved on a short header\n", (long long) k ); return 1; }
            if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 )
                { printf( "len=%%lld: destination was written\n", (long long) k ); return 1; }
        }
    }
`, snake, snake, snake)
		out, err := cRunVersionProbe(t, newer, nil, 0, filepath.Join(corpus, "new_field_append.bin"), body, "")
		if err != nil {
			t.Fatalf("c/F7_under_20_bytes: %v\n%s", err, out)
		}
	})

	// c/F8: every non-zero remainder of (len - 20 - layout_bytes) % record_bytes refuses wire_damaged.
	t.Run("c/F8_ragged_tail", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_field_append")
		u := cUnitOf(t, newer)
		root := cFixedRoot(t, u)
		snake := ir.RustSnake(root.Name)
		recBytes := 8 + ir.TableFixedTypeBytes(root)
		body := fmt.Sprintf(`
    {
        const int64_t record_bytes = %d;
        const int64_t clean_len = len;
        int64_t extra;
        if ( record_bytes < 2 ) { printf( "record_bytes=%%lld has no remainder to sweep\n", (long long) record_bytes ); return 1; }
        for ( extra = 1; extra < record_bytes; ++extra )
        {
            data[clean_len + extra - 1] = 0xA5u;
            memset( &fresh, 0, sizeof( fresh ) );
            memset( back, 0, sizeof( back ) );
            %s_reset( &fresh );
            %s_reset( &back[0] );
            memset( &r, 0, sizeof( r ) );
            n = %s_fixed_load( back, 8, data, clean_len + extra, plan, 4096, NULL, &r );
            if ( n != -1 )
                { printf( "extra=%%lld: n=%%lld, want -1\n", (long long) extra, (long long) n ); return 1; }
            if ( !r.refused || r.reason != 0 || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED )
                { printf( "extra=%%lld: refused=%%d reason=%%d refuse_reason=%%d\n", (long long) extra, r.refused, r.reason, r.refuse_reason ); return 1; }
            if ( r.widened != 0 || r.unknown != 0 || r.layout_hash != 0 )
                { printf( "extra=%%lld: counters moved on a ragged tail\n", (long long) extra ); return 1; }
            if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 )
                { printf( "extra=%%lld: destination was written\n", (long long) extra ); return 1; }
        }
    }
`, recBytes, snake, snake, snake)
		out, err := cRunVersionProbe(t, newer, nil, 0, filepath.Join(corpus, "new_field_append.bin"), body, "")
		if err != nil {
			t.Fatalf("c/F8_ragged_tail: %v\n%s", err, out)
		}
	})

	// c/F9: batch_too_large on identity and lineage lanes.
	t.Run("c/F9_batch_too_large", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_int_widen")
		older := cReadSchema(t, "VOLD_int_widen")
		body := `
    {
        const int64_t count = n;
        int64_t c;
        char who[64];
        if ( count < 1 || count > 8 ) { printf( "the corpus file carries no record\n" ); return 1; }
        for ( c = 0; c < count; ++c )
        {
            snprintf( who, sizeof( who ), "batch_too_large capacity=%lld of %lld", (long long) c, (long long) count );
            RESET_BACK();
            n = @LOAD@( back, c, data, len, plan, 4096, NULL, &r );
            EXPECT_REFUSED( who, SCHEMA_TABLE_BATCH_TOO_LARGE, 0 );
        }
        RESET_BACK();
        n = @LOAD@( back, count, data, len, plan, 4096, NULL, &r );
        if ( n != count || r.refused || r.reason != 0 || r.refuse_reason != 0 ) { printf( "capacity == count must read: n=%lld refused=%d reason=%d\n", (long long) n, r.refused, r.reason ); return 1; }
    }
`
		for _, lane := range []struct{ name, file string }{
			{"identity", "new_int_widen.bin"}, {"lineage", "old_int_widen.bin"},
		} {
			t.Run(lane.name, func(t *testing.T) {
				cStrictT01Probe(t, newer, []string{older}, 0, filepath.Join(corpus, lane.file), body)
			})
		}
	})

	// c/F12: second layout under a held hash refuses layout_malformed.
	t.Run("c/F12_second_layout", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_field_append")
		older := cReadSchema(t, "VOLD_field_append")
		newFile := filepath.Join(corpus, "new_field_append.bin")
		oldFile := filepath.Join(corpus, "old_field_append.bin")
		nh, nl, nr := cT01Split(t, newFile)
		oh, ol, or := cT01Split(t, oldFile)
		if bytes.Equal(nl, ol) {
			t.Fatal("the two lock entries carry identical layout bytes")
		}
		malformed := `
    {
        RESET_BACK();
        n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "a second layout under a held hash", SCHEMA_TABLE_LAYOUT_MALFORMED, 0 );
    }
`
		cStrictT01Probe(t, newer, []string{older}, 0, cT01Forge(t, "f1.bin", cT01Join(nh, ol, nr)), malformed)
		cStrictT01Probe(t, newer, []string{older}, 0, cT01Forge(t, "f2.bin", cT01Join(oh, nl, or)), malformed)
		cStrictT01Probe(t, newer, []string{older}, 0, newFile, `
    {
        static uint8_t orig[1 << 20];
        const int64_t lb = table_fixed_get32( data + kTableFixedHeaderBytes );
        int64_t b;
        char who[64];
        memcpy( orig, data, (size_t) len );
        for ( b = 0; b < lb; ++b )
        {
            memcpy( data, orig, (size_t) len );
            data[kTableFixedHeaderBytes + 4 + b] ^= 0x01;
            snprintf( who, sizeof( who ), "layout byte %lld bent", (long long) b );
            RESET_BACK();
            n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
            EXPECT_REFUSED( who, SCHEMA_TABLE_LAYOUT_MALFORMED, 0 );
        }
    }
`)
	})

	// c/R7: identity lane is an index comparison, never a recomputed hash.
	t.Run("c/R7_identity_lane", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_cfloat_res_refine")
		older := cReadSchema(t, "VOLD_cfloat_res_refine")
		newFile := filepath.Join(corpus, "new_cfloat_res_refine.bin")
		oldFile := filepath.Join(corpus, "old_cfloat_res_refine.bin")
		u := cUnitOf(t, newer)
		o := cUnitOf(t, older)
		lineage := map[string][]FixedLineageEntry{}
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
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
		seen := 0
		for _, data := range files {
			seen += strings.Count(string(data), "table_fixed_hash_of")
		}
		if seen != 1 {
			t.Fatalf("table_fixed_hash_of appears %d times in the generated unit, want 1", seen)
		}
		cStrictT01Probe(t, newer, []string{older}, 0, newFile, `
    if ( n != 1 || r.refused || r.reason != 0 || r.refuse_reason != 0 ) { printf( "own file: n=%lld refused=%d\n", (long long) n, r.refused ); return 1; }
    if ( @SNAKE@_fixed_lineage_ready != SCHEMA_TABLE_FIXED_ONCE_UNBUILT ) { printf( "the identity lane built the lineage\n" ); return 1; }
`)
		cStrictT01Probe(t, newer, []string{older}, 0, oldFile, `
    if ( n != 1 || r.refused || r.reason != 0 || r.refuse_reason != 0 ) { printf( "older file: n=%lld refused=%d\n", (long long) n, r.refused ); return 1; }
    if ( @SNAKE@_fixed_lineage_ready == SCHEMA_TABLE_FIXED_ONCE_UNBUILT || @SNAKE@_fixed_lineage[0].entries == NULL )
        { printf( "equal layout bytes under a different hash took the identity lane\n" ); return 1; }
`)
	})

	// c/R8: unknown hash refuses layout_newer with the file's hash.
	t.Run("c/R8_layout_newer", func(t *testing.T) {
		t.Parallel()
		older := cReadSchema(t, "VOLD_field_append")
		newFile := filepath.Join(corpus, "new_field_append.bin")
		h, l, _ := cT01Split(t, newFile)
		cStrictT01Probe(t, older, nil, 0, newFile, `
    RESET_BACK();
    n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
    EXPECT_REFUSED( "old reader, new writer's file", SCHEMA_TABLE_LAYOUT_NEWER, want );
    if ( want == 0 ) { printf( "the corpus hash is zero\n" ); return 1; }
`)
		cStrictT01Probe(t, older, nil, 0, cT01Forge(t, "f_r8.bin", cT01Join(h, l, nil)), `
    {
        const uint64_t strangers[] = { 1ull, 0xDEADBEEFCAFEF00Dull, ~0ull, want ^ ( 1ull << 63 ) };
        int c;
        char who[64];
        for ( c = 0; c < 4; ++c )
        {
            memcpy( data + kTableFixedHashAt, &strangers[c], 8 );
            snprintf( who, sizeof( who ), "stranger hash %d", c );
            RESET_BACK();
            n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
            EXPECT_REFUSED( who, SCHEMA_TABLE_LAYOUT_NEWER, strangers[c] );
        }
    }
`)
	})

	// c/R9: known hash with tampered layout length or bytes refuses layout_malformed.
	t.Run("c/R9_known_hash_lie", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_field_append")
		older := cReadSchema(t, "VOLD_field_append")
		body := `
    {
        static uint8_t orig[1 << 20];
        const int64_t L = len;
        const int64_t lb = table_fixed_get32( data + kTableFixedHeaderBytes );
        uint8_t * lay;
        int64_t flen;
        int c, k;
        char who[96];
        memcpy( orig, data, (size_t) L );
        for ( c = 0; c < 11; ++c )
        {
            memcpy( data, orig, (size_t) L );
            lay = data + kTableFixedHeaderBytes + 4;
            flen = L;
            switch ( c )
            {
            case 0: table_fixed_put32( lay, table_fixed_get32( lay ) + 1 ); break;
            case 1: lay[4 + 17 * 1 + 8] = 31; break;
            case 2: table_fixed_put32( lay + 4 + 17 * 1 + 9, table_fixed_get32( lay + 4 + 17 * 1 + 9 ) + 3 ); break;
            case 3: lay[4 + 8] = 14; break;
            case 4: table_fixed_put32( lay + 4 + 13, table_fixed_get32( lay + 4 + 13 ) + 1 ); break;
            case 5: table_fixed_put32( lay + 4 + 9, 65537 ); break;
            case 6:
                table_fixed_put32( data + kTableFixedHeaderBytes, 4 + 17 * 66 );
                table_fixed_put32( lay, 66 );
                for ( k = 0; k < 66; ++k )
                {
                    uint8_t * e = lay + 4 + 17 * k;
                    memset( e, 0, 17 );
                    e[8] = ( k == 65 ) ? 6 : 13;
                    table_fixed_put32( e + 9, 1 );
                    table_fixed_put32( e + 13, ( k == 65 ) ? 0 : 1 );
                }
                flen = kTableFixedHeaderBytes + 4 + 4 + 17 * 66 + 16;
                memset( lay + 4 + 17 * 66, 0, 16 );
                break;
            case 7: table_fixed_put32( data + kTableFixedHeaderBytes, 3 ); break;
            case 8: table_fixed_put32( data + kTableFixedHeaderBytes, 0 ); break;
            case 9: table_fixed_put32( data + kTableFixedHeaderBytes, (uint32_t) ( lb - 17 ) ); break;
            case 10: table_fixed_put32( data + kTableFixedHeaderBytes, (uint32_t) ( lb + 17 ) ); memset( data + L, 0, 17 ); flen = L + 17; break;
            }
            snprintf( who, sizeof( who ), "known_hash_lie case %d", c );
            RESET_BACK();
            n = @LOAD@( back, 8, data, flen, plan, 4096, NULL, &r );
            EXPECT_REFUSED( who, SCHEMA_TABLE_LAYOUT_MALFORMED, 0 );
        }
    }
`
		for _, lane := range []struct{ name, file string }{
			{"identity", "new_field_append.bin"}, {"lineage", "old_field_append.bin"},
		} {
			t.Run(lane.name, func(t *testing.T) {
				cStrictT01Probe(t, newer, []string{older}, 0, filepath.Join(corpus, lane.file), body)
			})
		}
	})

	// c/R13: REFUSE is total across all named and wire_damaged refusals.
	t.Run("c/R13_refuse_is_total", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_field_append")
		older := cReadSchema(t, "VOLD_field_append")
		newFile := filepath.Join(corpus, "new_field_append.bin")
		oldFile := filepath.Join(corpus, "old_field_append.bin")
		cStrictT01Probe(t, newer, []string{older}, 0, newFile, `
    {
        static uint8_t orig[1 << 20];
        const int64_t L = len;
        const int64_t lb = table_fixed_get32( data + kTableFixedHeaderBytes );
        memcpy( orig, data, (size_t) L );

        data[0] = 1; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "previous_form", SCHEMA_TABLE_PREVIOUS_FORM, 0 );
        memcpy( data, orig, (size_t) L );
        data[0] = 2; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "message_form_as_file", SCHEMA_TABLE_MESSAGE_FORM_AS_FILE, 0 );
        memcpy( data, orig, (size_t) L );
        data[0] = 9; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "newer_form", SCHEMA_TABLE_NEWER_FORM, 0 );
        memcpy( data, orig, (size_t) L );

        { const uint64_t stranger = want ^ 1ull; memcpy( data + kTableFixedHashAt, &stranger, 8 );
          RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
          EXPECT_REFUSED( "layout_newer", SCHEMA_TABLE_LAYOUT_NEWER, stranger ); }
        memcpy( data, orig, (size_t) L );

        data[kTableFixedHeaderBytes + 4] ^= 0x01; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "layout_malformed", SCHEMA_TABLE_LAYOUT_MALFORMED, 0 );
        memcpy( data, orig, (size_t) L );

        RESET_BACK(); n = @LOAD@( back, 0, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "batch_too_large", SCHEMA_TABLE_BATCH_TOO_LARGE, 0 );

        data[kTableFixedHeaderBytes + 4 + lb] ^= 0xFF; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "no_layout", SCHEMA_TABLE_NO_LAYOUT, 0 );
        memcpy( data, orig, (size_t) L );

        RESET_BACK(); n = @LOAD@( back, 8, data, L + 1, plan, 4096, NULL, &r );
        EXPECT_WIRE_DAMAGED( "ragged tail" );
        RESET_BACK(); n = @LOAD@( back, 8, data, kTableFixedHeaderBytes + 3, plan, 4096, NULL, &r );
        EXPECT_WIRE_DAMAGED( "under twenty bytes" );
        data[3] = 1; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_WIRE_DAMAGED( "nonzero reserved byte" );
        memcpy( data, orig, (size_t) L );
    }
`)
		cStrictT01Probe(t, newer, []string{older}, 1, oldFile, `
    RESET_BACK();
    n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
    EXPECT_REFUSED( "layout_unsupported", SCHEMA_TABLE_LAYOUT_UNSUPPORTED, want );
`)
		cStrictT01Probe(t, newer, []string{older}, 0, oldFile, `
    RESET_BACK();
    n = @LOAD@( back, 8, data, len, plan, 0, NULL, &r );
    EXPECT_REFUSED( "plan_too_large", SCHEMA_TABLE_PLAN_TOO_LARGE, 0 );
`)
	})

	// c/F10, c/R12: per-record hash mismatch and out-of-range refusal leave poisoned destination untouched.
	t.Run("c/F10_R12_refuse_writes_nothing", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_nested_append")
		older := cReadSchema(t, "VOLD_nested_append")
		poison := `    memset( back, 0x5A, sizeof( back ) );`
		pre := `
    {
        uint32_t layout_bytes;
        memcpy( &layout_bytes, data + kTableFixedHeaderBytes, 4 );
        if ( (int64_t) kTableFixedHeaderBytes + 4 + (int64_t) layout_bytes + 8 > len )
            { printf( "the corpus file carries no record header\n" ); return 1; }
        data[kTableFixedHeaderBytes + 4 + layout_bytes] ^= 0xFF;
    }`
		body := `
    if ( n != -1 )
        { printf( "a record whose 8-byte hash disagrees with the file's was not refused: n=%lld\n", (long long) n ); return 1; }
    if ( !r.refused || r.reason != SCHEMA_TABLE_NO_LAYOUT || r.refuse_reason != 0 )
        { printf( "expected refused=1 reason=SCHEMA_TABLE_NO_LAYOUT, got refused=%d reason=%d rr=%d\n", r.refused, r.reason, r.refuse_reason ); return 1; }
    if ( r.unknown != 0 || r.widened != 0 )
        { printf( "counters moved before refusal: u=%d w=%d\n", r.unknown, r.widened ); return 1; }
    {
        const uint8_t * p = (const uint8_t *) back;
        size_t i;
        for ( i = 0; i < sizeof( back ); ++i )
        {
            if ( p[i] != 0x5Au )
                { printf( "REFUSE wrote destination byte %zu (0x%02X, not 0x5A)\n", i, (unsigned) p[i] ); return 1; }
        }
    }
`
		out, err := cRunVersionProbe(t, newer, []string{older}, 0, filepath.Join(corpus, "old_nested_append.bin"), body, poison, pre)
		if err != nil {
			t.Fatalf("c/F10_R12_refuse_writes_nothing: %v\n%s", err, out)
		}
	})

	// c/R11: writer_bound_count with two older endpoints (VOLD bound 4, VMID bound 6, VNEW bound 8).
	// Under the strict read, forging count past the writer's bound refuses out_of_range!
	t.Run("c/R11_writer_bound_count", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_array_bounded_grow")
		older := cReadSchema(t, "VOLD_array_bounded_grow")
		mid := cReadSchema(t, "VMID_array_bounded_grow")
		forgeTo := func(forged byte) string {
			return fmt.Sprintf(`
    {
        int seen = 0;
        int64_t at;
        for ( at = 0; at + 8 <= len; ++at )
        {
            if ( data[at] == 0xAA && data[at+1] == 0xAA && data[at+2] == 0xAA && data[at+3] == 0xAA &&
                 data[at+4] == 0x04 && data[at+5] == 0x00 && data[at+6] == 0x00 && data[at+7] == 0x00 )
            {
                if ( seen ) { printf( "the count word needle matched twice\n" ); return 1; }
                seen = 1;
                data[at+4] = 0x%02X;
            }
        }
        if ( !seen ) { printf( "the count word needle never occurred\n" ); return 1; }
    }`, forged)
		}
		refuseBody := `
    if ( n != -1 ) { printf( "forged count past writer bound must refuse: n=%lld\n", (long long) n ); return 1; }
    if ( !r.refused || r.reason != 0 || r.refuse_reason != SCHEMA_TABLE_REFUSE_OUT_OF_RANGE )
        { printf( "expected SCHEMA_TABLE_REFUSE_OUT_OF_RANGE: refused=%d reason=%d rr=%d\n", r.refused, r.reason, r.refuse_reason ); return 1; }
    if ( r.unknown != 0 || r.widened != 0 || r.layout_hash != 0 )
        { printf( "counters moved on out_of_range refusal\n" ); return 1; }
    if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 )
        { printf( "out_of_range refusal left destination dirty\n" ); return 1; }
`
		for _, forged := range []byte{5, 7} {
			out, err := cRunVersionProbe(t, newer, []string{older, mid}, 0,
				filepath.Join(corpus, "old_array_bounded_grow.bin"), refuseBody, "", forgeTo(forged))
			if err != nil {
				t.Fatalf("writer_bound_count forged=%d: %v\n%s", forged, err, out)
			}
		}
	})

	// c/R30: compressed floats ride as float32 and obey strict TableReport.
	t.Run("c/R30_compressed_floats", func(t *testing.T) {
		t.Parallel()
		cc := os.Getenv("CC")
		if cc == "" {
			cc = "cc"
		}
		t14CiteCoarser(t)
		src := t14Emit(t, t14Old, nil)
		if !strings.Contains(src, "table_fixed_putf32(") || !strings.Contains(src, "SCHEMA_TABLE_FLOAT_FORCE_ROUND") {
			t.Fatal("missing table_fixed_putf32 or SCHEMA_TABLE_FLOAT_FORCE_ROUND")
		}
		dir := t.TempDir()
		file := filepath.Join(dir, "old.bin")
		strictReadProbe := fmt.Sprintf(`
int main(void) {
    uint8_t data[1 << 16];
    Reading back[1];
    TableReport r;
    static TableFixedEntry plan[4096];
    union { uint32_t u; float f; } bits;
    FILE * f = fopen(%q, "rb");
    int64_t n, len;
    if (f == NULL) { printf("missing file\n"); return 1; }
    len = (int64_t)fread(data, 1, sizeof data, f);
    fclose(f);
    memset(&r, 0, sizeof r);
    reading_reset(&back[0]);
    n = reading_fixed_load(back, 1, data, len, plan, 4096, NULL, &r);
    if (n != 1) { printf("load refused n=%%lld\n", (long long)n); return 1; }
    bits.f = back[0].aim;
    if (bits.u != 0x3E99999Au || back[0].lead != 1 || back[0].trail != 2) {
        printf("value moved %%08x lead %%u trail %%u\n", bits.u, back[0].lead, back[0].trail);
        return 1;
    }
    if (r.refused != 0 || r.reason != 0 || r.refuse_reason != 0 || r.unknown != 0 || r.widened != 0) {
        printf("unexpected report on clean compressed float read\n"); return 1;
    }
    return 0;
}
`, file)
		t14Run(t, cc, t14Old, nil, t14WriteProbe(file), "")
		t14Run(t, cc, t14Old, nil, strictReadProbe, "")
		t14Run(t, cc, t14Fine, []string{t14Old}, strictReadProbe, "")
		t14Run(t, cc, t14Dropped, []string{t14Old}, strictReadProbe, "")
		t14Run(t, cc, t14Old, nil, t14ContractProbe(), "")
	})

	// c/E3/fixed-array-element and fixed-arrays/c/valid-data + strict versioning rows.
	t.Run("c/E3_and_strict_versioning", func(t *testing.T) {
		t.Parallel()
		for _, r := range cVersionRows {
			t.Run(r.row, func(t *testing.T) {
				t.Parallel()
				newer := cReadSchema(t, "VNEW_"+r.row)
				older := cReadSchema(t, "VOLD_"+r.row)
				root := cFixedRoot(t, cUnitOf(t, older))
				lead, trail := cBrackets(t, corpus, r.row, ir.TableFixedTypeBytes(root))
				bracket := ""
				if cHasField(root, "lead") && cHasField(root, "trail") {
					bracket = fmt.Sprintf(`
    if ( back[0].lead != 0x%08Xu || back[0].trail != 0x%08Xu )
        { printf( "the row moved a neighbour: lead=%%u trail=%%u\n", back[0].lead, back[0].trail ); return 1; }`, lead, trail)
				}
				counters := `if ( r.widened != 0 ) { printf( "widened %d on non-widening row\n", r.widened ); return 1; }`
				if r.widens {
					counters = `if ( r.widened == 0 ) { printf( "a widening row moved no widened counter\n" ); return 1; }`
				}
				body := fmt.Sprintf(`
    if ( n < 1 ) { printf( "the newer reader refused the older writer's file: n=%%lld reason=%%d rr=%%d\n", (long long) n, r.reason, r.refuse_reason ); return 1; }
    if ( r.refused != 0 || r.reason != 0 || r.refuse_reason != 0 || r.unknown != 0 )
        { printf( "unexpected report on clean NEW-READS-OLD: refused=%%d reason=%%d rr=%%d u=%%d\n", r.refused, r.reason, r.refuse_reason, r.unknown ); return 1; }
    %s
    %s
    %s
`, counters, bracket, r.check)
				poison := ""
				if r.poison {
					poison = `    memset( back, 0x5A, sizeof( back ) );`
				}
				out, err := cRunVersionProbe(t, newer, []string{older}, 0,
					filepath.Join(corpus, "old_"+r.row+".bin"), body, poison)
				if err != nil {
					t.Fatalf("NEW-READS-OLD %s: %v\n%s", r.row, err, out)
				}

				// OLD-REFUSES-NEW
				file := filepath.Join(corpus, "new_"+r.row+".bin")
				var oldBody string
				if r.sameHash {
					oldBody = `
    if ( n < 1 || r.refused != 0 || r.reason != 0 || r.refuse_reason != 0 )
        { printf( "a zero-layout-change row must read in both directions\n" ); return 1; }
`
				} else {
					oldBody = `
    if ( n != -1 || !r.refused || r.reason != SCHEMA_TABLE_LAYOUT_NEWER || r.refuse_reason != 0 )
        { printf( "expected SCHEMA_TABLE_LAYOUT_NEWER: n=%lld refused=%d reason=%d rr=%d\n", (long long) n, r.refused, r.reason, r.refuse_reason ); return 1; }
    if ( r.layout_hash != want ) { printf( "layout_newer reports THE FILE'S hash\n" ); return 1; }
    if ( r.widened != 0 || r.unknown != 0 ) { printf( "REFUSE is total: no counter moves\n" ); return 1; }
    if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 ) { printf( "REFUSE wrote destination bytes\n" ); return 1; }
`
				}
				out, err = cRunVersionProbe(t, older, nil, 0, file, oldBody, "")
				if err != nil {
					t.Fatalf("OLD-REFUSES-NEW %s: %v\n%s", r.row, err, out)
				}
			})
		}
	})
}

func TestCFixedFormSourcesStrictNegativeControl(t *testing.T) {
	// 1. Source-level negative controls: plant retired tolerant spellings into real C source artifacts.
	t.Run("source_plants", func(t *testing.T) {
		base := loadCFixedFormSourcesFromDisk(t)
		plants := []struct {
			name string
			file string
			bad  string
		}{
			{"fx1_tolerant_call", "fixedform_fx1.c", "/* schema_tblfx1_fx_root_fixed_load_tolerant */\n"},
			{"fu1_clamped_field", "fixedform_fu1.c", "/* r.clamped */\n"},
			{"v1_malformed_field", "fixedform_v1.c", "/* r.malformed */\n"},
			{"v2_kind_mismatch_field", "fixedform_v2.c", "/* r.kind_mismatch */\n"},
		}
		for _, p := range plants {
			mut := maps.Clone(base)
			mut[p.file] += p.bad
			if err := checkCFixedFormSourcesStrict(mut); err == nil {
				t.Fatalf("negative control %s: checkCFixedFormSourcesStrict accepted planted %q in %s", p.name, p.bad, p.file)
			}
		}
		mut := maps.Clone(base)
		mut["fixedform_fx1.c"] = strings.ReplaceAll(mut["fixedform_fx1.c"], "_fixed_load(", "_fixed_save(")
		if err := checkCFixedFormSourcesStrict(mut); err == nil {
			t.Fatal("negative control missing_load_call: checkCFixedFormSourcesStrict accepted reader unit without *_fixed_load")
		}
	})

	slowtest.Gate(t, "functional negative control: invokes the C compiler (cc) on mutated C fixed-form artifacts")

	// 2. Functional negative control: plant a fault in FX1Table.h disabling out_of_range refusal in the clamp pass.
	t.Run("plant_fx1_range_refusal", func(t *testing.T) {
		t.Parallel()
		out, err := buildAndRunCFixedFormHarness(t, &cHarnessMutation{
			unitFile: "fx1/FX1Table.h",
			target:   "schema_tblfx1_fx_root_fixed_clamp_body_( value, &out_of_range, &damaged );",
			replace:  "schema_tblfx1_fx_root_fixed_clamp_body_( value, &out_of_range, &damaged ); out_of_range = 0;",
		})
		if err == nil {
			t.Fatalf("negative control failed: harness exited 0 when FX1 out_of_range refusal was disabled:\n%s", out)
		}
		if !bytes.Contains(out, []byte("FAIL: C RANGE:")) {
			t.Fatalf("negative control failed for the wrong reason (want FAIL: C RANGE:):\n%s", out)
		}
	})

	// 3. Functional negative control: plant a fault in FX1Table.h disabling the reserved-byte header check (c/F3).
	t.Run("plant_fx1_header_reserved_refusal", func(t *testing.T) {
		t.Parallel()
		out, err := buildAndRunCFixedFormHarness(t, &cHarnessMutation{
			unitFile: "fx1/FX1Table.h",
			target:   "if ( data[reserved_at] != 0 )",
			replace:  "if ( 0 && data[reserved_at] != 0 )",
		})
		if err == nil {
			t.Fatalf("negative control failed: harness exited 0 when FX1 reserved-byte check was disabled:\n%s", out)
		}
		if !bytes.Contains(out, []byte("FAIL: C FORM HEADER: reserved byte 3 nonzero")) {
			t.Fatalf("negative control failed for the wrong reason (want FAIL: C FORM HEADER: reserved byte 3 nonzero):\n%s", out)
		}
	})

	// 4. Functional negative control: plant a fault in V1Table.h disabling the ordinal/count bounds refusal (c/R11).
	t.Run("plant_v1_ordinal_count_refusal", func(t *testing.T) {
		t.Parallel()
		out, err := buildAndRunCFixedFormHarness(t, &cHarnessMutation{
			unitFile: "v1/V1Table.h",
			target:   "schema_tblv1_cfg_fixed_clamp_body_( value, &out_of_range, &damaged );",
			replace:  "schema_tblv1_cfg_fixed_clamp_body_( value, &out_of_range, &damaged ); out_of_range = 0;",
		})
		if err == nil {
			t.Fatalf("negative control failed: harness exited 0 when V1 out_of_range refusal was disabled:\n%s", out)
		}
		if !bytes.Contains(out, []byte("FAIL: C ORDINAL:")) {
			t.Fatalf("negative control failed for the wrong reason (want FAIL: C ORDINAL:):\n%s", out)
		}
	})

	// 5. Functional negative control: plant a fault in the real test/c-tables/fixedform_fx1.c artifact.
	t.Run("plant_fixedform_fx1_c_artifact", func(t *testing.T) {
		t.Parallel()
		out, err := buildAndRunCFixedFormHarness(t, &cHarnessMutation{
			unitFile: "fixedform_fx1.c",
			target:   "v.renamed = 5000;  /* declared | min = 0, max = 1000 */\n    v.gone = -7;",
			replace:  "v.renamed = 500;\n    v.gone = 7;",
		})
		if err == nil {
			t.Fatalf("negative control failed: harness exited 0 when fixedform_fx1.c out-of-range writer was planted in-range:\n%s", out)
		}
		if !bytes.Contains(out, []byte("FAIL: C RANGE:")) {
			t.Fatalf("negative control failed for the wrong reason (want FAIL: C RANGE:):\n%s", out)
		}
	})
}

var _ = binary.LittleEndian
