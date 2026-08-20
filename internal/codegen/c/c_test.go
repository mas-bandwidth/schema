// Emitter-shape tests for the C backend.
//
// The bulk-bytes path is a WIRE-SENSITIVE optimization: at a statically
// byte-aligned position, N unranged 8-bit writes and one aligned bulk copy
// are the same bytes (SPEC §4.3, and serialize.c's align contributes zero
// bits when already aligned) — but ANYWHERE ELSE the bulk path's internal
// align would insert padding the pinned encoding does not have. So the test
// that matters is not "does C use the bulk call" but "does C use it at
// exactly the positions the shared analysis marks, and nowhere else" — and
// that set must be the same set the C++ backend switches on, since the two
// are held to one golden.
package c

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/parser"
	"github.com/mas-bandwidth/schema/ir"
)

func unitFromSource(t *testing.T, name, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse(name, []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"),
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// The same shapes ir/align_test.go pins the ANALYSIS on, carried up
// to the EMISSION: each type holds one fixed [N]uint8 named data, and the
// name says whether the C wire functions may bulk-copy it.
const bulkCorpus = `package bulktest

type MarkedAfterAlign {
    lead bits(3)
    align
    data [17]uint8
}

type MarkedAfterString {
    s    string(31)
    data [3]uint8
}

type UnmarkedAtEntry {
    data [17]uint8
}

type UnmarkedOffBoundary {
    align
    flag bool
    data [4]uint8
}

type UnmarkedRanged {
    align
    data [4]uint8 [min = 0, max = 255]
}

type UnmarkedCounted {
    align
    data [<= 8]uint8
}
`

// wireBodies splits the generated wire header into the write_<type> and
// read_<type> function bodies, keyed by snake-cased type name.
func wireBodies(t *testing.T, header string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, prefix := range []string{"write_", "read_"} {
		for _, name := range []string{
			"marked_after_align", "marked_after_string", "unmarked_at_entry",
			"unmarked_off_boundary", "unmarked_ranged", "unmarked_counted",
		} {
			open := "int " + prefix + name + "( "
			i := strings.Index(header, open)
			if i < 0 {
				t.Fatalf("generated C has no %s%s function:\n%s", prefix, name, header)
			}
			body, _, ok := strings.Cut(header[i:], "\n}\n")
			if !ok {
				t.Fatalf("%s%s function body is unterminated", prefix, name)
			}
			out[prefix+name] = body
		}
	}
	return out
}

// TestBulkBytesEmission pins that the C emitter takes serialize.c's bulk
// bytes path for statically byte-aligned fixed [N]uint8 arrays and keeps the
// per-byte loop everywhere else. Before the bulk path landed, EVERY case here
// was a per-byte loop — correct on the wire but a byte at a time.
func TestBulkBytesEmission(t *testing.T) {
	u := unitFromSource(t, "Bulk.schema", bulkCorpus)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	header := string(files["BulkWire.h"])
	bodies := wireBodies(t, header)

	bulkWrite := "serialize_write_bytes( stream, value->data, 17 )"
	perByteWrite := "serialize_write_bits( stream, (serialize_uint32_t) value->data[i], 8 )"
	bulkRead := "serialize_read_bytes( stream, value->data, 17 )"
	perByteRead := "serialize_read_bits( stream, &raw, 8 )"

	// ---- marked: the bulk call, and NOT the loop
	if body := bodies["write_marked_after_align"]; !strings.Contains(body, bulkWrite) {
		t.Errorf("write_marked_after_align must bulk-copy its byte-aligned [17]uint8:\nwant %q in:\n%s", bulkWrite, body)
	}
	if body := bodies["read_marked_after_align"]; !strings.Contains(body, bulkRead) {
		t.Errorf("read_marked_after_align must bulk-copy its byte-aligned [17]uint8:\nwant %q in:\n%s", bulkRead, body)
	}
	if body := bodies["write_marked_after_align"]; strings.Contains(body, perByteWrite) {
		t.Errorf("write_marked_after_align still packs its byte array one byte at a time:\n%s", body)
	}
	if body := bodies["read_marked_after_align"]; strings.Contains(body, perByteRead) {
		t.Errorf("read_marked_after_align still unpacks its byte array one byte at a time:\n%s", body)
	}
	if body := bodies["write_marked_after_string"]; !strings.Contains(body, "serialize_write_bytes( stream, value->data, 3 )") {
		t.Errorf("write_marked_after_string must bulk-copy the [3]uint8 that follows a string (§4.7 ends aligned):\n%s", body)
	}
	if body := bodies["read_marked_after_string"]; !strings.Contains(body, "serialize_read_bytes( stream, value->data, 3 )") {
		t.Errorf("read_marked_after_string must bulk-copy the [3]uint8 that follows a string (§4.7 ends aligned):\n%s", body)
	}

	// ---- unmarked: the loop stays, because the bulk path's align would put
	// padding bits on a wire that has none
	for _, name := range []string{"unmarked_at_entry", "unmarked_off_boundary", "unmarked_ranged", "unmarked_counted"} {
		for _, prefix := range []string{"write_", "read_"} {
			body := bodies[prefix+name]
			call := "serialize_write_bytes( stream, value->data"
			if prefix == "read_" {
				call = "serialize_read_bytes( stream, value->data"
			}
			if strings.Contains(body, call) {
				t.Errorf("%s%s bulk-copied a byte array that is NOT statically byte-aligned — that moves the wire:\n%s", prefix, name, body)
			}
			if !strings.Contains(body, "for ( i = 0; i < ") {
				t.Errorf("%s%s must keep the per-byte loop:\n%s", prefix, name, body)
			}
		}
	}
}

// TestBulkBytesMatchesAnalysis pins the emitter to the SHARED analysis rather
// than to a predicate of its own: for every struct in the corpus, the fields
// the C wire functions bulk-copy are exactly ir.AlignedFixedByteArrays — the
// same set the C++ backend consults, which is what keeps the two byte-equal.
func TestBulkBytesMatchesAnalysis(t *testing.T) {
	u := unitFromSource(t, "Bulk.schema", bulkCorpus)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	header := string(files["BulkWire.h"])
	bodies := wireBodies(t, header)

	for _, d := range u.Files[0].Decls {
		st, ok := d.(*ir.Struct)
		if !ok {
			continue
		}
		marked := ir.AlignedFixedByteArrays(st)
		for _, f := range st.Fields {
			want := marked[f]
			for _, prefix := range []string{"write_", "read_"} {
				body := bodies[prefix+snake(st.Name)]
				call := "serialize_" + strings.TrimSuffix(prefix, "_") + "_bytes( stream, value->" + f.Name
				if got := strings.Contains(body, call); got != want {
					t.Errorf("%s%s: field %s bulk=%v, ir.AlignedFixedByteArrays says %v:\n%s",
						prefix, st.Name, f.Name, got, want, body)
				}
			}
		}
	}
}
