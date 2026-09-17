package cpptable

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// accessorDescriptorSchema declares one block (a fixed table) and one cook (a
// variable table with a pointer edge), so both reading tiers are exercised:
// the block's projection record, whose descriptor carries the compiler's own
// literal offset, and the cook's node record with its pointer SLOT, whose cook
// body writes at a compiler-chosen literal while the accessor reads the member.
const accessorDescriptorSchema = `package probe
fixed table Block
{
    id   uint32
    flag bool
}
table Node
{
    value int32
    next  *Node
}
`

// accessorDescriptorMain reads every field of both records TWICE — once
// through the generated accessor (the projection member, the cook body's own
// literal offset as it lands through the opened row, and the struct member)
// and once through the descriptor's own offset — and refuses when the two
// spellings of one layout disagree. The pointer slot is held separately,
// because its position is what a self-relative delta is relative to.
const accessorDescriptorMain = `#include "ProbeBlock.h"
#include "ProbeTable.h"

#include <cstddef>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>

using namespace probe;

static int failures = 0;

static void fail( const char * what )
{
    printf( "FAILED: %s\n", what );
    failures++;
}

int main()
{
    // ---- the BLOCK projection, accessor against descriptor ----
    BlockBlockStorage storage;
    if ( !storage.Create( TableBlockDefaultAllocator() ) ) { fail( "block storage" ); return 1; }
    BlockBlock block;
    if ( !BlockBlockBegin( block, storage, BlockCounts(), NULL ) ) { fail( "block begin" ); return 1; }
    block.projection->id = 0x11223344u;
    block.projection->flag = true;

    const TableBlockInfo * binfo = BlockBlock::Type();
    for ( int i = 0; i < binfo->num_fields; i++ )
    {
        const TableBlockFieldInfo & f = binfo->fields[i];
        if ( strcmp( f.name, "id" ) == 0 )
        {
            if ( (uint32_t) offsetof( BlockBlock::Projection, id ) != f.offset )
                fail( "block.id: the accessor's offset is not the descriptor's" );
            uint32_t via = 0;
            memcpy( &via, block.base + f.offset, sizeof( via ) );
            if ( via != block.projection->id )
                fail( "block.id: the accessor and the descriptor disagree about the value" );
        }
        else if ( strcmp( f.name, "flag" ) == 0 )
        {
            if ( (uint32_t) offsetof( BlockBlock::Projection, flag ) != f.offset )
                fail( "block.flag: the accessor's offset is not the descriptor's" );
            uint8_t via = 0;
            memcpy( &via, block.base + f.offset, sizeof( via ) );
            if ( (bool) via != block.projection->flag )
                fail( "block.flag: the accessor and the descriptor disagree about the value" );
        }
    }
    storage.Destroy();

    // ---- the COOK node, accessor against descriptor, and its pointer SLOT ----
    Node node;
    NodeReset( node );
    node.value = 7;
    const int64_t need = NodeCookMeasure( &node );
    if ( need <= 0 ) { fail( "cook measure" ); return 1; }
    uint8_t * bytes = (uint8_t *) malloc( (size_t) need );
    if ( !NodeCook( &node, bytes, (uint64_t) need, TableByteOrder::Little ) ) { fail( "cook" ); return 1; }
    const Node * row = NodeOpen( bytes, (uint64_t) need );
    if ( row == NULL ) { fail( "cook open" ); return 1; }

    // the cook body wrote each field at a compiler-chosen literal offset; the
    // accessor reads the struct member. The two must be the same place.
    if ( row->value != 7 ) { fail( "cook.value: the cooked value did not read back through the accessor" ); }

    const TableTypeInfo * ninfo = NodeTableType();
    for ( int i = 0; i < ninfo->num_fields; i++ )
    {
        const TableFieldInfo & f = ninfo->fields[i];
        if ( strcmp( f.name, "value" ) == 0 )
        {
            if ( (uint32_t) offsetof( Node, value ) != f.offset )
                fail( "cook.value: the accessor's offset is not the descriptor's" );
            int32_t via = 0;
            memcpy( &via, (const char *) row + f.offset, sizeof( via ) );
            if ( via != row->value )
                fail( "cook.value: the accessor and the descriptor disagree about the value" );
        }
        else if ( strcmp( f.name, "next" ) == 0 )
        {
            if ( (uint32_t) offsetof( Node, next ) != f.offset )
                fail( "cook.next: the slot accessor's offset is not the descriptor's" );
            TableRef via;
            via.value = 0;
            memcpy( &via, (const char *) row + f.offset, sizeof( via ) );
            if ( via.value != row->next.value )
                fail( "cook.next: the accessor and the descriptor disagree about the delta" );
        }
    }
    free( bytes );

    if ( failures != 0 ) { printf( "%d failure(s)\n", failures ); return 1; }
    printf( "accessor/descriptor agreement: all fields agree\n" );
    return 0;
}
`

// TestCppAccessorDescriptorAgreement is the C++ half of the J1 technique
// (docs/PORTING.md, schema#421). The generated ACCESSOR — the projection
// member, the cook's compiler-chosen literal, and the struct member it lands
// in — and the generated DESCRIPTOR are two independent derivations of one
// layout, and a reading tier that only ever walks the descriptors could read
// the descriptors twice and never know. This generates the C++ reference,
// compiles it, and reads both ways, so a moved accessor or a moved descriptor
// offset is seen without a pinned dump that happens to cover the field.
func TestCppAccessorDescriptorAgreement(t *testing.T) {
	if out, err := runCppGeneratedEdited(t, accessorDescriptorSchema, accessorDescriptorMain, nil); err != nil {
		t.Fatalf("the accessor/descriptor gate: %v\n%s", err, out)
	}
}

// TestCppAccessorDescriptorScalarNegativeControl moves a generated block
// projection's descriptor offset four bytes and requires the gate to go red.
// Without it the accessor half could be reading the descriptors twice and
// nobody would know.
func TestCppAccessorDescriptorScalarNegativeControl(t *testing.T) {
	out, err := runCppGeneratedEdited(t, accessorDescriptorSchema, accessorDescriptorMain, func(files map[string][]byte) {
		patchCpp(t, files,
			`{ "id", 24u,`,
			`{ "id", 28u, /* SABOTAGED */`)
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a block scalar offset four bytes off left the gate GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("block.id: the accessor's offset is not the descriptor's")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the accessor/descriptor disagreement\n%s", out)
	}
}

// TestCppAccessorDescriptorSlotNegativeControl moves a generated cook pointer
// slot's own descriptor offset eight bytes — the position a self-relative
// delta is relative to (§6.3) — and requires the gate to go red on the slot.
func TestCppAccessorDescriptorSlotNegativeControl(t *testing.T) {
	out, err := runCppGeneratedEdited(t, accessorDescriptorSchema, accessorDescriptorMain, func(files map[string][]byte) {
		patchCpp(t, files,
			`(uint32_t) offsetof( Node, next )`,
			`(uint32_t) offsetof( Node, next ) + 8u /* SABOTAGED */`)
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a pointer slot eight bytes off left the gate GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("cook.next: the slot accessor's offset is not the descriptor's")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the pointer slot\n%s", out)
	}
}

// patchCpp replaces one substring across every generated file and refuses a
// control whose sabotage patched nothing.
func patchCpp(t *testing.T, files map[string][]byte, old, new string) {
	t.Helper()
	n := 0
	for name, data := range files {
		s := string(data)
		if !strings.Contains(s, old) {
			continue
		}
		files[name] = []byte(strings.ReplaceAll(s, old, new))
		n++
	}
	if n == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage %q patched nothing", old)
	}
}

// runCppGeneratedEdited generates the C++ reference for the schema, applies an
// optional sabotage to the generated files, compiles them with a small
// accessor/descriptor program beside them, and runs it. It answers the
// program's combined output and its error, so a caller can require a green run
// or require a red one on a named line.
func runCppGeneratedEdited(t *testing.T, schema, mainSource string, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	u := cppUnitFrom(t, schema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.cpp"), []byte(mainSource), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-I", dir, filepath.Join(dir, "main.cpp")}
	for _, name := range []string{"ProbeBlock.cpp", "ProbeTable.cpp"} {
		args = append(args, filepath.Join(dir, name))
	}
	bin := filepath.Join(dir, "probe")
	args = append(args, "-o", bin)
	compile := exec.Command("c++", args...)
	if out, err := compile.CombinedOutput(); err != nil {
		return out, err
	}
	return exec.Command(bin).CombinedOutput()
}

// cppUnitFrom parses and checks one schema source into the IR the C++ table
// emitter takes.
func cppUnitFrom(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}
