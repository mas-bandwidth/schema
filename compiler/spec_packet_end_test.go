// TestSpecPacketEndIsChecked pins the canonical-read rule of serialize format
// 2.0, which the packet wire inherits: every value has exactly one encoding,
// so a packet read refuses a whole byte left after the top-level object and
// non-zero unused bits in the final byte, and the end check runs once, after
// the top-level object, never inside a nested type's read. The page law is
// docs/SPEC.md §5; docs/PACKET-WIRE-ALGORITHM.md §1.1 and §3 state the same
// wire, the two port pages agree, and docs/rulings.json carries the ruling
// `packet-canonical-end` (2026-10-22). The three vectors under
// testdata/wire/packet-end hold the accepted packet and the two refusals.
package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packetEndFlat folds a page's line wrapping so a literal is pinned by its
// words and not by where the page happens to break a line.
func packetEndFlat(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func readPacketEndFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// packetEndSection returns path's text from start up to end, so a literal is
// pinned in the section the row names and not elsewhere on the page.
func packetEndSection(t *testing.T, path, start, end string) string {
	t.Helper()
	page := readPacketEndFile(t, path)
	from := strings.Index(page, start)
	if from < 0 {
		t.Fatalf("%s has no heading %q", path, start)
	}
	to := strings.Index(page[from:], end)
	if to < 0 {
		t.Fatalf("%s has no heading %q after %q", path, end, start)
	}
	return page[from : from+to]
}

func packetEndMustState(t *testing.T, what, section, literal string) {
	t.Helper()
	if !strings.Contains(packetEndFlat(section), packetEndFlat(literal)) {
		t.Errorf("%s: the section does not hold %q", what, literal)
	}
}

func packetEndMustNotState(t *testing.T, what, section, literal string) {
	t.Helper()
	if strings.Contains(packetEndFlat(section), packetEndFlat(literal)) {
		t.Errorf("%s: the section still holds the stale %q", what, literal)
	}
}

func TestSpecPacketEndIsChecked(t *testing.T) {
	t.Parallel()

	spec := packetEndSection(t, "../docs/SPEC.md",
		"## 5. Trust model — inherited", "## 6. Generated code")
	packetEndMustState(t, "SPEC §5 the rule", spec,
		"Every value has exactly one encoding")
	packetEndMustState(t, "SPEC §5 the trailing byte", spec,
		"a whole byte left over is a refusal")
	packetEndMustState(t, "SPEC §5 the padding", spec,
		"non-zero unused bits in the final byte")
	packetEndMustState(t, "SPEC §5 the once-after rule", spec,
		"runs **once**, after the top-level object")
	packetEndMustState(t, "SPEC §5 the nested exception", spec,
		"never inside a nested type's read")
	packetEndMustState(t, "SPEC §5 the public read", spec,
		"generated public `Read<T>`")
	packetEndMustState(t, "SPEC §5 the runtime end check", spec,
		"ends with that check")
	packetEndMustState(t, "SPEC §5 the boolean-only open question", spec,
		"return a boolean only")
	packetEndMustState(t, "SPEC §5 the bits-consumed open question", spec,
		"do not report bits consumed today")
	packetEndMustNotState(t, "SPEC §5 the retired justification", spec,
		"caller's concern")
	packetEndMustNotState(t, "SPEC §5 the retired framing", spec,
		"frame several objects in one buffer")

	waves := packetEndSection(t, "../docs/PACKET-WIRE-ALGORITHM.md",
		"### 1.1 PUT_BITS and GET_BITS", "### 1.2 ALIGN")
	packetEndMustState(t, "PACKET-WIRE-ALGORITHM §1.1", waves,
		"REFUSES a final byte")
	packetEndMustState(t, "PACKET-WIRE-ALGORITHM §1.1", waves,
		"whose unused bits are not zero")
	packetEndMustNotState(t, "PACKET-WIRE-ALGORITHM §1.1", waves,
		"does not check them")

	reader := packetEndSection(t, "../docs/PACKET-WIRE-ALGORITHM.md",
		"## 3. The reader", "### 3.1 READ_ITEM")
	packetEndMustState(t, "PACKET-WIRE-ALGORITHM §3 end check", reader,
		"if not END(r): return fail")
	packetEndMustState(t, "PACKET-WIRE-ALGORITHM §3 trailing byte", reader,
		"a whole byte left after the object")
	packetEndMustState(t, "PACKET-WIRE-ALGORITHM §3 padding", reader,
		"nonzero unused bits in the final byte")
	packetEndMustState(t, "PACKET-WIRE-ALGORITHM §3 once", reader,
		"runs **once**, after the top-level object")
	packetEndMustNotState(t, "PACKET-WIRE-ALGORITHM §3 the retired comment",
		reader, "caller's concern")

	for _, port := range []string{"../docs/PORT-MOO.md", "../docs/PORT-SQUIRREL.md"} {
		page := readPacketEndFile(t, port)
		packetEndMustState(t, port+" the end call", page, "serializeEnd")
		packetEndMustState(t, port+" the once-after rule", page,
			"once, after the top-level object")
		packetEndMustState(t, port+" the trailing byte", page, "trailing_data")
		packetEndMustState(t, port+" the padding", page, "nonzero_padding")
		packetEndMustState(t, port+" the law", page, "SPEC §5")
	}
	packetEndMustNotState(t, "PORT-SQUIRREL the retired optional sentence",
		readPacketEndFile(t, "../docs/PORT-SQUIRREL.md"),
		"is optional and, as in the nine main ports, not emitted")

	type entry struct {
		ID     string   `json:"id"`
		Date   string   `json:"date"`
		Quotes []string `json:"quotes"`
		Source string   `json:"source"`
	}
	var rulings struct {
		Rulings []entry `json:"rulings"`
	}
	if err := json.Unmarshal([]byte(readPacketEndFile(t, "../docs/rulings.json")),
		&rulings); err != nil {
		t.Fatalf("docs/rulings.json does not parse: %v", err)
	}
	var end *entry
	for i := range rulings.Rulings {
		if rulings.Rulings[i].ID == "packet-canonical-end" {
			end = &rulings.Rulings[i]
			break
		}
	}
	if end == nil {
		t.Fatal("docs/rulings.json has no packet-canonical-end entry")
	}
	if end.Date != "2026-10-22" {
		t.Errorf("packet-canonical-end date is %q, want 2026-10-22", end.Date)
	}
	if len(end.Quotes) != 1 || end.Quotes[0] != "Approved. Let's go." {
		t.Errorf("packet-canonical-end quotes are %q, want the approval", end.Quotes)
	}
	if !strings.Contains(end.Source, "canonical-read rule") {
		t.Errorf("packet-canonical-end source does not name the canonical-read rule: %q", end.Source)
	}

	schema := readPacketEndFile(t, "../test/packet-end/End.schema")
	if !strings.Contains(schema, "type End") ||
		!strings.Contains(schema, "uint8 | max = 5") {
		t.Errorf("test/packet-end/End.schema is not the three-bit End type: %q", schema)
	}

	vectorDir := "../testdata/wire/packet-end"
	vectors := []struct {
		name    string
		bits    string
		verdict string
		data    []byte
	}{
		{name: "exact.bin", bits: "3\n", verdict: "accepted", data: []byte{0x03}},
		{name: "trailing.bin", bits: "3\n", verdict: "refused", data: []byte{0x03, 0xff}},
		{name: "padding.bin", bits: "3\n", verdict: "refused", data: []byte{0xfb}},
	}
	manifest := readPacketEndFile(t, filepath.Join(vectorDir, "README.md"))
	for _, v := range vectors {
		got, err := os.ReadFile(filepath.Join(vectorDir, v.name))
		if err != nil {
			t.Fatalf("packet-end vector %s: %v", v.name, err)
		}
		if !bytes.Equal(got, v.data) {
			t.Errorf("packet-end vector %s is % x, want % x", v.name, got, v.data)
		}
		bits, err := os.ReadFile(filepath.Join(vectorDir,
			strings.TrimSuffix(v.name, ".bin")+".bits"))
		if err != nil {
			t.Fatalf("packet-end bits for %s: %v", v.name, err)
		}
		if string(bits) != v.bits {
			t.Errorf("packet-end bits for %s are %q, want %q", v.name, bits, v.bits)
		}
		if !strings.Contains(manifest, v.name) {
			t.Errorf("packet-end manifest does not name %s", v.name)
		}
		if !strings.Contains(manifest, v.verdict) {
			t.Errorf("packet-end manifest does not carry the %q verdict", v.verdict)
		}
	}
	for _, want := range []string{"03", "ff", "fb", "3"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("packet-end manifest does not state %q", want)
		}
	}
}
