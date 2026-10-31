package cpptable

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// noWriterUnit carries one of every body the refusals below reach: a nested
// table, a bounded array, an enum-keyed array, a union whose arm is a union,
// and, under a variable root, a map.
const noWriterUnit = `package nowriter

enum Slot { Alpha, Beta }

fixed table Inner
{
    n int32 = 0
}

union In
{
    p int32
    q int32
}

union Out
{
    inner In
    v     int32
}

fixed table Root
{
    a     int32 = 0
    inner Inner
    vals  [..4]int32
    slots [Slot]int32
    alt   In
    pick  Out
    b     int32 = 0
}

table Graph
{
    a    int32 = 0
    m    map[uint8]Inner
    next *Graph
}
`

// nwFile is a form-1 file built by hand: a body over an id table of names in
// first-use order, so each vector says exactly which bytes no writer produces.
type nwFile struct {
	ids  []uint64
	body []byte
}

func (f *nwFile) ref(name string) byte {
	id := ir.TableWireId(name)
	for i, v := range f.ids {
		if v == id {
			return byte(i + 1)
		}
	}
	f.ids = append(f.ids, id)
	return byte(len(f.ids))
}

func (f *nwFile) put(b ...byte) *nwFile { f.body = append(f.body, b...); return f }

func (f *nwFile) i32(name string, v int32) *nwFile {
	f.put(f.ref(name), byte(ir.TableKindI32))
	return f.put(binary.LittleEndian.AppendUint32(nil, uint32(v))...)
}

func (f *nwFile) bytes() []byte {
	out := append([]byte{ir.TableWireForm}, f.body...)
	out = append(out, 0)
	for _, id := range f.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(f.ids)))
}

func le32(v int32) []byte { return binary.LittleEndian.AppendUint32(nil, uint32(v)) }

// nwVector is one wire and the engine's verdict on it, asked first, so every
// vector is damage by the rule and its control is clean by the rule.
func nwVector(t *testing.T, m *tabletext.Model, root string, wire []byte, damaged bool) string {
	t.Helper()
	var r tabletext.Report
	ok, err := tablewire.Decode(m, m.New(m.Lookup(root)), wire, &r)
	if err != nil || r.Malformed != damaged || ok == damaged {
		t.Fatalf("%s %x: the engine reads ok=%v err=%v %+v, want damaged=%v", root, wire, ok, err, r, damaged)
	}
	return cppBytes(wire)
}

// TestNoWriterInputIsDamageCPP is the C++ reference's gate for the rule that
// input no conforming writer produces is damage (Glenn, 2026-10-31; #391:
// "Table reads are not trusted, they will come over the network."). Each item
// is checked with a clean control beside it, and each failure names its item,
// so a sabotage of one refusal turns exactly that line red:
//
//   - item2: one id twice in one body, root and nested, known and unknown, and
//     one slot key twice in a keyed body
//   - item3: an array or keyed body too short for its header, and a header
//     count spelled past the field's L under a foreign element kind
//   - item4-wire: one map key twice
//   - item4-json: one JSON member name twice, field or unknown, keyed slot, and
//     two spellings of one map key
//   - item5: bytes inside a nested-union arm's L after the whole inner union
func TestNoWriterInputIsDamageCPP(t *testing.T) {
	t.Parallel()
	u := loadUnitFromSource(t, noWriterUnit)
	m := tabletext.NewModel(u)
	fixed := func(damaged bool, build func(f *nwFile)) string {
		f := &nwFile{}
		build(f)
		return nwVector(t, m, "Root", f.bytes(), damaged)
	}
	var probes []string
	add := func(item, wire string, damaged bool) {
		probes = append(probes, fmt.Sprintf("    { \"%s\", %v, { %s } },", item, damaged, wire))
	}

	// item2
	add("item2: a root id twice", fixed(true, func(f *nwFile) { f.i32("a", 1).i32("b", 2).i32("a", 3) }), true)
	add("item2: control, each root id once", fixed(false, func(f *nwFile) { f.i32("a", 1).i32("b", 2) }), false)
	add("item2: a nested id twice", fixed(true, func(f *nwFile) {
		f.put(f.ref("inner"), byte(ir.TableKindTable), 13)
		f.i32("n", 1).i32("n", 2).put(0)
	}), true)
	add("item2: an unknown id twice", fixed(true, func(f *nwFile) { f.i32("zzz", 1).i32("zzz", 2) }), true)
	add("item2: control, an unknown id once", fixed(false, func(f *nwFile) { f.i32("zzz", 1).i32("a", 2) }), false)
	add("item2: a keyed slot twice", fixed(true, func(f *nwFile) {
		f.put(f.ref("slots"), byte(ir.TableKindKeyed), 14, byte(ir.TableKindI32), 2)
		f.put(f.ref("Alpha"), 4).put(le32(1)...)
		f.put(f.ref("Alpha"), 4).put(le32(2)...)
	}), true)
	add("item2: control, two keyed slots", fixed(false, func(f *nwFile) {
		f.put(f.ref("slots"), byte(ir.TableKindKeyed), 14, byte(ir.TableKindI32), 2)
		f.put(f.ref("Alpha"), 4).put(le32(1)...)
		f.put(f.ref("Beta"), 4).put(le32(2)...)
	}), false)

	// item3
	add("item3: an array body of one byte", fixed(true, func(f *nwFile) {
		f.put(f.ref("vals"), byte(ir.TableKindArray), 1, byte(ir.TableKindI32)).i32("b", 2)
	}), true)
	add("item3: a keyed body of one byte", fixed(true, func(f *nwFile) {
		f.put(f.ref("slots"), byte(ir.TableKindKeyed), 1, byte(ir.TableKindI32)).i32("b", 2)
	}), true)
	add("item3: control, an empty array body", fixed(false, func(f *nwFile) {
		f.put(f.ref("vals"), byte(ir.TableKindArray), 2, byte(ir.TableKindI32), 0).i32("b", 2)
	}), false)
	add("item3: a count spelled past L", fixed(true, func(f *nwFile) {
		// a foreign element kind, then a count whose continuation is the
		// next field's reference
		f.put(f.ref("vals"), byte(ir.TableKindArray), 2, byte(ir.TableKindEnum), 0x82).i32("b", 2)
	}), true)
	add("item3: control, a foreign element kind inside L", fixed(false, func(f *nwFile) {
		f.put(f.ref("vals"), byte(ir.TableKindArray), 2, byte(ir.TableKindEnum), 0).i32("b", 2)
	}), false)

	// item5
	nested := func(trailing bool) func(f *nwFile) {
		return func(f *nwFile) {
			inner := []byte{f.ref("p"), byte(ir.TableKindI32), 4}
			inner = append(inner, le32(5)...)
			if trailing {
				inner = append(inner, 0xEE)
			}
			f.put(f.ref("pick"), byte(ir.TableKindUnion))
			f.put(f.ref("inner"), byte(ir.TableKindUnion), byte(len(inner)))
			f.put(inner...)
		}
	}
	add("item5: a byte after the inner union", fixed(true, nested(true)), true)
	add("item5: control, the inner union exactly", fixed(false, nested(false)), false)

	// item4-wire, under the variable root
	graph := func(keys ...byte) string {
		f := &nwFile{}
		var entries []byte
		keyRef := f.ref(ir.MapKeyFieldName)
		for _, k := range keys {
			entries = append(entries, 4, keyRef, byte(ir.TableKindU8), k, 0)
		}
		f.put(f.ref("m"), byte(ir.TableKindArray), byte(2+len(entries)), byte(ir.TableKindTable), byte(len(keys)))
		f.put(entries...)
		return nwVector(t, m, "Graph", f.bytes(), len(keys) == 2 && keys[0] == keys[1])
	}
	mapRepeat, mapControl := graph(2, 2), graph(2, 3)

	var b strings.Builder
	b.WriteString("    struct Probe { const char * item; bool damaged; std::vector<uint8_t> wire; };\n")
	b.WriteString("    const Probe probes[] = {\n" + strings.Join(probes, "\n") + "\n    };\n")
	b.WriteString(`    for ( const Probe & p : probes )
    {
        Root v; RootReset( v );
        TableReport r; memset( &r, 0, sizeof( r ) );
        const bool ok = RootLoadTolerant( v, p.wire.data(), (int64_t) p.wire.size(), &r );
        if ( p.damaged && ( ok || !r.malformed ) ) { printf( "%s: the load was not refused\n", p.item ); return 1; }
        if ( !p.damaged && ( !ok || r.malformed ) ) { printf( "%s: a clean load was refused\n", p.item ); return 1; }
    }
`)
	fmt.Fprintf(&b, `    {
        const uint8_t repeat[] = { %s };
        const uint8_t control[] = { %s };
        const uint8_t * wires[2] = { repeat, control };
        const int64_t sizes[2] = { (int64_t) sizeof( repeat ), (int64_t) sizeof( control ) };
        for ( int i = 0; i < 2; i++ )
        {
            const int64_t need = GraphLoadMeasure( wires[i], sizes[i] );
            std::vector<uint8_t> region( (size_t) ( need > 0 ? need : 4096 ) + 64 );
            uint8_t * aligned = region.data();
            while ( ( (uintptr_t) aligned & 63 ) != 0 ) { aligned++; }
            TableReport g; memset( &g, 0, sizeof( g ) );
            const Graph * root = GraphLoadTolerant( aligned, need > 0 ? need : 4096, wires[i], sizes[i], &g );
            if ( i == 0 && ( root != NULL || !g.malformed ) ) { printf( "item4-wire: one map key twice was not refused\n" ); return 1; }
            if ( i == 1 && ( root == NULL || g.malformed ) ) { printf( "item4-wire: control, two map keys were refused\n" ); return 1; }
            GraphBuilder builder;
            TableReport br; memset( &br, 0, sizeof( br ) );
            const bool built = GraphLoadBuilderTolerant( builder, wires[i], sizes[i], &br );
            if ( i == 0 && ( built || !br.malformed ) ) { printf( "item4-wire: the builder took one map key twice\n" ); return 1; }
            if ( i == 1 && ( !built || br.malformed ) ) { printf( "item4-wire: control, the builder refused two map keys\n" ); return 1; }
        }
    }
`, mapRepeat, mapControl)
	b.WriteString(`    {
        struct Text { const char * item; bool damaged; bool graph; const char * text; };
        const Text texts[] = {
            { "item4-json: a field named twice", true, false, "{ \"a\": 1, \"a\": 2 }" },
            { "item4-json: control, each field once", false, false, "{ \"a\": 1, \"b\": 2 }" },
            { "item4-json: an unknown name twice", true, false, "{ \"zz\": 1, \"a\": 1, \"zz\": 2 }" },
            { "item4-json: two spellings of one name", true, false, "{ \"a\": 1, \"\\u0061\": 2 }" },
            { "item4-json: a keyed slot twice", true, false, "{ \"slots\": { \"Alpha\": 1, \"Alpha\": 2 } }" },
            { "item4-json: control, two keyed slots", false, false, "{ \"slots\": { \"Alpha\": 1, \"Beta\": 2 } }" },
            { "item4-json: nine names, the ninth twice", true, false, "{ \"a\": 1, \"b\": 2, \"z1\": 0, \"z2\": 0, \"z3\": 0, \"z4\": 0, \"z5\": 0, \"z6\": 0, \"z7\": 0, \"z8\": 0, \"z8\": 1 }" },
            { "item4-json: control, nine names once", false, false, "{ \"a\": 1, \"b\": 2, \"z1\": 0, \"z2\": 0, \"z3\": 0, \"z4\": 0, \"z5\": 0, \"z6\": 0, \"z7\": 0, \"z8\": 0 }" },
            { "item4-json: one map key twice", true, true, "{ \"m\": { \"2\": {}, \"2\": {} } }" },
            { "item4-json: two spellings of one map key", true, true, "{ \"m\": { \"2\": {}, \"2.0\": {} } }" },
            { "item4-json: control, two map keys", false, true, "{ \"m\": { \"2\": {}, \"3\": {} } }" },
        };
        for ( const Text & x : texts )
        {
            TableReport r; memset( &r, 0, sizeof( r ) );
            bool ok = false;
            if ( x.graph )
            {
                GraphBuilder builder;
                ok = GraphFromJsonTolerant( builder, x.text, (int64_t) strlen( x.text ), &r );
            }
            else
            {
                Root v; RootReset( v );
                ok = RootFromJsonTolerant( v, x.text, (int64_t) strlen( x.text ), &r );
            }
            if ( x.damaged && ( ok || !r.malformed ) ) { printf( "%s: the read was not refused\n", x.item ); return 1; }
            if ( !x.damaged && ( !ok || r.malformed ) ) { printf( "%s: a clean read was refused\n", x.item ); return 1; }
            if ( r.duplicate != 0 ) { printf( "%s: the retired duplicate counter moved\n", x.item ); return 1; }
        }
    }
`)
	out, err := cppHeldRun(t, u, b.String())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
