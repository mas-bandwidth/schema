package cpptable

// THE ANNOUNCEMENT'S SHAPES ARE BOUNDED AS THE UNSIGNED WIDTHS THE WIRE
// CARRIES (docs/SPEC-TABLES.md §3.3, ir.DecodeShape): every number a shape
// spells is compared in its own unsigned 64-bit domain before anything
// narrows it, so a value with the high bit set is the hostile width it is and
// never a negative number that slips past a signed comparison.
//
// The probes below append ONE entry to a unit's own announcement and compare
// the generated C++ AnnounceRead with the Go engine on the same bytes. For the
// ranged width the hostile value is 2^63 + the kind's own width; its lawful
// neighbours are the kind's own width and the largest max each bounded kind
// may carry. A hostile width sets no vocabulary, and a body under it does not
// load. Verdicts only, never reason names (docs/rulings.json,
// refusal-verdict-only).

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const announceShapeBoundsSchema = `package shapebounds

fixed table Root
{
    x uint8
}
`

const announceShapeBoundsId = uint64(0x8877665544332211)

type announceShapeBoundsCase struct {
	name  string
	entry []byte
	want  bool
}

func announceShapeBoundsLeb(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func announceShapeBoundsEntry(kind byte, shape ...byte) []byte {
	out := binary.LittleEndian.AppendUint64(nil, announceShapeBoundsId)
	out = append(out, kind)
	return append(out, shape...)
}

// announceShapeBoundsWideBase is the sixteen-byte base a fixed-point or
// 128-bit shape carries.
func announceShapeBoundsWideBase() []byte { return make([]byte, 16) }

func announceShapeBoundsFixed8Shape(bits uint64) []byte {
	shape := []byte{ir.TableMessageRanged}
	shape = append(shape, announceShapeBoundsLeb(bits)...)
	return append(shape, announceShapeBoundsWideBase()...)
}

func announceShapeBoundsRangedFixed8(bits uint64) []byte {
	return append(announceShapeBoundsEntry(ir.TableKindFixed8), announceShapeBoundsFixed8Shape(bits)...)
}

func announceShapeBoundsArrayOfFixed8(bits uint64) []byte {
	return append(announceShapeBoundsEntry(ir.TableKindArray, 0, 1, ir.TableKindFixed8),
		announceShapeBoundsFixed8Shape(bits)...)
}

func announceShapeBoundsCases() []announceShapeBoundsCase {
	// The largest lawful LEB integer in each bounded domain.
	const maxString = uint64(1<<31 - 1)            // the int32 storage cap
	const maxList = uint64(ir.TableMessageListMax) // the 32-bit count
	hostileBase := func(width uint64) uint64 { return uint64(1)<<63 | width }
	return []announceShapeBoundsCase{
		{
			// A ranged kind announced at its own width is lawful.
			name:  "ranged_bits_kind_width",
			entry: announceShapeBoundsRangedFixed8(8),
			want:  true,
		},
		{
			// 2^63 + the kind's own width: bit 63 set is a hostile width.
			name:  "ranged_bits_unsigned_high",
			entry: announceShapeBoundsRangedFixed8(hostileBase(8)),
		},
		{
			// The same high bit with a value that narrows to a width above
			// the kind's own.
			name:  "ranged_bits_unsigned_high_64",
			entry: announceShapeBoundsRangedFixed8(hostileBase(64)),
		},
		{
			// A string's largest lawful max, then the high bit set.
			name:  "string_max_largest",
			entry: announceShapeBoundsEntry(ir.TableKindString, announceShapeBoundsLeb(maxString)...),
			want:  true,
		},
		{
			name:  "string_max_unsigned_high",
			entry: announceShapeBoundsEntry(ir.TableKindString, announceShapeBoundsLeb(hostileBase(16))...),
		},
		{
			// An array's min and max at the largest lawful count.
			name: "array_min_max_largest",
			entry: announceShapeBoundsEntry(ir.TableKindArray,
				append(append(announceShapeBoundsLeb(maxList), announceShapeBoundsLeb(maxList)...),
					ir.TableKindU8, ir.TableMessageRaw)...),
			want: true,
		},
		{
			name: "array_min_unsigned_high",
			entry: announceShapeBoundsEntry(ir.TableKindArray,
				append(append(announceShapeBoundsLeb(hostileBase(1)), announceShapeBoundsLeb(maxList)...),
					ir.TableKindU8, ir.TableMessageRaw)...),
		},
		{
			name: "array_max_unsigned_high",
			entry: announceShapeBoundsEntry(ir.TableKindArray,
				append(append(announceShapeBoundsLeb(0), announceShapeBoundsLeb(hostileBase(1))...),
					ir.TableKindU8, ir.TableMessageRaw)...),
		},
		{
			// A keyed entry's max carries the same bound.
			name:  "keyed_max_largest",
			entry: announceShapeBoundsEntry(ir.TableKindKeyed, append(announceShapeBoundsLeb(maxList), ir.TableKindU8, ir.TableMessageRaw)...),
			want:  true,
		},
		{
			name:  "keyed_max_unsigned_high",
			entry: announceShapeBoundsEntry(ir.TableKindKeyed, append(announceShapeBoundsLeb(hostileBase(1)), ir.TableKindU8, ir.TableMessageRaw)...),
		},
		{
			// The nested element's own ranged width takes the same check.
			name:  "nested_ranged_bits_kind_width",
			entry: announceShapeBoundsArrayOfFixed8(8),
			want:  true,
		},
		{
			name:  "nested_ranged_bits_unsigned_high",
			entry: announceShapeBoundsArrayOfFixed8(hostileBase(8)),
		},
		{
			// A base is the unsigned domain, whole: the high half is lawful.
			name: "ranged_base_unsigned_high",
			entry: announceShapeBoundsEntry(ir.TableKindU64,
				append(append([]byte{ir.TableMessageRanged}, announceShapeBoundsLeb(64)...),
					announceShapeBoundsLeb(hostileBase(5))...)...),
			want: true,
		},
	}
}

// announceShapeBoundsBatch is one body naming the appended entry (slot
// `total`) ahead of a payload of zero bytes, then the zero reference.
func announceShapeBoundsBatch(refBits, total, payloadBits int) []byte {
	var bits []int
	put := func(v, width int) {
		for i := range width {
			bits = append(bits, (v>>i)&1)
		}
	}
	put(total, refBits)
	for range payloadBits {
		bits = append(bits, 0)
	}
	put(0, refBits)
	out := make([]byte, 2+(len(bits)+7)/8)
	out[0] = ir.TableWireMessageForm
	out[1] = 0 // one body
	for i, b := range bits {
		if b != 0 {
			out[2+(i>>3)] |= byte(1 << (i & 7))
		}
	}
	return out
}

func TestCppAnnounceShapeBoundsUnsigned(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")

	u := cppUnitFrom(t, announceShapeBoundsSchema)
	vocabulary := tablewire.MessageVocabulary(u)
	base := ir.TableVocabularyBytes(vocabulary)
	build := ir.BuildVersion(u)
	total := len(vocabulary) + 1
	refBits := ir.TableMessageRefBits(total)

	cases := announceShapeBoundsCases()
	anns := make([][]byte, len(cases))
	batches := make([][]byte, len(cases))
	for i, c := range cases {
		vocab := append(append([]byte{}, base...), c.entry...)
		anns[i] = depthBoundaryEncodeAnnouncement(build, vocab)
		batches[i] = announceShapeBoundsBatch(refBits, total, 128)

		// THE ENGINE IS THE ORACLE: the same bytes through AnnounceRead.
		v := &tablewire.Vocabulary{}
		var report tabletext.Report
		err := v.AnnounceRead(anns[i], &report)
		accepted := err == nil && v.Announced()
		if accepted != c.want {
			t.Fatalf("%s: the engine's AnnounceRead=%t, the case wants %t (%v %+v)", c.name, accepted, c.want, err, report)
		}
	}

	var body strings.Builder
	body.WriteString("    static TableMessageEntry entries[8192];\n")
	body.WriteString("    bool failed = false;\n")
	for i, c := range cases {
		want := "false"
		if c.want {
			want = "true"
		}
		fmt.Fprintf(&body, "    {\n")
		fmt.Fprintf(&body, "        static const uint8_t ann%d[] = { %s };\n", i, cppBytes(anns[i]))
		fmt.Fprintf(&body, "        static const uint8_t batch%d[] = { %s };\n", i, cppBytes(batches[i]))
		body.WriteString("        TableVocabulary v( entries, 8192 );\n")
		body.WriteString("        TableReport r; memset( &r, 0, sizeof( r ) );\n")
		fmt.Fprintf(&body, "        const bool accepted = AnnounceRead( v, ann%d, (int64_t) sizeof( ann%d ), &r );\n", i, i)
		fmt.Fprintf(&body, "        if ( accepted != %s ) { printf( \"FAIL %s: AnnounceRead accepted=%%d want=%s\\n\", (int) accepted ); failed = true; }\n", want, c.name, want)
		body.WriteString("        if ( !accepted )\n        {\n")
		fmt.Fprintf(&body, "            if ( v.announced || v.count != 0 ) { printf( \"FAIL %s: a refused announcement set a vocabulary\\n\" ); failed = true; }\n", c.name)
		body.WriteString("            Root storage[1]; RootReset( storage[0] );\n")
		body.WriteString("            int64_t count = 1;\n")
		body.WriteString("            TableReport lr; memset( &lr, 0, sizeof( lr ) );\n")
		fmt.Fprintf(&body, "            if ( RootLoadMessages( storage, &count, v, batch%d, (int64_t) sizeof( batch%d ), &lr ) || count != 0 || lr.unknown != 0 ) { printf( \"FAIL %s: a body under the refused shape loaded: count=%%lld unknown=%%d\\n\", (long long) count, (int) lr.unknown ); failed = true; }\n", i, i, c.name)
		body.WriteString("        }\n    }\n")
	}
	body.WriteString("    return failed ? 1 : 0;\n")

	out, err := cppHeldRun(t, u, body.String())
	if err != nil {
		t.Fatalf("the announcement shape bounds probe: %v\n%s", err, out)
	}
}
