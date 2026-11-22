// WHAT A FILE IS, BY ITS LEADING BYTES, held by test (docs/SPEC-TABLES.md §3,
// §7.1, §19.1). The classifier is the one table every tool path reads, so what
// is measured here is that it covers every live form in the registry and both
// accelerators in both byte orders, that the magic and the form byte cannot be
// mistaken for each other, and that an unknown leading byte is a refusal that
// lists what is recognised.
package ir

import (
	"encoding/binary"
	"strings"
	"testing"
)

// EVERY LIVE FORM HAS A ROW, AND EVERY RESERVED BYTE'S FORMAT HAS A MAGIC ROW.
// The registry is the source: a form added to it with no classifier row is a
// form the tool writes and then cannot name, and this is the test that sees it.
func TestFileFormatsCoverTheRegistry(t *testing.T) {
	for _, form := range WireForms {
		if form.Byte == 0 {
			if c := ClassifyFile([]byte{0}); c.Format != nil {
				t.Errorf("byte 0 is never assigned and classifies as %q", c.Format.Name)
			}
			continue
		}
		var row *FileFormat
		for i := range FileFormats {
			if FileFormats[i].Name == form.Name {
				row = &FileFormats[i]
			}
		}
		if row == nil {
			t.Errorf("form %d, %s, has no row in FileFormats", form.Byte, form.Name)
			continue
		}
		got := ClassifyFile([]byte{form.Byte, 0, 0, 0, 0, 0, 0, 0})
		if form.Planned {
			// the byte is reserved and nothing writes it: the format is
			// identified by its magic, and the byte alone names no file
			if row.Magic == 0 {
				t.Errorf("%s is reserved byte %d and has no magic: nothing identifies a file of it", form.Name, form.Byte)
			}
			if got.Format != nil {
				t.Errorf("reserved byte %d classifies as %q: no build writes that byte", form.Byte, got.Format.Name)
			}
			continue
		}
		if got.Format != row || row.Form != form.Byte || row.Where != form.Where {
			t.Errorf("form %d classifies as %+v, want the row %+v", form.Byte, got.Format, *row)
		}
		// a form byte is the whole of what identifies a form: one byte is enough
		if one := ClassifyFile([]byte{form.Byte}); one.Format != row {
			t.Errorf("a one-byte head of form %d classifies as %+v", form.Byte, one.Format)
		}
	}
	for _, row := range FileFormats {
		if row.Check == "" || row.Where == "" || row.Kind == FileUnknown {
			t.Errorf("%s: a row names its kind, its section and how a file of it is validated: %+v", row.Name, row)
		}
		if (row.Magic == 0) == (row.Form == 0) {
			t.Errorf("%s: a row is identified by a form byte or by a magic, exactly one: %+v", row.Name, row)
		}
	}
}

// BOTH ACCELERATORS, IN BOTH BYTE ORDERS, and the line each prints.
func TestClassifyFileReadsTheMagicInBothOrders(t *testing.T) {
	for _, row := range []struct {
		magic uint64
		big   bool
		kind  FileKind
		want  []string
	}{
		{CookMagic, false, FileCook, []string{"COOK, little-endian: magic SCHMCOOK (docs/SPEC-TABLES.md §7.1)", "`schema cook-check --root <Table> <file>`", "--attribution"}},
		{CookMagic, true, FileCook, []string{"COOK, big-endian: magic KOOCMHCS (docs/SPEC-TABLES.md §7.1)", "`schema cook-check --root <Table> <file>`"}},
		{BlockMagic, false, FileBlock, []string{"BLOCK FORM, little-endian: magic SCHMABLK (docs/SPEC-TABLES.md §19.1)", "BlockOpen"}},
		{BlockMagic, true, FileBlock, []string{"BLOCK FORM, big-endian: magic KLBAMHCS (docs/SPEC-TABLES.md §19.1)", "BlockOpen"}},
	} {
		head := make([]byte, 24)
		if row.big {
			binary.BigEndian.PutUint64(head, row.magic)
		} else {
			binary.LittleEndian.PutUint64(head, row.magic)
		}
		c := ClassifyFile(head)
		if c.Kind() != row.kind || c.Big != row.big {
			t.Errorf("magic 0x%016x big=%v classifies as kind %d big=%v", row.magic, row.big, c.Kind(), c.Big)
		}
		// exactly the magic's eight bytes are enough, and they are all it reads
		if short := ClassifyFile(head[:FileHeadBytes]); short != c {
			t.Errorf("the first %d bytes classify as %+v and the whole head as %+v", FileHeadBytes, short, c)
		}
		line := FileLine(head)
		for _, w := range row.want {
			if !strings.Contains(line, w) {
				t.Errorf("FileLine = %q, want it to carry %q", line, w)
			}
		}
		// a magic cut short, or with any one byte changed, is not that magic
		if cut := ClassifyFile(head[:FileHeadBytes-1]); cut.Kind() == row.kind {
			t.Errorf("seven bytes of a magic classify as kind %d", cut.Kind())
		}
		for i := range FileHeadBytes {
			bent := append([]byte(nil), head...)
			bent[i] ^= 0x20
			if got := ClassifyFile(bent); got.Kind() == row.kind {
				t.Errorf("magic 0x%016x with byte %d changed still classifies as kind %d", row.magic, i, got.Kind())
			}
		}
	}
}

// THE MAGIC IS READ FIRST AND THE FORM BYTE SECOND, which is only sound while
// no byte the registry assigns or reserves begins a magic in either order.
func TestNoMagicBeginsWithARegistryByte(t *testing.T) {
	for _, row := range FileFormats {
		if row.Magic == 0 {
			continue
		}
		for _, first := range []byte{byte(row.Magic), byte(row.Magic >> 56)} {
			if form, assigned := WireFormOf(first); assigned {
				t.Errorf("%s's magic begins with byte %d, which the registry gives to %q", row.Name, first, form.Name)
			}
		}
	}
}

// THE LIVE FORMS SAY WHICH THEY ARE AND HOW THEY ARE READ, ON ONE LINE. A
// reserved byte says the BYTE is planned and where a real file of that format
// begins, because the cook and the block are implemented and a line calling
// them planned would be reporting that they do not exist.
func TestFileLineNamesTheForms(t *testing.T) {
	for _, row := range []struct {
		b    byte
		want []string
	}{
		{1, []string{"FORM 1 the variable form (docs/SPEC-TABLES.md §3); read it with `schema unpack --root <Table> --in <file> <tree-dir>`"}},
		{2, []string{"FORM 2 the message form (docs/SPEC-TABLES.md §3.3); read it with `schema unpack --root <Table> --announce <announcement> --in <file> <tree-dir>`"}},
		{3, []string{"FORM 3 the fixed form (docs/SPEC-TABLES.md §3.4); read it with `schema unpack --root <Table> --in <file> <tree-dir>`"}},
		{4, []string{"FORM 4 the cook — PLANNED as a form byte, which no build writes or reads yet", "the cook today begins with the magic SCHMCOOK (docs/SPEC-TABLES.md §7.1)"}},
		{5, []string{"FORM 5 the block form — PLANNED as a form byte, which no build writes or reads yet", "the block form today begins with the magic SCHMABLK (docs/SPEC-TABLES.md §19.1)"}},
	} {
		got := FileLine([]byte{row.b, 0, 0})
		for _, w := range row.want {
			if !strings.Contains(got, w) {
				t.Errorf("FileLine(%d) = %q, want it to carry %q", row.b, got, w)
			}
		}
		if strings.Contains(got, "REFUSED") {
			t.Errorf("FileLine(%d) refuses a registry byte: %q", row.b, got)
		}
	}
}

// BYTE 0 AND ANY UNASSIGNED VALUE ARE REFUSED BY NAME, AND THE REFUSAL LISTS
// WHAT IS RECOGNISED. `6` is the byte the unknown-form negative control plants
// precisely because no form defines or reserves it; `83` is the first byte of
// a little-endian magic, which alone identifies nothing.
func TestFileLineRefusesByNameAndListsTheKnown(t *testing.T) {
	if got := FileLine(nil); !strings.HasPrefix(got, "REFUSED: an empty file carries no form byte") {
		t.Errorf("FileLine(nil) = %q", got)
	}
	for _, row := range []struct {
		data []byte
		want string
	}{
		{[]byte{0}, "REFUSED: form 0 is never assigned"},
		{[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, "REFUSED: form 0 is never assigned"},
		{[]byte{WireFormNone}, "REFUSED: form 6 is assigned by no form"},
		{[]byte{0xFF}, "REFUSED: form 255 is assigned by no form"},
		{[]byte("S"), "REFUSED: form 83 is assigned by no form"},
		{[]byte("SCHMCOOL"), "REFUSED: form 83 is assigned by no form"},
	} {
		got := FileLine(row.data)
		if !strings.HasPrefix(got, row.want) {
			t.Errorf("FileLine(%v) = %q, want a line starting %q", row.data, got, row.want)
		}
		if !strings.HasSuffix(got, FileRecognised()) {
			t.Errorf("FileLine(%v) = %q, want it to end with the list of what is recognised", row.data, got)
		}
	}
	// the list is built from the table, so it names every row
	list := FileRecognised()
	for _, row := range FileFormats {
		if !strings.Contains(list, "("+row.Name+")") {
			t.Errorf("the recognised list %q does not name %s", list, row.Name)
		}
	}
	for _, w := range []string{"form byte 1", "form byte 2", "form byte 3", "SCHMCOOK or KOOCMHCS", "SCHMABLK or KLBAMHCS", "separated attribution part"} {
		if !strings.Contains(list, w) {
			t.Errorf("the recognised list %q does not carry %q", list, w)
		}
	}
}
