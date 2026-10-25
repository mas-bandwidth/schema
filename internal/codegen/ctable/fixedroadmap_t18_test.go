package ctable

// TestFixedCT18DirectArmLines and TestFixedRoadmapCT18DirectArms are the c
// leg's card c-fixed-form-writes-direct-text-and-array-arms: the fixed-tables
// cell "Union arms holding scalar, text or array fields" for c, whose leaf is
// union-field-arms/c/valid-data (docs/roadmap-evidence/fixed-c.sexp:63), and
// the one red that holds all 31 C Variable-tables cells, vt/c/generator-panic
// (docs/roadmap.sexp:8329).
//
// A DIRECT text, bytes or array arm — `union Pick { label string(64) }` — is
// an arm whose type is a field's, not a type's (§2.6). The shared fixed-form
// gate already admits it (ir/fixedform.go TableFixedSupported:268-273 refuses
// only a pointer, map, list, keyed or optional arm), and the layout walk and
// plan already spell the arm's storage as the arm's own unnamed struct —
// `value` beside `value_length` or `value_count` (§2.6) — so the gap is the C
// WRITER: the union case of emitFixedWriteElement (internal/codegen/ctable/
// fixedform.go:331) recursed on the arm field itself, and emitFixedWriteElement
// sizes a text arm by ir.TableFixedStorageBytes (line 346; ir/fixedform.go:136)
// as 0, so line 363 emitted `table_fixed_put0(..., (uint0_t) ...)`. The bounds
// pass had the twin gap: a direct text arm carried no content rule and a direct
// array arm no element loop.
//
// The unit tier reads the emitted header text only — no compiler, clock, socket
// or network. The functional tier writes and reads each arm through the leg's
// own fixed save/load, compiled and run with cc through the cT10Run shape, and
// a second subtest generates A2.schema (the arm-retype reader, whose union
// Value carries a direct `c string(8)`) and compiles the header.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
)

// t18DirectArms is the FIXTURE used as written: four direct arms — a string, a
// bytes, a fixed array and a counted array — over the field grammar (§2.6),
// held by a fixed table. Newlines end fields and there are no semicolons.
const t18DirectArms = `package probe

union Pick
{
    label string(8)
    blob  bytes(4)
    cells [3]int32
    span  [..3]int32
}

fixed table Root
{
    pick Pick
    seq  int32
}
`

// TestFixedCT18DirectArmLines is the unit tier: it builds the DIRECTARMS unit
// and test/tables/A2.schema in process with unitFrom and GenerateLineage, as
// TestGenerateR2DoesNotPanic does, and asserts the generated header text only.
// The save must hold no table_fixed_put0 store — a direct text or bytes arm is
// a used-length store and that many units, a counted array arm its count then
// its live elements, a fixed array arm one element store per slot (§3.4) — and
// the arm's companion is spelled through the arm's own unnamed struct, `.as.<arm>.value`
// beside `.as.<arm>.value_length` or `.value_count` (§2.6).
func TestFixedCT18DirectArmLines(t *testing.T) {
	t.Parallel()

	u := unitFrom(t, t18DirectArms)
	files, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	header := ""
	for name, out := range files {
		if strings.HasSuffix(name, "Table.h") {
			header += string(out)
		}
	}
	if header == "" {
		t.Fatal("no *Table.h was emitted for the DIRECTARMS unit")
	}
	if strings.Contains(header, "table_fixed_put0") {
		t.Fatalf("a direct arm was sized as zero storage: the header emits table_fixed_put0:\n%s", header)
	}

	body := fixedWriteBody(t, header, "schema_probe_root_fixed_write_body_")
	// The string arm and the bytes arm are used-length stores: the length
	// companion is read and that many units copied (§3.4).
	if !strings.Contains(body, "value->pick.as.label.value_length") {
		t.Fatalf("the string arm's used length is not stored:\n%s", body)
	}
	if !strings.Contains(body, "value->pick.as.blob.value_length") {
		t.Fatalf("the bytes arm's used length is not stored:\n%s", body)
	}
	// The counted array arm is a live-count store then that many elements.
	if !strings.Contains(body, "value->pick.as.span.value_count") {
		t.Fatalf("the counted array arm's live count is not stored:\n%s", body)
	}
	if !strings.Contains(body, "value->pick.as.span.value[i]") {
		t.Fatalf("the counted array arm's elements are not stored:\n%s", body)
	}
	// The fixed array arm is one element store per slot, no count (§3.4).
	if !strings.Contains(body, "value->pick.as.cells[i]") {
		t.Fatalf("the fixed array arm's elements are not stored:\n%s", body)
	}

	// A2.schema carries the same direct string arm (`c string(8)` in union
	// Value); its header must hold no table_fixed_put0 either.
	a2, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "A2.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u2 := unitFrom(t, string(a2))
	files2, err := GenerateLineage(u2, nil)
	if err != nil {
		t.Fatalf("generate A2.schema: %v", err)
	}
	a2header := ""
	for name, out := range files2 {
		if strings.HasSuffix(name, "Table.h") {
			a2header += string(out)
		}
	}
	if a2header == "" {
		t.Fatal("no *Table.h was emitted for A2.schema")
	}
	if strings.Contains(a2header, "table_fixed_put0") {
		t.Fatalf("A2.schema's direct string arm was sized as zero storage: table_fixed_put0:\n%s", a2header)
	}
}

// TestFixedRoadmapCT18DirectArms is the functional tier: every direct arm
// round-trips through the generated C, written and read through the leg's own
// fixed save/load with non-default and boundary values, and A2.schema generates
// and compiles.
func TestFixedRoadmapCT18DirectArms(t *testing.T) {
	t.Parallel()
	t.Run("union-field-arms/c/valid-data", func(t *testing.T) {
		t.Parallel()
		cT10Run(t, t18DirectArms, "Root", `
    /* RECORD 1 — the string arm, full length (8) at the bound. */
    {
        int k;
        static const char want[] = "12345678";
        v.pick.type = PICK_TYPE_LABEL;
        memcpy( v.pick.as.label.value, want, 8 );
        v.pick.as.label.value_length = 8;
        v.seq = 2147483647;
        n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "label save failed\n" ); return 1; }
        memset( back, 0x5A, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        if ( root_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "label load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].pick.type != PICK_TYPE_LABEL ) { printf( "label arm=%d\n", (int) back[0].pick.type ); return 1; }
        if ( back[0].pick.as.label.value_length != 8 )
            { printf( "label length=%d\n", (int) back[0].pick.as.label.value_length ); return 1; }
        if ( memcmp( back[0].pick.as.label.value, want, 8 ) != 0 )
            { printf( "label value=%.8s\n", back[0].pick.as.label.value ); return 1; }
        if ( back[0].seq != 2147483647 ) { printf( "label moved seq=%d\n", (int) back[0].seq ); return 1; }
        if ( r.refused || r.malformed || r.clamped || r.widened )
            { printf( "label report dirty: %d %d %d %d\n", r.refused, r.malformed, r.clamped, r.widened ); return 1; }

        /* RECORD 2 — the string arm, zero length: nothing under the tag. */
        memset( &v, 0, sizeof( v ) );
        v.pick.type = PICK_TYPE_LABEL;
        v.pick.as.label.value_length = 0;
        v.seq = 2147483646;
        n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "empty label save failed\n" ); return 1; }
        memset( back, 0x5A, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        if ( root_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "empty label load refused=%d\n", r.refused ); return 1; }
        if ( back[0].pick.as.label.value_length != 0 )
            { printf( "empty label length=%d\n", (int) back[0].pick.as.label.value_length ); return 1; }
        if ( back[0].seq != 2147483646 ) { printf( "empty label moved seq=%d\n", (int) back[0].seq ); return 1; }

        /* RECORD 3 — the bytes arm, full length (4). */
        memset( &v, 0, sizeof( v ) );
        v.pick.type = PICK_TYPE_BLOB;
        for ( k = 0; k < 4; ++k ) { v.pick.as.blob.value[k] = (uint8_t) ( 0x80 + k ); }
        v.pick.as.blob.value_length = 4;
        v.seq = 2147483647;
        n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "blob save failed\n" ); return 1; }
        memset( back, 0x5A, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        if ( root_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "blob load refused=%d\n", r.refused ); return 1; }
        if ( back[0].pick.type != PICK_TYPE_BLOB ) { printf( "blob arm=%d\n", (int) back[0].pick.type ); return 1; }
        if ( back[0].pick.as.blob.value_length != 4 )
            { printf( "blob length=%d\n", (int) back[0].pick.as.blob.value_length ); return 1; }
        for ( k = 0; k < 4; ++k )
            if ( back[0].pick.as.blob.value[k] != (uint8_t) ( 0x80 + k ) )
                { printf( "blob byte %d is %02x\n", k, back[0].pick.as.blob.value[k] ); return 1; }
        if ( back[0].seq != 2147483647 ) { printf( "blob moved seq=%d\n", (int) back[0].seq ); return 1; }

        /* RECORD 4 — the bytes arm, zero length. */
        memset( &v, 0, sizeof( v ) );
        v.pick.type = PICK_TYPE_BLOB;
        v.pick.as.blob.value_length = 0;
        v.seq = 2147483646;
        n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "empty blob save failed\n" ); return 1; }
        memset( back, 0x5A, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        if ( root_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "empty blob load refused=%d\n", r.refused ); return 1; }
        if ( back[0].pick.as.blob.value_length != 0 )
            { printf( "empty blob length=%d\n", (int) back[0].pick.as.blob.value_length ); return 1; }

        /* RECORD 5 — the fixed array arm: every slot is live (§3.4). */
        memset( &v, 0, sizeof( v ) );
        v.pick.type = PICK_TYPE_CELLS;
        for ( k = 0; k < 3; ++k ) { v.pick.as.cells[k] = (int32_t) ( 1000 + k ); }
        v.seq = 2147483647;
        n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "cells save failed\n" ); return 1; }
        memset( back, 0x5A, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        if ( root_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "cells load refused=%d\n", r.refused ); return 1; }
        if ( back[0].pick.type != PICK_TYPE_CELLS ) { printf( "cells arm=%d\n", (int) back[0].pick.type ); return 1; }
        for ( k = 0; k < 3; ++k )
            if ( back[0].pick.as.cells[k] != (int32_t) ( 1000 + k ) )
                { printf( "cells[%d]=%d\n", k, (int) back[0].pick.as.cells[k] ); return 1; }
        if ( back[0].seq != 2147483647 ) { printf( "cells moved seq=%d\n", (int) back[0].seq ); return 1; }

        /* RECORD 6 — the counted array arm, full (3) live elements. */
        memset( &v, 0, sizeof( v ) );
        v.pick.type = PICK_TYPE_SPAN;
        for ( k = 0; k < 3; ++k ) { v.pick.as.span.value[k] = (int32_t) ( 2000 + k ); }
        v.pick.as.span.value_count = 3;
        v.seq = 2147483647;
        n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "span save failed\n" ); return 1; }
        memset( back, 0x5A, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        if ( root_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "span load refused=%d\n", r.refused ); return 1; }
        if ( back[0].pick.type != PICK_TYPE_SPAN ) { printf( "span arm=%d\n", (int) back[0].pick.type ); return 1; }
        if ( back[0].pick.as.span.value_count != 3 )
            { printf( "span count=%d\n", (int) back[0].pick.as.span.value_count ); return 1; }
        for ( k = 0; k < 3; ++k )
            if ( back[0].pick.as.span.value[k] != (int32_t) ( 2000 + k ) )
                { printf( "span[%d]=%d\n", k, (int) back[0].pick.as.span.value[k] ); return 1; }
        if ( back[0].seq != 2147483647 ) { printf( "span moved seq=%d\n", (int) back[0].seq ); return 1; }

        /* RECORD 7 — the counted array arm, empty: the count is 0 and nothing
           else rides (§3.4, the slack stays the template's zeros). */
        memset( &v, 0, sizeof( v ) );
        v.pick.type = PICK_TYPE_SPAN;
        v.pick.as.span.value_count = 0;
        v.seq = 2147483646;
        n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "empty span save failed\n" ); return 1; }
        memset( back, 0x5A, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        if ( root_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "empty span load refused=%d\n", r.refused ); return 1; }
        if ( back[0].pick.as.span.value_count != 0 )
            { printf( "empty span count=%d\n", (int) back[0].pick.as.span.value_count ); return 1; }
        if ( back[0].seq != 2147483646 ) { printf( "empty span moved seq=%d\n", (int) back[0].seq ); return 1; }
    }
`)
	})

	t.Run("vt/c/generator-panic", func(t *testing.T) {
		t.Parallel()
		// ./bin/schema generate --lang c test/tables/A2.schema must exit 0 (no
		// panic) and the generated header must compile with no table_fixed_put0.
		// The in-process generator is the same code path the binary runs; the
		// header is then compiled with cc, as the conformance build does.
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "A2.schema"))
		if err != nil {
			t.Fatal(err)
		}
		u := unitFrom(t, string(data))
		packet, err := cgen.Generate(u)
		if err != nil {
			t.Fatalf("generate packet A2.schema: %v", err)
		}
		files, err := GenerateLineage(u, nil)
		if err != nil {
			t.Fatalf("generate A2.schema: %v", err)
		}
		maps.Copy(files, packet)
		dir := t.TempDir()
		var header string
		for name, out := range files {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, name), out, 0o600); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(name, "Table.h") {
				header = name
			}
		}
		if header == "" {
			t.Fatal("no Table.h was generated for A2.schema")
		}
		probe := fmt.Sprintf("#include \"%s\"\nint main(void){ return 0; }\n", header)
		if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		cc := os.Getenv("CC")
		if cc == "" {
			cc = "cc"
		}
		out, err := exec.Command(cc, "-std=c99", "-Wall", "-Wextra", "-Werror", "-I"+dir,
			"-c", filepath.Join(dir, "probe.c"), "-o", filepath.Join(dir, "probe.o")).CombinedOutput()
		if err != nil {
			t.Fatalf("the generated A2 header did not compile: %v\n%s", err, out)
		}
		for _, f := range files {
			if strings.Contains(string(f), "table_fixed_put0") {
				t.Fatalf("A2.schema's header emits table_fixed_put0:\n%s", f)
			}
		}
	})
}
