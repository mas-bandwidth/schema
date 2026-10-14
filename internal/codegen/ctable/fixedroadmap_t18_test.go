package ctable

// TestFixedCT18DirectArmLines and TestFixedRoadmapCT18DirectArms are the c
// leg's card c-fixed-form-writes-direct-text-and-array-arms: the fixed-tables
// row union-field-arms/c/valid-data and the Variable-tables leaf
// vt/c/generator-panic (docs/roadmap.sexp, docs/roadmap-evidence/fixed-c.sexp).
//
// A union arm is a FIELD LINE (docs/SPEC-TABLES.md §2.6) and "an arm's type is
// any type a field's is": a DIRECT text, bytes or array arm rides as its own
// field type under the arm's position, with the arm's value and its length or
// count companion inside the arm's own unnamed struct (`.as.<arm>.value` beside
// `.as.<arm>.value_length` or `.value_count`). The fixed-form writer must store
// such an arm the way it stores the same field (§3.4):
//
//	string(N)  — 4 (the used length in BYTES, int32 LE) + N bytes
//	wstring(N) — 4 (the used length in CODE UNITS, int32 LE) + 2N bytes
//	[N]T       — N elements, every slot live
//	[..N]T     — 4 (the live count, int32 LE) + N elements, slack zero
//
// Before this card the union case of emitFixedWriteElement recursed on the arm
// field itself, so a text arm was sized by ir.TableFixedStorageBytes as 0 and
// the body emitted `table_fixed_put0(..., (uint0_t) ...)` — a body no C
// compiler accepts (build/tables-generated-c/a2/A2Table.h, unions on a direct
// `string(8)`; wide/CaptionTable.h on a direct `wide wstring(4)`).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// directArmSchema is the unit under test: a fixed table whose union `Arm`
// carries a DIRECT string, bytes, fixed array and counted array arm, each one
// §3.4's constant-size table row with the arm's companion spelling.
const directArmSchema = `package probe
union Arm
{
    text       string(8)
    blob       bytes(4)
    fixedarr   [3]int32
    countedarr [..3]int32
}
fixed table Root
{
    arm  Arm
    tail uint32
}
`

// TestFixedCT18DirectArmLines is the UNIT tier: it builds directArmSchema and
// test/tables/A2.schema in process with unitFrom and GenerateLineage, the way
// TestGenerateR2DoesNotPanic does, and asserts the generated header TEXT only —
// no compiler, no clock, no socket, no network. It pins, line by line:
//
//   - no `table_fixed_put0` anywhere: a text, bytes or array arm is never sized
//     as a scalar and stored through a zero-width put;
//   - a used-length store (`.as.<arm>.value_length`) for the string and bytes
//     arms, beside the arm's value and not a sibling field's;
//   - a live-count store (`.as.<arm>.value_count`) for the counted array arm;
//   - an element store per slot for the fixed array arm.
func TestFixedCT18DirectArmLines(t *testing.T) {
	t.Parallel()

	u := unitFrom(t, directArmSchema)
	files, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("generate directArmSchema: %v", err)
	}
	header := tableHeaders(files)
	if header == "" {
		t.Fatal("no *Table.h was emitted for directArmSchema")
	}
	if strings.Contains(header, "table_fixed_put0") {
		t.Fatalf("the direct-arm writer sized an arm as a zero-width scalar:\n%s", linesWith(header, "table_fixed_put0"))
	}
	for _, want := range []string{
		".as.text.value_length",       // the string arm's used length, §3.4
		".as.text.value",              // and its value, inside the arm's struct
		".as.blob.value_length",       // the bytes arm's used length
		".as.countedarr.value_count",  // the counted array arm's live count
		".as.fixedarr[",               // the fixed array arm's element store
		"table_fixed_put32",           // the count/length companions' width
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("the direct-arm writer never spells %q", want)
		}
	}

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "A2.schema"))
	if err != nil {
		t.Fatal(err)
	}
	a2 := unitFrom(t, string(raw))
	a2files, err := GenerateLineage(a2, nil)
	if err != nil {
		t.Fatalf("generate A2.schema: %v", err)
	}
	a2header := tableHeaders(a2files)
	if a2header == "" {
		t.Fatal("no *Table.h was emitted for A2.schema")
	}
	if strings.Contains(a2header, "table_fixed_put0") {
		t.Fatalf("A2.schema's direct `string(8)` arm was sized as zero:\n%s", linesWith(a2header, "table_fixed_put0"))
	}
	if !strings.Contains(a2header, ".as.c.value_length") {
		t.Fatal("A2.schema's direct `string(8)` arm has no used-length store")
	}
}

// TestFixedRoadmapCT18DirectArms is the FUNCTIONAL tier: it compiles and runs
// the emitted C with the system compiler, the shape cT10Run uses. The first
// subtest, union-field-arms/c/valid-data, selects each direct arm in turn with
// non-default and boundary values — a full-length string and a zero-length one,
// a full counted array and an empty one — through the generated save, and reads
// them back through the generated load with a clean report. The second subtest,
// vt/c/generator-panic, generates C for test/tables/A2.schema and compiles the
// header, which at BASE carries the `table_fixed_put0` store.
func TestFixedRoadmapCT18DirectArms(t *testing.T) {
	t.Parallel()
	t.Run("union-field-arms/c/valid-data", func(t *testing.T) {
		t.Parallel()
		cT18DirectArmsProbe(t)
	})
	t.Run("vt/c/generator-panic", func(t *testing.T) {
		t.Parallel()
		cT18GeneratorPanicProbe(t)
	})
}

// cT18DirectArmsProbe exercises a direct string, a direct bytes, a direct fixed
// array and a direct counted array arm, each on non-default and boundary values.
func cT18DirectArmsProbe(t *testing.T) {
	cT10Run(t, directArmSchema, "Root", `
    {
        int k;
        /* the string arm: a FULL-length value (8 bytes) */
        v.arm.type = ARM_TYPE_TEXT;
        memcpy( v.arm.as.text.value, "ABCDEFGH", 8 );
        v.arm.as.text.value_length = 8;
        v.tail = 0x12345678u;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].arm.type != ARM_TYPE_TEXT ) { printf( "arm=%d\n", (int) back[0].arm.type ); return 1; }
        if ( back[0].arm.as.text.value_length != 8 )
            { printf( "text_length=%d\n", (int) back[0].arm.as.text.value_length ); return 1; }
        if ( memcmp( back[0].arm.as.text.value, "ABCDEFGH", 8 ) != 0 )
            { printf( "text=%.*s\n", (int) back[0].arm.as.text.value_length, back[0].arm.as.text.value ); return 1; }
        if ( back[0].tail != 0x12345678u ) { printf( "tail=%08x\n", back[0].tail ); return 1; }

        /* the string arm: a ZERO-length value */
        v.arm.as.text.value_length = 0;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].arm.as.text.value_length != 0 )
            { printf( "empty text_length=%d\n", (int) back[0].arm.as.text.value_length ); return 1; }
        if ( back[0].tail != 0x12345678u ) { printf( "empty tail=%08x\n", back[0].tail ); return 1; }

        /* the bytes arm: every byte distinct, the full bound */
        v.arm.type = ARM_TYPE_BLOB;
        for ( k = 0; k < 4; ++k ) { v.arm.as.blob.value[k] = (uint8_t) ( 0x80 + k ); }
        v.arm.as.blob.value_length = 4;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].arm.type != ARM_TYPE_BLOB ) { printf( "blob arm=%d\n", (int) back[0].arm.type ); return 1; }
        if ( back[0].arm.as.blob.value_length != 4 )
            { printf( "blob_length=%d\n", (int) back[0].arm.as.blob.value_length ); return 1; }
        for ( k = 0; k < 4; ++k )
            if ( back[0].arm.as.blob.value[k] != (uint8_t) ( 0x80 + k ) )
                { printf( "blob byte %d is %02x\n", k, back[0].arm.as.blob.value[k] ); return 1; }
        if ( back[0].tail != 0x12345678u ) { printf( "blob tail=%08x\n", back[0].tail ); return 1; }

        /* the fixed array arm: every slot live */
        v.arm.type = ARM_TYPE_FIXEDARR;
        for ( k = 0; k < 3; ++k ) { v.arm.as.fixedarr[k] = (int32_t) ( 1000 + k ); }
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].arm.type != ARM_TYPE_FIXEDARR ) { printf( "fixedarr arm=%d\n", (int) back[0].arm.type ); return 1; }
        for ( k = 0; k < 3; ++k )
            if ( back[0].arm.as.fixedarr[k] != (int32_t) ( 1000 + k ) )
                { printf( "fixedarr[%d]=%d\n", k, (int) back[0].arm.as.fixedarr[k] ); return 1; }
        if ( back[0].tail != 0x12345678u ) { printf( "fixedarr tail=%08x\n", back[0].tail ); return 1; }

        /* the counted array arm: a FULL count */
        v.arm.type = ARM_TYPE_COUNTEDARR;
        for ( k = 0; k < 3; ++k ) { v.arm.as.countedarr.value[k] = (int32_t) ( 2000 + k ); }
        v.arm.as.countedarr.value_count = 3;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].arm.type != ARM_TYPE_COUNTEDARR ) { printf( "counted arm=%d\n", (int) back[0].arm.type ); return 1; }
        if ( back[0].arm.as.countedarr.value_count != 3 )
            { printf( "count=%d\n", (int) back[0].arm.as.countedarr.value_count ); return 1; }
        for ( k = 0; k < 3; ++k )
            if ( back[0].arm.as.countedarr.value[k] != (int32_t) ( 2000 + k ) )
                { printf( "counted[%d]=%d\n", k, (int) back[0].arm.as.countedarr.value[k] ); return 1; }
        if ( back[0].tail != 0x12345678u ) { printf( "counted tail=%08x\n", back[0].tail ); return 1; }

        /* the counted array arm: an EMPTY count */
        v.arm.as.countedarr.value_count = 0;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].arm.as.countedarr.value_count != 0 )
            { printf( "empty count=%d\n", (int) back[0].arm.as.countedarr.value_count ); return 1; }
        if ( back[0].tail != 0x12345678u ) { printf( "empty counted tail=%08x\n", back[0].tail ); return 1; }
    }
`)
}

// cT18GeneratorPanicProbe is vt/c/generator-panic: A2.schema's union `Value`
// carries a direct `string(8)` arm, and the C generator must emit a header that
// compiles. cT10Run generates the whole unit and compiles a probe that includes
// the header, so a `table_fixed_put0` store fails the compile by name.
func cT18GeneratorPanicProbe(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "A2.schema"))
	if err != nil {
		t.Fatal(err)
	}
	cT10Run(t, string(raw), "Root", `
    {
        v.id = 7u;
        v.value.type = VALUE_TYPE_C;
        memcpy( v.value.as.c.value, "hello", 5 );
        v.value.as.c.value_length = 5;
        v.tail = 9u;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].value.type != VALUE_TYPE_C ) { printf( "arm=%d\n", (int) back[0].value.type ); return 1; }
        if ( back[0].value.as.c.value_length != 5 )
            { printf( "c_length=%d\n", (int) back[0].value.as.c.value_length ); return 1; }
        if ( memcmp( back[0].value.as.c.value, "hello", 5 ) != 0 )
            { printf( "c=%.*s\n", (int) back[0].value.as.c.value_length, back[0].value.as.c.value ); return 1; }
        if ( back[0].id != 7u || back[0].tail != 9u )
            { printf( "id=%u tail=%u\n", back[0].id, back[0].tail ); return 1; }
    }
`)
}

// tableHeaders concatenates every *Table.h in a generated file set, the way
// TestGenerateR2DoesNotPanic reads the fixed form's writer.
func tableHeaders(files map[string][]byte) string {
	var b strings.Builder
	for name, out := range files {
		if strings.HasSuffix(name, "Table.h") {
			b.Write(out)
		}
	}
	return b.String()
}

// linesWith returns only the lines of src that contain needle, so a failure
// pastes the offending generated line rather than the whole header.
func linesWith(src, needle string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, needle) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
