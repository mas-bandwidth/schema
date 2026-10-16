// Spec agreement pins for the C++ packet reference (docs/CPP-REFERENCE-REVIEW.md,
// part 1). Each test emits a small unit, reads what the emitter actually does,
// and then holds docs/SPEC.md to the same fact, so a sentence that describes
// the reference cannot drift from it in either direction: change the emitter
// and the sentence goes red, change the sentence and the emitter does.
package cpp

import (
	"os"
	"strings"
	"testing"
)

// specSection returns the text of docs/SPEC.md from the first line holding
// start up to (not including) the first later line holding end, whitespace
// folded to single spaces so a sentence can wrap anywhere.
func specSection(t *testing.T, start, end string) string {
	t.Helper()
	raw, err := os.ReadFile("../../../docs/SPEC.md")
	if err != nil {
		t.Fatalf("read docs/SPEC.md: %v", err)
	}
	text := string(raw)
	i := strings.Index(text, start)
	if i < 0 {
		t.Fatalf("docs/SPEC.md: anchor %q not found", start)
	}
	rest := text[i+len(start):]
	if j := strings.Index(rest, end); j >= 0 {
		rest = rest[:j]
	}
	return strings.Join(strings.Fields(start+rest), " ")
}

// TestSpecSerializeIncludeRule pins SPEC.md §6.1's C++ bullet to the include
// rule the emitter keeps: the DATA header pulls in serialize.h only for
// 128-bit storage (fixed below 128 bits is a plain intN_t), and the WIRE
// header pulls it in only when the file emits a wire function — a file of
// constants, enums and flags has a wire header with no serialize dependency.
func TestSpecSerializeIncludeRule(t *testing.T) {
	t.Parallel()
	u := unitFromSources(t, map[string]string{
		"Narrow.schema": `package t
type Narrow {
    w fixed(2, 30) = 1.0 | min = -1, max = 1
}
`,
		"Wide.schema": `package t
type Wide {
    big uint128
}
`,
		"Consts.schema": `package t
const Limit = 7
enum Mode { Idle, Run }
`,
	})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	const inc = "#include \"serialize.h\""
	for name, want := range map[string]bool{
		"Narrow.h":     false, // fixed below 128 bits is int32_t storage
		"Wide.h":       true,  // serialize::uint128_t storage
		"Consts.h":     false,
		"NarrowWire.h": true,
		"WideWire.h":   true,
		"ConstsWire.h": false, // no wire function, no serialize dependency
	} {
		if got := strings.Contains(string(files[name]), inc); got != want {
			t.Errorf("%s: includes serialize.h = %v, want %v", name, got, want)
		}
	}

	bullet := specSection(t, "- **C++: the header splits into a data/wire pair.**", "- **")
	if strings.Contains(bullet, "int128/fixed") {
		t.Errorf("SPEC.md §6.1 says the data header needs serialize.h for fixed storage; only 128-bit storage does:\n%s", bullet)
	}
	for _, want := range []string{"128-bit", "emits a wire function"} {
		if !strings.Contains(bullet, want) {
			t.Errorf("SPEC.md §6.1's C++ bullet must state the include rule the emitter keeps (missing %q):\n%s", want, bullet)
		}
	}
}

// TestSpecBulkBytesAndInlining pins SPEC.md §6.2 to the two places the C++
// reference departs from "honest loops": a fixed [N]uint8 at a statically
// byte-aligned position rides ONE write_bytes/read_bytes, and every
// generated Read/Write is spelled with the force-inline demand. Both are
// byte-identical to the plain form; the spec has to say they exist.
func TestSpecBulkBytesAndInlining(t *testing.T) {
	t.Parallel()
	u := unitFromSources(t, map[string]string{
		"Blob.schema": `package t
type Blob {
    align
    digest [16]uint8
}
`,
	})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(files["BlobWire.h"])
	for _, want := range []string{
		"write_bytes( stream, value.digest, 16 ); // byte-aligned [N]uint8",
		"read_bytes( stream, value.digest, 16 ); // byte-aligned [N]uint8",
		"SCHEMA_WRITE_INLINE bool WriteBlob(",
		"SCHEMA_READ_INLINE bool ReadBlob(",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("BlobWire.h: want %q\n%s", want, wire)
		}
	}

	section := specSection(t, "### 6.2", "### 6.3")
	for _, want := range []string{"write_bytes", "SCHEMA_READ_INLINE", "SCHEMA_WRITE_INLINE"} {
		if !strings.Contains(section, want) {
			t.Errorf("SPEC.md §6.2 must name the C++ reference's %s path, which the emitter takes:\n%s", want, section)
		}
	}
}
